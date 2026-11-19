package auth

import (
	"context"
	"strings"
	"testing"

	"github.com/uncrm/uncrm/internal/testdb"
)

func TestSeedAdminIsIdempotent(t *testing.T) {
	ctx := context.Background()
	pool := testdb.New(t)
	users := NewUsers(pool)

	if err := users.SeedAdmin(ctx, "", ""); err != nil {
		t.Fatalf("no ADMIN_EMAIL should be a no-op: %v", err)
	}
	for range 2 {
		if err := users.SeedAdmin(ctx, " Admin@Example.com ", "first-password"); err != nil {
			t.Fatal(err)
		}
	}
	if err := users.SeedAdmin(ctx, "admin@example.com", "changed-in-env"); err != nil {
		t.Fatal(err)
	}

	var n int
	var hash string
	if err := pool.QueryRow(ctx, `SELECT count(*), min(password_hash) FROM users WHERE email = 'admin@example.com'`).Scan(&n, &hash); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("%d admin rows after three boots, want 1", n)
	}
	if !strings.HasPrefix(hash, "pbkdf2-sha256$600000$") {
		t.Fatalf("stored hash %q is not in the documented format", hash)
	}
}
