-- +goose Up

-- Seed grid_configs for phases 4..99 so a period can keep advancing during a
-- 24h window. Phases 4+ all use 10x10 (same fidelity as phase 3) — each new
-- phase is another round of redrawing the same picture, replacing the prior
-- mosaic. Keeps tile counts sane.
INSERT INTO grid_configs (phase, columns, rows)
SELECT g.phase, 10, 10
FROM generate_series(4, 99) AS g(phase)
ON CONFLICT (phase) DO NOTHING;

-- +goose Down
DELETE FROM grid_configs WHERE phase BETWEEN 4 AND 99;
