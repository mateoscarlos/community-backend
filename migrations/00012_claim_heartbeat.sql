-- +goose Up

-- Heartbeat tracking: a claim is considered "stale" if the user hasn't pinged
-- the heartbeat endpoint recently. The sweep frees both expired (past TTL)
-- and stale claims so a user closing their tab releases the tile within ~90s.
ALTER TABLE claims ADD COLUMN last_heartbeat_at TIMESTAMPTZ NOT NULL DEFAULT now();
CREATE INDEX claims_heartbeat_idx ON claims (last_heartbeat_at) WHERE released_at IS NULL;

-- Replace the sweep to release on either expiry OR heartbeat staleness.
DROP FUNCTION IF EXISTS sweep_expired_claims();

-- +goose StatementBegin
CREATE FUNCTION sweep_expired_claims() RETURNS SETOF UUID LANGUAGE plpgsql AS $$
BEGIN
    RETURN QUERY
    WITH expired AS (
        UPDATE claims SET released_at = now()
        WHERE released_at IS NULL
          AND (
              expires_at < now()
              OR last_heartbeat_at < now() - interval '90 seconds'
          )
        RETURNING tile_id
    )
    UPDATE tiles SET status = 'free', updated_at = now()
    WHERE id IN (SELECT tile_id FROM expired)
      AND status = 'locked'
    RETURNING id;
END;
$$;
-- +goose StatementEnd

-- Heartbeat function: refreshes last_heartbeat_at if the caller actually owns
-- the claim and it isn't already released. Returns the row so the API can
-- echo back the (possibly extended) expires_at.
-- +goose StatementBegin
CREATE FUNCTION heartbeat_claim(
    p_tile_id UUID,
    p_session TEXT
) RETURNS TABLE (
    claim_id    UUID,
    expires_at  TIMESTAMPTZ
) LANGUAGE plpgsql AS $$
BEGIN
    RETURN QUERY
    UPDATE claims
    SET last_heartbeat_at = now()
    WHERE tile_id = p_tile_id
      AND session_id = p_session
      AND released_at IS NULL
    RETURNING id, claims.expires_at;
END;
$$;
-- +goose StatementEnd

-- Extend function: pushes expires_at forward by p_extra_seconds. Capped via
-- p_max_total_seconds so a user can't keep extending forever.
-- +goose StatementBegin
CREATE FUNCTION extend_claim(
    p_tile_id            UUID,
    p_session            TEXT,
    p_extra_seconds      INT,
    p_max_total_seconds  INT
) RETURNS TABLE (
    claim_id    UUID,
    expires_at  TIMESTAMPTZ
) LANGUAGE plpgsql AS $$
BEGIN
    RETURN QUERY
    UPDATE claims
    SET expires_at        = LEAST(expires_at + (p_extra_seconds || ' seconds')::interval,
                                  claimed_at + (p_max_total_seconds || ' seconds')::interval),
        last_heartbeat_at = now()
    WHERE tile_id = p_tile_id
      AND session_id = p_session
      AND released_at IS NULL
    RETURNING id, claims.expires_at;
END;
$$;
-- +goose StatementEnd

-- +goose Down

DROP FUNCTION IF EXISTS extend_claim(UUID, TEXT, INT, INT);
DROP FUNCTION IF EXISTS heartbeat_claim(UUID, TEXT);

DROP FUNCTION IF EXISTS sweep_expired_claims();

-- +goose StatementBegin
CREATE FUNCTION sweep_expired_claims() RETURNS SETOF UUID LANGUAGE plpgsql AS $$
BEGIN
    RETURN QUERY
    WITH expired AS (
        UPDATE claims SET released_at = now()
        WHERE released_at IS NULL AND expires_at < now()
        RETURNING tile_id
    )
    UPDATE tiles SET status = 'free', updated_at = now()
    WHERE id IN (SELECT tile_id FROM expired)
      AND status = 'locked'
    RETURNING id;
END;
$$;
-- +goose StatementEnd

DROP INDEX IF EXISTS claims_heartbeat_idx;
ALTER TABLE claims DROP COLUMN IF EXISTS last_heartbeat_at;
