-- +goose Up

-- Allow periods to belong to a non-photo game (prompt-based, where the user
-- draws from a text prompt instead of copying a daily image).
ALTER TABLE periods ALTER COLUMN daily_image_id DROP NOT NULL;
ALTER TABLE periods ADD COLUMN prompt TEXT;

-- Each game runs in parallel — one active period per game_type, not one total.
DROP INDEX IF EXISTS periods_one_active_idx;
CREATE UNIQUE INDEX periods_one_active_per_game_idx
    ON periods (status, game_type) WHERE status = 'active';

-- A photo period must have an image; a prompt period must have a prompt.
ALTER TABLE periods ADD CONSTRAINT periods_payload_check CHECK (
    (game_type = 'photo'  AND daily_image_id IS NOT NULL AND prompt IS NULL) OR
    (game_type = 'prompt' AND daily_image_id IS NULL     AND prompt IS NOT NULL)
);

-- +goose Down
ALTER TABLE periods DROP CONSTRAINT IF EXISTS periods_payload_check;
DROP INDEX IF EXISTS periods_one_active_per_game_idx;
CREATE UNIQUE INDEX periods_one_active_idx ON periods (status) WHERE status = 'active';
ALTER TABLE periods DROP COLUMN IF EXISTS prompt;
ALTER TABLE periods ALTER COLUMN daily_image_id SET NOT NULL;
