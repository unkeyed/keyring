// Package db applies the embedded SQL migrations at startup.
package db

import (
	"context"
	"embed"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Migrate applies every migrations/NNN_*.sql not yet recorded in
// schema_migrations, in name order, each in its own transaction.
// It holds pg_advisory_xact_lock(hashtext('uncrm.migrate')) per file, so
// replicas booting together apply each file exactly once and the losers
// wait, then find nothing to do. Files are never edited after release;
// a change is a new file.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	return errors.New("not implemented")
}
