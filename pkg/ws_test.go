package syncinator

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http/httptest"
	"os"
	"path"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/hiimjako/syncinator/internal/repository"
	"github.com/hiimjako/syncinator/internal/testutils"
	"github.com/hiimjako/syncinator/pkg/diff"
	"github.com/hiimjako/syncinator/pkg/filestorage"
	"github.com/hiimjako/syncinator/pkg/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "github.com/mattn/go-sqlite3"
)

func createWsUrlWithAuth(t testing.TB, url string, workspaceID int64, secret []byte) string {
	token, err := middleware.CreateToken(middleware.AuthOptions{SecretKey: secret}, workspaceID)
	require.NoError(t, err)

	newUrl := strings.Replace(url, "http", "ws", 1) + PathWebSocket
	urlWithAuth := fmt.Sprintf("%s?jwt=%s", newUrl, token)

	return urlWithAuth
}

func Test_wsAuth(t *testing.T) {
	db := testutils.CreateDB(t)

	mockFileStorage := new(filestorage.MockFileStorage)
	authOptions := Options{JWTSecret: []byte("secret")}
	handler := New(db, mockFileStorage, authOptions)
	ts := httptest.NewServer(handler)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)

	t.Cleanup(func() {
		cancel()
		ts.Close()
		handler.Close()
	})

	t.Run("authorized", func(t *testing.T) {
		var workspaceID int64 = 10
		url := createWsUrlWithAuth(t, ts.URL, workspaceID, authOptions.JWTSecret)

		//nolint:bodyclose
		sender, _, err := websocket.Dial(ctx, url, nil)
		require.NoError(t, err)

		time.Sleep(100 * time.Millisecond)

		handler.subscribersMu.Lock()
		assert.Len(t, handler.subscribers, 1)
		for subscriber := range handler.subscribers {
			assert.Equal(t, workspaceID, subscriber.workspaceID)
			assert.NotEmpty(t, subscriber.clientID)
		}
		handler.subscribersMu.Unlock()

		sender.Close(websocket.StatusNormalClosure, "")
	})

	t.Run("unauthorized", func(t *testing.T) {
		var workspaceID int64 = 11
		url := createWsUrlWithAuth(t, ts.URL, workspaceID, []byte("invalid secret"))

		//nolint:bodyclose
		_, _, err := websocket.Dial(ctx, url, nil)
		require.Error(t, err)
	})
}

