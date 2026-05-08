-- +goose Up

-- Tracks when a submission's R2/MinIO object was deleted by the storage
-- cleanup sweeper. We keep the DB row for archival/stat purposes; only the
-- backing object disappears. NULL means the object is still live.
ALTER TABLE submissions ADD COLUMN storage_cleaned_at TIMESTAMPTZ;

-- Partial index speeds up the "find candidates" query in the cleanup
-- sweeper, which only ever reads rows still pointing at live objects.
CREATE INDEX submissions_uncleaned_idx
    ON submissions (tile_id)
    WHERE storage_cleaned_at IS NULL;

-- +goose Down
DROP INDEX IF EXISTS submissions_uncleaned_idx;
ALTER TABLE submissions DROP COLUMN IF EXISTS storage_cleaned_at;
