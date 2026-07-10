-- +goose Up

-- New model: tiles are seeded once for the full final grid at period start,
-- one row per (period, row, col) instead of one row per (period, phase, row,
-- col). Existing tile rows carry the old per-phase semantics and won't fit
-- the new unique constraint (a single (row, col) has multiple rows, one per
-- phase). Wipe them along with their dependent claims/submissions — the
-- composed mosaics on period_mosaics are the archive artefact, not the raw
-- tile rows, so nothing user-visible is lost. The next period spawned by
-- the sweeper is seeded fresh under the new model.
DELETE FROM submissions WHERE tile_id IN (SELECT id FROM tiles);
DELETE FROM claims WHERE tile_id IN (SELECT id FROM tiles);
DELETE FROM tiles;

-- Drop the old (period_id, phase, row_index, col_index) uniqueness. One row
-- per position for the whole period lifetime now.
ALTER TABLE tiles DROP CONSTRAINT IF EXISTS tiles_period_id_phase_row_index_col_index_key;

-- The old covering index sped up per-phase fetches. Post-rework we query by
-- period only, so the constraint below covers our access pattern.
DROP INDEX IF EXISTS tiles_period_phase_idx;

-- One row per (period, row, col) for the period's lifetime.
ALTER TABLE tiles ADD CONSTRAINT tiles_period_position_key
    UNIQUE (period_id, row_index, col_index);

-- Future-locked tiles are seeded at period start but not yet playable. Phase
-- advance flips this to FALSE for the tiles in the newly-unlocked ring.
-- Default FALSE so any legacy rows that somehow survived (dev only) stay
-- playable rather than silently disappearing from the grid.
ALTER TABLE tiles ADD COLUMN phase_locked BOOLEAN NOT NULL DEFAULT FALSE;

-- +goose Down

ALTER TABLE tiles DROP COLUMN IF EXISTS phase_locked;
ALTER TABLE tiles DROP CONSTRAINT IF EXISTS tiles_period_position_key;
CREATE INDEX tiles_period_phase_idx ON tiles (period_id, phase);
ALTER TABLE tiles ADD CONSTRAINT tiles_period_id_phase_row_index_col_index_key
    UNIQUE (period_id, phase, row_index, col_index);
