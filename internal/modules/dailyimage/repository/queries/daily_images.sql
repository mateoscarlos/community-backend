-- name: GetActiveDailyImage :one
SELECT id, date, storage_key, width, height, is_active, created_at, updated_at
FROM daily_images
WHERE is_active = true
LIMIT 1;

-- name: GetDailyImageByDate :one
SELECT id, date, storage_key, width, height, is_active, created_at, updated_at
FROM daily_images
WHERE date = $1;

-- name: GetDailyImageByID :one
SELECT id, date, storage_key, width, height, is_active, created_at, updated_at
FROM daily_images
WHERE id = $1;

-- name: UpsertScheduledImage :one
INSERT INTO daily_image_schedule (date, storage_key, width, height)
VALUES ($1, $2, $3, $4)
ON CONFLICT (date) DO UPDATE
    SET storage_key = EXCLUDED.storage_key,
        width       = EXCLUDED.width,
        height      = EXCLUDED.height,
        updated_at  = now()
RETURNING date, storage_key, width, height, created_at, updated_at;

-- name: GetScheduledByDate :one
SELECT date, storage_key, width, height, created_at, updated_at
FROM daily_image_schedule
WHERE date = $1;

-- name: ListScheduleRange :many
SELECT date, storage_key, width, height, created_at, updated_at
FROM daily_image_schedule
WHERE date >= $1 AND date <= $2
ORDER BY date ASC;

-- name: DeleteScheduledByDate :exec
DELETE FROM daily_image_schedule WHERE date = $1;

-- --- Prompt schedule (parallel "draw from a prompt" game) ---

-- name: UpsertScheduledPrompt :one
INSERT INTO daily_prompt_schedule (date, prompt)
VALUES ($1, $2)
ON CONFLICT (date) DO UPDATE
    SET prompt     = EXCLUDED.prompt,
        updated_at = now()
RETURNING date, prompt, created_at, updated_at;

-- name: GetScheduledPromptByDate :one
SELECT date, prompt, created_at, updated_at
FROM daily_prompt_schedule
WHERE date = $1;

-- name: ListPromptScheduleRange :many
SELECT date, prompt, created_at, updated_at
FROM daily_prompt_schedule
WHERE date >= $1 AND date <= $2
ORDER BY date ASC;

-- name: DeleteScheduledPromptByDate :exec
DELETE FROM daily_prompt_schedule WHERE date = $1;

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
