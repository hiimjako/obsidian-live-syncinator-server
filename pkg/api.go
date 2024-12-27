package rtsync

import (
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"path"
	"strconv"

	"github.com/hiimjako/real-time-sync-obsidian-be/internal/repository"
	"github.com/hiimjako/real-time-sync-obsidian-be/internal/requestutils"
	"github.com/hiimjako/real-time-sync-obsidian-be/pkg/filestorage"
	"github.com/hiimjako/real-time-sync-obsidian-be/pkg/middleware"
)

const (
	MultipartFileField     = "file"
	MultipartFilepathField = "path"
)

type UpdateFileBody struct {
	Path string `json:"path"`
}

const (
	ErrDuplicateFile   = "duplicated file"
	ErrInvalidFile     = "impossilbe to create file"
	ErrReadingFile     = "impossilbe to read file"
	ErrNotExistingFile = "not existing file"
)

func (rts *realTimeSyncServer) apiHandler() http.Handler {
	router := http.NewServeMux()
	router.HandleFunc("GET /file", rts.listFilesHandler)
	router.HandleFunc("GET /file/{id}", rts.fetchFileHandler)
	router.HandleFunc("POST /file", rts.createFileHandler)
	router.HandleFunc("DELETE /file/{id}", rts.deleteFileHandler)
	router.HandleFunc("PATCH /file/{id}", rts.updateFileHandler)

	stack := middleware.CreateStack(
		middleware.Logging,
		middleware.Cors(middleware.CorsOptions{
			AllowedOrigins: []string{"*"},
			AllowedMethods: []string{"HEAD", "GET", "POST", "OPTIONS", "DELETE", "PATCH"},
			AllowedHeaders: []string{"Origin", "X-Requested-With", "Content-Type", "Accept", "Authorization"},
		}),
		middleware.IsAuthenticated(middleware.AuthOptions{SecretKey: rts.jwtSecret}),
	)

	routerWithStack := stack(router)
	return routerWithStack
}

func (rts *realTimeSyncServer) listFilesHandler(w http.ResponseWriter, r *http.Request) {
	workspaceID := middleware.WorkspaceIDFromCtx(r.Context())

	files, err := rts.db.FetchFiles(r.Context(), workspaceID)
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

func (rts *realTimeSyncServer) fetchFileHandler(w http.ResponseWriter, r *http.Request) {
	fileId, err := strconv.Atoi(r.PathValue("id"))

	if fileId == 0 || err != nil {
		http.Error(w, "invalid file id", http.StatusBadRequest)
		return
	}

	file, err := rts.db.FetchFile(r.Context(), int64(fileId))
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

	metaPart, err := mw.CreatePart(textproto.MIMEHeader{
		"Content-Type": []string{"application/json"},
	})
	if err != nil {
		http.Error(w, "Error creating metadata part", http.StatusInternalServerError)
		return
	}
	if err := json.NewEncoder(metaPart).Encode(file); err != nil {
		http.Error(w, "error creating JSON part", http.StatusInternalServerError)
		return
	}

	fileContent, err := rts.storage.ReadObject(file.DiskPath)
	if err != nil {
		http.Error(w, ErrReadingFile, http.StatusInternalServerError)
		return
	}
	defer fileContent.Close()

	filename := path.Base(file.WorkspacePath)
	filePart, err := mw.CreatePart(textproto.MIMEHeader{
		"Content-Type":        []string{"application/octet-stream"},
		"Content-Disposition": []string{fmt.Sprintf(`attachment; filename=%q`, filename)},
	})
	if err != nil {
		http.Error(w, "Error creating file part", http.StatusInternalServerError)
		return
	}
	_, err = io.Copy(filePart, fileContent)
	if err != nil {
		http.Error(w, "Error streaming file content", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "multipart/mixed; boundary="+mw.Boundary())
	w.WriteHeader(http.StatusOK)
}

func (rts *realTimeSyncServer) createFileHandler(w http.ResponseWriter, r *http.Request) {
	if !requestutils.IsMultipartFormData(r) {
		errMsg := fmt.Sprintf("Unsupported Content-Type %q", r.Header.Get("Content-Type"))
		http.Error(w, errMsg, http.StatusUnsupportedMediaType)
		return
	}

	err := r.ParseMultipartForm(rts.maxFileSize)
	if err != nil {
		http.Error(w, "Unable to parse form", http.StatusBadRequest)
		return
	}

	file, _, err := r.FormFile(MultipartFileField)
	if err != nil {
		http.Error(w, "Error retrieving the file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	filepath := r.FormValue(MultipartFilepathField)

	// if there isn't any file an error is returned
	_, err = rts.db.FetchFileFromWorkspacePath(r.Context(), filepath)
	if err == nil {
		http.Error(w, ErrDuplicateFile, http.StatusConflict)
		return
	}

	diskPath, err := rts.storage.CreateObject(file)
	if err != nil {
		http.Error(w, ErrInvalidFile, http.StatusInternalServerError)
		return
	}

	mimeType := requestutils.DetectFileMimeType(file)
	hash := filestorage.GenerateHash(file)
	workspaceID := middleware.WorkspaceIDFromCtx(r.Context())

	dbFile, err := rts.db.CreateFile(r.Context(), repository.CreateFileParams{
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

func (rts *realTimeSyncServer) deleteFileHandler(w http.ResponseWriter, r *http.Request) {
	fileId, err := strconv.Atoi(r.PathValue("id"))

	if fileId == 0 || err != nil {
		http.Error(w, "invalid file id", http.StatusBadRequest)
		return
	}

	file, err := rts.db.FetchFile(r.Context(), int64(fileId))
	if err != nil {
		http.Error(w, ErrNotExistingFile, http.StatusNotFound)
		return
	}

	workspaceID := middleware.WorkspaceIDFromCtx(r.Context())
	if file.WorkspaceID != workspaceID {
		http.Error(w, ErrNotExistingFile, http.StatusNotFound)
		return
	}

	if err := rts.storage.DeleteObject(file.DiskPath); err != nil {
		http.Error(w, ErrNotExistingFile, http.StatusInternalServerError)
		return
	}

	err = rts.db.DeleteFile(r.Context(), int64(fileId))
	if err != nil {
		http.Error(w, ErrInvalidFile, http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (rts *realTimeSyncServer) updateFileHandler(w http.ResponseWriter, r *http.Request) {
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

	file, err := rts.db.FetchFile(r.Context(), int64(fileId))
	if err != nil {
		http.Error(w, ErrNotExistingFile, http.StatusNotFound)
		return
	}

	workspaceID := middleware.WorkspaceIDFromCtx(r.Context())
	if file.WorkspaceID != workspaceID {
		http.Error(w, ErrNotExistingFile, http.StatusNotFound)
		return
	}

	err = rts.db.UpdateWorkspacePath(r.Context(), repository.UpdateWorkspacePathParams{
		WorkspacePath: data.Path,
		ID:            file.ID,
	})
	if err != nil {
		http.Error(w, ErrInvalidFile, http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