func Test_handleChunk(t *testing.T) {
	t.Run("should apply a correct operation", func(t *testing.T) {
		fs := filestorage.NewDisk(t.TempDir())
		diskPath, err := fs.CreateObject(strings.NewReader(""))
		assert.NoError(t, err)

		db := testutils.CreateDB(t)
		repo := repository.New(db)

		var workspaceID int64 = 1
		file, err := repo.CreateFile(context.Background(), repository.CreateFileParams{
			DiskPath:      diskPath,
			WorkspacePath: "workspace_path",
			MimeType:      "text/plain",
			Hash:          "oldHash",
			WorkspaceID:   workspaceID,
		})
		require.NoError(t, err)

		opts := Options{JWTSecret: []byte("secret"), FlushInterval: time.Second}
		handler := New(db, fs, opts)
		ts := httptest.NewServer(handler)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

		var workspaceID1 int64 = 1
		var workspaceID2 int64 = 2
		urlWorkspace1 := createWsUrlWithAuth(t, ts.URL, workspaceID1, opts.JWTSecret)
		urlWorkspace2 := createWsUrlWithAuth(t, ts.URL, workspaceID2, opts.JWTSecret)

		//nolint:bodyclose
		senderWorkspace1, _, err := websocket.Dial(ctx, urlWorkspace1, nil)
		require.NoError(t, err)

		//nolint:bodyclose
		receiverWorkspace1, _, err := websocket.Dial(ctx, urlWorkspace1, nil)
		require.NoError(t, err)

		//nolint:bodyclose
		receiverWorkspace2, _, err := websocket.Dial(ctx, urlWorkspace2, nil)
		require.NoError(t, err)

		// add time to update updatedAt
		time.Sleep(1 * time.Second)

		msg := ChunkMessage{
			WsMessageHeader: WsMessageHeader{
				Type:   ChunkEventType,
				FileID: file.ID,
			},
			Version: 0,
			Chunks: []diff.Chunk{
				{
					Position: 0,
					Type:     diff.Add,
					Text:     "Hello!",
					Len:      6,
				},
			},
		}

		wg := sync.WaitGroup{}
		wg.Add(3)

		// check that only ws on same workspace receive the event

		go func() {
			// the sender should receive the message message
			var recMsg ChunkMessage
			err := wsjson.Read(ctx, senderWorkspace1, &recMsg)
			assert.NoError(t, err)

			// only version should differ
			assert.Equal(t, msg.Version+1, recMsg.Version)
			recMsg.Version = msg.Version
			assert.Equal(t, msg, recMsg)

			wg.Done()
		}()

		go func() {
			// the receiver on other workspace should not receive any message
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			var recMsg ChunkMessage
			err := wsjson.Read(ctx, receiverWorkspace2, &recMsg)
			assert.Error(t, err)
			cancel()

			wg.Done()
		}()

		go func() {
			// the receiver on the same workspace should receive the message
			var recMsg ChunkMessage
			err := wsjson.Read(ctx, receiverWorkspace1, &recMsg)
			assert.NoError(t, err)

			// only version should differ
			assert.Equal(t, msg.Version+1, recMsg.Version)
			recMsg.Version = msg.Version
			assert.Equal(t, msg, recMsg)

			wg.Done()
		}()

		assert.NoError(t, wsjson.Write(ctx, senderWorkspace1, msg))

		wg.Wait()

		updatedFile, err := handler.db.FetchFile(context.Background(), file.ID)
		assert.NoError(t, err)

		handler.mut.Lock()
		assert.Equal(t, int64(1), handler.files[file.ID].Version)
		assert.Equal(t, int64(1), updatedFile.Version)
		assert.Greater(t, updatedFile.UpdatedAt, file.UpdatedAt)
		assert.Equal(t, "334d016f755cd6dc58c53a86e183882f8ec14f52fb05345887c8a5edd42c87b7", updatedFile.Hash)
		handler.mut.Unlock()

		// check operation history
		operations, err := handler.db.FetchFileOperationsFromVersion(
			context.Background(),
			repository.FetchFileOperationsFromVersionParams{
				FileID:      file.ID,
				Version:     0,
				WorkspaceID: workspaceID1,
			},
		)
		assert.NoError(t, err)
		require.Len(t, operations, 1)

		assert.Equal(t, repository.Operation{
			FileID:    file.ID,
			Version:   1,
			Operation: marshal(t, msg.Chunks),
			CreatedAt: operations[0].CreatedAt,
		}, operations[0])

		t.Cleanup(func() {
			cancel()
			senderWorkspace1.Close(websocket.StatusNormalClosure, "")
			receiverWorkspace1.Close(websocket.StatusNormalClosure, "")
			receiverWorkspace2.Close(websocket.StatusNormalClosure, "")
			ts.Close()
			handler.Close()
		})
	})

	t.Run("should transform concurrent or older chunk", func(t *testing.T) {
		startingString := "foo"

		fs := filestorage.NewDisk(t.TempDir())
		diskPath, err := fs.CreateObject(strings.NewReader(startingString))
		assert.NoError(t, err)

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

		authOptions := Options{JWTSecret: []byte("secret")}
		handler := New(db, fs, authOptions)
		ts := httptest.NewServer(handler)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

		urlWorkspace := createWsUrlWithAuth(t, ts.URL, workspaceID, authOptions.JWTSecret)

		client1Content := startingString
		//nolint:bodyclose
		client1, _, err := websocket.Dial(ctx, urlWorkspace, nil)
		require.NoError(t, err)

		client2Content := startingString
		//nolint:bodyclose
		client2, _, err := websocket.Dial(ctx, urlWorkspace, nil)
		require.NoError(t, err)

		msgClient1 := ChunkMessage{
			WsMessageHeader: WsMessageHeader{
				Type:   ChunkEventType,
				FileID: file.ID,
			},
			Version: file.Version,
			Chunks: []diff.Chunk{
				{
					Position: 0,
					Type:     diff.Add,
					Text:     "Hello!",
					Len:      6,
				},
			},
		}

		msgClient2 := ChunkMessage{
			WsMessageHeader: WsMessageHeader{
				Type:   ChunkEventType,
				FileID: file.ID,
			},
			Version: file.Version,
			Chunks: []diff.Chunk{
				{
					Position: 0,
					Type:     diff.Remove,
					Text:     startingString,
					Len:      int64(len(startingString)),
				},
			},
		}

		client1Content = diff.ApplyMultiple(client1Content, msgClient1.Chunks)
		client2Content = diff.ApplyMultiple(client2Content, msgClient2.Chunks)

		wg := sync.WaitGroup{}
		wg.Add(2)

		go func() {
			// the client1 should receive the first message
			var recMsg ChunkMessage
			err := wsjson.Read(ctx, client1, &recMsg)
			assert.NoError(t, err)
			assert.Equal(t, ChunkMessage{
				WsMessageHeader: WsMessageHeader{
					Type:   ChunkEventType,
					FileID: file.ID,
				},
				Version: 1,
				Chunks: []diff.Chunk{
					{
						Position: 0,
						Type:     diff.Add,
						Text:     "Hello!",
						Len:      6,
					},
				},
			}, recMsg)

			// the client1 should receive the transformed chunk of client2
			var recMsg2 ChunkMessage
			err = wsjson.Read(ctx, client1, &recMsg2)
			assert.NoError(t, err)
			assert.Equal(t, ChunkMessage{
				WsMessageHeader: WsMessageHeader{
					Type:   ChunkEventType,
					FileID: file.ID,
				},
				Version: 2,
				Chunks: []diff.Chunk{
					{
						Position: 6,
						Type:     diff.Remove,
						Text:     startingString,
						Len:      int64(len(startingString)),
					},
				},
			}, recMsg2)

			client1Content = diff.ApplyMultiple(client1Content, recMsg2.Chunks)
			wg.Done()
		}()

		go func() {
			// the client2 should receive the first message of client1
			var recMsg ChunkMessage
			err := wsjson.Read(ctx, client2, &recMsg)
			assert.NoError(t, err)
			assert.Equal(t, ChunkMessage{
				WsMessageHeader: WsMessageHeader{
					Type:   ChunkEventType,
					FileID: file.ID,
				},
				Version: 1,
				Chunks: []diff.Chunk{
					{
						Position: 0,
						Type:     diff.Add,
						Text:     "Hello!",
						Len:      6,
					},
				},
			}, recMsg)

			client2Content = diff.ApplyMultiple(client2Content, recMsg.Chunks)

			// and the transformed one
			var recMsg2 ChunkMessage
			err = wsjson.Read(ctx, client2, &recMsg2)
			assert.NoError(t, err)
			assert.Equal(t, ChunkMessage{
				WsMessageHeader: WsMessageHeader{
					Type:   ChunkEventType,
					FileID: file.ID,
				},
				Version: 2,
				Chunks: []diff.Chunk{
					{
						Position: 6,
						Type:     diff.Remove,
						Text:     startingString,
						Len:      int64(len(startingString)),
					},
				},
			}, recMsg2)
			wg.Done()
		}()

		assert.NoError(t, wsjson.Write(ctx, client1, msgClient1))
		time.Sleep(100 * time.Millisecond)
		assert.NoError(t, wsjson.Write(ctx, client2, msgClient2))

		wg.Wait()

		assert.Equal(t, client2Content, client1Content)
		assert.Equal(t, "Hello!", client1Content)

		updatedFile, err := handler.db.FetchFile(context.Background(), file.ID)
		require.NoError(t, err)

		assert.Equal(t, int64(2), handler.files[file.ID].Version)
		assert.Equal(t, int64(2), updatedFile.Version)

		// check operation history
		operations, err := handler.db.FetchFileOperationsFromVersion(
			context.Background(),
			repository.FetchFileOperationsFromVersionParams{
				FileID:      file.ID,
				Version:     0,
				WorkspaceID: workspaceID,
			},
		)
		require.NoError(t, err)
		require.Len(t, operations, 2)

		assert.Equal(t, []repository.Operation{
			{
				FileID:  file.ID,
				Version: 1,
				Operation: marshal(t,
					[]diff.Chunk{
						{
							Position: 0,
							Type:     diff.Add,
							Text:     "Hello!",
							Len:      6,
						},
					}),
				CreatedAt: operations[0].CreatedAt,
			},
			{
				FileID:  file.ID,
				Version: 2,
				Operation: marshal(t, []diff.Chunk{
					{
						Position: 6,
						Type:     diff.Remove,
						Text:     startingString,
						Len:      int64(len(startingString)),
					},
				}),
				CreatedAt: operations[1].CreatedAt,
			},
		}, operations)

		t.Cleanup(func() {
			cancel()
			client1.Close(websocket.StatusNormalClosure, "")
			client2.Close(websocket.StatusNormalClosure, "")
			ts.Close()
			handler.Close()
		})
	})
}

