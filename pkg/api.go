package syncinator

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/hiimjako/syncinator/internal/repository"
	"github.com/hiimjako/syncinator/internal/requestutils"
	"github.com/hiimjako/syncinator/pkg/diff"
	"github.com/hiimjako/syncinator/pkg/filestorage"
	"github.com/hiimjako/syncinator/pkg/middleware"
	"github.com/hiimjako/syncinator/pkg/mimeutils"
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
	FileID    int64        `json:"fileId"`
	Version   int64        `json:"version"`
	Operation []diff.Chunk `json:"operation"`
	CreatedAt time.Time    `json:"createdAt"`
}

const (
	ErrDuplicateFile       = "duplicated file"
	ErrInvalidFile         = "impossible to create file"
	ErrReadingFile         = "impossible to read file"
	ErrNotExistingFile     = "not existing file"
	ErrNotExistingSnapshot = "not existing snapshot"
)

func (s *syncinator) apiHandler() http.Handler {
	router := http.NewServeMux()
	router.HandleFunc("GET /export", s.exportHandler)
	router.HandleFunc("GET /file", s.listFilesHandler)
	router.HandleFunc("GET /file/{id}", s.fetchFileHandler)
	router.HandleFunc("GET /file/{id}/snapshot", s.listFileSnapshotsHandler)
	router.HandleFunc("GET /file/{id}/snapshot/{version}", s.fetchSnapshotHandler)
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

func writeMultipartResponse(w http.ResponseWriter, metadata any, mimeType string, filename string, content io.Reader) error {
	mw := multipart.NewWriter(w)
	defer mw.Close()

	w.Header().Set("Content-Type", "multipart/mixed; boundary="+mw.Boundary())
	w.WriteHeader(http.StatusOK)

	metaPart, err := mw.CreatePart(textproto.MIMEHeader{
		"Content-Type":        {"application/json"},
		"Content-Disposition": {fmt.Sprintf("form-data; name=%q", MultipartMetadata)},
	})
	if err != nil {
		return fmt.Errorf("creating metadata part: %w", err)
	}
	if err := json.NewEncoder(metaPart).Encode(metadata); err != nil {
		return fmt.Errorf("encoding metadata: %w", err)
	}

	mimeHeader := textproto.MIMEHeader{
		"Content-Type":        {mimeType},
		"Content-Disposition": {fmt.Sprintf(`form-data; filename=%q`, filename)},
	}
	if !mimeutils.IsText(mimeType) {
		mimeHeader["Content-Transfer-Encoding"] = []string{"base64"}
	}

	filePart, err := mw.CreatePart(mimeHeader)
	if err != nil {
		return fmt.Errorf("creating file part: %w", err)
	}

	var writer io.Writer = filePart
	if !mimeutils.IsText(mimeType) {
		encoder := base64.NewEncoder(base64.StdEncoding, filePart)
		defer encoder.Close()
		writer = encoder
	}

	if _, err = io.Copy(writer, content); err != nil {
		return fmt.Errorf("writing content: %w", err)
	}

	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(v); err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	buf.WriteTo(w) //nolint:errcheck
}

func (s *syncinator) exportHandler(w http.ResponseWriter, r *http.Request) {
	workspaceID, _ := middleware.WorkspaceIDFromCtx(r.Context())

	files, err := s.db.FetchFiles(r.Context(), workspaceID)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	zipWriter := zip.NewWriter(w)

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set(
		"Content-Disposition",
		fmt.Sprintf("attachment; filename=workspace-%d-%s.zip", workspaceID, time.Now().Format(time.DateOnly)),
	)
	for _, file := range files {
		f, err := zipWriter.Create(file.WorkspacePath)
		if err != nil {
			log.Printf("failed to create file in zip: %v", err)
			return
		}

		r, err := s.storage.ReadObject(file.DiskPath)
		if err != nil {
			log.Printf("failed to read object for zip: %v", err)
			return
		}

		_, err = io.Copy(f, r)
		if err != nil {
			r.Close()
			log.Printf("failed to write content to zip: %v", err)
			return
		}
		r.Close()
	}

	if err = zipWriter.Close(); err != nil {
		log.Printf("failed to close zip writer: %v", err)
		return
	}
}

func (s *syncinator) listFilesHandler(w http.ResponseWriter, r *http.Request) {
	workspaceID, _ := middleware.WorkspaceIDFromCtx(r.Context())

	files, err := s.db.FetchFiles(r.Context(), workspaceID)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, files)
}

