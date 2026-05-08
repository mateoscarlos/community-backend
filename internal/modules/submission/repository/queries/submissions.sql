-- name: GetSubmissionByTileID :one
SELECT id, tile_id, claim_id, storage_key, crop_x, crop_y, crop_width,
       crop_height, created_at, storage_cleaned_at
FROM submissions
WHERE tile_id = $1;

-- name: ListSubmissionsByPeriodAndPhase :many
SELECT s.id, s.tile_id, s.claim_id, s.storage_key,
       s.crop_x, s.crop_y, s.crop_width, s.crop_height,
       s.created_at, s.storage_cleaned_at
FROM submissions s
JOIN tiles t ON t.id = s.tile_id
WHERE t.period_id = $1 AND t.phase = $2;

-- name: ListCleanupCandidates :many
-- Submissions whose phase has been composed into a mosaic and whose backing
-- R2/MinIO object hasn't been pruned yet. The sweeper deletes these objects
-- and stamps storage_cleaned_at; the row stays for audit.
SELECT s.id, s.storage_key
FROM submissions s
JOIN tiles t           ON t.id = s.tile_id
JOIN period_mosaics pm ON pm.period_id = t.period_id AND pm.phase = t.phase
WHERE s.storage_cleaned_at IS NULL
LIMIT $1;

-- name: MarkSubmissionsCleaned :exec
UPDATE submissions
SET storage_cleaned_at = now()
WHERE id = ANY(@ids::uuid[]);
