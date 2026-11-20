package crm

import (
	"context"
	"strings"
	"testing"

	"github.com/unkeyed/keyring/internal/secretbox"
	"github.com/unkeyed/keyring/internal/signup"
)

func TestDailyLimitStopsEnrichment(t *testing.T) {
	ctx := context.Background()
	seed, pool := newService(t, nil)
	ingest(t, seed, clerk, created(clerk, "user_1", "ada@stripe.com"))
	ingest(t, seed, clerk, created(clerk, "user_2", "bob@linear.app"))

	w := New(pool, fakeEnricher{}, secretbox.ForTest())
	w.LimitEnrichment(2)
	for i := range 2 {
		if ran, err := w.enrichOnce(ctx); !ran || err != nil {
			t.Fatalf("call %d: ran %v, err %v", i+1, ran, err)
		}
	}
	if ran, err := w.enrichOnce(ctx); ran || err != nil {
		t.Fatalf("third call ran %v, err %v; want no claim past the daily limit", ran, err)
	}
	st, err := w.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.EnrichmentCallsToday != 2 || st.EnrichmentDailyLimit != 2 || st.Jobs[JobPending] == 0 {
		t.Fatalf("status %+v, want 2 of 2 calls used and jobs still pending", st)
	}

	w.LimitEnrichment(0)
	if ran, err := w.enrichOnce(ctx); !ran || err != nil {
		t.Fatalf("with no limit: ran %v, err %v", ran, err)
	}
}

func TestWebhookSecretsAreSealedAtRest(t *testing.T) {
	ctx := context.Background()
	s, pool := newService(t, nil)
	if _, err := s.ConnectProvider(ctx, human, "clerk", "whsec_c2VjcmV0"); err != nil {
		t.Fatal(err)
	}
	var stored string
	if err := pool.QueryRow(ctx, `SELECT secret FROM provider_connections WHERE provider = 'clerk'`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(stored, "enc:v1:") || strings.Contains(stored, "c2VjcmV0") {
		t.Fatalf("provider_connections.secret = %q, want it sealed", stored)
	}

	// A secret stored before encryption is sealed by SealProviderSecrets.
	if _, err := pool.Exec(ctx, `UPDATE provider_connections SET secret = 'whsec_c2VjcmV0' WHERE provider = 'clerk'`); err != nil {
		t.Fatal(err)
	}
	if n, err := s.SealProviderSecrets(ctx); n != 1 || err != nil {
		t.Fatalf("SealProviderSecrets = %d, %v; want 1", n, err)
	}
	// A value moved to another provider's row does not open there.
	if _, err := pool.Exec(ctx, `INSERT INTO provider_connections (provider, secret) SELECT 'workos', secret FROM provider_connections WHERE provider = 'clerk'`); err != nil {
		t.Fatal(err)
	}
	d := signup.Delivery{Body: []byte(`{}`)}
	if err := s.ReceiveWebhook(ctx, "workos", d); err == nil || strings.Contains(err.Error(), "bad signature") {
		t.Fatalf("delivery against a secret copied from another row: %v, want an error opening the secret", err)
	}
}
