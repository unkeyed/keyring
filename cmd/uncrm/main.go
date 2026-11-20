// Command uncrm is the whole product in one process: REST + UI API, MCP,
// webhooks, the embedded React app and the enrichment worker.
package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
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
	Auth          auth.Config      // PUBLIC_URL, PASSWORD_LOGIN, ALLOWED_EMAIL_DOMAINS, CLIENT_IP_HEADER, providers
	SetupToken    string           // SETUP_TOKEN, optional: the setup link's token instead of a random one
}

// loadConfig reads and validates the environment. Every config error is
// reported here, before anything connects, with the variable's name.
func loadConfig() (config, error) {
	c := config{
		Port:          os.Getenv("PORT"),
		ContextDevKey: os.Getenv("CONTEXT_DEV_API_KEY"),
		Unkey:         auth.UnkeyConfig{RootKey: os.Getenv("UNKEY_ROOT_KEY"), APIID: os.Getenv("UNKEY_API_ID")},
		SetupToken:    os.Getenv("SETUP_TOKEN"),
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
	if os.Getenv("ADMIN_EMAIL") != "" || os.Getenv("ADMIN_PASSWORD") != "" {
		return c, errors.New("ADMIN_EMAIL and ADMIN_PASSWORD are gone: uncrm no longer seeds a password. Unset them and open the setup link uncrm logs at start (docs/self-hosting.md)")
	}
	if c.SetupToken != "" && len(c.SetupToken) < auth.MinSetupTokenLen {
		return c, fmt.Errorf("SETUP_TOKEN must be at least %d characters; generate one with openssl rand -hex 32", auth.MinSetupTokenLen)
	}
	c.Auth, err = loadAuthConfig(c.Port)
	return c, err
}

// loadAuthConfig reads how people sign in.
func loadAuthConfig(port string) (auth.Config, error) {
	var c auth.Config
	raw := cmp.Or(os.Getenv("PUBLIC_URL"), "http://localhost:"+port)
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || strings.Trim(u.Path, "/") != "" || u.RawQuery != "" {
		return c, fmt.Errorf("PUBLIC_URL must be an origin such as https://crm.example.com, got %q", raw)
	}
	if u.Scheme == "http" && u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1" {
		return c, fmt.Errorf("PUBLIC_URL must use https unless its host is localhost: passkeys need a secure origin and session cookies must not travel in the clear, got %q", raw)
	}
	u.Path = ""
	c.PublicURL = u
	if v := os.Getenv("PASSWORD_LOGIN"); v != "" {
		if c.PasswordLogin, err = strconv.ParseBool(v); err != nil {
			return c, fmt.Errorf("PASSWORD_LOGIN must be true or false, got %q", v)
		}
	}
	for _, d := range strings.Split(os.Getenv("ALLOWED_EMAIL_DOMAINS"), ",") {
		if d = strings.ToLower(strings.TrimSpace(d)); d != "" {
			if strings.Contains(d, "@") {
				return c, fmt.Errorf("ALLOWED_EMAIL_DOMAINS lists domains such as example.com, got %q", d)
			}
			c.AllowedEmailDomains = append(c.AllowedEmailDomains, d)
		}
	}
	c.ClientIPHeader = os.Getenv("CLIENT_IP_HEADER")
	for _, p := range []struct{ id, name, env, issuer string }{
		{"google", "Google", "GOOGLE", "https://accounts.google.com"},
		{"github", "GitHub", "GITHUB", ""},
		{"oidc", cmp.Or(os.Getenv("OIDC_NAME"), "SSO"), "OIDC", os.Getenv("OIDC_ISSUER")},
	} {
		id, secret := os.Getenv(p.env+"_CLIENT_ID"), os.Getenv(p.env+"_CLIENT_SECRET")
		oidc := p.id == "oidc"
		if id == "" && secret == "" && (!oidc || p.issuer == "") {
			continue
		}
		if id == "" || secret == "" || (oidc && p.issuer == "") {
			if oidc {
				return c, errors.New("set all of OIDC_ISSUER, OIDC_CLIENT_ID and OIDC_CLIENT_SECRET, or none")
			}
			return c, fmt.Errorf("set both %s_CLIENT_ID and %s_CLIENT_SECRET, or neither", p.env, p.env)
		}
		c.Providers = append(c.Providers, auth.ProviderConfig{ID: p.id, Name: p.name, ClientID: id, ClientSecret: secret, Issuer: p.issuer})
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

	users, err := auth.NewUsers(pool, cfg.Auth)
	if err != nil {
		return err
	}
	if err := announceSetup(ctx, log, users, cfg.SetupToken); err != nil {
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
	log.Info("uncrm listening", "port", cfg.Port, "version", version, "enrichment", enricher != nil, "keys", authn.Keys.Mode(),
		"public_url", cfg.Auth.PublicURL.String(), "password_login", cfg.Auth.PasswordLogin, "providers", len(cfg.Auth.Providers))

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

// announceSetup logs the setup link while nobody can sign in, as a log
// field and as a plain banner on stderr that is easy to spot in
// docker compose up or make dev.
func announceSetup(ctx context.Context, log *slog.Logger, users *auth.Users, fixed string) error {
	token, err := users.PrepareSetup(ctx, fixed)
	if err != nil || token == "" {
		return err
	}
	link := users.SetupURL(token)
	log.Warn("nobody can sign in yet: open the setup link to create the first account", "setup_url", link)
	fmt.Fprintf(os.Stderr, "\n  uncrm has no account that can sign in yet. Open this link to create the first one:\n\n    %s\n\n  It works once and stays valid until someone uses it.\n\n", link)
	return nil
}