func Test_handleEvent(t *testing.T) {
	db := testutils.CreateDB(t)

	mockFileStorage := new(filestorage.MockFileStorage)
	authOptions := Options{JWTSecret: []byte("secret")}
	handler := New(db, mockFileStorage, authOptions)
	ts := httptest.NewServer(handler)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)

	var workspaceID1 int64 = 1
	var workspaceID2 int64 = 2
	urlWorkspace1 := createWsUrlWithAuth(t, ts.URL, workspaceID1, authOptions.JWTSecret)
	urlWorkspace2 := createWsUrlWithAuth(t, ts.URL, workspaceID2, authOptions.JWTSecret)

	//nolint:bodyclose
	senderWorkspace1, _, err := websocket.Dial(ctx, urlWorkspace1, nil)
	require.NoError(t, err)

	//nolint:bodyclose
	receiverWorkspace1, _, err := websocket.Dial(ctx, urlWorkspace1, nil)
	require.NoError(t, err)

	//nolint:bodyclose
	receiverWorkspace2, _, err := websocket.Dial(ctx, urlWorkspace2, nil)
	require.NoError(t, err)

	msg := EventMessage{
		WsMessageHeader: WsMessageHeader{
			Type:   CreateEventType,
			FileID: 1,
		},
		ObjectType: "file",
	}

	wg := sync.WaitGroup{}
	wg.Add(3)

	// check that only ws on same workspace receive the event

	go func() {
		// the sender should not receive any message
		var recMsg EventMessage
		err := wsjson.Read(ctx, senderWorkspace1, &recMsg)
		assert.Error(t, err)

		wg.Done()
	}()

	go func() {
		// the receiver on other workspace should not receive any message
		var recMsg EventMessage
		err := wsjson.Read(ctx, receiverWorkspace2, &recMsg)
		assert.Error(t, err)

		wg.Done()
	}()

	go func() {
		// the receiver on the same workspace should receive the message
		var recMsg EventMessage
		err := wsjson.Read(ctx, receiverWorkspace1, &recMsg)
		assert.NoError(t, err)
		assert.Equal(t, msg, recMsg)

		wg.Done()
	}()

	assert.NoError(t, wsjson.Write(ctx, senderWorkspace1, msg))

	wg.Wait()

	t.Cleanup(func() {
		cancel()
		senderWorkspace1.Close(websocket.StatusNormalClosure, "")
		receiverWorkspace1.Close(websocket.StatusNormalClosure, "")
		receiverWorkspace2.Close(websocket.StatusNormalClosure, "")
		ts.Close()
		handler.Close()
	})
}

