-- +goose Up

-- Periods: one per challenge cycle, linked to a daily image.
CREATE TABLE periods (
    id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    daily_image_id UUID        NOT NULL REFERENCES daily_images(id),
    game_type      TEXT        NOT NULL DEFAULT 'photo',
    status         TEXT        NOT NULL DEFAULT 'active',
    phase          INT         NOT NULL DEFAULT 1,
    started_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    ended_at       TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX periods_one_active_idx ON periods (status) WHERE status = 'active';

-- Grid configs: defines tile layout per phase. Stored in DB, never hardcoded.
CREATE TABLE grid_configs (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    phase      INT         NOT NULL UNIQUE,
    columns    INT         NOT NULL,
    rows       INT         NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO grid_configs (phase, columns, rows) VALUES
    (1, 3, 3),
    (2, 6, 6),
    (3, 10, 10);

-- Tiles: individual cells within a period+phase.
CREATE TABLE tiles (
    id        UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    period_id UUID        NOT NULL REFERENCES periods(id),
    phase     INT         NOT NULL,
    row_index INT         NOT NULL,
    col_index INT         NOT NULL,
    status    TEXT        NOT NULL DEFAULT 'free',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (period_id, phase, row_index, col_index)
);

CREATE INDEX tiles_period_phase_idx ON tiles (period_id, phase);

-- +goose Down
DROP TABLE IF EXISTS tiles;
DROP TABLE IF EXISTS grid_configs;
DROP TABLE IF EXISTS periods;
