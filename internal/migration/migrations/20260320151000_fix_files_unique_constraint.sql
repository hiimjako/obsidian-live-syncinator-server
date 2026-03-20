-- +goose Up
-- +goose StatementBegin
CREATE TABLE files_new (
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
  UNIQUE (workspace_id, workspace_path)
);

INSERT INTO
  files_new
SELECT
  *
FROM
  files;

DROP TABLE files;

ALTER TABLE files_new
RENAME TO files;

-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
CREATE TABLE files_old (
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

INSERT INTO
  files_old
SELECT
  *
FROM
  files;

DROP TABLE files;

ALTER TABLE files_old
RENAME TO files;

-- +goose StatementEnd
