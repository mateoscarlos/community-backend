-- name: GetActiveDailyImage :one
SELECT id, date, storage_key, width, height, is_active, created_at, updated_at
FROM daily_images
WHERE is_active = true
LIMIT 1;

-- name: GetDailyImageByDate :one
SELECT id, date, storage_key, width, height, is_active, created_at, updated_at
FROM daily_images
WHERE date = $1;

-- name: DeactivateAllDailyImages :exec
UPDATE daily_images SET is_active = false, updated_at = now()
WHERE is_active = true;

-- name: UpsertDailyImage :one
INSERT INTO daily_images (date, storage_key, width, height, is_active)
VALUES ($1, $2, $3, $4, true)
ON CONFLICT (date) DO UPDATE
    SET storage_key = EXCLUDED.storage_key,
        width       = EXCLUDED.width,
        height      = EXCLUDED.height,
        is_active   = true,
        updated_at  = now()
RETURNING *;
