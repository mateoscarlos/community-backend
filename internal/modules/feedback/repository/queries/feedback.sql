-- name: CreateFeedback :one
INSERT INTO feedback (message, contact)
VALUES ($1, $2)
RETURNING *;

-- name: ListFeedback :many
SELECT id, message, contact, created_at
FROM feedback
ORDER BY created_at DESC
LIMIT $1 OFFSET $2;
