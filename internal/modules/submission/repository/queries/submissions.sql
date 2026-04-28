-- name: GetSubmissionByTileID :one
SELECT id, tile_id, claim_id, storage_key, crop_x, crop_y, crop_width, crop_height, created_at
FROM submissions
WHERE tile_id = $1;

-- name: ListSubmissionsByPeriodAndPhase :many
SELECT s.id, s.tile_id, s.claim_id, s.storage_key,
       s.crop_x, s.crop_y, s.crop_width, s.crop_height, s.created_at
FROM submissions s
JOIN tiles t ON t.id = s.tile_id
WHERE t.period_id = $1 AND t.phase = $2;
