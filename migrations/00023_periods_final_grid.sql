-- +goose Up

-- Every period now carries the side length of its fully-revealed grid.
-- Phase 1 unlocks the center 3×3, subsequent phases unlock concentric rings
-- outward until final_grid_size × final_grid_size tiles are all playable.
-- See docs/game-model-rework.md.
ALTER TABLE periods ADD COLUMN final_grid_size INT NOT NULL DEFAULT 9;

-- Sanity guard: an odd side length is required so the grid has a single-tile
-- geometric center to anchor the phase rings on. Enforced at write time in
-- the grid service too — the CHECK is here to catch out-of-band inserts.
ALTER TABLE periods ADD CONSTRAINT periods_final_grid_size_odd
    CHECK (final_grid_size >= 3 AND final_grid_size % 2 = 1);

-- +goose Down

ALTER TABLE periods DROP CONSTRAINT IF EXISTS periods_final_grid_size_odd;
ALTER TABLE periods DROP COLUMN IF EXISTS final_grid_size;