func (s *syncinator) listFileSnapshotsHandler(w http.ResponseWriter, r *http.Request) {
	fileID, err := strconv.Atoi(r.PathValue("id"))

	if fileID == 0 || err != nil {
		http.Error(w, "invalid file id", http.StatusBadRequest)
		return
	}

	workspaceID, _ := middleware.WorkspaceIDFromCtx(r.Context())
	files, err := s.db.FetchSnapshots(r.Context(), repository.FetchSnapshotsParams{
		FileID:      int64(fileID),
		WorkspaceID: workspaceID,
	})
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, files)
}

func (s *syncinator) listOperationsHandler(w http.ResponseWriter, r *http.Request) {
	fromVersion, err := strconv.Atoi(r.URL.Query().Get("from"))
	if fromVersion < 0 || err != nil {
		http.Error(w, "invalid \"from version\"", http.StatusBadRequest)
		return
	}

	fileID, err := strconv.Atoi(r.URL.Query().Get("fileId"))
	if fileID < 0 || err != nil {
		http.Error(w, "invalid \"fileId\"", http.StatusBadRequest)
		return
	}

	workspaceID, _ := middleware.WorkspaceIDFromCtx(r.Context())
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
		var chunks []diff.Chunk
		err := json.Unmarshal([]byte(dbOperations[i].Operation), &chunks)
		if err != nil {
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		operations[i] = Operation{
			FileID:    dbOperations[i].FileID,
			Version:   dbOperations[i].Version,
			Operation: chunks,
			CreatedAt: dbOperations[i].CreatedAt,
		}
	}

	writeJSON(w, http.StatusOK, operations)
}

func (s *syncinator) fetchFileHandler(w http.ResponseWriter, r *http.Request) {
	fileID, err := strconv.Atoi(r.PathValue("id"))

	if fileID == 0 || err != nil {
		http.Error(w, "invalid file id", http.StatusBadRequest)
		return
	}

	file, err := s.db.FetchFile(r.Context(), int64(fileID))
	if err != nil {
		http.Error(w, ErrNotExistingFile, http.StatusNotFound)
		return
	}

	workspaceID, _ := middleware.WorkspaceIDFromCtx(r.Context())
	if file.WorkspaceID != workspaceID {
		http.Error(w, ErrNotExistingFile, http.StatusNotFound)
		return
	}

	err = s.WriteFileToStorage(file.ID)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	fileContent, err := s.storage.ReadObject(file.DiskPath)
	if err != nil {
		log.Printf("error reading file %d: %v", file.ID, err)
		return
	}
	defer fileContent.Close()

	if err := writeMultipartResponse(w, file, file.MimeType, path.Base(file.WorkspacePath), fileContent); err != nil {
		log.Printf("error writing multipart response: %v", err)
		return
	}
}

func (s *syncinator) fetchSnapshotHandler(w http.ResponseWriter, r *http.Request) {
	fileID, err := strconv.Atoi(r.PathValue("id"))
	if fileID == 0 || err != nil {
		http.Error(w, "invalid file id", http.StatusBadRequest)
		return
	}

	snapshotVersion, err := strconv.Atoi(r.PathValue("version"))
	if snapshotVersion < 0 || err != nil {
		http.Error(w, "invalid snapshot version", http.StatusBadRequest)
		return
	}

	workspaceID, _ := middleware.WorkspaceIDFromCtx(r.Context())
	snapshot, err := s.db.FetchSnapshotByVersion(r.Context(), repository.FetchSnapshotByVersionParams{
		FileID:      int64(fileID),
		Version:     int64(snapshotVersion),
		WorkspaceID: workspaceID,
	})
	if err != nil {
		http.Error(w, ErrNotExistingSnapshot, http.StatusNotFound)
		return
	}

	// Reconstruct the file content at this version
	content, err := s.ReconstructSnapshot(int64(fileID), int64(snapshotVersion), workspaceID)
	if err != nil {
		http.Error(w, "Error reconstructing snapshot", http.StatusInternalServerError)
		return
	}

	fileMeta, err := s.db.FetchFile(r.Context(), snapshot.FileID)
	if err != nil {
		log.Printf("error fetching file metadata for snapshot: %v", err)
		return
	}

	if err := writeMultipartResponse(w, snapshot, fileMeta.MimeType, path.Base(fileMeta.WorkspacePath), strings.NewReader(content)); err != nil {
		log.Printf("error writing multipart response: %v", err)
		return
	}
}