func Test_handleCursor(t *testing.T) {
	db := testutils.CreateDB(t)

	mockFileStorage := new(filestorage.MockFileStorage)
	authOptions := Options{JWTSecret: []byte("secret")}
	handler := New(db, mockFileStorage, authOptions)
	ts := httptest.NewServer(handler)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)

	var workspaceID1 int64 = 1
	var workspaceID2 int64 = 2
	urlWorkspace1 := createWsUrlWithAuth(t, ts.URL, workspaceID1, authOptions.JWTSecret)
	urlWorkspace2 := createWsUrlWithAuth(t, ts.URL, workspaceID2, authOptions.JWTSecret)

	//nolint:bodyclose
	senderWorkspace1, _, err := websocket.Dial(ctx, urlWorkspace1, nil)
	require.NoError(t, err)

	//nolint:bodyclose
	receiverWorkspace1, _, err := websocket.Dial(ctx, urlWorkspace1, nil)
	require.NoError(t, err)

	//nolint:bodyclose
	receiverWorkspace2, _, err := websocket.Dial(ctx, urlWorkspace2, nil)
	require.NoError(t, err)

	msg := CursorMessage{
		WsMessageHeader: WsMessageHeader{
			Type:   CursorEventType,
			FileID: 1,
		},
		Path:  "foo.md",
		Label: "user",
		Color: "red",
		Line:  1,
		Ch:    2,
	}

	wg := sync.WaitGroup{}
	wg.Add(3)

	// check that only ws on same workspace receive the event

	go func() {
		// the sender should not receive any message
		var recMsg CursorMessage
		err := wsjson.Read(ctx, senderWorkspace1, &recMsg)
		assert.Error(t, err)

		wg.Done()
	}()

	go func() {
		// the receiver on other workspace should not receive any message
		var recMsg CursorMessage
		err := wsjson.Read(ctx, receiverWorkspace2, &recMsg)
		assert.Error(t, err)

		wg.Done()
	}()

	go func() {
		// the receiver on the same workspace should receive the message
		var recMsg CursorMessage
		err := wsjson.Read(ctx, receiverWorkspace1, &recMsg)
		assert.NoError(t, err)
		msg.ID = recMsg.ID // set the random uuid
		assert.Equal(t, msg, recMsg)

		wg.Done()
	}()

	assert.NoError(t, wsjson.Write(ctx, senderWorkspace1, msg))

	wg.Wait()

	t.Cleanup(func() {
		cancel()
		senderWorkspace1.Close(websocket.StatusNormalClosure, "")
		receiverWorkspace1.Close(websocket.StatusNormalClosure, "")
		receiverWorkspace2.Close(websocket.StatusNormalClosure, "")
		ts.Close()
		handler.Close()
	})
}

