package syncinator

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/hiimjako/syncinator/internal/repository"
	"github.com/hiimjako/syncinator/internal/requestutils"
	"github.com/hiimjako/syncinator/syncinator/diff"
	"github.com/hiimjako/syncinator/syncinator/filestorage"
	"github.com/hiimjako/syncinator/syncinator/middleware"
)

const (
	MultipartFileField     = "file"
	MultipartFilepathField = "path"
	MultipartMetadata      = "metadata"
)

type UpdateFileBody struct {
	Path string `json:"path"`
}

type Operation struct {
	FileID    int64            `json:"fileId"`
	Version   int64            `json:"version"`
	Operation []diff.DiffChunk `json:"operation"`
	CreatedAt time.Time        `json:"createdAt"`
}

const (
	ErrDuplicateFile   = "duplicated file"
	ErrInvalidFile     = "impossible to create file"
	ErrReadingFile     = "impossible to read file"
	ErrNotExistingFile = "not existing file"
)

func (s *syncinator) apiHandler() http.Handler {
	router := http.NewServeMux()
	router.HandleFunc("GET /file", s.listFilesHandler)
	router.HandleFunc("GET /file/{id}", s.fetchFileHandler)
	router.HandleFunc("POST /file", s.createFileHandler)
	router.HandleFunc("DELETE /file/{id}", s.deleteFileHandler)
	router.HandleFunc("PATCH /file/{id}", s.updateFileHandler)
	router.HandleFunc("GET /operation", s.listOperationsHandler)

	stack := middleware.CreateStack(
		middleware.Logging,
		middleware.Cors(middleware.CorsOptions{
			AllowedOrigins: []string{"*"},
			AllowedMethods: []string{"HEAD", "GET", "POST", "OPTIONS", "DELETE", "PATCH"},
			AllowedHeaders: []string{"Origin", "X-Requested-With", "Content-Type", "Accept", "Authorization"},
		}),
		middleware.IsAuthenticated(middleware.AuthOptions{SecretKey: s.jwtSecret}, middleware.ExtractBearerToken),
	)

	routerWithStack := stack(router)
	return routerWithStack
}

func (s *syncinator) listFilesHandler(w http.ResponseWriter, r *http.Request) {
	workspaceID := middleware.WorkspaceIDFromCtx(r.Context())

	files, err := s.db.FetchFiles(r.Context(), workspaceID)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(files); err != nil {
		http.Error(w, "error reading request body", http.StatusInternalServerError)
		return
	}
}

