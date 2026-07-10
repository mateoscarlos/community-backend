-- +goose Up

-- Prompt periods are gone. The check constraint that gated payload shape by
-- game_type has to come off before we can reshape the active-period index.
ALTER TABLE periods DROP CONSTRAINT IF EXISTS periods_payload_check;

-- Close every active period so the single-active unique index below can be
-- applied cleanly on databases that were live during the two-parallel-games
-- era (one active photo period + one active prompt period was normal). The
-- next rotation seeds a fresh active period under the new model. Completed
-- periods stay in the DB so the Museum still lists them; their composed
-- mosaics on period_mosaics are the archive artefact, not the raw tile rows.
UPDATE periods
   SET status = 'completed',
       ended_at = COALESCE(ended_at, now()),
       updated_at = now()
 WHERE status = 'active';

-- One active period at a time again (used to be one per game_type).
DROP INDEX IF EXISTS periods_one_active_per_game_idx;
CREATE UNIQUE INDEX periods_one_active_idx ON periods (status) WHERE status = 'active';

-- Prompt-only column drops entirely. Legacy prompt periods keep their
-- game_type='prompt' for archive listing, but the prompt text itself is lost
-- — the archive UI only reads mosaics/dates, so this is acceptable.
ALTER TABLE periods DROP COLUMN IF EXISTS prompt;

-- Keep daily_image_id nullable so the (potentially many) legacy prompt
-- periods with NULL image can stay in place; add a CHECK that guarantees
-- every new active period anchors to a daily image. See the M1.5 note in
-- docs/game-model-rework.md.
ALTER TABLE periods ADD CONSTRAINT periods_active_has_image
    CHECK (status != 'active' OR daily_image_id IS NOT NULL);

-- Keep the game_type column for archive compatibility but pin it to 'photo'
-- for new inserts.
ALTER TABLE periods ALTER COLUMN game_type SET DEFAULT 'photo';

-- +goose Down

ALTER TABLE periods ALTER COLUMN game_type DROP DEFAULT;
ALTER TABLE periods DROP CONSTRAINT IF EXISTS periods_active_has_image;
ALTER TABLE periods ADD COLUMN prompt TEXT;

DROP INDEX IF EXISTS periods_one_active_idx;
CREATE UNIQUE INDEX periods_one_active_per_game_idx
    ON periods (status, game_type) WHERE status = 'active';

ALTER TABLE periods ADD CONSTRAINT periods_payload_check CHECK (
    (game_type = 'photo'  AND daily_image_id IS NOT NULL AND prompt IS NULL) OR
    (game_type = 'prompt' AND daily_image_id IS NULL     AND prompt IS NOT NULL)
);
