// Command uncrm is the whole product in one process: REST + UI API, MCP,
// webhooks, the embedded React app and the enrichment worker.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
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

// config is every environment variable uncrm reads. Only DATABASE_URL is
// required.
type config struct {
	DatabaseURL   string           // DATABASE_URL, required
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
		DatabaseURL:   os.Getenv("DATABASE_URL"),
		Port:          os.Getenv("PORT"),
		ContextDevKey: os.Getenv("CONTEXT_DEV_API_KEY"),
		Unkey:         auth.UnkeyConfig{RootKey: os.Getenv("UNKEY_ROOT_KEY"), APIID: os.Getenv("UNKEY_API_ID")},
		AdminEmail:    os.Getenv("ADMIN_EMAIL"),
		AdminPassword: os.Getenv("ADMIN_PASSWORD"),
	}
	if c.Port == "" {
		c.Port = "8080"
	}
	if c.DatabaseURL == "" {
		return c, errors.New("DATABASE_URL is required, e.g. postgres://user:pass@host:5432/uncrm")
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
	if err := run(log); err != nil {
		log.Error("uncrm stopped", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := db.Migrate(ctx, pool); err != nil {
		return err
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

	go func() {
		if err := svc.RunEnrichment(ctx); err != nil && !errors.Is(err, context.Canceled) {
			log.Error("enrichment worker stopped", "err", err)
		}
	}()

	srv := &http.Server{Addr: ":" + cfg.Port, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	log.Info("uncrm listening", "port", cfg.Port, "enrichment", enricher != nil, "keys", authn.Keys.Mode())
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
