package rtsync

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
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
	repo := repository.New(db)
	authOptions := Options{JWTSecret: []byte("secret")}
	handler := New(repo, mockFileStorage, authOptions)
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
		var workspaceID int64 = 10
		url := createWsUrlWithAuth(t, ts.URL, workspaceID, []byte("invalid secret"))

		//nolint:bodyclose
		_, _, err := websocket.Dial(ctx, url, nil)
		require.Error(t, err)
	})
}

func Test_handleChunk(t *testing.T) {
	db := testutils.CreateDB(t)

	mockFileStorage := new(filestorage.MockFileStorage)
	repo := repository.New(db)
	authOptions := Options{JWTSecret: []byte("secret")}
	handler := New(repo, mockFileStorage, authOptions)
	ts := httptest.NewServer(handler)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)

	var workspaceID1 int64 = 1
	var workspaceID2 int64 = 2
	urlWorkspace1 := createWsUrlWithAuth(t, ts.URL, workspaceID1, authOptions.JWTSecret)
	urlWorkspace2 := createWsUrlWithAuth(t, ts.URL, workspaceID2, authOptions.JWTSecret)

	//nolint:bodyclose
	senderWorkspace1, _, err := websocket.Dial(ctx, urlWorkspace1, nil)
	require.NoError(t, err)

	//nolint:bodyclose
	reciverWorkspace1, _, err := websocket.Dial(ctx, urlWorkspace1, nil)
	require.NoError(t, err)

	//nolint:bodyclose
	reciverWorkspace2, _, err := websocket.Dial(ctx, urlWorkspace2, nil)
	require.NoError(t, err)

	file, err := repo.CreateFile(context.Background(), repository.CreateFileParams{
		DiskPath:      "disk_path",
		WorkspacePath: "workspace_path",
		MimeType:      "",
		Hash:          "",
		WorkspaceID:   1,
	})
	assert.NoError(t, err)

	// add time to update updatedAt
	time.Sleep(1 * time.Second)

	msg := ChunkMessage{
		WsMessageHeader: WsMessageHeader{
			Type:   ChunkEventType,
			FileId: file.ID,
		},
		Version: 0,
		Chunks: []diff.DiffChunk{
			{
				Position: 0,
				Type:     diff.DiffAdd,
				Text:     "Hello!",
				Len:      6,
			},
		},
	}

	mockFileStorage.On("PersistChunk", file.DiskPath, msg.Chunks[0]).Return(nil)

	wg := sync.WaitGroup{}
	wg.Add(3)

	// check that only ws on same workspace recive the event

	go func() {
		// the sender should not recive any message
		var recMsg ChunkMessage
		err := wsjson.Read(ctx, senderWorkspace1, &recMsg)
		assert.Error(t, err)

		wg.Done()
	}()

	go func() {
		// the reciver on other workspace should not recive any message
		var recMsg ChunkMessage
		err := wsjson.Read(ctx, reciverWorkspace2, &recMsg)
		assert.Error(t, err)

		wg.Done()
	}()

	go func() {
		// the reciver on the same workspace should recive the message
		var recMsg ChunkMessage
		err := wsjson.Read(ctx, reciverWorkspace1, &recMsg)
		assert.NoError(t, err)

		// only version should differ
		assert.Equal(t, msg.Version+1, recMsg.Version)
		recMsg.Version = msg.Version
		assert.Equal(t, msg, recMsg)

		wg.Done()
	}()

	assert.NoError(t, wsjson.Write(ctx, senderWorkspace1, msg))

	wg.Wait()

	mockFileStorage.AssertCalled(t, "PersistChunk", file.DiskPath, msg.Chunks[0])

	updatedFile, err := repo.FetchFile(context.Background(), file.ID)
	assert.NoError(t, err)

	assert.Equal(t, int64(1), handler.files[file.ID].Version)
	assert.Equal(t, int64(1), updatedFile.Version)
	assert.Greater(t, updatedFile.UpdatedAt, file.UpdatedAt)

	// check operation history
	operations, err := repo.FetchFileOperationsFromVersion(
		context.Background(),
		repository.FetchFileOperationsFromVersionParams{
			FileID:  file.ID,
			Version: 0,
		},
	)
	assert.NoError(t, err)
	require.Len(t, operations, 1)

	msg.Version = 1
	operationJson, err := json.Marshal(msg)
	require.NoError(t, err)

	assert.Equal(t, repository.Operation{
		FileID:    file.ID,
		Version:   1,
		Operation: string(operationJson),
		CreatedAt: operations[0].CreatedAt,
	}, operations[0])

	t.Cleanup(func() {
		cancel()
		senderWorkspace1.Close(websocket.StatusNormalClosure, "")
		reciverWorkspace1.Close(websocket.StatusNormalClosure, "")
		reciverWorkspace2.Close(websocket.StatusNormalClosure, "")
		ts.Close()
		handler.Close()
	})
}

func Test_handleEvent(t *testing.T) {
	db := testutils.CreateDB(t)

	mockFileStorage := new(filestorage.MockFileStorage)
	repo := repository.New(db)
	authOptions := Options{JWTSecret: []byte("secret")}
	handler := New(repo, mockFileStorage, authOptions)
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
	reciverWorkspace1, _, err := websocket.Dial(ctx, urlWorkspace1, nil)
	require.NoError(t, err)

	//nolint:bodyclose
	reciverWorkspace2, _, err := websocket.Dial(ctx, urlWorkspace2, nil)
	require.NoError(t, err)

	msg := EventMessage{
		WsMessageHeader: WsMessageHeader{
			Type:   CreateEventType,
			FileId: 1,
		},
		ObjectType: "file",
	}

	wg := sync.WaitGroup{}
	wg.Add(3)

	// check that only ws on same workspace recive the event

	go func() {
		// the sender should not recive any message
		var recMsg EventMessage
		err := wsjson.Read(ctx, senderWorkspace1, &recMsg)
		assert.Error(t, err)

		wg.Done()
	}()

	go func() {
		// the reciver on other workspace should not recive any message
		var recMsg EventMessage
		err := wsjson.Read(ctx, reciverWorkspace2, &recMsg)
		assert.Error(t, err)

		wg.Done()
	}()

	go func() {
		// the reciver on the same workspace should recive the message
		var recMsg EventMessage
		err := wsjson.Read(ctx, reciverWorkspace1, &recMsg)
		assert.NoError(t, err)
		assert.Equal(t, msg, recMsg)

		wg.Done()
	}()

	assert.NoError(t, wsjson.Write(ctx, senderWorkspace1, msg))

	wg.Wait()

	t.Cleanup(func() {
		cancel()
		senderWorkspace1.Close(websocket.StatusNormalClosure, "")
		reciverWorkspace1.Close(websocket.StatusNormalClosure, "")
		reciverWorkspace2.Close(websocket.StatusNormalClosure, "")
		ts.Close()
		handler.Close()
	})
}
