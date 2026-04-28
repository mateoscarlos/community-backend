-- +goose Up

CREATE TABLE feedback (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    message    TEXT        NOT NULL,
    contact    TEXT        NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS feedback;
