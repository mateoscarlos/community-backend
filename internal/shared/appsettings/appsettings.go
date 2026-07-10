// Package appsettings is a tiny raw-SQL accessor for the app_settings
// key/value table — runtime-tunable knobs the team changes from the admin
// panel (no redeploy). Intentionally not sqlc: it's two trivial queries and
// keeps the migration/codegen surface small.
package appsettings

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"
)

// KeyPeriodDurationHours controls how long a period runs before the sweeper
// closes it and composes the final mosaic. Unset → legacy daily cutoff.
const KeyPeriodDurationHours = "period_duration_hours"

// KeyTileRetentionDays controls how long after a period ends its per-tile
// rows (tiles/claims/submissions) are kept before the retention sweeper
// purges them. Unset → DefaultTileRetentionDays.
const KeyTileRetentionDays = "tile_retention_days"

// DefaultTileRetentionDays is the fallback retention window (~1 month).
const DefaultTileRetentionDays = 30

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

// TileRetentionDays returns the configured retention window in days, falling
// back to DefaultTileRetentionDays when unset or invalid. Always >= 1.
func TileRetentionDays(ctx context.Context, db *sql.DB) int {
	raw, found, err := Get(ctx, db, KeyTileRetentionDays)
	if err != nil || !found {
		return DefaultTileRetentionDays
	}
	days, err := strconv.Atoi(raw)
	if err != nil || days < 1 {
		return DefaultTileRetentionDays
	}
	return days
}

// SetTileRetentionDays stores the retention window in days (>= 1).
func SetTileRetentionDays(ctx context.Context, db *sql.DB, days int) error {
	if days < 1 {
		days = 1
	}
	return Set(ctx, db, KeyTileRetentionDays, strconv.Itoa(days))
}

// KeyPhaseGridSizes is a JSON int array describing the concentric-ring
// progression of the game grid: entry i is the side length of the unlocked
// window at phase (i+1). Values must be odd, strictly increasing, and the
// first must be >= 3 (see docs/game-model-rework.md).
const KeyPhaseGridSizes = "phase_grid_sizes"

// KeyOuterTileDisplay controls how the frontend renders tiles whose phase
// > period.phase. "blocked" shows them dark and inert; "hidden" omits them
// entirely so the viewport is exactly the currently-unlocked window.
const KeyOuterTileDisplay = "outer_tile_display"

// OuterTileDisplayBlocked / OuterTileDisplayHidden are the only two valid
// values for KeyOuterTileDisplay. Anything else falls back to Blocked.
const (
	OuterTileDisplayBlocked = "blocked"
	OuterTileDisplayHidden  = "hidden"
)

// DefaultPhaseGridSizes is the fallback progression (3×3 → 5×5 → 7×7 → 9×9).
// Returning a copy from PhaseGridSizes; callers may mutate freely.
var DefaultPhaseGridSizes = []int{3, 5, 7, 9}

// PhaseGridSizes returns the configured ring progression, falling back to
// DefaultPhaseGridSizes when unset or invalid. Always returns a validated
// slice — callers do not need to re-check invariants.
func PhaseGridSizes(ctx context.Context, db *sql.DB) []int {
	raw, found, err := Get(ctx, db, KeyPhaseGridSizes)
	if err != nil || !found {
		return append([]int(nil), DefaultPhaseGridSizes...)
	}
	sizes, err := parsePhaseGridSizes(raw)
	if err != nil {
		return append([]int(nil), DefaultPhaseGridSizes...)
	}
	return sizes
}

// SetPhaseGridSizes validates and stores the ring progression. Returns an
// error if the array violates the invariants (see KeyPhaseGridSizes).
func SetPhaseGridSizes(ctx context.Context, db *sql.DB, sizes []int) error {
	if err := validatePhaseGridSizes(sizes); err != nil {
		return err
	}
	encoded, err := json.Marshal(sizes)
	if err != nil {
		return fmt.Errorf("appsettings: marshal phase_grid_sizes: %w", err)
	}
	return Set(ctx, db, KeyPhaseGridSizes, string(encoded))
}

// OuterTileDisplay returns the configured outer-tile render mode, falling
// back to OuterTileDisplayBlocked when unset or invalid.
func OuterTileDisplay(ctx context.Context, db *sql.DB) string {
	raw, found, err := Get(ctx, db, KeyOuterTileDisplay)
	if err != nil || !found {
		return OuterTileDisplayBlocked
	}
	if raw != OuterTileDisplayBlocked && raw != OuterTileDisplayHidden {
		return OuterTileDisplayBlocked
	}
	return raw
}

// SetOuterTileDisplay validates and stores the render mode.
func SetOuterTileDisplay(ctx context.Context, db *sql.DB, mode string) error {
	if mode != OuterTileDisplayBlocked && mode != OuterTileDisplayHidden {
		return fmt.Errorf("appsettings: outer_tile_display must be %q or %q, got %q",
			OuterTileDisplayBlocked, OuterTileDisplayHidden, mode)
	}
	return Set(ctx, db, KeyOuterTileDisplay, mode)
}

func parsePhaseGridSizes(raw string) ([]int, error) {
	var sizes []int
	if err := json.Unmarshal([]byte(raw), &sizes); err != nil {
		return nil, fmt.Errorf("appsettings: parse phase_grid_sizes: %w", err)
	}
	if err := validatePhaseGridSizes(sizes); err != nil {
		return nil, err
	}
	return sizes, nil
}

// validatePhaseGridSizes enforces the ring-progression invariants documented
// on KeyPhaseGridSizes. Called by both the reader (to guard against manual
// SQL edits) and the setter (to guard against the admin UI).
func validatePhaseGridSizes(sizes []int) error {
	if len(sizes) == 0 {
		return errors.New("appsettings: phase_grid_sizes must have at least one entry")
	}
	prev := 0
	for i, s := range sizes {
		if s < 3 {
			return fmt.Errorf("appsettings: phase_grid_sizes[%d]=%d must be >= 3", i, s)
		}
		if s%2 == 0 {
			return fmt.Errorf("appsettings: phase_grid_sizes[%d]=%d must be odd", i, s)
		}
		if s <= prev {
			return fmt.Errorf("appsettings: phase_grid_sizes must be strictly increasing (index %d: %d <= %d)", i, s, prev)
		}
		prev = s
	}
	return nil
}
