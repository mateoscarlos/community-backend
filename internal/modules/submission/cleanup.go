package submission

import (
	"context"
	"time"

	"github.com/rs/zerolog"
	"go.uber.org/fx"
)

// storageCleanupInterval picks a cadence that's frequent enough that the
// backlog never grows large but rare enough that we don't waste R2 Class A
// (write) operations. 30 min strikes a balance — at typical traffic, a
// phase advance produces ~9–230 stale tile JPGs, well under the 500-per-tick
// batch size, so backlogs clear in one tick.
const storageCleanupInterval = 30 * time.Minute

// StartStorageSweeper runs a background goroutine that prunes orphaned tile
// JPGs from object storage. Tile submissions become orphaned the moment a
// phase mosaic is composed: the mosaic image is what users see in the
// archive, so the per-tile sources are dead weight after that.
func StartStorageSweeper(lc fx.Lifecycle, svc *Service, log zerolog.Logger) {
	lc.Append(fx.Hook{
		OnStart: func(_ context.Context) error {
			go func() {
				// Run once shortly after boot so a freshly-deployed instance
				// catches up on any backlog left by an older release.
				time.Sleep(5 * time.Minute)
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
				if _, err := svc.CleanupOrphanedTileObjects(ctx); err != nil {
					log.Error().Err(err).Msg("initial storage cleanup failed")
				}
				cancel()

				ticker := time.NewTicker(storageCleanupInterval)
				defer ticker.Stop()
				for range ticker.C {
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
					if _, err := svc.CleanupOrphanedTileObjects(ctx); err != nil {
						log.Error().Err(err).Msg("storage cleanup failed")
					}
					cancel()
				}
			}()
			return nil
		},
	})
}
