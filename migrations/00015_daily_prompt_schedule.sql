-- +goose Up

-- Prompts queued up by date for the prompt-based parallel game. The period
-- sweeper promotes the row whose date matches today (Cph) and creates an
-- active prompt period from it. Mirrors daily_image_schedule.
CREATE TABLE daily_prompt_schedule (
    date       DATE         PRIMARY KEY,
    prompt     TEXT         NOT NULL,
    created_at TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX daily_prompt_schedule_date_idx ON daily_prompt_schedule (date);

-- +goose Down
DROP TABLE IF EXISTS daily_prompt_schedule;
