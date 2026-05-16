package config

import (
	"github.com/kelseyhightower/envconfig"
)

// Config holds all application configuration loaded from environment variables.
// The app fails fast at startup if required variables are missing.
type Config struct {
	// Server
	Port               string `envconfig:"PORT" default:"8080"`
	Env                string `envconfig:"ENV" default:"local"` // local | dev | prod
	CORSAllowedOrigins []string `envconfig:"CORS_ALLOWED_ORIGINS"`

	// Admin: shared secret that gates the /debug admin endpoints. When empty
	// the admin routes are not registered at all (in any environment). When
	// set, the routes exist but every request must present a matching
	// X-Admin-Secret header.
	AdminSecret string `envconfig:"ADMIN_SECRET"`

	// Logging
	LogLevel     string `envconfig:"LOG_LEVEL" default:"info"` // debug | info | warn | error
	LokiURL      string `envconfig:"LOKI_URL"`
	LokiUsername string `envconfig:"LOKI_USERNAME"`
	LokiPassword string `envconfig:"LOKI_PASSWORD"`

	// Database (placeholder for next phase)
	DatabaseURL string `envconfig:"DATABASE_URL"`

	// Storage
	StorageDriver  string `envconfig:"STORAGE_DRIVER" default:"minio"` // minio | s3
	MinioEndpoint  string `envconfig:"MINIO_ENDPOINT" default:"localhost:9000"`
	MinioAccessKey string `envconfig:"MINIO_ACCESS_KEY"`
	MinioSecretKey string `envconfig:"MINIO_SECRET_KEY"`
	MinioBucket    string `envconfig:"MINIO_BUCKET" default:"community-assets"`
	AWSRegion      string `envconfig:"AWS_REGION"`
	AWSAccessKeyID string `envconfig:"AWS_ACCESS_KEY_ID"`
	AWSSecretKey   string `envconfig:"AWS_SECRET_ACCESS_KEY"`
	S3Bucket       string `envconfig:"S3_BUCKET"`
	S3Endpoint     string `envconfig:"S3_ENDPOINT" default:"s3.amazonaws.com"` // override for R2/other S3-compatible
}

// Load reads configuration from environment variables.
func Load() (*Config, error) {
	var cfg Config
	if err := envconfig.Process("", &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
