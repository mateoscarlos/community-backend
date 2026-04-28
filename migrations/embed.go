package migrations

import "embed"

// FS contains all SQL migration files, embedded into the binary at build time.
//
//go:embed *.sql
var FS embed.FS
