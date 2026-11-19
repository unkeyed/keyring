// Command uncrm is the whole product in one process: REST + UI API, MCP,
// webhooks, the embedded React app and the enrichment worker.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/uncrm/uncrm/internal/auth"
	"github.com/uncrm/uncrm/internal/contextdev"
	"github.com/uncrm/uncrm/internal/crm"
	"github.com/uncrm/uncrm/internal/db"
	"github.com/uncrm/uncrm/internal/httpapi"
	"github.com/uncrm/uncrm/internal/mcpserver"
	"github.com/uncrm/uncrm/web"
)

var version = "dev" // set by -ldflags at build

// config is every environment variable uncrm reads, parsed once at boot.
// Only DATABASE_URL is required.
type config struct {
	DB            *pgxpool.Config  // DATABASE_URL, required
	Port          string           // PORT, default 8080 (Unkey sets it)
	ContextDevKey string           // CONTEXT_DEV_API_KEY, optional: enrichment off without it
	Unkey         auth.UnkeyConfig // UNKEY_ROOT_KEY + UNKEY_API_ID, both or neither
	AdminEmail    string           // ADMIN_EMAIL, seeds the first user
	AdminPassword string           // ADMIN_PASSWORD
}

// loadConfig reads and validates the environment. Every config error is
// reported here, before anything connects, with the variable's name.
func loadConfig() (config, error) {
	c := config{
		Port:          os.Getenv("PORT"),
		ContextDevKey: os.Getenv("CONTEXT_DEV_API_KEY"),
		Unkey:         auth.UnkeyConfig{RootKey: os.Getenv("UNKEY_ROOT_KEY"), APIID: os.Getenv("UNKEY_API_ID")},
		AdminEmail:    os.Getenv("ADMIN_EMAIL"),
		AdminPassword: os.Getenv("ADMIN_PASSWORD"),
	}
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		return c, errors.New("DATABASE_URL is required, e.g. postgres://user:pass@host:5432/uncrm")
	}
	db, err := pgxpool.ParseConfig(url)
	if err != nil {
		// pgx's message can echo the URL, password included, so it is not logged.
		return c, errors.New("DATABASE_URL is not a Postgres connection string; want postgres://user:pass@host:5432/uncrm")
	}
	c.DB = db
	if c.Port == "" {
		c.Port = "8080"
	}
	if n, err := strconv.Atoi(c.Port); err != nil || n < 1 || n > 65535 {
		return c, fmt.Errorf("PORT must be a port number, got %q", c.Port)
	}
	if (c.Unkey.RootKey == "") != (c.Unkey.APIID == "") {
		return c, errors.New("set both UNKEY_ROOT_KEY and UNKEY_API_ID to use Unkey keys, or neither for built-in keys")
	}
	if (c.AdminEmail == "") != (c.AdminPassword == "") {
		return c, errors.New("set both ADMIN_EMAIL and ADMIN_PASSWORD, or neither")
	}
	return c, nil
}

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(log)
	if err := run(log); err != nil {
		log.Error("uncrm stopped", "err", err)
		os.Exit(1)
	}
}

// shutdownGrace is how long in-flight requests get after SIGTERM, inside
// the 30s most platforms wait before SIGKILL.
const shutdownGrace = 20 * time.Second

// run serves until SIGTERM or Ctrl-C. Then it stops accepting connections,
// lets in-flight requests finish, waits for the enrichment worker to let go
// of its job (the lease hands it to the next claim), and only then closes
// the database pool.
func run(log *slog.Logger) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()

	pool, err := pgxpool.NewWithConfig(ctx, cfg.DB)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := db.Migrate(ctx, pool); err != nil {
		return fmt.Errorf("migrate the database at DATABASE_URL: %w", err)
	}

	var enricher crm.Enricher
	if cfg.ContextDevKey != "" {
		enricher = contextdev.New(cfg.ContextDevKey)
	}
	svc := crm.New(pool, enricher)

	users := auth.NewUsers(pool)
	if err := users.SeedAdmin(ctx, cfg.AdminEmail, cfg.AdminPassword); err != nil {
		return err
	}
	authn := &auth.Authenticator{Keys: auth.NewKeyStore(pool, cfg.Unkey), Users: users}

	mux := httpapi.NewMux(svc, authn, log)
	mux.Handle("/mcp", mcpserver.Handler(svc, authn.Keys, version))
	mux.Handle("/", web.Handler())

	var worker sync.WaitGroup
	defer worker.Wait()
	worker.Go(func() {
		if err := svc.RunEnrichment(ctx); err != nil && !errors.Is(err, context.Canceled) {
			log.Error("enrichment worker stopped", "err", err)
		}
	})

	srv := &http.Server{Addr: ":" + cfg.Port, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	served := make(chan error, 1)
	go func() { served <- srv.ListenAndServe() }()
	log.Info("uncrm listening", "port", cfg.Port, "version", version, "enrichment", enricher != nil, "keys", authn.Keys.Mode())

	select {
	case err := <-served:
		stop()
		return err
	case <-ctx.Done():
	}
	log.Info("shutting down", "grace", shutdownGrace.String())
	drain, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()
	if err := srv.Shutdown(drain); err != nil {
		return fmt.Errorf("drain HTTP: %w", err)
	}
	log.Info("HTTP drained")
	return nil
}
