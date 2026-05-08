-- name: GetActivePeriod :one
SELECT p.id, p.daily_image_id, p.game_type, p.status, p.phase,
       p.started_at, p.ended_at, p.created_at, p.updated_at,
       p.final_image_key, p.composed_at, p.prompt
FROM periods p
WHERE p.status = 'active' AND p.game_type = $1
LIMIT 1;

-- name: GetPeriodByID :one
SELECT id, daily_image_id, game_type, status, phase,
       started_at, ended_at, created_at, updated_at,
       final_image_key, composed_at, prompt
FROM periods
WHERE id = $1;

-- name: GetPeriodByTileID :one
SELECT p.id, p.daily_image_id, p.game_type, p.status, p.phase,
       p.started_at, p.ended_at, p.created_at, p.updated_at,
       p.final_image_key, p.composed_at, p.prompt
FROM periods p
JOIN tiles t ON t.period_id = p.id
WHERE t.id = $1
LIMIT 1;

-- name: ListCompletedPeriods :many
-- Lists periods of a single game (photo or prompt) that have a composed
-- mosaic. Includes both completed and still-active periods, so today's
-- in-progress mosaic appears in the archive.
SELECT id, daily_image_id, game_type, status, phase,
       started_at, ended_at, created_at, updated_at,
       final_image_key, composed_at, prompt
FROM periods
WHERE final_image_key IS NOT NULL
  AND game_type = $1
ORDER BY started_at DESC
LIMIT $2 OFFSET $3;

-- name: ListCompletedPeriodsMissingFinalImage :many
SELECT id, daily_image_id, game_type, status, phase,
       started_at, ended_at, created_at, updated_at,
       final_image_key, composed_at, prompt
FROM periods
WHERE status = 'completed' AND final_image_key IS NULL
ORDER BY ended_at DESC;

-- name: CreatePeriod :one
INSERT INTO periods (daily_image_id, game_type, status, phase, prompt)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

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

-- name: GetGridConfig :one
SELECT id, phase, columns, rows, created_at
FROM grid_configs
WHERE phase = $1;

-- name: GetTilesByPeriodAndPhase :many
SELECT t.id, t.period_id, t.phase, t.row_index, t.col_index, t.status,
       COALESCE(s.storage_key, '') AS submission_key,
       t.created_at, t.updated_at
FROM tiles t
LEFT JOIN submissions s ON s.tile_id = t.id
WHERE t.period_id = $1 AND t.phase = $2
ORDER BY t.row_index, t.col_index;

-- name: CreateTile :one
INSERT INTO tiles (period_id, phase, row_index, col_index, status)
VALUES ($1, $2, $3, $4, 'free')
RETURNING *;

-- name: UpdateTileStatus :exec
UPDATE tiles SET status = $2, updated_at = now() WHERE id = $1;

-- name: CountTilesByStatus :one
SELECT
    count(*) FILTER (WHERE status = 'drawn') AS drawn_count,
    count(*) AS total_count
FROM tiles
WHERE period_id = $1 AND phase = $2;
