package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/community-app/community-backend/internal/config"
	"github.com/community-app/community-backend/internal/shared/logger"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
	"github.com/rs/zerolog"
	"go.uber.org/fx"
)

// version is injected at build time via -ldflags.
var version = "dev"

func main() {
	fx.New(
		fx.Provide(
			config.Load,
			logger.New,
			newRouter,
		),
		fx.Invoke(startServer),
	).Run()
}

func newRouter(cfg *config.Config, log zerolog.Logger) *chi.Mux {
	r := chi.NewRouter()
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{cfg.CORSAllowedOrigins},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		AllowCredentials: true,
	}))
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		log.Info().
			Str("method", r.Method).
			Str("path", r.URL.Path).
			Str("remote_addr", r.RemoteAddr).
			Msg("health check")

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]string{
			"status":  "ok",
			"version": version,
		}); err != nil {
			log.Error().Err(err).Msg("failed to write health response")
		}
	})
	return r
}

func startServer(lc fx.Lifecycle, cfg *config.Config, log zerolog.Logger, r *chi.Mux) {
	addr := fmt.Sprintf(":%s", cfg.Port)
	srv := &http.Server{Addr: addr, Handler: r}

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			log.Info().Str("addr", addr).Str("version", version).Msg("server starting")
			go func() {
				if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
					log.Fatal().Err(err).Msg("server error")
				}
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			log.Info().Msg("server shutting down")
			return srv.Shutdown(ctx)
		},
	})
}
