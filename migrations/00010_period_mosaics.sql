-- +goose Up

-- One mosaic JPEG per (period, phase). Lets us show every "draw" of the day
-- on the archive detail page; periods.final_image_key still points at the
-- latest one for the calendar preview.
CREATE TABLE period_mosaics (
    period_id   UUID        NOT NULL REFERENCES periods(id) ON DELETE CASCADE,
    phase       INT         NOT NULL,
    storage_key TEXT        NOT NULL,
    composed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (period_id, phase)
);

CREATE INDEX period_mosaics_period_idx ON period_mosaics (period_id, phase DESC);

-- +goose Down
DROP TABLE IF EXISTS period_mosaics;
