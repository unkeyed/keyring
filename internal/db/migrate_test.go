package db_test

import (
	"context"
	"io/fs"
	"os"
	"sync"
	"testing"

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
