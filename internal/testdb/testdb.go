// Package testdb gives each test its own freshly migrated Postgres database,
// so tests can run in parallel and never see each other's rows.
//
// It connects to TEST_DATABASE_URL, defaulting to the docker compose
// Postgres (`docker compose up -d db`). The role there must be allowed to
// CREATE DATABASE. An unreachable server fails the test instead of skipping
// it, so a missing database can never pass CI by accident.
package testdb

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/unkeyed/keyring/internal/db"
)

// DefaultURL is the docker compose database.
const DefaultURL = "postgres://keyring:keyring@localhost:5432/keyring?sslmode=disable"

// New creates a database named keyring_test_<random>, applies every migration
// and returns a pool on it. The database is dropped when the test ends.
func New(t testing.TB) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	url := adminURL()
	admin, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatalf("testdb: Postgres is unreachable at %s: %v\nStart it with `docker compose up -d db`, or set TEST_DATABASE_URL.", redact(url), err)
	}
	defer admin.Close(context.Background())

	name := "keyring_test_" + randomHex(8)
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatalf("testdb: create %s: %v", name, err)
	}
	t.Cleanup(func() { drop(t, url, name) })

	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatalf("testdb: %v", err)
	}
	cfg.ConnConfig.Database = name
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("testdb: connect %s: %v", name, err)
	}
	t.Cleanup(pool.Close)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("testdb: migrate %s: %v", name, err)
	}
	return pool
}

func drop(t testing.TB, url, name string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Errorf("testdb: drop %s: %v", name, err)
		return
	}
	defer admin.Close(ctx)
	if _, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)"); err != nil {
		t.Errorf("testdb: drop %s: %v", name, err)
	}
}

func adminURL() string {
	if u := os.Getenv("TEST_DATABASE_URL"); u != "" {
		return u
	}
	return DefaultURL
}

func redact(url string) string {
	cfg, err := pgx.ParseConfig(url)
	if err != nil {
		return "TEST_DATABASE_URL"
	}
	return fmt.Sprintf("%s:%d/%s", cfg.Host, cfg.Port, cfg.Database)
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
