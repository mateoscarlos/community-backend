-- +goose Up

-- Generic key/value store for runtime-tunable settings the team can change
-- from the admin panel without a redeploy (e.g. how long a period runs).
CREATE TABLE app_settings (
    key        TEXT        PRIMARY KEY,
    value      TEXT        NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS app_settings;
