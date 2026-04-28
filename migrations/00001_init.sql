-- +goose Up
CREATE TABLE IF NOT EXISTS schema_info (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

INSERT INTO schema_info (key, value) VALUES ('version', '1');

-- +goose Down
DROP TABLE IF EXISTS schema_info;
