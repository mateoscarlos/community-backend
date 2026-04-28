-- +goose Up

CREATE TABLE claims (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tile_id     UUID        NOT NULL REFERENCES tiles(id),
    nickname    TEXT        NOT NULL,
    session_id  TEXT        NOT NULL,
    claimed_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at  TIMESTAMPTZ NOT NULL,
    released_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Only one active (non-released) claim per tile at a time.
CREATE UNIQUE INDEX claims_one_active_per_tile ON claims (tile_id) WHERE released_at IS NULL;

CREATE INDEX claims_expires_idx ON claims (expires_at) WHERE released_at IS NULL;
CREATE INDEX claims_session_idx ON claims (session_id);

-- +goose StatementBegin
CREATE FUNCTION claim_tile(
    p_tile_id   UUID,
    p_nickname  TEXT,
    p_session   TEXT,
    p_expires   TIMESTAMPTZ
) RETURNS TABLE (
    claim_id    UUID,
    tile_id     UUID,
    nickname    TEXT,
    session_id  TEXT,
    claimed_at  TIMESTAMPTZ,
    expires_at  TIMESTAMPTZ
) LANGUAGE plpgsql AS $$
DECLARE
    v_status TEXT;
    v_claim  claims%ROWTYPE;
BEGIN
    -- Lock the tile row. Any concurrent claim attempt blocks here.
    SELECT t.status INTO v_status
    FROM tiles t
    WHERE t.id = p_tile_id
    FOR UPDATE;

    IF NOT FOUND THEN
        RAISE EXCEPTION 'tile_not_found';
    END IF;

    IF v_status <> 'free' THEN
        RAISE EXCEPTION 'tile_not_free';
    END IF;

    INSERT INTO claims (tile_id, nickname, session_id, expires_at)
    VALUES (p_tile_id, p_nickname, p_session, p_expires)
    RETURNING * INTO v_claim;

    UPDATE tiles SET status = 'locked', updated_at = now()
    WHERE id = p_tile_id;

    RETURN QUERY SELECT v_claim.id, v_claim.tile_id, v_claim.nickname,
                        v_claim.session_id, v_claim.claimed_at, v_claim.expires_at;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION release_claim(
    p_tile_id   UUID,
    p_session   TEXT
) RETURNS BOOLEAN LANGUAGE plpgsql AS $$
DECLARE
    v_claim_id UUID;
BEGIN
    PERFORM 1 FROM tiles WHERE id = p_tile_id FOR UPDATE;

    SELECT id INTO v_claim_id
    FROM claims
    WHERE tile_id = p_tile_id AND session_id = p_session AND released_at IS NULL;

    IF NOT FOUND THEN
        RETURN FALSE;
    END IF;

    UPDATE claims SET released_at = now() WHERE id = v_claim_id;
    UPDATE tiles SET status = 'free', updated_at = now() WHERE id = p_tile_id;

    RETURN TRUE;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION sweep_expired_claims() RETURNS INT LANGUAGE plpgsql AS $$
DECLARE
    v_count INT;
BEGIN
    WITH expired AS (
        UPDATE claims SET released_at = now()
        WHERE released_at IS NULL AND expires_at < now()
        RETURNING tile_id
    )
    UPDATE tiles SET status = 'free', updated_at = now()
    WHERE id IN (SELECT tile_id FROM expired)
      AND status = 'locked';

    GET DIAGNOSTICS v_count = ROW_COUNT;
    RETURN v_count;
END;
$$;
-- +goose StatementEnd

-- +goose Down
DROP FUNCTION IF EXISTS sweep_expired_claims();
DROP FUNCTION IF EXISTS release_claim(UUID, TEXT);
DROP FUNCTION IF EXISTS claim_tile(UUID, TEXT, TEXT, TIMESTAMPTZ);
DROP TABLE IF EXISTS claims;
