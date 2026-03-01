package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/community-app/community-backend/internal/config"
	"github.com/rs/zerolog"
)

// New creates a zerolog logger. If Loki vars are configured, logs are also
// shipped to Grafana Cloud Loki via the HTTP push API. Stdout is always included.
func New(cfg *config.Config) zerolog.Logger {
	level := parseLevel(cfg.LogLevel)

	writers := []io.Writer{
		zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339},
	}

	if cfg.LokiURL != "" {
		writers = append(writers, newLokiWriter(cfg))
	}

	multi := zerolog.MultiLevelWriter(writers...)

	return zerolog.New(multi).
		Level(level).
		With().
		Timestamp().
		Str("env", cfg.Env).
		Logger()
}

func parseLevel(s string) zerolog.Level {
	switch s {
	case "debug":
		return zerolog.DebugLevel
	case "warn":
		return zerolog.WarnLevel
	case "error":
		return zerolog.ErrorLevel
	default:
		return zerolog.InfoLevel
	}
}

// lokiWriter ships log lines to Grafana Cloud Loki via the HTTP push API.
// It is fire-and-forget: failures are printed to stderr and do not affect stdout logging.
type lokiWriter struct {
	url      string
	username string
	password string
	client   *http.Client
	labels   string
}

func newLokiWriter(cfg *config.Config) *lokiWriter {
	return &lokiWriter{
		url:      cfg.LokiURL,
		username: cfg.LokiUsername,
		password: cfg.LokiPassword,
		client:   &http.Client{Timeout: 5 * time.Second},
		labels:   fmt.Sprintf(`{app="community-backend",env="%s"}`, cfg.Env),
	}
}

// Write implements io.Writer. Each call ships one log line to Loki.
func (w *lokiWriter) Write(p []byte) (n int, err error) {
	line := string(bytes.TrimRight(p, "\n"))
	ts := fmt.Sprintf("%d", time.Now().UnixNano())

	payload := map[string]any{
		"streams": []map[string]any{
			{
				"stream": map[string]string{"app": "community-backend"},
				"values": [][]string{{ts, line}},
			},
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return len(p), nil
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, w.url, bytes.NewReader(body))
	if err != nil {
		return len(p), nil
	}
	req.Header.Set("Content-Type", "application/json")
	if w.username != "" {
		req.SetBasicAuth(w.username, w.password)
	}

	resp, err := w.client.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "loki: send error: %v\n", err)
		return len(p), nil
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		fmt.Fprintf(os.Stderr, "loki: unexpected status %d\n", resp.StatusCode)
	}

	return len(p), nil
}
