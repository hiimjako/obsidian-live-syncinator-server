-- name: CreateSnapshot :exec
INSERT INTO snapshots (file_id, version, disk_path, type, hash, workspace_id)
VALUES (?, ?, ?, ?, ?, ?);

-- name: FetchSnapshots :many
SELECT *
FROM snapshots
WHERE file_id = ?
  AND workspace_id = ?
ORDER BY created_at DESC, version DESC;

-- name: FetchLatestSnapshotForFile :one
SELECT *
FROM snapshots
WHERE file_id = ?
ORDER BY created_at DESC, version DESC
LIMIT 1;

-- name: FetchSnapshotByVersion :one
SELECT *
FROM snapshots
WHERE file_id = ?
  AND version = ?
  AND workspace_id = ?;

-- name: DeleteSnapshotsForFile :exec
DELETE FROM snapshots
WHERE file_id = ?;
