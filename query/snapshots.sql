-- name: CreateSnapshot :exec
INSERT INTO snapshots (file_id, version, disk_path, hash, type)
VALUES (?, ?, ?, ?, ?);

-- name: FetchSnapshots :many
SELECT s.*, f.workspace_id
FROM snapshots s
JOIN files f ON f.id = s.file_id
WHERE s.file_id = ? AND f.workspace_id = ?
ORDER BY s.version ASC;

-- name: FetchSnapshot :one
SELECT s.*, f.workspace_id, f.mime_type, f.workspace_path
FROM snapshots s
JOIN files f ON f.id = s.file_id
WHERE s.file_id = ? AND s.version = ? AND f.workspace_id = ?
LIMIT 1;

-- name: DeleteSnapshot :exec
DELETE FROM operations
WHERE file_id < ? AND version = ?;

