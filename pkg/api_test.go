package rtsync

import (
	"context"
	"io"
	"net/http"
	"os"
	"strconv"
	"testing"

	"github.com/hiimjako/syncinator/internal/repository"
	"github.com/hiimjako/syncinator/internal/testutils"
	"github.com/hiimjako/syncinator/pkg/filestorage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	_ "github.com/mattn/go-sqlite3"
)

// Test_listFilesHandler tests the listFileHandler using mocked storage
func Test_listFilesHandler(t *testing.T) {
	mockFileStorage := new(filestorage.MockFileStorage)
	db := testutils.CreateDB(t)
	repo := repository.New(db)
	options := Options{JWTSecret: []byte("secret")}
	server := New(repo, mockFileStorage, options)

	t.Cleanup(func() { server.Close() })

	workspaceID := int64(10)
	filesToInsert := []struct {
		file        []byte
		filepath    string
		workspaceID int64
	}{
		{
			file:        []byte("here a new file 1!"),
			filepath:    "/home/file/1",
			workspaceID: workspaceID,
		},
		{
			file:        []byte("here a new file 2!"),
			filepath:    "/home/file/2",
			workspaceID: workspaceID,
		},
		{
			file:        []byte("here a new file 3!"),
			filepath:    "/home/file/3",
			workspaceID: 123,
		},
	}

	for _, f := range filesToInsert {
		mockFileStorage.On("CreateObject", mock.AnythingOfType("multipart.sectionReadCloser")).Return(f.filepath, nil).Once()

		form, contentType := testutils.CreateMultipart(t, f.filepath, f.file, false)
		res, _ := testutils.DoRequest[repository.File](
			t,
			server,
			http.MethodPost,
			PathHttpApi+"/file",
			form,
			testutils.WithAuthHeader(options.JWTSecret, f.workspaceID),
			testutils.WithContentTypeHeader(contentType),
		)
		assert.Equal(t, http.StatusCreated, res.Code)
	}

	// fetch files
	res, body := testutils.DoRequest[[]repository.File](
		t,
		server,
		http.MethodGet,
		PathHttpApi+"/file",
		nil,
		testutils.WithAuthHeader(options.JWTSecret, workspaceID),
	)
	assert.Equal(t, http.StatusOK, res.Code)
	assert.Len(t, body, 2)

	// check mock assertions
	mockFileStorage.AssertNumberOfCalls(t, "CreateObject", len(filesToInsert))
}

// Test_fetchFileHandler tests the fetchFileHandler using mocked storage
func Test_fetchFileHandler(t *testing.T) {
	mockFileStorage := new(filestorage.MockFileStorage)
	db := testutils.CreateDB(t)
	repo := repository.New(db)
	options := Options{JWTSecret: []byte("secret")}
	server := New(repo, mockFileStorage, options)

	t.Cleanup(func() { server.Close() })

	workspaceID := int64(10)
	filesToInsert := []struct {
		file        []byte
		filepath    string
		workspaceID int64
	}{
		{
			file:        []byte("here a new file 1!"),
			filepath:    "/home/file/1",
			workspaceID: 123,
		},
		{
			file:        []byte("here a new file 2!"),
			filepath:    "/home/file/2",
			workspaceID: workspaceID,
		},
	}

	for _, f := range filesToInsert {
		mockFileStorage.On("CreateObject", mock.AnythingOfType("multipart.sectionReadCloser")).Return(f.filepath, nil).Once()

		form, contentType := testutils.CreateMultipart(t, f.filepath, f.file, false)
		res, _ := testutils.DoRequest[repository.File](
			t,
			server,
			http.MethodPost,
			PathHttpApi+"/file",
			form,
			testutils.WithAuthHeader(options.JWTSecret, f.workspaceID),
			testutils.WithContentTypeHeader(contentType),
		)
		assert.Equal(t, http.StatusCreated, res.Code)
	}

	// file of other workspace
	res, _ := testutils.DoRequest[string](
		t,
		server,
		http.MethodGet,
		PathHttpApi+"/file/1",
		nil,
		testutils.WithAuthHeader(options.JWTSecret, workspaceID),
	)
	assert.Equal(t, http.StatusNotFound, res.Code)

	// fetch file
	mockFileStorage.On("ReadObject", filesToInsert[1].filepath).Return(filesToInsert[1].file, nil)

	res, body := testutils.DoRequest[testutils.FileWithContent](
		t,
		server,
		http.MethodGet,
		PathHttpApi+"/file/2",
		nil,
		testutils.WithAuthHeader(options.JWTSecret, workspaceID),
	)
	assert.Equal(t, http.StatusOK, res.Code)
	assert.Equal(t, testutils.FileWithContent{
		Metadata: repository.File{
			ID:            2,
			DiskPath:      "/home/file/2",
			WorkspacePath: "/home/file/2",
			MimeType:      "text/plain; charset=utf-8",
			Hash:          body.Metadata.Hash,
			CreatedAt:     body.Metadata.CreatedAt,
			UpdatedAt:     body.Metadata.UpdatedAt,
			WorkspaceID:   workspaceID,
		},
		Content: []byte("here a new file 2!"),
	}, body)

	// check mock assertions
	mockFileStorage.AssertNumberOfCalls(t, "CreateObject", len(filesToInsert))
	mockFileStorage.AssertCalled(t, "ReadObject", "/home/file/2")
}

