package rtsync

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/hiimjako/real-time-sync-obsidian-be/pkg/middleware"
	"golang.org/x/crypto/bcrypt"
)

type WorkspaceCredentials struct {
	Name     string `json:"name"`
	Password string `json:"password"`
}

type LoginResponse struct {
	Token string `json:"token"`
}

const (
	ErrIncorrectPassword = "incorrect password"
	ErrWorkspaceNotFound = "workspace not found"
)

func (rts *realTimeSyncServer) authHandler() http.Handler {
	router := http.NewServeMux()
	router.HandleFunc("POST /login", rts.fetchWorkspaceHandler)

	stack := middleware.CreateStack(
		middleware.Logging,
		middleware.Cors(middleware.CorsOptions{
			AllowedOrigins: []string{"*"},
			AllowedMethods: []string{"HEAD", "GET", "POST", "OPTIONS", "DELETE"},
			AllowedHeaders: []string{"Origin", "X-Requested-With", "Content-Type", "Accept", "Authorization"},
		}),
	)

	routerWithStack := stack(router)
	return routerWithStack
}

func (rts *realTimeSyncServer) fetchWorkspaceHandler(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "error reading request body", http.StatusInternalServerError)
		return
	}

	var data WorkspaceCredentials
	if err := json.Unmarshal(body, &data); err != nil {
		http.Error(w, "error parsing JSON", http.StatusBadRequest)
		return
	}

	workspace, err := rts.db.FetchWorkspace(r.Context(), data.Name)
	if err != nil {
		http.Error(w, ErrWorkspaceNotFound, http.StatusNotFound)
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(workspace.Password), []byte(data.Password)); err != nil {
		http.Error(w, ErrIncorrectPassword, http.StatusUnauthorized)
		return
	}

	token, err := middleware.CreateToken(middleware.AuthOptions{SecretKey: rts.jwtSecret}, workspace.ID)
	if err != nil {
		http.Error(w, "error while creating auth token", http.StatusInternalServerError)
		return
	}

	response := LoginResponse{
		Token: token,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "error reading request body", http.StatusInternalServerError)
		return
	}
}
