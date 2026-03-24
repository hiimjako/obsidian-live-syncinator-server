package syncinator

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hiimjako/syncinator/internal/repository"
	"github.com/hiimjako/syncinator/internal/testutils"
	"github.com/hiimjako/syncinator/pkg/filestorage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClose_CancelsContext(t *testing.T) {
	mockFileStorage := new(filestorage.MockFileStorage)
	db := testutils.CreateDB(t)

	opts := Options{JWTSecret: []byte("secret")}
	server := New(db, mockFileStorage, opts)

	require.NoError(t, server.ctx.Err())

	err := server.Close()
	require.NoError(t, err)

	assert.Error(t, server.ctx.Err())
	assert.Equal(t, context.Canceled, server.ctx.Err())
}

func TestForeignKeysEnforced(t *testing.T) {
	db := testutils.CreateDB(t)
	repo := repository.New(db)

	_, err := repo.CreateFile(context.Background(), repository.CreateFileParams{
		DiskPath: "dp", WorkspacePath: "wp", MimeType: "text/plain",
		Hash: "h", WorkspaceID: 99999,
	})
	assert.Error(t, err, "inserting file with non-existent workspace_id should fail")
}

func TestUniqueConstraint_WorkspaceIdAndPath(t *testing.T) {
	db := testutils.CreateDB(t)
	repo := repository.New(db)

	_, err := repo.CreateFile(context.Background(), repository.CreateFileParams{
		DiskPath: "disk1", WorkspacePath: "/same/path", MimeType: "text/plain",
		Hash: "h1", WorkspaceID: 1,
	})
	require.NoError(t, err)

	_, err = repo.CreateFile(context.Background(), repository.CreateFileParams{
		DiskPath: "disk2", WorkspacePath: "/same/path", MimeType: "text/plain",
		Hash: "h2", WorkspaceID: 1,
	})
	assert.Error(t, err, "same workspace_id + workspace_path should violate UNIQUE constraint")

	_, err = repo.CreateFile(context.Background(), repository.CreateFileParams{
		DiskPath: "disk3", WorkspacePath: "/same/path", MimeType: "text/plain",
		Hash: "h3", WorkspaceID: 2,
	})
	assert.NoError(t, err, "same workspace_path in different workspace should be allowed")
}

func TestNew(t *testing.T) {
	mockFileStorage := new(filestorage.MockFileStorage)
	db := testutils.CreateDB(t)
	repo := repository.New(db)

	file, err := repo.CreateFile(context.Background(), repository.CreateFileParams{
		DiskPath:      "disk_path",
		WorkspacePath: "workspace_path",
		MimeType:      "text/plain; charset=utf-8",
		Hash:          "123",
		WorkspaceID:   1,
	})
	require.NoError(t, err)

	_, err = repo.CreateFile(context.Background(), repository.CreateFileParams{
		DiskPath:      "disk_path/image_to_not_load",
		WorkspacePath: "workspace_path/image_to_not_load",
		MimeType:      "image/png",
		Hash:          "123",
		WorkspaceID:   1,
	})
	require.NoError(t, err)

	fileContent := []byte("hello world!")
	mockFileStorage.On("CreateObject", bytes.NewReader(fileContent)).Return(file.DiskPath, nil)
	mockFileStorage.On("ReadObject", file.DiskPath).Return(fileContent, nil)

	_, err = mockFileStorage.CreateObject(bytes.NewReader(fileContent))
	require.NoError(t, err)

	var server *syncinator
	require.NotPanics(t, func() {
		options := Options{JWTSecret: []byte("secret")}
		server = New(db, mockFileStorage, options)
	})
	t.Cleanup(func() { server.Close() })

	assert.Len(t, server.fileCache.Keys(), 0)
}

func TestHealthz(t *testing.T) {
	db := testutils.CreateDB(t)
	mockFS := new(filestorage.MockFileStorage)
	handler := New(db, mockFS, Options{JWTSecret: []byte("secret")})
	ts := httptest.NewServer(handler)
	t.Cleanup(func() { ts.Close(); handler.Close() })

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, ts.URL+"/healthz", http.NoBody)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestReadyz(t *testing.T) {
	db := testutils.CreateDB(t)
	mockFS := new(filestorage.MockFileStorage)
	handler := New(db, mockFS, Options{JWTSecret: []byte("secret")})
	ts := httptest.NewServer(handler)
	t.Cleanup(func() { ts.Close(); handler.Close() })

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, ts.URL+"/readyz", http.NoBody)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestReadyz_DBDown(t *testing.T) {
	db := testutils.CreateDB(t)
	mockFS := new(filestorage.MockFileStorage)
	handler := New(db, mockFS, Options{JWTSecret: []byte("secret")})
	ts := httptest.NewServer(handler)
	t.Cleanup(func() { ts.Close(); handler.Close() })

	db.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, ts.URL+"/readyz", http.NoBody)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
}
