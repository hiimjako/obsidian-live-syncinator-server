// Code generated by sqlc. DO NOT EDIT.
// versions:
//   sqlc v1.27.0

package repository

import (
	"database/sql"
	"time"
)

type File struct {
	ID            int64     `json:"id"`
	DiskPath      string    `json:"diskPath"`
	WorkspacePath string    `json:"workspacePath"`
	MimeType      string    `json:"mimeType"`
	Hash          string    `json:"hash"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
	Version       int64     `json:"version"`
	WorkspaceID   int64     `json:"workspaceId"`
}

type Operation struct {
	FileID    int64     `json:"fileId"`
	Version   int64     `json:"version"`
	Operation string    `json:"operation"`
	CreatedAt time.Time `json:"createdAt"`
}

type Snapshot struct {
	FileID    int64     `json:"fileId"`
	Version   int64     `json:"version"`
	DiskPath  string    `json:"diskPath"`
	Hash      string    `json:"hash"`
	CreatedAt time.Time `json:"createdAt"`
	Type      string    `json:"type"`
}

type Workspace struct {
	ID        int64        `json:"id"`
	Name      string       `json:"name"`
	Password  string       `json:"password"`
	CreatedAt sql.NullTime `json:"createdAt"`
	UpdatedAt sql.NullTime `json:"updatedAt"`
}