func Test_processFileChanges(t *testing.T) {
	t.Run("should not write file to storage and save snapshot if too early", func(t *testing.T) {
		db := testutils.CreateDB(t)
		fs := filestorage.NewDisk(t.TempDir())
		opts := Options{
			JWTSecret:           []byte("secret"),
			FlushInterval:       500 * time.Millisecond,
			MinChangesThreshold: 2,
		}
		// processFileChanges is already running. Started in New()
		handler := New(db, fs, opts)

		handler.mut.Lock()
		handler.files[1] = CachedFile{
			pendingChanges: 1,
			Content:        "foo",
			File: repository.File{
				ID:          1,
				Version:     1,
				DiskPath:    "file.md",
				WorkspaceID: 1,
				UpdatedAt:   time.Now(),
			},
		}
		handler.mut.Unlock()

		time.Sleep(200 * time.Millisecond)

		_, err := fs.ReadObject("file.md")
		assert.Error(t, err)

		_, err = handler.db.FetchSnapshot(handler.ctx, repository.FetchSnapshotParams{
			FileID:  1,
			Version: 1,
		})
		assert.Error(t, err)
	})

	t.Run("should write file to storage and save snapshot after x changes", func(t *testing.T) {
		dir := t.TempDir()

		db := testutils.CreateDB(t)
		fs := filestorage.NewDisk(dir)
		opts := Options{
			JWTSecret:           []byte("secret"),
			FlushInterval:       100 * time.Millisecond,
			MinChangesThreshold: 2,
		}
		// processFileChanges is already running. Started in New()
		handler := New(db, fs, opts)

		filename := "file.md"

		_, err := os.Create(path.Join(dir, filename))
		require.NoError(t, err)

		_, err = handler.db.CreateFile(handler.ctx, repository.CreateFileParams{
			WorkspaceID:   1,
			WorkspacePath: "path",
			MimeType:      "mime",
		})
		require.NoError(t, err)

		handler.mut.Lock()
		handler.files[1] = CachedFile{
			pendingChanges: 3,
			Content:        "foo",
			File: repository.File{
				ID:          1,
				Version:     1,
				DiskPath:    filename,
				WorkspaceID: 1,
				UpdatedAt:   time.Now().Add(1 * time.Hour),
			},
		}
		handler.mut.Unlock()
		time.Sleep(2 * opts.FlushInterval)

		// check file write
		fileReader, err := fs.ReadObject(filename)
		assert.NoError(t, err)
		defer fileReader.Close()
		fileContent, err := io.ReadAll(fileReader)
		assert.NoError(t, err)
		assert.Equal(t, "foo", string(fileContent))

		// check snapshot
		s, err := handler.db.FetchSnapshot(handler.ctx, repository.FetchSnapshotParams{
			FileID:      1,
			Version:     1,
			WorkspaceID: 1,
		})
		assert.NoError(t, err)
		assert.Equal(t, repository.FetchSnapshotRow{
			FileID:        1,
			Version:       1,
			DiskPath:      s.DiskPath,
			CreatedAt:     s.CreatedAt,
			Type:          "file",
			WorkspaceID:   1,
			WorkspacePath: "path",
			MimeType:      "mime",
			Hash:          "2c26b46b68ffc68ff99b453c1d30413413422d706483bfa0f98a5e886266e7ae",
		}, s)

		sFileReader, err := fs.ReadObject(s.DiskPath)
		assert.NoError(t, err)
		defer sFileReader.Close()
		sFileContent, err := io.ReadAll(sFileReader)
		assert.NoError(t, err)
		assert.Equal(t, "foo", string(sFileContent))

		handler.mut.Lock()
		assert.EqualValues(t, 0, handler.files[1].pendingChanges)
		handler.mut.Unlock()
	})

	t.Run("should write file to storage and save snapshot after time elapsed", func(t *testing.T) {
		dir := t.TempDir()

		db := testutils.CreateDB(t)
		fs := filestorage.NewDisk(dir)
		opts := Options{
			JWTSecret:           []byte("secret"),
			FlushInterval:       100 * time.Millisecond,
			MinChangesThreshold: 2,
		}
		// processFileChanges is already running. Started in New()
		handler := New(db, fs, opts)

		filename := "file.md"

		_, err := os.Create(path.Join(dir, filename))
		require.NoError(t, err)

		_, err = handler.db.CreateFile(handler.ctx, repository.CreateFileParams{
			WorkspaceID:   1,
			WorkspacePath: "path",
			MimeType:      "mime",
		})
		require.NoError(t, err)

		handler.mut.Lock()
		handler.files[1] = CachedFile{
			pendingChanges: 1,
			Content:        "foo",
			File: repository.File{
				ID:          1,
				Version:     1,
				DiskPath:    filename,
				WorkspaceID: 1,
				UpdatedAt:   time.Now().Add(-1 * time.Hour),
			},
		}
		handler.mut.Unlock()
		time.Sleep(2 * opts.FlushInterval)

		// check file write
		fileReader, err := fs.ReadObject(filename)
		assert.NoError(t, err)
		defer fileReader.Close()
		fileContent, err := io.ReadAll(fileReader)
		assert.NoError(t, err)
		assert.Equal(t, "foo", string(fileContent))

		// check snapshot
		s, err := handler.db.FetchSnapshot(handler.ctx, repository.FetchSnapshotParams{
			FileID:      1,
			Version:     1,
			WorkspaceID: 1,
		})
		assert.NoError(t, err)
		assert.Equal(t, repository.FetchSnapshotRow{
			FileID:        1,
			Version:       1,
			DiskPath:      s.DiskPath,
			CreatedAt:     s.CreatedAt,
			Type:          "file",
			WorkspaceID:   1,
			WorkspacePath: "path",
			MimeType:      "mime",
			Hash:          "2c26b46b68ffc68ff99b453c1d30413413422d706483bfa0f98a5e886266e7ae",
		}, s)

		sFileReader, err := fs.ReadObject(s.DiskPath)
		assert.NoError(t, err)
		defer sFileReader.Close()
		sFileContent, err := io.ReadAll(sFileReader)
		assert.NoError(t, err)
		assert.Equal(t, "foo", string(sFileContent))

		handler.mut.Lock()
		assert.EqualValues(t, 0, handler.files[1].pendingChanges)
		handler.mut.Unlock()
	})
}

