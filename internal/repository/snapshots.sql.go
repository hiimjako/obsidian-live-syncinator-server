// Code generated by sqlc. DO NOT EDIT.
// versions:
//   sqlc v1.27.0
// source: snapshots.sql

package repository

import (
	"context"
)

const createSnapshot = `-- name: CreateSnapshot :exec
INSERT INTO snapshots (file_id, version, disk_path, type)
VALUES (?, ?, ?, ?)
`

type CreateSnapshotParams struct {
	FileID   int64  `json:"fileId"`
	Version  int64  `json:"version"`
	DiskPath string `json:"diskPath"`
	Type     string `json:"type"`
}

func (q *Queries) CreateSnapshot(ctx context.Context, arg CreateSnapshotParams) error {
	_, err := q.db.ExecContext(ctx, createSnapshot,
		arg.FileID,
		arg.Version,
		arg.DiskPath,
		arg.Type,
	)
	return err
}

const deleteSnapshot = `-- name: DeleteSnapshot :exec
DELETE FROM operations
WHERE file_id < ? AND version = ?
`

type DeleteSnapshotParams struct {
	FileID  int64 `json:"fileId"`
	Version int64 `json:"version"`
}

func (q *Queries) DeleteSnapshot(ctx context.Context, arg DeleteSnapshotParams) error {
	_, err := q.db.ExecContext(ctx, deleteSnapshot, arg.FileID, arg.Version)
	return err
}

const fetchSnapshot = `-- name: FetchSnapshot :one
SELECT file_id, version, disk_path, created_at, type
FROM snapshots
WHERE file_id = ? AND version = ?
LIMIT 1
`

type FetchSnapshotParams struct {
	FileID  int64 `json:"fileId"`
	Version int64 `json:"version"`
}

func (q *Queries) FetchSnapshot(ctx context.Context, arg FetchSnapshotParams) (Snapshot, error) {
	row := q.db.QueryRowContext(ctx, fetchSnapshot, arg.FileID, arg.Version)
	var i Snapshot
	err := row.Scan(
		&i.FileID,
		&i.Version,
		&i.DiskPath,
		&i.CreatedAt,
		&i.Type,
	)
	return i, err
}

const fetchSnapshots = `-- name: FetchSnapshots :many
SELECT s.file_id, s.version, s.disk_path, s.created_at, s.type
FROM snapshots s
JOIN files f ON f.id = s.file_id
WHERE s.file_id = ? AND f.workspace_id = ?
ORDER BY s.version ASC
`

type FetchSnapshotsParams struct {
	FileID      int64 `json:"fileId"`
	WorkspaceID int64 `json:"workspaceId"`
}

func (q *Queries) FetchSnapshots(ctx context.Context, arg FetchSnapshotsParams) ([]Snapshot, error) {
	rows, err := q.db.QueryContext(ctx, fetchSnapshots, arg.FileID, arg.WorkspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []Snapshot
	for rows.Next() {
		var i Snapshot
		if err := rows.Scan(
			&i.FileID,
			&i.Version,
			&i.DiskPath,
			&i.CreatedAt,
			&i.Type,
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
