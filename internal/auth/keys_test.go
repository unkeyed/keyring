package auth

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/uncrm/uncrm/internal/crm"
	"github.com/uncrm/uncrm/internal/testdb"
)

func TestBuiltinKeyLifecycle(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testdb.New(t)
	keys := NewKeyStore(pool, UnkeyConfig{})
	if keys.Mode() != "builtin" {
		t.Fatalf("mode %q without Unkey config, want builtin", keys.Mode())
	}

	token, created, err := keys.Create(ctx, crm.Actor{Kind: crm.ActorSystem, ID: "test"}, "  zapier  ")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(token, "uncrm_") || len(token) < len("uncrm_")+50 {
		t.Fatalf("token %q is not uncrm_ + 32 encoded bytes", token)
	}
	if created.Name != "zapier" || created.Prefix != token[:10] {
		t.Fatalf("created %+v, want name zapier and prefix %q", created, token[:10])
	}
	if _, _, err := keys.Create(ctx, crm.Actor{}, " "); !errors.Is(err, crm.ErrInvalid) {
		t.Fatalf("blank name: %v, want crm.ErrInvalid", err)
	}

	actor, err := keys.Verify(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	want := crm.Actor{Kind: crm.ActorAPIKey, ID: created.ID, Label: "zapier"}
	if actor != want {
		t.Fatalf("verify gave %+v, want %+v", actor, want)
	}
	for _, bad := range []string{"", "uncrm_", token[:len(token)-1], token + "a", strings.ToUpper(token)} {
		if _, err := keys.Verify(ctx, bad); !errors.Is(err, ErrInvalidKey) {
			t.Fatalf("verify %q: %v, want ErrInvalidKey", bad, err)
		}
	}

	list, err := keys.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != created.ID || list[0].Prefix != created.Prefix || list[0].LastUsedAt == nil {
		t.Fatalf("list %+v, want the one key with last_used_at set by Verify", list)
	}

	if err := keys.Revoke(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
	if err := keys.Revoke(ctx, created.ID); err != nil {
		t.Fatalf("second revoke: %v, want idempotent success", err)
	}
	if _, err := keys.Verify(ctx, token); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("revoked key verified: %v", err)
	}
	if list, _ := keys.List(ctx); len(list) != 0 {
		t.Fatalf("list after revoke %+v, want empty", list)
	}
	for _, id := range []string{"999999", "not-a-number"} {
		if err := keys.Revoke(ctx, id); !errors.Is(err, crm.ErrNotFound) {
			t.Fatalf("revoke %q: %v, want crm.ErrNotFound", id, err)
		}
	}
}

func TestBuiltinVerifyWritesLastUsedAtMostOnceAMinute(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testdb.New(t)
	keys := NewKeyStore(pool, UnkeyConfig{})
	token, k, err := keys.Create(ctx, crm.Actor{}, "agent")
	if err != nil {
		t.Fatal(err)
	}

	lastUsed := func() (stamp string) {
		t.Helper()
		if err := pool.QueryRow(ctx, `SELECT coalesce(last_used_at::text, '') FROM api_keys WHERE id = $1::bigint`, k.ID).Scan(&stamp); err != nil {
			t.Fatal(err)
		}
		return stamp
	}
	if _, err := keys.Verify(ctx, token); err != nil {
		t.Fatal(err)
	}
	first := lastUsed()
	if first == "" {
		t.Fatal("first verify left last_used_at NULL")
	}
	if _, err := keys.Verify(ctx, token); err != nil {
		t.Fatal(err)
	}
	if again := lastUsed(); again != first {
		t.Fatalf("second verify within a minute rewrote last_used_at: %s -> %s", first, again)
	}

	if _, err := pool.Exec(ctx, `UPDATE api_keys SET last_used_at = now() - interval '2 minutes'`); err != nil {
		t.Fatal(err)
	}
	stale := lastUsed()
	if _, err := keys.Verify(ctx, token); err != nil {
		t.Fatal(err)
	}
	if lastUsed() == stale {
		t.Fatal("verify after a stale minute did not refresh last_used_at")
	}
}
