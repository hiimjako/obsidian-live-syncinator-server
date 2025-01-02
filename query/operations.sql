-- name: CreateOperation :one
INSERT INTO operations (file_id, version, operation)
VALUES (?, ?, ?)
RETURNING *;

-- name: FetchOperation :one
SELECT *
FROM operations
WHERE file_id = ? AND version = ?
LIMIT 1;

-- name: FetchFileOperationsFromVersion :many
SELECT *
FROM operations
WHERE file_id = ? AND version > ?
ORDER BY version ASC;
