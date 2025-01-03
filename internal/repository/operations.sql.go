// Code generated by sqlc. DO NOT EDIT.
// versions:
//   sqlc v1.27.0
// source: operations.sql

package repository

import (
	"context"
	"time"
)

const createOperation = `-- name: CreateOperation :exec
INSERT INTO operations (file_id, version, operation)
VALUES (?, ?, ?)
`

type CreateOperationParams struct {
	FileID    int64  `json:"fileId"`
	Version   int64  `json:"version"`
	Operation string `json:"operation"`
}

func (q *Queries) CreateOperation(ctx context.Context, arg CreateOperationParams) error {
	_, err := q.db.ExecContext(ctx, createOperation, arg.FileID, arg.Version, arg.Operation)
	return err
}

const deleteOperationOlderThan = `-- name: DeleteOperationOlderThan :exec
DELETE FROM operations
WHERE created_at < ?
`

func (q *Queries) DeleteOperationOlderThan(ctx context.Context, createdAt time.Time) error {
	_, err := q.db.ExecContext(ctx, deleteOperationOlderThan, createdAt)
	return err
}

const fetchFileOperationsFromVersion = `-- name: FetchFileOperationsFromVersion :many
SELECT o.file_id, o.version, o.operation, o.created_at
FROM operations o
JOIN files f ON o.file_id = f.id
WHERE o.file_id = ? AND o.version > ? AND f.workspace_id = ?
ORDER BY o.version ASC
`

type FetchFileOperationsFromVersionParams struct {
	FileID      int64 `json:"fileId"`
	Version     int64 `json:"version"`
	WorkspaceID int64 `json:"workspaceId"`
}

func (q *Queries) FetchFileOperationsFromVersion(ctx context.Context, arg FetchFileOperationsFromVersionParams) ([]Operation, error) {
	rows, err := q.db.QueryContext(ctx, fetchFileOperationsFromVersion, arg.FileID, arg.Version, arg.WorkspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []Operation
	for rows.Next() {
		var i Operation
		if err := rows.Scan(
			&i.FileID,
			&i.Version,
			&i.Operation,
			&i.CreatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const fetchOperation = `-- name: FetchOperation :one
SELECT file_id, version, operation, created_at
FROM operations
WHERE file_id = ? AND version = ?
LIMIT 1
`

type FetchOperationParams struct {
	FileID  int64 `json:"fileId"`
	Version int64 `json:"version"`
}

func (q *Queries) FetchOperation(ctx context.Context, arg FetchOperationParams) (Operation, error) {
	row := q.db.QueryRowContext(ctx, fetchOperation, arg.FileID, arg.Version)
	var i Operation
	err := row.Scan(
		&i.FileID,
		&i.Version,
		&i.Operation,
		&i.CreatedAt,
	)
	return i, err
}
