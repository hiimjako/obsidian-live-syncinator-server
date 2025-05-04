package syncinator

import (
	"context"
	"io"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"math/rand/v2"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/hiimjako/syncinator/internal/repository"
	"github.com/hiimjako/syncinator/internal/testutils"
	"github.com/hiimjako/syncinator/pkg/diff"
	"github.com/hiimjako/syncinator/pkg/filestorage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testOperation struct {
	clientID int
	sequence int
	chunk    diff.Chunk
}

func Test_loadTest(t *testing.T) {
	t.Run("deterministic concurrent operations", func(t *testing.T) {
		seed := rand.NewPCG(42, 1024)
		rnd := rand.New(seed)

		t.Logf("Using seed: %d", seed)

		const (
			numClients     = 10
			opsPerClient   = 25
			maxChunkSize   = 10
			initialContent = "The quick brown fox jumps over the lazy dog"
		)

		operations := generateDeterministicOperations(rnd, numClients, opsPerClient, maxChunkSize, initialContent)

		fs := filestorage.NewDisk(t.TempDir())
		diskPath, err := fs.CreateObject(strings.NewReader(initialContent))
		require.NoError(t, err)

		db := testutils.CreateDB(t)
		repo := repository.New(db)

		var workspaceID int64 = 1
		file, err := repo.CreateFile(context.Background(), repository.CreateFileParams{
			DiskPath:      diskPath,
			WorkspacePath: "workspace_path",
			MimeType:      "text/plain",
			Hash:          "",
			WorkspaceID:   workspaceID,
		})
		require.NoError(t, err)

		opts := Options{
			JWTSecret:           []byte("secret"),
			FlushInterval:       time.Second,
			MinChangesThreshold: 5,
		}
		handler := New(db, fs, opts)
		ts := httptest.NewServer(handler)
		defer ts.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Create and initialize clients
		clients := make([]*testClient, numClients)
		urlWorkspace := createWsUrlWithAuth(t, ts.URL, workspaceID, opts.JWTSecret)

		for i := range numClients {
			//nolint
			conn, _, err := websocket.Dial(ctx, urlWorkspace, nil)
			require.NoError(t, err)

			clients[i] = &testClient{
				id:       i,
				conn:     conn,
				content:  initialContent,
				version:  0,
				received: make(chan ChunkMessage, opsPerClient*numClients),
			}

			go clients[i].listen(ctx)
		}

		// Execute operations in deterministic order
		for _, op := range operations {
			client := clients[op.clientID]

			client.mu.Lock()
			msg := ChunkMessage{
				WsMessageHeader: WsMessageHeader{
					Type:   ChunkEventType,
					FileID: file.ID,
				},
				Version: client.version,
				Chunks:  []diff.Chunk{op.chunk},
			}
			client.mu.Unlock()

			err := wsjson.Write(ctx, client.conn, msg)
			require.NoError(t, err)

			// Process all pending messages for all clients
			for _, c := range clients {
				timeout := time.After(time.Second)
				select {
				case recvMsg := <-c.received:
					c.mu.Lock()
					c.content = diff.ApplyMultiple(c.content, recvMsg.Chunks)
					c.version = recvMsg.Version
					c.mu.Unlock()
				case <-timeout:
					t.Fatalf("Timeout waiting for message processing after operation %d from client %d", op.sequence, op.clientID)
				}
			}

			time.Sleep(time.Millisecond * 10)
		}

		// Verify final state

		// Verify all clients have the same content
		clients[0].mu.Lock()
		finalContent := clients[0].content
		clients[0].mu.Unlock()

		for i := 1; i < len(clients); i++ {
			clients[i].mu.Lock()
			assert.Equal(t, finalContent, clients[i].content, "Client %d content mismatch", i)
			clients[i].mu.Unlock()
		}

		// Verify server state matches
		handler.mut.Lock()
		serverContent := handler.files[1].Content
		handler.mut.Unlock()
		assert.Equal(t, serverContent, finalContent, "Server content mismatch")

		// Verify file on disk matches after flush
		time.Sleep(opts.FlushInterval * 2)
		fileReader, err := fs.ReadObject(diskPath)
		require.NoError(t, err)
		defer fileReader.Close()

		diskContent, err := io.ReadAll(fileReader)
		require.NoError(t, err)
		assert.Equal(t, serverContent, string(diskContent), "Disk content mismatch")

		for _, c := range clients {
			c.conn.Close(websocket.StatusNormalClosure, "")
		}
	})
}

type testClient struct {
	id       int
	conn     *websocket.Conn
	content  string
	version  int64
	received chan ChunkMessage
	mu       sync.Mutex
}

func (c *testClient) listen(ctx context.Context) {
	for {
		var msg ChunkMessage
		err := wsjson.Read(ctx, c.conn, &msg)
		if err != nil {
			// closing socket
			return
		}
		c.received <- msg
	}
}

func generateDeterministicOperations(
	rnd *rand.Rand,
	numClients, opsPerClient, maxChunkSize int,
	initialContent string,
) []testOperation {
	operations := make([]testOperation, numClients*opsPerClient)
	currentContent := initialContent

	for i := range numClients * opsPerClient {
		clientID := i % numClients
		sequence := i

		pos := rnd.IntN(len(currentContent) + 1)
		opType := diff.Add
		if rnd.Float32() < 0.5 {
			opType = diff.Remove
		}

		var chunk diff.Chunk
		if opType == diff.Add {
			length := rnd.IntN(maxChunkSize) + 1
			text := make([]byte, length)
			for j := range text {
				text[j] = byte(rnd.IntN(26) + 'a')
			}
			chunk = diff.Chunk{
				Position: int64(pos),
				Type:     opType,
				Text:     string(text),
				Len:      int64(length),
			}
		} else {
			length := rnd.IntN(maxChunkSize)
			chunk = diff.Chunk{
				Position: int64(pos),
				Type:     opType,
				Text:     "",
				Len:      int64(length),
			}
		}

		operations[i] = testOperation{
			clientID: clientID,
			sequence: sequence,
			chunk:    chunk,
		}

		currentContent = diff.ApplyMultiple(currentContent, []diff.Chunk{chunk})
	}

	return operations
}
