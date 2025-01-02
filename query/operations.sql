-- name: CreateOperation :exec
INSERT INTO operations (file_id, version, operation)
VALUES (?, ?, ?);

-- name: FetchOperation :one
SELECT *
FROM operations
WHERE file_id = ? AND version = ?
LIMIT 1;

-- name: FetchFileOperationsFromVersion :many
SELECT o.*
FROM operations o
JOIN files f ON o.file_id = f.id
WHERE o.file_id = ? AND o.version > ? AND f.workspace_id = ?
ORDER BY o.version ASC;
