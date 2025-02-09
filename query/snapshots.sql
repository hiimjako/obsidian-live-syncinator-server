-- name: CreateSnapshot :exec
INSERT INTO snapshots (file_id, version, disk_path, type)
VALUES (?, ?, ?, ?);

-- name: FetchSnapshots :many
SELECT s.*
FROM snapshots s
JOIN files f ON f.id = s.file_id
WHERE s.file_id = ? AND f.workspace_id = ?
ORDER BY s.version ASC;

-- name: FetchSnapshot :one
SELECT *
FROM snapshots
WHERE file_id = ? AND version = ?
LIMIT 1;

-- name: DeleteSnapshot :exec
DELETE FROM operations
WHERE file_id < ? AND version = ?;

