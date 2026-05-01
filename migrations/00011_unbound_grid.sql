-- +goose Up

-- Remove the bulk-seeded phase 4..99 rows. The grid module now computes
-- dimensions from a formula for any phase beyond what's stored in this table,
-- so there's no upper bound — the masterpiece keeps subdividing.
DELETE FROM grid_configs WHERE phase >= 4;

-- +goose Down
INSERT INTO grid_configs (phase, columns, rows)
SELECT g.phase, 10, 10
FROM generate_series(4, 99) AS g(phase)
ON CONFLICT (phase) DO NOTHING;