// Test_createFileHandler tests the createFileHandler using mocked storage
func Test_createFileHandler(t *testing.T) {
	t.Run("should create a text file", func(t *testing.T) {
		mockFileStorage := new(filestorage.MockFileStorage)
		db := testutils.CreateDB(t)
		repo := repository.New(db)
		options := Options{JWTSecret: []byte("secret")}
		server := New(repo, mockFileStorage, options)

		t.Cleanup(func() { server.Close() })

		var workspaceID int64 = 10
		filepath := "/home/file"
		content := []byte("here a new file!")
		diskPath := "/foo/bar"

		mockFileStorage.On("CreateObject", mock.AnythingOfType("multipart.sectionReadCloser")).
			Return(diskPath, nil).
			Once()

		form, contentType := testutils.CreateMultipart(t, filepath, content, false)
		res, body := testutils.DoRequest[repository.File](
			t,
			server,
			http.MethodPost,
			PathHttpApi+"/file",
			form,
			testutils.WithAuthHeader(options.JWTSecret, workspaceID),
			testutils.WithContentTypeHeader(contentType),
		)

		// check response
		assert.Equal(t, http.StatusCreated, res.Code)
		assert.Equal(t, repository.File{
			ID:            1,
			DiskPath:      diskPath,
			WorkspacePath: filepath,
			MimeType:      "text/plain; charset=utf-8",
			Hash:          "17750bc8e19b7f86eb26e11fa76e075578d2163d49a159368ed18497407576ac",
			CreatedAt:     body.CreatedAt,
			UpdatedAt:     body.UpdatedAt,
			WorkspaceID:   workspaceID,
		}, body)

		// check db
		files, err := repo.FetchWorkspaceFiles(context.Background(), workspaceID)
		assert.NoError(t, err)
		assert.Len(t, files, 1)
		assert.Equal(t, repository.File{
			ID:            1,
			DiskPath:      diskPath,
			WorkspacePath: filepath,
			MimeType:      "text/plain; charset=utf-8",
			Hash:          "17750bc8e19b7f86eb26e11fa76e075578d2163d49a159368ed18497407576ac",
			CreatedAt:     files[0].CreatedAt,
			UpdatedAt:     files[0].UpdatedAt,
			WorkspaceID:   workspaceID,
		}, files[0])

		// check mock assertions
		mockFileStorage.AssertNumberOfCalls(t, "CreateObject", 1)
	})

	t.Run("should create a non-text file", func(t *testing.T) {
		mockFileStorage := new(filestorage.MockFileStorage)
		db := testutils.CreateDB(t)
		repo := repository.New(db)
		options := Options{JWTSecret: []byte("secret")}
		server := New(repo, mockFileStorage, options)

		t.Cleanup(func() { server.Close() })

		var workspaceID int64 = 10
		filepath := "/home/image"
		diskPath := "/foo/image"

		mockFileStorage.On("CreateObject", mock.AnythingOfType("*bytes.Reader")).
			Return(diskPath, nil).
			Once()

		image, err := os.Open("./testdata/image.png")
		require.NoError(t, err)

		imageBytes, err := io.ReadAll(image)
		require.NoError(t, err)

		form, contentType := testutils.CreateMultipart(t, filepath, imageBytes, true)
		res, body := testutils.DoRequest[repository.File](
			t,
			server,
			http.MethodPost,
			PathHttpApi+"/file",
			form,
			testutils.WithAuthHeader(options.JWTSecret, workspaceID),
			testutils.WithContentTypeHeader(contentType),
		)

		// check response
		assert.Equal(t, http.StatusCreated, res.Code)
		assert.Equal(t, repository.File{
			ID:            1,
			DiskPath:      diskPath,
			WorkspacePath: filepath,
			MimeType:      "image/png",
			Hash:          "625e880acc3a38581bd71f456489f9a5c50ff31fa58631326b83ac7f2081960e",
			CreatedAt:     body.CreatedAt,
			UpdatedAt:     body.UpdatedAt,
			WorkspaceID:   workspaceID,
		}, body)

		// check db
		file, err := repo.FetchFile(context.Background(), 1)
		assert.NoError(t, err)
		assert.Equal(t, repository.File{
			ID:            1,
			DiskPath:      diskPath,
			WorkspacePath: filepath,
			MimeType:      "image/png",
			Hash:          "625e880acc3a38581bd71f456489f9a5c50ff31fa58631326b83ac7f2081960e",
			CreatedAt:     file.CreatedAt,
			UpdatedAt:     file.UpdatedAt,
			WorkspaceID:   workspaceID,
		}, file)

		// check mock assertions
		mockFileStorage.AssertNumberOfCalls(t, "CreateObject", 1)
	})

	t.Run("should not insert duplicate paths", func(t *testing.T) {
		mockFileStorage := new(filestorage.MockFileStorage)
		db := testutils.CreateDB(t)
		repo := repository.New(db)
		options := Options{JWTSecret: []byte("secret")}
		server := New(repo, mockFileStorage, options)

		t.Cleanup(func() { server.Close() })

		var workspaceID int64 = 10
		filepath := "/home/file"
		content := []byte("here a new file!")
		diskPath := "/foo/bar"

		mockFileStorage.On("CreateObject", mock.AnythingOfType("multipart.sectionReadCloser")).
			Return(diskPath, nil).
			Once()

		// create
		form, contentType := testutils.CreateMultipart(t, filepath, content, false)
		res, _ := testutils.DoRequest[string](
			t,
			server,
			http.MethodPost,
			PathHttpApi+"/file",
			form,
			testutils.WithAuthHeader(options.JWTSecret, workspaceID),
			testutils.WithContentTypeHeader(contentType),
		)
		assert.Equal(t, http.StatusCreated, res.Code)

		// duplicate
		form, contentType = testutils.CreateMultipart(t, filepath, content, false)
		res, body := testutils.DoRequest[string](
			t,
			server,
			http.MethodPost,
			PathHttpApi+"/file",
			form,
			testutils.WithAuthHeader(options.JWTSecret, workspaceID),
			testutils.WithContentTypeHeader(contentType),
		)

		// check response
		assert.Equal(t, http.StatusConflict, res.Code)
		assert.Equal(t, ErrDuplicateFile, body)

		// check db
		files, err := repo.FetchWorkspaceFiles(context.Background(), workspaceID)
		assert.NoError(t, err)
		assert.Len(t, files, 1)

		// check mock assertions
		mockFileStorage.AssertNumberOfCalls(t, "CreateObject", 1)
	})
}

