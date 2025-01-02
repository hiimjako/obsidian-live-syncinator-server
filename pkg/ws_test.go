package rtsync

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
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

func Test_wsHandler(t *testing.T) {
	db := testutils.CreateDB(t)

	mockFileStorage := new(filestorage.MockFileStorage)
	repo := repository.New(db)
	authOptions := Options{JWTSecret: []byte("secret")}
	handler := New(repo, mockFileStorage, authOptions)
	ts := httptest.NewServer(handler)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)

	var workspaceID int64 = 10
	url := createWsUrlWithAuth(t, ts.URL, workspaceID, authOptions.JWTSecret)

	//nolint:bodyclose
	sender, _, err := websocket.Dial(ctx, url, nil)
	require.NoError(t, err)

	//nolint:bodyclose
	reciver, _, err := websocket.Dial(ctx, url, nil)
	require.NoError(t, err)

	file, err := repo.CreateFile(context.Background(), repository.CreateFileParams{
		DiskPath:      "disk_path",
		WorkspacePath: "workspace_path",
		MimeType:      "",
		Hash:          "",
		WorkspaceID:   1,
	})
	assert.NoError(t, err)

	msg := ChunkMessage{
		WsMessageHeader: WsMessageHeader{
			Type:   ChunkEventType,
			FileId: file.ID,
		},
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

	err = wsjson.Write(ctx, sender, msg)
	assert.NoError(t, err)
	go func() {
		// should not recive any message
		var recMsg ChunkMessage
		err := wsjson.Read(ctx, sender, &recMsg)
		assert.Error(t, err)
	}()

	var recMsg ChunkMessage
	err = wsjson.Read(ctx, reciver, &recMsg)
	assert.NoError(t, err)

	msg.SenderId = recMsg.SenderId
	assert.Equal(t, msg, recMsg)

	time.Sleep(10 * time.Millisecond)
	mockFileStorage.AssertCalled(t, "PersistChunk", file.DiskPath, msg.Chunks[0])

	t.Cleanup(func() {
		cancel()
		sender.Close(websocket.StatusNormalClosure, "")
		reciver.Close(websocket.StatusNormalClosure, "")
		ts.Close()
		handler.Close()
	})
}
