package claim

import (
	"context"
	"time"

	"github.com/rs/zerolog"
	"go.uber.org/fx"
)

const sweepInterval = 30 * time.Second

// StartSweeper runs a background goroutine that releases expired claims.
func StartSweeper(lc fx.Lifecycle, svc *Service, log zerolog.Logger) {
	lc.Append(fx.Hook{
		OnStart: func(_ context.Context) error {
			go func() {
				ticker := time.NewTicker(sweepInterval)
				defer ticker.Stop()
				for range ticker.C {
					ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					if _, err := svc.SweepExpired(ctx); err != nil {
						log.Error().Err(err).Msg("claim sweep failed")
					}
					cancel()
				}
			}()
			return nil
		},
	})
}
