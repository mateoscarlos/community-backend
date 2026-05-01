-- +goose Up

-- Final composed mosaic image, generated when the period transitions to completed.
ALTER TABLE periods
    ADD COLUMN final_image_key TEXT,
    ADD COLUMN composed_at     TIMESTAMPTZ;

-- +goose Down
ALTER TABLE periods
    DROP COLUMN IF EXISTS composed_at,
    DROP COLUMN IF EXISTS final_image_key;
