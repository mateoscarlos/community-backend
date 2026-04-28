package postgres

import (
	"database/sql"
	"fmt"

	"github.com/community-app/community-backend/internal/config"
	"github.com/community-app/community-backend/migrations"
	_ "github.com/lib/pq" // postgres driver
	"github.com/pressly/goose/v3"
	"github.com/rs/zerolog"
)

// New opens a connection to Postgres, runs any pending migrations,
// and returns the raw *sql.DB. Each module constructs its own SQLC
// Queries from this connection.
func New(cfg *config.Config, log zerolog.Logger) (*sql.DB, error) {
	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is not set")
	}

	conn, err := sql.Open("postgres", cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	if err := conn.Ping(); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}

	log.Info().Msg("database connected")

	goose.SetBaseFS(migrations.FS)
	goose.SetLogger(goose.NopLogger())

	if err := goose.SetDialect("postgres"); err != nil {
		return nil, fmt.Errorf("goose set dialect: %w", err)
	}

	if err := goose.Up(conn, "."); err != nil {
		return nil, fmt.Errorf("run migrations: %w", err)
	}

	log.Info().Msg("migrations applied")

	return conn, nil
}
