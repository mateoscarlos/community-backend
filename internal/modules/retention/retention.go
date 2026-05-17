// Package retention purges the per-tile data (tiles, claims, submissions) of
// periods that ended longer ago than the configured window. The period row,
// its composed phase mosaics and final image are kept, so the Museum/archive
// is unaffected — only the raw per-tile rows (and any stray storage objects)
// are removed. Pure raw SQL: no sqlc, no schema change.
package retention

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/community-app/community-backend/internal/shared/appsettings"
	"github.com/community-app/community-backend/internal/shared/storage"
	"github.com/rs/zerolog"
)

// Eligible = closed period whose end is older than the retention window.
// Only the period alias `p` is referenced so it works in every query below.
const eligibleWhere = `
	p.status <> 'active'
	AND p.ended_at IS NOT NULL
	AND p.ended_at < now() - make_interval(days => $1)`

// Stats reports how much per-tile data the eligible periods hold (or how
// much a purge removed).
type Stats struct {
	Periods     int `json:"periods"`
	Tiles       int `json:"tiles"`
	Claims      int `json:"claims"`
	Submissions int `json:"submissions"`
}

// CountEligible returns how many periods/tiles/claims/submissions are past
// the retention window right now (used by the admin readout).
func CountEligible(ctx context.Context, db *sql.DB, retentionDays int) (Stats, error) {
	q := `
		SELECT
		  (SELECT count(*) FROM periods p WHERE ` + eligibleWhere + `),
		  (SELECT count(*) FROM tiles t
		     JOIN periods p ON p.id = t.period_id WHERE ` + eligibleWhere + `),
		  (SELECT count(*) FROM claims c
		     JOIN tiles t ON t.id = c.tile_id
		     JOIN periods p ON p.id = t.period_id WHERE ` + eligibleWhere + `),
		  (SELECT count(*) FROM submissions s
		     JOIN tiles t ON t.id = s.tile_id
		     JOIN periods p ON p.id = t.period_id WHERE ` + eligibleWhere + `)`
	var st Stats
	if err := db.QueryRowContext(ctx, q, retentionDays).Scan(
		&st.Periods, &st.Tiles, &st.Claims, &st.Submissions,
	); err != nil {
		return Stats{}, fmt.Errorf("retention: count eligible: %w", err)
	}
	return st, nil
}

// Purge removes per-tile rows for every eligible period. It first deletes any
// still-live storage objects for those submissions (so we never orphan an R2
// object by dropping the row that holds its key), then deletes
// submissions → claims → tiles in one transaction. Period/mosaic/final-image
// rows and objects are kept.
func Purge(
	ctx context.Context,
	db *sql.DB,
	store *storage.Storage,
	log zerolog.Logger,
	retentionDays int,
) (Stats, error) {
	// 1. Storage objects that the periodic sweeper hasn't cleaned yet.
	rows, err := db.QueryContext(ctx, `
		SELECT s.storage_key
		FROM submissions s
		JOIN tiles t ON t.id = s.tile_id
		JOIN periods p ON p.id = t.period_id
		WHERE `+eligibleWhere+`
		  AND s.storage_cleaned_at IS NULL
		  AND s.storage_key <> ''`, retentionDays)
	if err != nil {
		return Stats{}, fmt.Errorf("retention: list keys: %w", err)
	}
	var keys []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			rows.Close()
			return Stats{}, fmt.Errorf("retention: scan key: %w", err)
		}
		keys = append(keys, k)
	}
	rows.Close()
	if len(keys) > 0 {
		if _, derr := store.DeleteObjects(ctx, keys); derr != nil {
			// Best-effort: a residual object is acceptable; the rows still go.
			log.Warn().Err(derr).Int("attempted", len(keys)).
				Msg("retention: some object deletes failed")
		}
	}

	// 2. Delete rows in FK order inside a transaction.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return Stats{}, fmt.Errorf("retention: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	tilesSubq := `
		SELECT t.id FROM tiles t
		JOIN periods p ON p.id = t.period_id
		WHERE ` + eligibleWhere

	delSubs, err := tx.ExecContext(ctx,
		`DELETE FROM submissions WHERE tile_id IN (`+tilesSubq+`)`, retentionDays)
	if err != nil {
		return Stats{}, fmt.Errorf("retention: delete submissions: %w", err)
	}
	delClaims, err := tx.ExecContext(ctx,
		`DELETE FROM claims WHERE tile_id IN (`+tilesSubq+`)`, retentionDays)
	if err != nil {
		return Stats{}, fmt.Errorf("retention: delete claims: %w", err)
	}
	delTiles, err := tx.ExecContext(ctx,
		`DELETE FROM tiles WHERE id IN (`+tilesSubq+`)`, retentionDays)
	if err != nil {
		return Stats{}, fmt.Errorf("retention: delete tiles: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Stats{}, fmt.Errorf("retention: commit: %w", err)
	}

	subs, _ := delSubs.RowsAffected()
	claims, _ := delClaims.RowsAffected()
	tiles, _ := delTiles.RowsAffected()
	st := Stats{
		Tiles:       int(tiles),
		Claims:      int(claims),
		Submissions: int(subs),
	}
	log.Info().
		Int("tiles", st.Tiles).
		Int("claims", st.Claims).
		Int("submissions", st.Submissions).
		Int("retention_days", retentionDays).
		Msg("retention purge")
	return st, nil
}

// RetentionDays reads the configured window (admin-tunable, default 30).
func RetentionDays(ctx context.Context, db *sql.DB) int {
	return appsettings.TileRetentionDays(ctx, db)
}
