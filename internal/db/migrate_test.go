package db_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/unkeyed/keyring/internal/db"
	"github.com/unkeyed/keyring/internal/testdb"
)

// testdb.New already migrated the database, so these runs find every file
// recorded. Running them concurrently also exercises the advisory lock the
// way replicas booting together do.
func TestMigrateIsIdempotent(t *testing.T) {
	ctx := context.Background()
	pool := testdb.New(t)

	var wg sync.WaitGroup
	errs := make(chan error, 3)
	for range 3 {
		wg.Go(func() { errs <- db.Migrate(ctx, pool) })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}

	files, err := fs.Glob(os.DirFS("migrations"), "*.sql")
	if err != nil {
		t.Fatal(err)
	}
	var recorded int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&recorded); err != nil {
		t.Fatal(err)
	}
	if recorded != len(files) {
		t.Fatalf("schema_migrations has %d rows, want one per file (%d)", recorded, len(files))
	}
}

// A role that can only read and write data, like PlanetScale Postgres's
// read/write role, starts against a database another role already migrated,
// and is refused once a migration is pending.
func TestMigrateNeedsNoDDLWhenUpToDate(t *testing.T) {
	ctx := context.Background()
	pool := testdb.New(t)

	b := make([]byte, 8)
	_, _ = rand.Read(b)
	role := "keyring_rw_" + hex.EncodeToString(b)
	if _, err := pool.Exec(ctx, `CREATE ROLE `+role+` NOLOGIN IN ROLE pg_read_all_data, pg_write_all_data`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DROP ROLE IF EXISTS `+role) })
	if _, err := pool.Exec(ctx, `REVOKE CREATE ON SCHEMA public FROM PUBLIC`); err != nil {
		t.Fatal(err)
	}

	cfg := pool.Config().Copy()
	cfg.AfterConnect = func(ctx context.Context, c *pgx.Conn) error {
		_, err := c.Exec(ctx, `SET ROLE `+role)
		return err
	}
	rw, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer rw.Close()

	if err := db.Migrate(ctx, rw); err != nil {
		t.Fatalf("Migrate as a read/write role on an up-to-date database: %v", err)
	}

	// Forget the last migration so it is pending again.
	if _, err := pool.Exec(ctx, `DELETE FROM schema_migrations WHERE name = (SELECT max(name) FROM schema_migrations)`); err != nil {
		t.Fatal(err)
	}
	var pgErr *pgconn.PgError
	if err := db.Migrate(ctx, rw); !errors.As(err, &pgErr) || pgErr.Code != "42501" {
		t.Fatalf("Migrate as a read/write role with a migration pending: got %v, want permission denied (42501)", err)
	}
}
