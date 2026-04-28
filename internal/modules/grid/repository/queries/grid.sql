-- name: GetActivePeriod :one
SELECT p.id, p.daily_image_id, p.game_type, p.status, p.phase,
       p.started_at, p.ended_at, p.created_at, p.updated_at
FROM periods p
WHERE p.status = 'active'
LIMIT 1;

-- name: GetPeriodByID :one
SELECT id, daily_image_id, game_type, status, phase,
       started_at, ended_at, created_at, updated_at
FROM periods
WHERE id = $1;

-- name: ListCompletedPeriods :many
SELECT id, daily_image_id, game_type, status, phase,
       started_at, ended_at, created_at, updated_at
FROM periods
WHERE status = 'completed'
ORDER BY ended_at DESC
LIMIT $1 OFFSET $2;

-- name: CreatePeriod :one
INSERT INTO periods (daily_image_id, game_type, status, phase)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: UpdatePeriodPhase :exec
UPDATE periods SET phase = $2, updated_at = now() WHERE id = $1;

-- name: CompletePeriod :exec
UPDATE periods SET status = 'completed', ended_at = now(), updated_at = now() WHERE id = $1;

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
