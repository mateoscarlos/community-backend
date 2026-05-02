-- name: GetClaimByTileID :one
SELECT id, tile_id, nickname, session_id, claimed_at, expires_at, released_at, created_at, last_heartbeat_at
FROM claims
WHERE tile_id = $1 AND released_at IS NULL;

-- name: GetActiveClaimBySessionAndTile :one
SELECT id, tile_id, nickname, session_id, claimed_at, expires_at, released_at, created_at, last_heartbeat_at
FROM claims
WHERE tile_id = $1 AND session_id = $2 AND released_at IS NULL;
