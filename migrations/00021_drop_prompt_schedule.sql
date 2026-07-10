-- +goose Up

-- The prompt-based parallel game is being removed as part of the single-photo
-- concentric-ring rework (see docs/game-model-rework.md). Its schedule table
-- becomes dead weight — drop it before we simplify periods.
DROP INDEX IF EXISTS daily_prompt_schedule_date_idx;
DROP TABLE IF EXISTS daily_prompt_schedule;

-- +goose Down

CREATE TABLE daily_prompt_schedule (
    date       DATE         PRIMARY KEY,
    prompt     TEXT         NOT NULL,
    created_at TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX daily_prompt_schedule_date_idx ON daily_prompt_schedule (date);
