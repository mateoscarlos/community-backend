package grid

import (
	"context"
	"time"

	"github.com/rs/zerolog"
	"go.uber.org/fx"
)

const periodSweepInterval = 60 * time.Second

// StartPeriodSweeper runs a background goroutine that closes any active period
// past its daily cutoff (midnight in PeriodTimezone), regardless of whether
// anyone is currently drawing.
func StartPeriodSweeper(lc fx.Lifecycle, svc *Service, log zerolog.Logger) {
	lc.Append(fx.Hook{
		OnStart: func(_ context.Context) error {
			go func() {
				ticker := time.NewTicker(periodSweepInterval)
				defer ticker.Stop()
				for range ticker.C {
					ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					if err := svc.SweepExpiredPeriod(ctx); err != nil {
						log.Error().Err(err).Msg("period sweep failed")
					}
					cancel()
				}
			}()
			return nil
		},
	})
}
