-- name: CreateSnapshot :exec
INSERT INTO snapshots (file_id, version, disk_path, type)
VALUES (?, ?, ?, ?);

-- name: FetchSnapshots :many
SELECT *
FROM snapshots
WHERE file_id = ? 
ORDER BY version ASC;

-- name: FetchSnapshot :one
SELECT *
FROM snapshots
WHERE file_id = ? AND version = ?
LIMIT 1;

-- name: DeleteSnapshot :exec
DELETE FROM operations
WHERE file_id < ? AND version = ?;

