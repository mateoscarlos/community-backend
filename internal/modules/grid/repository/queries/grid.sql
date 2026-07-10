-- name: GetActivePeriod :one
-- One active period at a time (see migrations/00022_drop_prompt_mode.sql —
-- the unique index enforces this).
SELECT id, daily_image_id, game_type, status, phase, final_grid_size,
       started_at, ended_at, created_at, updated_at,
       final_image_key, composed_at
FROM periods
WHERE status = 'active'
LIMIT 1;

-- name: GetLatestCompletedPeriod :one
-- The most recently ended period. Used by the resting "come back tomorrow"
-- view when no active period exists (masterpiece just finished, next
-- rotation not yet triggered). Newest first so the frontend shows the last
-- masterpiece, not an older archived one.
SELECT id, daily_image_id, game_type, status, phase, final_grid_size,
       started_at, ended_at, created_at, updated_at,
       final_image_key, composed_at
FROM periods
WHERE status = 'completed'
ORDER BY ended_at DESC NULLS LAST, started_at DESC
LIMIT 1;

-- name: GetPeriodByID :one
SELECT id, daily_image_id, game_type, status, phase, final_grid_size,
       started_at, ended_at, created_at, updated_at,
       final_image_key, composed_at
FROM periods
WHERE id = $1;

-- name: GetPeriodByTileID :one
SELECT p.id, p.daily_image_id, p.game_type, p.status, p.phase, p.final_grid_size,
       p.started_at, p.ended_at, p.created_at, p.updated_at,
       p.final_image_key, p.composed_at
FROM periods p
JOIN tiles t ON t.period_id = p.id
WHERE t.id = $1
LIMIT 1;

-- name: ListCompletedPeriods :many
-- Lists periods that have a composed mosaic. Includes both completed and
-- still-active periods, so today's in-progress mosaic appears in the archive.
SELECT id, daily_image_id, game_type, status, phase, final_grid_size,
       started_at, ended_at, created_at, updated_at,
       final_image_key, composed_at
FROM periods
WHERE final_image_key IS NOT NULL
ORDER BY started_at DESC
LIMIT $1 OFFSET $2;

-- name: ListCompletedPeriodsMissingFinalImage :many
SELECT id, daily_image_id, game_type, status, phase, final_grid_size,
       started_at, ended_at, created_at, updated_at,
       final_image_key, composed_at
FROM periods
WHERE status = 'completed' AND final_image_key IS NULL
ORDER BY ended_at DESC;

-- name: CreatePeriod :one
-- game_type is pinned to 'photo' at the DB level (see migrations/00022) —
-- the column is retained for archive compatibility but no longer written from
-- the app.
INSERT INTO periods (daily_image_id, status, phase, final_grid_size)
VALUES ($1, $2, $3, $4)
RETURNING id, daily_image_id, game_type, status, phase, final_grid_size,
          started_at, ended_at, created_at, updated_at,
          final_image_key, composed_at;

-- name: UpdatePeriodPhase :exec
UPDATE periods SET phase = $2, updated_at = now() WHERE id = $1;

-- name: CompletePeriod :exec
UPDATE periods SET status = 'completed', ended_at = now(), updated_at = now() WHERE id = $1;

-- name: SetPeriodFinalImage :exec
UPDATE periods
SET final_image_key = $2, composed_at = now(), updated_at = now()
WHERE id = $1;

-- name: UpsertPeriodMosaic :exec
INSERT INTO period_mosaics (period_id, phase, storage_key)
VALUES ($1, $2, $3)
ON CONFLICT (period_id, phase) DO UPDATE
SET storage_key = EXCLUDED.storage_key,
    composed_at = now();

-- name: ListMosaicsByPeriod :many
SELECT period_id, phase, storage_key, composed_at
FROM period_mosaics
WHERE period_id = $1
ORDER BY phase ASC;

-- name: GetTilesByPeriod :many
-- All tiles for a period. In the concentric-ring model, tiles are seeded once
-- at period start for the full final_grid_size × final_grid_size grid; there
-- is no per-phase re-seeding. Callers use `phase` + `phase_locked` to decide
-- what to render.
SELECT t.id, t.period_id, t.phase, t.row_index, t.col_index, t.status,
       t.phase_locked,
       COALESCE(s.storage_key, '') AS submission_key,
       t.created_at, t.updated_at
FROM tiles t
LEFT JOIN submissions s ON s.tile_id = t.id
WHERE t.period_id = $1
ORDER BY t.row_index, t.col_index;

-- name: CreateTile :one
-- phase_locked defaults to FALSE; phase-1 tiles are always unlocked. The
-- caller passes TRUE for any tile whose ring hasn't unlocked yet.
INSERT INTO tiles (period_id, phase, row_index, col_index, status, phase_locked)
VALUES ($1, $2, $3, $4, 'free', $5)
RETURNING id, period_id, phase, row_index, col_index, status, phase_locked, created_at, updated_at;

-- name: UpdateTileStatus :exec
UPDATE tiles SET status = $2, updated_at = now() WHERE id = $1;

-- name: UnlockPhaseRing :exec
-- Flips phase_locked to FALSE for every tile at the given phase. Called on
-- phase advance: the tiles in the newly-unlocked ring become playable while
-- their drawings (from earlier phases) remain untouched.
UPDATE tiles
SET phase_locked = FALSE, updated_at = now()
WHERE period_id = $1 AND phase = $2 AND phase_locked = TRUE;

-- name: CountTilesUpToPhase :one
-- Counts drawn vs total for the currently-playable window (phase <= max_phase
-- AND phase_locked = FALSE). Used by phase-completion checks: when
-- drawn_count = total_count, the current ring is done.
SELECT
    count(*) FILTER (WHERE status = 'drawn') AS drawn_count,
    count(*) AS total_count
FROM tiles
WHERE period_id = $1 AND phase <= $2 AND phase_locked = FALSE;