func (s *syncinator) listOperationsHandler(w http.ResponseWriter, r *http.Request) {
	fromVersion, err := strconv.Atoi(r.URL.Query().Get("from"))
	if fromVersion < 0 || err != nil {
		http.Error(w, "invalid \"from version\"", http.StatusBadRequest)
		return
	}

	fileID, err := strconv.Atoi(r.URL.Query().Get("fileId"))
	if fromVersion < 0 || err != nil {
		http.Error(w, "invalid \"fileId\"", http.StatusBadRequest)
		return
	}

	workspaceID := middleware.WorkspaceIDFromCtx(r.Context())
	dbOperations, err := s.db.FetchFileOperationsFromVersion(r.Context(), repository.FetchFileOperationsFromVersionParams{
		FileID:      int64(fileID),
		Version:     int64(fromVersion),
		WorkspaceID: workspaceID,
	})
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	operations := make([]Operation, len(dbOperations))
	for i := 0; i < len(operations); i++ {
		var chunks []diff.DiffChunk
		err := json.Unmarshal([]byte(dbOperations[i].Operation), &chunks)
		if err != nil {
			continue
		}

		operations[i] = Operation{
			FileID:    dbOperations[i].FileID,
			Version:   dbOperations[i].Version,
			Operation: chunks,
			CreatedAt: dbOperations[i].CreatedAt,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(operations); err != nil {
		http.Error(w, "error reading request body", http.StatusInternalServerError)
		return
	}
}

func (s *syncinator) fetchFileHandler(w http.ResponseWriter, r *http.Request) {
	fileId, err := strconv.Atoi(r.PathValue("id"))

	if fileId == 0 || err != nil {
		http.Error(w, "invalid file id", http.StatusBadRequest)
		return
	}

	file, err := s.db.FetchFile(r.Context(), int64(fileId))
	if err != nil {
		http.Error(w, ErrNotExistingFile, http.StatusNotFound)
		return
	}

	workspaceID := middleware.WorkspaceIDFromCtx(r.Context())
	if file.WorkspaceID != workspaceID {
		http.Error(w, ErrNotExistingFile, http.StatusNotFound)
		return
	}

	mw := multipart.NewWriter(w)
	defer mw.Close()

	w.Header().Set("Content-Type", "multipart/mixed; boundary="+mw.Boundary())
	w.WriteHeader(http.StatusOK)

	metaPart, err := mw.CreatePart(textproto.MIMEHeader{
		"Content-Type":        []string{"application/json"},
		"Content-Disposition": []string{fmt.Sprintf("form-data; name=%q", MultipartMetadata)},
	})
	if err != nil {
		http.Error(w, "Error creating metadata part", http.StatusInternalServerError)
		return
	}
	if err := json.NewEncoder(metaPart).Encode(file); err != nil {
		http.Error(w, "error creating JSON part", http.StatusInternalServerError)
		return
	}

	fileContent, err := s.storage.ReadObject(file.DiskPath)
	if err != nil {
		http.Error(w, ErrReadingFile, http.StatusInternalServerError)
		return
	}
	defer fileContent.Close()

	filename := path.Base(file.WorkspacePath)
	mimeHeader := textproto.MIMEHeader{
		"Content-Type":        []string{file.MimeType},
		"Content-Disposition": []string{fmt.Sprintf(`form-data; filename=%q`, filename)},
	}
	if !strings.HasPrefix(file.MimeType, "text/") {
		mimeHeader["Content-Transfer-Encoding"] = []string{"base64"}
	}

	filePart, err := mw.CreatePart(mimeHeader)
	if err != nil {
		http.Error(w, "Error creating file part", http.StatusInternalServerError)
		return
	}

	var writer = filePart
	// if it is a non text file encode it in base64
	if !strings.HasPrefix(file.MimeType, "text/") {
		encoder := base64.NewEncoder(base64.StdEncoding, filePart)
		defer encoder.Close()

		writer = encoder
	}

	_, err = io.Copy(writer, fileContent)
	if err != nil {
		http.Error(w, "Error streaming file content", http.StatusInternalServerError)
		return
	}
}

func (s *syncinator) createFileHandler(w http.ResponseWriter, r *http.Request) {
	workspaceID := middleware.WorkspaceIDFromCtx(r.Context())

	if !requestutils.IsMultipartFormData(r) {
		errMsg := fmt.Sprintf("Unsupported Content-Type %q", r.Header.Get("Content-Type"))
		http.Error(w, errMsg, http.StatusUnsupportedMediaType)
		return
	}

	err := r.ParseMultipartForm(s.maxFileSize)
	if err != nil {
		http.Error(w, "Unable to parse form", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile(MultipartFileField)
	if err != nil {
		http.Error(w, "Error retrieving the file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	filepath := r.FormValue(MultipartFilepathField)
	if filepath == "" {
		http.Error(w, "Error invalid filepath", http.StatusBadRequest)
		return
	}

	// if there isn't any file an error is returned
	_, err = s.db.FetchFileFromWorkspacePath(r.Context(), repository.FetchFileFromWorkspacePathParams{
		WorkspaceID:   workspaceID,
		WorkspacePath: filepath,
	})
	if err == nil {
		http.Error(w, ErrDuplicateFile, http.StatusConflict)
		return
	}

	var fileReader io.ReadSeeker = file
	if header.Header.Get("Content-Transfer-Encoding") == "base64" {
		decoder := base64.NewDecoder(base64.StdEncoding, file)

		data, err := io.ReadAll(decoder)
		if err != nil {
			http.Error(w, "Unable to parse base64", http.StatusBadRequest)
			return
		}

		fileReader = bytes.NewReader(data)
	}

	diskPath, err := s.storage.CreateObject(fileReader)
	if err != nil {
		http.Error(w, ErrInvalidFile, http.StatusInternalServerError)
		return
	}

	mimeType := requestutils.DetectFileMimeType(fileReader)
	hash := filestorage.GenerateHash(fileReader)

	dbFile, err := s.db.CreateFile(r.Context(), repository.CreateFileParams{
		DiskPath:      diskPath,
		WorkspacePath: filepath,
		MimeType:      mimeType,
		Hash:          hash,
		WorkspaceID:   workspaceID,
	})

	if err != nil {
		http.Error(w, ErrInvalidFile, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(dbFile); err != nil {
		http.Error(w, "error reading request body", http.StatusInternalServerError)
		return
	}
}

func (s *syncinator) deleteFileHandler(w http.ResponseWriter, r *http.Request) {
	fileId, err := strconv.Atoi(r.PathValue("id"))

	if fileId == 0 || err != nil {
		http.Error(w, "invalid file id", http.StatusBadRequest)
		return
	}

	file, err := s.db.FetchFile(r.Context(), int64(fileId))
	if err != nil {
		http.Error(w, ErrNotExistingFile, http.StatusNotFound)
		return
	}

	workspaceID := middleware.WorkspaceIDFromCtx(r.Context())
	if file.WorkspaceID != workspaceID {
		http.Error(w, ErrNotExistingFile, http.StatusNotFound)
		return
	}

	if err := s.storage.DeleteObject(file.DiskPath); err != nil {
		http.Error(w, ErrNotExistingFile, http.StatusInternalServerError)
		return
	}

	err = s.db.DeleteFile(r.Context(), int64(fileId))
	if err != nil {
		http.Error(w, ErrInvalidFile, http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *syncinator) updateFileHandler(w http.ResponseWriter, r *http.Request) {
	fileId, err := strconv.Atoi(r.PathValue("id"))

	if fileId == 0 || err != nil {
		http.Error(w, "invalid file id", http.StatusBadRequest)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "error reading request body", http.StatusInternalServerError)
		return
	}

	var data UpdateFileBody
	if err = json.Unmarshal(body, &data); err != nil {
		http.Error(w, "error parsing JSON", http.StatusBadRequest)
		return
	}

	if data.Path == "" {
		http.Error(w, "invalid path ''", http.StatusBadRequest)
		return
	}

	file, err := s.db.FetchFile(r.Context(), int64(fileId))
	if err != nil {
		http.Error(w, ErrNotExistingFile, http.StatusNotFound)
		return
	}

	workspaceID := middleware.WorkspaceIDFromCtx(r.Context())
	if file.WorkspaceID != workspaceID {
		http.Error(w, ErrNotExistingFile, http.StatusNotFound)
		return
	}

	err = s.db.UpdateWorkspacePath(r.Context(), repository.UpdateWorkspacePathParams{
		WorkspacePath: data.Path,
		ID:            file.ID,
	})
	if err != nil {
		http.Error(w, ErrInvalidFile, http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