// Test_deleteFileHandler tests the deleteFileHandler using mocked storage
func Test_deleteFileHandler(t *testing.T) {
	mockFileStorage := new(filestorage.MockFileStorage)
	db := testutils.CreateDB(t)
	repo := repository.New(db)
	options := Options{JWTSecret: []byte("secret")}
	server := New(repo, mockFileStorage, options)

	t.Cleanup(func() { server.Close() })

	t.Run("successfully delete a file", func(t *testing.T) {
		var workspaceID int64 = 10
		filepath := "/home/file"
		content := []byte("here a new file!")
		diskPath := "/foo/bar"

		mockFileStorage.On("DeleteObject", diskPath).Return(nil).Once()
		mockFileStorage.On("CreateObject", mock.AnythingOfType("multipart.sectionReadCloser")).
			Return(diskPath, nil).
			Once()

		// creating file
		form, contentType := testutils.CreateMultipart(t, filepath, content, false)
		res, createBody := testutils.DoRequest[repository.File](
			t,
			server,
			http.MethodPost,
			PathHttpApi+"/file",
			form,
			testutils.WithAuthHeader(options.JWTSecret, workspaceID),
			testutils.WithContentTypeHeader(contentType),
		)
		assert.Equal(t, http.StatusCreated, res.Code)

		// deleting a file
		res, deleteBody := testutils.DoRequest[string](
			t,
			server,
			http.MethodDelete,
			PathHttpApi+"/file/"+strconv.Itoa(int(createBody.ID)),
			nil,
			testutils.WithAuthHeader(options.JWTSecret, workspaceID),
		)
		assert.Equal(t, http.StatusNoContent, res.Code)
		assert.Equal(t, "", deleteBody)

		// check db
		files, err := repo.FetchWorkspaceFiles(context.Background(), workspaceID)
		assert.NoError(t, err)
		assert.Len(t, files, 0)

		// check mock assertions
		mockFileStorage.AssertNumberOfCalls(t, "CreateObject", 1)
		mockFileStorage.AssertCalled(t, "DeleteObject", diskPath)
	})

	t.Run("unauthorize to delete a file of other workspace", func(t *testing.T) {
		var workspaceID int64 = 10
		filepath := "/home/file/2"
		content := []byte("here a new file!")
		diskPath := "/foo/bar"

		mockFileStorage.On("CreateObject", mock.AnythingOfType("multipart.sectionReadCloser")).
			Return(diskPath, nil).
			Once()

		// creating file
		form, contentType := testutils.CreateMultipart(t, filepath, content, false)
		res, createBody := testutils.DoRequest[repository.File](
			t,
			server,
			http.MethodPost,
			PathHttpApi+"/file",
			form,
			testutils.WithAuthHeader(options.JWTSecret, workspaceID),
			testutils.WithContentTypeHeader(contentType),
		)
		assert.Equal(t, http.StatusCreated, res.Code)

		// deleting a file
		anotherWorkspaceID := int64(20)
		res, deleteBody := testutils.DoRequest[string](
			t,
			server,
			http.MethodDelete,
			PathHttpApi+"/file/"+strconv.Itoa(int(createBody.ID)),
			nil,
			testutils.WithAuthHeader(options.JWTSecret, anotherWorkspaceID),
		)
		assert.Equal(t, http.StatusNotFound, res.Code)
		assert.Equal(t, ErrNotExistingFile, deleteBody)

		// check db
		files, err := repo.FetchWorkspaceFiles(context.Background(), workspaceID)
		assert.NoError(t, err)
		assert.Len(t, files, 1)

		// check mock assertions
		mockFileStorage.AssertNotCalled(t, "DeleteObject")
	})
}

