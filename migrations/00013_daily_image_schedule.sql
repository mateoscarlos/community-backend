-- +goose Up

-- Pre-uploaded images keyed by Copenhagen calendar date. Admin uploads images
-- into this table; at midnight Cph the period sweeper looks up the row for
-- the new date, promotes it via daily_images.SetActive, and spawns a new
-- active period — fully unattended daily rotation.
CREATE TABLE daily_image_schedule (
    date        DATE         PRIMARY KEY,
    storage_key TEXT         NOT NULL,
    width       INT          NOT NULL,
    height      INT          NOT NULL,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX daily_image_schedule_date_idx ON daily_image_schedule (date);

-- +goose Down
DROP TABLE IF EXISTS daily_image_schedule;
