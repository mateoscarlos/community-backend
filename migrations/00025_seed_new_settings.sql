-- +goose Up

-- Runtime knobs for the concentric-ring rework. Admins can change either
-- from the admin panel (M5) without a redeploy; the grid service reads them
-- via community-backend/internal/shared/appsettings.

-- phase_grid_sizes: JSON array of odd ints, strictly increasing, first ≥ 3.
-- Default [3,5,7,9] gives four phases from 3×3 (phase 1 center) to a 9×9
-- final grid.
INSERT INTO app_settings (key, value)
VALUES ('phase_grid_sizes', '[3,5,7,9]')
ON CONFLICT (key) DO NOTHING;

-- outer_tile_display: how the frontend renders tiles whose phase >
-- period.phase. "blocked" shows them dark and inert; "hidden" omits them,
-- so the viewport is only the current unlocked window.
INSERT INTO app_settings (key, value)
VALUES ('outer_tile_display', 'blocked')
ON CONFLICT (key) DO NOTHING;

-- +goose Down

DELETE FROM app_settings WHERE key IN ('phase_grid_sizes', 'outer_tile_display');
