-- +goose Up
-- +goose StatementBegin
CREATE TABLE snapshots (
  file_id INTEGER NOT NULL,
  version INTEGER NOT NULL,
  disk_path TEXT NOT NULL,
  hash TEXT NOT NULL,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP NOT NULL,
  type TEXT CHECK (type IN ('file', 'diff')) NOT NULL,
  workspace_id INTEGER NOT NULL,
  PRIMARY KEY (file_id, version),
  FOREIGN KEY (file_id) REFERENCES files (id),
  FOREIGN KEY (workspace_id) REFERENCES workspaces (id)
);

-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DROP TABLE snapshots;

-- +goose StatementEnd
