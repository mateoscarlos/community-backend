-- +goose Up

CREATE TABLE submissions (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tile_id     UUID        NOT NULL REFERENCES tiles(id) UNIQUE,
    claim_id    UUID        NOT NULL REFERENCES claims(id),
    storage_key TEXT        NOT NULL,
    crop_x      DOUBLE PRECISION NOT NULL DEFAULT 0,
    crop_y      DOUBLE PRECISION NOT NULL DEFAULT 0,
    crop_width  DOUBLE PRECISION NOT NULL DEFAULT 1,
    crop_height DOUBLE PRECISION NOT NULL DEFAULT 1,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose StatementBegin
CREATE FUNCTION submit_tile(
    p_tile_id     UUID,
    p_session     TEXT,
    p_storage_key TEXT,
    p_crop_x      DOUBLE PRECISION,
    p_crop_y      DOUBLE PRECISION,
    p_crop_w      DOUBLE PRECISION,
    p_crop_h      DOUBLE PRECISION
) RETURNS TABLE (
    submission_id UUID,
    tile_id       UUID,
    claim_id      UUID,
    storage_key   TEXT,
    created_at    TIMESTAMPTZ
) LANGUAGE plpgsql AS $$
DECLARE
    v_status   TEXT;
    v_claim_id UUID;
    v_sub      submissions%ROWTYPE;
BEGIN
    SELECT t.status INTO v_status
    FROM tiles t WHERE t.id = p_tile_id FOR UPDATE;

    IF NOT FOUND THEN
        RAISE EXCEPTION 'tile_not_found';
    END IF;
    IF v_status <> 'locked' THEN
        RAISE EXCEPTION 'tile_not_locked';
    END IF;

    SELECT c.id INTO v_claim_id
    FROM claims c
    WHERE c.tile_id = p_tile_id AND c.session_id = p_session AND c.released_at IS NULL;

    IF NOT FOUND THEN
        RAISE EXCEPTION 'not_your_claim';
    END IF;

    INSERT INTO submissions (tile_id, claim_id, storage_key, crop_x, crop_y, crop_width, crop_height)
    VALUES (p_tile_id, v_claim_id, p_storage_key, p_crop_x, p_crop_y, p_crop_w, p_crop_h)
    RETURNING * INTO v_sub;

    UPDATE tiles SET status = 'drawn', updated_at = now() WHERE id = p_tile_id;
    UPDATE claims SET released_at = now() WHERE id = v_claim_id;

    RETURN QUERY SELECT v_sub.id, v_sub.tile_id, v_sub.claim_id,
                        v_sub.storage_key, v_sub.created_at;
END;
$$;
-- +goose StatementEnd

-- +goose Down
DROP FUNCTION IF EXISTS submit_tile(UUID, TEXT, TEXT, DOUBLE PRECISION, DOUBLE PRECISION, DOUBLE PRECISION, DOUBLE PRECISION);
DROP TABLE IF EXISTS submissions;
