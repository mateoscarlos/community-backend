-- +goose Up

-- Replace sweep function to return freed tile IDs (for SSE notifications).
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

-- +goose Down

-- Restore original sweep function returning INT.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION sweep_expired_claims() RETURNS INT LANGUAGE plpgsql AS $$
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
