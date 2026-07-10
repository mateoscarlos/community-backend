-- +goose Up

-- Prompt periods are gone. The check constraint that gated payload shape by
-- game_type has to come off before we can drop `prompt` and re-tighten the
-- daily_image_id nullability.
ALTER TABLE periods DROP CONSTRAINT IF EXISTS periods_payload_check;

-- One active period at a time again (used to be one per game_type).
DROP INDEX IF EXISTS periods_one_active_per_game_idx;
CREATE UNIQUE INDEX periods_one_active_idx ON periods (status) WHERE status = 'active';

-- Prompt-only column drops entirely. Fresh-DB rollout — no data to preserve
-- (see docs/game-model-rework.md §6).
ALTER TABLE periods DROP COLUMN IF EXISTS prompt;

-- Every period now anchors to a daily image again.
ALTER TABLE periods ALTER COLUMN daily_image_id SET NOT NULL;

-- Keep the game_type column for archive compatibility but pin it to 'photo'.
-- The frontend and API stop reading it after M2; leaving it here means we
-- don't have to rewrite the archive query patterns in this milestone.
ALTER TABLE periods ALTER COLUMN game_type SET DEFAULT 'photo';

-- +goose Down

ALTER TABLE periods ALTER COLUMN game_type DROP DEFAULT;
ALTER TABLE periods ALTER COLUMN daily_image_id DROP NOT NULL;
ALTER TABLE periods ADD COLUMN prompt TEXT;

DROP INDEX IF EXISTS periods_one_active_idx;
CREATE UNIQUE INDEX periods_one_active_per_game_idx
    ON periods (status, game_type) WHERE status = 'active';

ALTER TABLE periods ADD CONSTRAINT periods_payload_check CHECK (
    (game_type = 'photo'  AND daily_image_id IS NOT NULL AND prompt IS NULL) OR
    (game_type = 'prompt' AND daily_image_id IS NULL     AND prompt IS NOT NULL)
);
