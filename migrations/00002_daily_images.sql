-- +goose Up
CREATE TABLE IF NOT EXISTS daily_images (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    date        DATE        NOT NULL UNIQUE,
    storage_key TEXT        NOT NULL,
    width       INT         NOT NULL,
    height      INT         NOT NULL,
    is_active   BOOLEAN     NOT NULL DEFAULT false,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Enforce at most one active image at a time at the DB level.
CREATE UNIQUE INDEX IF NOT EXISTS daily_images_one_active_idx
    ON daily_images (is_active)
    WHERE is_active = true;

-- +goose Down
DROP TABLE IF EXISTS daily_images;
