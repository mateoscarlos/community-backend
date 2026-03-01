package config

import (
	"github.com/kelseyhightower/envconfig"
)

// Config holds all application configuration loaded from environment variables.
// The app fails fast at startup if required variables are missing.
type Config struct {
	// Server
	Port string `envconfig:"PORT" default:"8080"`
	Env  string `envconfig:"ENV" default:"local"` // local | dev | prod

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
}

// Load reads configuration from environment variables.
func Load() (*Config, error) {
	var cfg Config
	if err := envconfig.Process("", &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
