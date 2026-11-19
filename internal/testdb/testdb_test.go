package testdb_test

import (
	"context"
	"testing"

	"github.com/uncrm/uncrm/internal/testdb"
)

func TestDatabasesAreMigratedAndIsolated(t *testing.T) {
	ctx := context.Background()
	a, b := testdb.New(t), testdb.New(t)

	var stages int
	if err := a.QueryRow(ctx, `SELECT count(*) FROM stages`).Scan(&stages); err != nil {
		t.Fatal(err)
	}
	if stages != 4 {
		t.Fatalf("fresh database has %d stages, want the 4 seeded by the migration", stages)
	}

	if _, err := a.Exec(ctx, `INSERT INTO stages (name, position) VALUES ('only-in-a', 9)`); err != nil {
		t.Fatal(err)
	}
	var inB int
	if err := b.QueryRow(ctx, `SELECT count(*) FROM stages WHERE name = 'only-in-a'`).Scan(&inB); err != nil {
		t.Fatal(err)
	}
	if inB != 0 {
		t.Fatal("a row written in one test database is visible in another")
	}
}
