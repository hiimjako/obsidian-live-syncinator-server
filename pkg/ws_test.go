package rtsync

import (
	"context"
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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "github.com/mattn/go-sqlite3"
)

func Test_wsHandler(t *testing.T) {
	db := testutils.CreateDB(t)

	mockFileStorage := new(filestorage.MockFileStorage)
	repo := repository.New(db)
	handler := New(repo, mockFileStorage, Options{JWTSecret: []byte("secret")})
	ts := httptest.NewServer(handler)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)

	url := strings.Replace(ts.URL, "http", "ws", 1) + PathWebSocket
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

// func Test_internalBusProcessor(t *testing.T) {
//
// }
