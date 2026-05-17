package retention

import (
	"context"
	"database/sql"
	"time"

	"github.com/community-app/community-backend/internal/shared/storage"
	"github.com/rs/zerolog"
	"go.uber.org/fx"
)

// sweepInterval — once a day is plenty; eligibility is measured in days and
// the periods table is tiny (one row per period).
const sweepInterval = 24 * time.Hour

// Module wires the background retention sweeper. No new providers — it reuses
// the shared *sql.DB / *storage.Storage already in the graph.
var Module = fx.Options(fx.Invoke(StartRetentionSweeper))

// StartRetentionSweeper runs a daily goroutine that purges per-tile rows of
// periods older than the configured retention window.
func StartRetentionSweeper(
	lc fx.Lifecycle,
	db *sql.DB,
	store *storage.Storage,
	log zerolog.Logger,
) {
	run := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		days := RetentionDays(ctx, db)
		if _, err := Purge(ctx, db, store, log, days); err != nil {
			log.Error().Err(err).Msg("retention sweep failed")
		}
	}

	lc.Append(fx.Hook{
		OnStart: func(_ context.Context) error {
			go func() {
				// Settle after boot, then catch up on any backlog.
				time.Sleep(10 * time.Minute)
				run()
				ticker := time.NewTicker(sweepInterval)
				defer ticker.Stop()
				for range ticker.C {
					run()
				}
			}()
			return nil
		},
	})
}
