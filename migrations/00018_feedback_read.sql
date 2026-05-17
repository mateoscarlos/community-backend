-- +goose Up

-- Tracks which feedback entries an admin has marked as read. Kept in its own
-- table so the existing `feedback` queries (sqlc, RETURNING *) are untouched.
-- A row present = viewed; absent = new.
CREATE TABLE feedback_read (
    feedback_id UUID        PRIMARY KEY REFERENCES feedback(id) ON DELETE CASCADE,
    viewed_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS feedback_read;