func marshal(t *testing.T, thing any) string {
	j, err := json.Marshal(thing)
	require.NoError(t, err)
	return string(j)
}

func Benchmark_onChunkMessage(b *testing.B) {
	log.SetOutput(io.Discard)

	fs := filestorage.NewDisk(b.TempDir())
	diskPath, err := fs.CreateObject(strings.NewReader(""))
	require.NoError(b, err)

	db := testutils.CreateDB(b)
	repo := repository.New(db)

	var workspaceID int64 = 1
	file, err := repo.CreateFile(context.Background(), repository.CreateFileParams{
		DiskPath:      diskPath,
		WorkspacePath: "workspace_path",
		MimeType:      "text/plain",
		Hash:          "",
		WorkspaceID:   workspaceID,
	})
	require.NoError(b, err)

	authOptions := Options{JWTSecret: []byte("secret")}
	handler := New(db, fs, authOptions)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		msg := ChunkMessage{
			WsMessageHeader: WsMessageHeader{
				Type:   ChunkEventType,
				FileID: file.ID,
			},
			Version: int64(i),
			Chunks: []diff.Chunk{
				{
					Position: 0,
					Type:     diff.Add,
					Text:     "Hello!",
					Len:      6,
				},
			},
		}
		handler.onChunkMessage(nil, msg)
	}
}
