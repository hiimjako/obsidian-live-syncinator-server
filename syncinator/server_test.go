package syncinator

import (
	"bytes"
	"context"
	"testing"

	"github.com/hiimjako/syncinator/internal/repository"
	"github.com/hiimjako/syncinator/internal/testutils"
	"github.com/hiimjako/syncinator/syncinator/filestorage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
		server = New(repo, mockFileStorage, options)
	})
	t.Cleanup(func() { server.Close() })

	assert.Len(t, server.files, 1)
	assert.Equal(t, CachedFile{
		File: repository.File{
			ID:            file.ID,
			DiskPath:      "disk_path",
			WorkspacePath: "workspace_path",
			MimeType:      "text/plain; charset=utf-8",
			Hash:          "123",
			WorkspaceID:   1,
			CreatedAt:     server.files[file.ID].CreatedAt,
			UpdatedAt:     server.files[file.ID].UpdatedAt,
		},
		Content: string(fileContent),
	}, server.files[file.ID])
}
