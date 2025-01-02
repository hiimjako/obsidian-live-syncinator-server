-- +goose Up
-- +goose StatementBegin
CREATE TABLE operations (
  file_id INTEGER NOT NULL,
  version INTEGER NOT NULL,
  operation TEXT CHECK (json_valid(operation)) NOT NULL,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP NOT NULL,
  PRIMARY KEY(file_id, version),
  FOREIGN KEY(file_id) REFERENCES files(id)
);

-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DROP TABLE operations;

-- +goose StatementEnd

