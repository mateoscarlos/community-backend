-- +goose Up

-- New model: tiles are seeded once for the full final grid at period start.
-- Each tile carries the phase at which it *unlocks* (the smallest phase whose
-- window covers it) instead of "which phase iteration created it". This lets
-- drawings freeze in place across phase advances — advancing a phase just
-- flips phase_locked on the newly-covered ring, no tile rows are created or
-- destroyed. See docs/game-model-rework.md §M1.3–M1.4.

-- Drop the old (period_id, phase, row_index, col_index) uniqueness — there
-- used to be one row per position *per phase*. Now there's one row per
-- position for the whole period lifetime.
ALTER TABLE tiles DROP CONSTRAINT IF EXISTS tiles_period_id_phase_row_index_col_index_key;

-- The old covering index on (period_id, phase) was there to speed up per-phase
-- fetches. Post-rework we query by period only, so the constraint below covers
-- our access pattern.
DROP INDEX IF EXISTS tiles_period_phase_idx;

-- One row per (period, row, col) for the period's lifetime.
ALTER TABLE tiles ADD CONSTRAINT tiles_period_position_key
    UNIQUE (period_id, row_index, col_index);

-- Future-locked tiles are seeded at period start but not yet playable. Phase
-- advance flips this to FALSE for the tiles in the newly-unlocked ring.
-- Default FALSE so any legacy rows that were manually created (dev only)
-- stay playable rather than silently disappearing from the grid.
ALTER TABLE tiles ADD COLUMN phase_locked BOOLEAN NOT NULL DEFAULT FALSE;

-- +goose Down

ALTER TABLE tiles DROP COLUMN IF EXISTS phase_locked;
ALTER TABLE tiles DROP CONSTRAINT IF EXISTS tiles_period_position_key;
CREATE INDEX tiles_period_phase_idx ON tiles (period_id, phase);
ALTER TABLE tiles ADD CONSTRAINT tiles_period_id_phase_row_index_col_index_key
    UNIQUE (period_id, phase, row_index, col_index);
