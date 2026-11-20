package crm

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/unkeyed/keyring/internal/signup"
)

// ListProviders returns one row per registered signup.Provider (from
// signup.All), joined with provider_connections. Providers are never listed
// anywhere else.
func (s *Service) ListProviders(ctx context.Context) ([]ProviderStatus, error) {
	type conn struct {
		lastDelivery, lastErrorAt *time.Time
		lastError                 string
	}
	conns := map[string]conn{}
	rows, err := s.db.Query(ctx, `SELECT provider, last_delivery_at, last_error, last_error_at FROM provider_connections`)
	if err != nil {
		return nil, err
	}
	var name string
	var c conn
	if _, err := pgx.ForEachRow(rows, []any{&name, &c.lastDelivery, &c.lastError, &c.lastErrorAt}, func() error {
		conns[name] = c
		return nil
	}); err != nil {
		return nil, err
	}
	var out []ProviderStatus
	for _, p := range signup.All() {
		c, connected := conns[p.Name]
		out = append(out, ProviderStatus{
			Name: p.Name, Title: p.Title, WebhookPath: "/webhooks/" + p.Name, IssueSecret: p.IssueSecret, Setup: p.Setup,
			Connected: connected, LastDeliveryAt: c.lastDelivery, LastError: c.lastError, LastErrorAt: c.lastErrorAt,
		})
	}
	return out, nil
}

// ConnectProvider stores the webhook secret for a registered provider,
// replacing any previous one. When secret is "" and the provider has
// IssueSecret, a random "whsec_<base64 32 bytes>" is generated and returned
// so the UI can show it once for the user to paste into the provider.
// Reconnecting clears the delivery status, which described the old secret.
func (s *Service) ConnectProvider(ctx context.Context, actor Actor, name, secret string) (string, error) {
	p, ok := signup.Lookup(name)
	if !ok {
		return "", fmt.Errorf("%w: %q", ErrUnknownProvider, name)
	}
	secret = strings.TrimSpace(secret)
	if secret == "" {
		if !p.IssueSecret {
			return "", fmt.Errorf("%w: paste the signing secret from %s", ErrInvalid, p.Title)
		}
		key := make([]byte, 32)
		_, _ = rand.Read(key)
		secret = "whsec_" + base64.StdEncoding.EncodeToString(key)
	}
	_, err := s.db.Exec(ctx, `
		INSERT INTO provider_connections (provider, secret) VALUES ($1, $2)
		ON CONFLICT (provider) DO UPDATE
		   SET secret = EXCLUDED.secret, connected_at = now(),
		       last_delivery_at = NULL, last_error = '', last_error_at = NULL`, name, s.secrets.Seal(secret, providerSecretContext(name)))
	return secret, err
}

func providerSecretContext(provider string) string { return "provider_connections:" + provider }

// SealProviderSecrets seals every webhook secret not yet sealed with the
// current ENCRYPTION_KEY: plaintext from before encryption, or a value
// sealed with ENCRYPTION_KEY_PREVIOUS. main runs it at every start.
func (s *Service) SealProviderSecrets(ctx context.Context) (int, error) {
	rows, err := s.db.Query(ctx, `SELECT provider, secret FROM provider_connections`)
	if err != nil {
		return 0, err
	}
	type row struct{ provider, secret string }
	all, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (row, error) {
		var x row
		return x, r.Scan(&x.provider, &x.secret)
	})
	if err != nil {
		return 0, err
	}
	n := 0
	for _, x := range all {
		sealed, changed, err := s.secrets.Reseal(x.secret, providerSecretContext(x.provider))
		if err != nil {
			return n, fmt.Errorf("the %s webhook secret: %w", x.provider, err)
		}
		if !changed {
			continue
		}
		if _, err := s.db.Exec(ctx, `UPDATE provider_connections SET secret = $2 WHERE provider = $1 AND secret = $3`,
			x.provider, sealed, x.secret); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// DisconnectProvider deletes the connection. Deliveries then get signup.ErrBadSignature.
// Identities and contacts from that provider stay.
func (s *Service) DisconnectProvider(ctx context.Context, actor Actor, name string) error {
	if _, ok := signup.Lookup(name); !ok {
		return fmt.Errorf("%w: %q", ErrUnknownProvider, name)
	}
	_, err := s.db.Exec(ctx, `DELETE FROM provider_connections WHERE provider = $1`, name)
	return err
}
