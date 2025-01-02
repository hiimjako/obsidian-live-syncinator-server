-- +goose Up
-- +goose StatementBegin
CREATE TABLE files (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  disk_path TEXT NOT NULL,
  workspace_path TEXT NOT NULL,
  mime_type TEXT NOT NULL,
  hash TEXT NOT NULL,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP NOT NULL,
  updated_at DATETIME DEFAULT CURRENT_TIMESTAMP NOT NULL,
  version INTEGER DEFAULT 0 NOT NULL,
  workspace_id INTEGER NOT NULL,
  FOREIGN KEY (workspace_id) REFERENCES workspaces (id),
  UNIQUE (disk_path, workspace_path)
);

-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DROP TABLE files;

-- +goose StatementEnd
