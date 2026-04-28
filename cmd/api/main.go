package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"

	"github.com/community-app/community-backend/internal/config"
	"github.com/community-app/community-backend/internal/modules/claim"
	"github.com/community-app/community-backend/internal/modules/dailyimage"
	"github.com/community-app/community-backend/internal/modules/debug"
	"github.com/community-app/community-backend/internal/modules/feedback"
	"github.com/community-app/community-backend/internal/modules/grid"
	"github.com/community-app/community-backend/internal/modules/submission"
	"github.com/community-app/community-backend/internal/shared/httpserver"
	"github.com/community-app/community-backend/internal/shared/logger"
	"github.com/community-app/community-backend/internal/shared/postgres"
	shareddb "github.com/community-app/community-backend/internal/shared/postgres/db"
	"github.com/community-app/community-backend/internal/shared/sse"
	"github.com/community-app/community-backend/internal/shared/storage"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
	"github.com/joho/godotenv"
	"github.com/rs/zerolog"
	"go.uber.org/fx"
)

// version is injected at build time via -ldflags.
var version = "dev"

func init() {
	_ = godotenv.Load() // load .env if present; silently ignored in production
}

func main() {
	fx.New(
		fx.Provide(
			config.Load,
			logger.New,
			postgres.New,
			storage.New,
			sse.NewBroker,
			newRouter,
		),
		fx.Options(dailyimage.Module),
		fx.Options(grid.Module),
		fx.Options(claim.Module),
		fx.Options(submission.Module),
		fx.Options(feedback.Module),
		fx.Options(debug.Module),
		fx.Invoke(startServer),
	).Run()
}

type routerParams struct {
	fx.In

	Config     *config.Config
	Log        zerolog.Logger
	DB         *sql.DB
	Store      *storage.Storage
	Registrars []httpserver.RouteRegistrar `group:"routes"`
}

func newRouter(p routerParams) *chi.Mux {
	r := chi.NewRouter()
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   p.Config.CORSAllowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		AllowCredentials: true,
	}))

	queries := shareddb.New(p.DB)
	r.Get("/health", healthHandler(queries, p.Store, p.Log))

	// Mount all module routes — to add a new module, register it in its module.go.
	for _, reg := range p.Registrars {
		reg.RegisterRoutes(r)
	}

	return r
}

func healthHandler(queries *shareddb.Queries, store *storage.Storage, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		dbStatus := "ok"
		if _, err := queries.Ping(r.Context()); err != nil {
			log.Error().Err(err).Msg("health check: db ping failed")
			dbStatus = "error"
		}

		storageStatus := "ok"
		if err := store.Ping(r.Context()); err != nil {
			log.Error().Err(err).Msg("health check: storage ping failed")
			storageStatus = "error"
		}

		log.Info().Str("db", dbStatus).Str("storage", storageStatus).Msg("health check")

		httpserver.WriteJSON(w, http.StatusOK, map[string]string{
			"status":  "ok",
			"db":      dbStatus,
			"storage": storageStatus,
			"version": version,
		})
	}
}

func startServer(lc fx.Lifecycle, cfg *config.Config, log zerolog.Logger, r *chi.Mux) {
	addr := fmt.Sprintf(":%s", cfg.Port)
	srv := &http.Server{Addr: addr, Handler: r}

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			log.Info().Str("addr", addr).Str("version", version).Strs("cors_origins", cfg.CORSAllowedOrigins).Msg("server starting")
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