// Test_updateFileHandler tests the updateFileHandler using mocked storage
func Test_updateFileHandler(t *testing.T) {
	mockFileStorage := new(filestorage.MockFileStorage)
	db := testutils.CreateDB(t)
	repo := repository.New(db)
	options := Options{JWTSecret: []byte("secret")}
	server := New(repo, mockFileStorage, options)

	t.Cleanup(func() { server.Close() })

	t.Run("successfully rename a file", func(t *testing.T) {
		var workspaceID int64 = 10
		filepath := "/home/file"
		content := []byte("here a new file!")
		diskPath := "/foo/bar"

		mockFileStorage.On("CreateObject", mock.AnythingOfType("multipart.sectionReadCloser")).
			Return(diskPath, nil).
			Once()

		// creating file
		form, contentType := testutils.CreateMultipart(t, filepath, content, false)
		res, createBody := testutils.DoRequest[repository.File](
			t,
			server,
			http.MethodPost,
			PathHttpApi+"/file",
			form,
			testutils.WithAuthHeader(options.JWTSecret, workspaceID),
			testutils.WithContentTypeHeader(contentType),
		)
		assert.Equal(t, http.StatusCreated, res.Code)

		// updating a file
		updateData := UpdateFileBody{
			Path: "/home/new-fancy-name",
		}
		res, updateBody := testutils.DoRequest[string](
			t,
			server,
			http.MethodPatch,
			PathHttpApi+"/file/"+strconv.Itoa(int(createBody.ID)),
			updateData,
			testutils.WithAuthHeader(options.JWTSecret, workspaceID),
		)
		assert.Equal(t, http.StatusNoContent, res.Code)
		assert.Equal(t, "", updateBody)

		// check db
		files, err := repo.FetchWorkspaceFiles(context.Background(), workspaceID)
		assert.NoError(t, err)
		assert.Len(t, files, 1)
		assert.Equal(t, repository.File{
			ID:            1,
			DiskPath:      diskPath,
			WorkspacePath: updateData.Path,
			MimeType:      "text/plain; charset=utf-8",
			Hash:          "17750bc8e19b7f86eb26e11fa76e075578d2163d49a159368ed18497407576ac",
			CreatedAt:     files[0].CreatedAt,
			UpdatedAt:     files[0].UpdatedAt,
			WorkspaceID:   workspaceID,
		}, files[0])

		// check mock assertions
		mockFileStorage.AssertNumberOfCalls(t, "CreateObject", 1)
	})

	t.Run("unauthorize to rename a file of other workspace", func(t *testing.T) {
		var workspaceID int64 = 10
		filepath := "/home/file/2"
		content := []byte("here a new file!")
		diskPath := "/foo/bar"

		mockFileStorage.On("CreateObject", mock.AnythingOfType("multipart.sectionReadCloser")).
			Return(diskPath, nil).
			Once()

		// creating file
		form, contentType := testutils.CreateMultipart(t, filepath, content, false)
		res, createBody := testutils.DoRequest[repository.File](
			t,
			server,
			http.MethodPost,
			PathHttpApi+"/file",
			form,
			testutils.WithAuthHeader(options.JWTSecret, workspaceID),
			testutils.WithContentTypeHeader(contentType),
		)
		assert.Equal(t, http.StatusCreated, res.Code)

		// updating a file
		updateData := UpdateFileBody{
			Path: "/home/new-fancy-name",
		}
		anotherWorkspaceID := int64(20)
		res, deleteBody := testutils.DoRequest[string](
			t,
			server,
			http.MethodPatch,
			PathHttpApi+"/file/"+strconv.Itoa(int(createBody.ID)),
			updateData,
			testutils.WithAuthHeader(options.JWTSecret, anotherWorkspaceID),
		)
		assert.Equal(t, http.StatusNotFound, res.Code)
		assert.Equal(t, ErrNotExistingFile, deleteBody)

		// check db
		file, err := repo.FetchFile(context.Background(), createBody.ID)
		assert.NoError(t, err)
		assert.Equal(t, filepath, file.WorkspacePath)
	})
}
