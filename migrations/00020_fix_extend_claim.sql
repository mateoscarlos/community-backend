-- +goose Up

-- Fix ambiguous column reference in extend_claim: the local PL/pgSQL return-table
-- column "expires_at" clashed with the table column "claims.expires_at" in the
-- SET clause, causing ERROR 42702 on every heartbeat extension.
DROP FUNCTION IF EXISTS extend_claim(UUID, TEXT, INT, INT);

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
    SET expires_at        = LEAST(claims.expires_at + (p_extra_seconds || ' seconds')::interval,
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