func (s *syncinator) createFileHandler(w http.ResponseWriter, r *http.Request) {
	workspaceID, _ := middleware.WorkspaceIDFromCtx(r.Context())

	if !requestutils.IsMultipartFormData(r) {
		errMsg := fmt.Sprintf("Unsupported Content-Type %q", r.Header.Get("Content-Type"))
		http.Error(w, errMsg, http.StatusUnsupportedMediaType)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, s.maxFileSizeBytes)
	err := r.ParseMultipartForm(s.maxFileSizeBytes)
	if err != nil {
		http.Error(w, "Unable to parse form", http.StatusBadRequest)
		return
	}
	if r.MultipartForm != nil {
		defer func() { _ = r.MultipartForm.RemoveAll() }()
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
	if !errors.Is(err, sql.ErrNoRows) {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
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

	mimeType := requestutils.DetectFileMimeType(fileReader, filepath)
	hash, err := filestorage.GenerateHash(fileReader)
	if err != nil {
		http.Error(w, ErrInvalidFile, http.StatusInternalServerError)
		return
	}

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

	writeJSON(w, http.StatusCreated, dbFile)
}

func (s *syncinator) deleteFileHandler(w http.ResponseWriter, r *http.Request) {
	fileID, err := strconv.Atoi(r.PathValue("id"))

	if fileID == 0 || err != nil {
		http.Error(w, "invalid file id", http.StatusBadRequest)
		return
	}

	file, err := s.db.FetchFile(r.Context(), int64(fileID))
	if err != nil {
		http.Error(w, ErrNotExistingFile, http.StatusNotFound)
		return
	}

	workspaceID, _ := middleware.WorkspaceIDFromCtx(r.Context())
	if file.WorkspaceID != workspaceID {
		http.Error(w, ErrNotExistingFile, http.StatusNotFound)
		return
	}

	if cached, ok := s.fileCache.Get(file.ID); ok {
		cached.mut.Lock()
		cached.pendingChanges = 0
		cached.mut.Unlock()
		s.fileCache.Remove(file.ID)
	}

	snapshots, err := s.db.FetchSnapshots(r.Context(), repository.FetchSnapshotsParams{
		FileID:      int64(fileID),
		WorkspaceID: workspaceID,
	})
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	// delete storage objects first — if any fail, DB records are untouched
	for _, snapshot := range snapshots {
		if err := s.storage.DeleteObject(snapshot.DiskPath); err != nil {
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
	}

	if err := s.storage.DeleteObject(file.DiskPath); err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	// delete DB records in a transaction — all or nothing
	tx, err := s.conn.BeginTx(r.Context(), nil)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	txq := s.db.WithTx(tx)
	if err := txq.DeleteSnapshotsForFile(r.Context(), int64(fileID)); err != nil {
		_ = tx.Rollback()
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if err := txq.DeleteFile(r.Context(), int64(fileID)); err != nil {
		_ = tx.Rollback()
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if err := tx.Commit(); err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *syncinator) updateFileHandler(w http.ResponseWriter, r *http.Request) {
	fileID, err := strconv.Atoi(r.PathValue("id"))

	if fileID == 0 || err != nil {
		http.Error(w, "invalid file id", http.StatusBadRequest)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
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

	file, err := s.db.FetchFile(r.Context(), int64(fileID))
	if err != nil {
		http.Error(w, ErrNotExistingFile, http.StatusNotFound)
		return
	}

	workspaceID, _ := middleware.WorkspaceIDFromCtx(r.Context())
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

	updatedFile, err := s.db.FetchFile(r.Context(), int64(fileID))
	if err != nil {
		http.Error(w, ErrNotExistingFile, http.StatusNotFound)
		return
	}

	writeJSON(w, http.StatusOK, updatedFile)
}
