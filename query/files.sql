-- name: CreateFile :one
INSERT INTO files (disk_path, workspace_path, mime_type, hash, workspace_id)
VALUES (?, ?, ?, ?, ?)
RETURNING *;

-- name: FetchFile :one
SELECT *
FROM files
WHERE id = ?
LIMIT 1;

-- name: FetchFiles :many
SELECT *
FROM files
WHERE workspace_id = ?;

-- name: FetchWorkspaceFiles :many
SELECT *
FROM files
WHERE workspace_id = ?;

-- name: FetchAllFiles :many
SELECT *
FROM files;

-- name: FetchAllTextFiles :many
SELECT *
FROM files
WHERE mime_type LIKE 'text/%';

-- name: DeleteFile :exec
DELETE FROM files
WHERE id = ?;

-- name: FetchFileFromWorkspacePath :one
SELECT *
FROM files
WHERE workspace_path = ?
LIMIT 1;

-- name: UpdateUpdatedAt :exec
UPDATE files
SET 
    updated_at = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: UpdateWorkspacePath :exec
UPDATE files
SET 
    workspace_path = ?
WHERE id = ?;

