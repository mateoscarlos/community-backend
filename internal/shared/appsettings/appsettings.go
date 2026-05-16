// Package appsettings is a tiny raw-SQL accessor for the app_settings
// key/value table — runtime-tunable knobs the team changes from the admin
// panel (no redeploy). Intentionally not sqlc: it's two trivial queries and
// keeps the migration/codegen surface small.
package appsettings

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"time"
)

// KeyPeriodDurationHours controls how long a period runs before the sweeper
// closes it and composes the final mosaic. Unset → legacy daily cutoff.
const KeyPeriodDurationHours = "period_duration_hours"

// Get returns the raw string value for a key, ok=false if it's not set.
func Get(ctx context.Context, db *sql.DB, key string) (string, bool, error) {
	var v string
	err := db.QueryRowContext(ctx,
		`SELECT value FROM app_settings WHERE key = $1`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}

// Set upserts a key/value.
func Set(ctx context.Context, db *sql.DB, key, value string) error {
	_, err := db.ExecContext(ctx,
		`INSERT INTO app_settings (key, value, updated_at)
		 VALUES ($1, $2, now())
		 ON CONFLICT (key) DO UPDATE
		   SET value = EXCLUDED.value, updated_at = now()`,
		key, value)
	return err
}

// PeriodDuration returns the configured period length and ok=true when a
// positive value is set. ok=false means callers should fall back to the
// legacy daily (midnight) cutoff.
func PeriodDuration(ctx context.Context, db *sql.DB) (time.Duration, bool) {
	raw, found, err := Get(ctx, db, KeyPeriodDurationHours)
	if err != nil || !found {
		return 0, false
	}
	hours, err := strconv.ParseFloat(raw, 64)
	if err != nil || hours <= 0 {
		return 0, false
	}
	return time.Duration(hours * float64(time.Hour)), true
}

// SetPeriodDurationHours stores the period length in hours (>0).
func SetPeriodDurationHours(ctx context.Context, db *sql.DB, hours float64) error {
	return Set(ctx, db, KeyPeriodDurationHours,
		strconv.FormatFloat(hours, 'f', -1, 64))
}
