package auth

import (
	"errors"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/unkeyed/keyring/internal/crm"
)

func TestRecoveryLinkAddsAPasskeyToTheAccount(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	oldPasskey, oldCookie := f.firstUser("ada@example.com")

	if _, err := IssueRecovery(f.ctx, f.pool, f.cfg.PublicURL, "nobody@example.com"); !errors.Is(err, crm.ErrNotFound) {
		t.Fatalf("recovery for an unknown email: %v, want ErrNotFound", err)
	}
	link, err := IssueRecovery(f.ctx, f.pool, f.cfg.PublicURL, " ADA@example.com ")
	f.must(err)
	if !strings.HasPrefix(link, testOrigin+"/setup/") {
		t.Fatalf("recovery link %q, want PUBLIC_URL/setup/<token>", link)
	}
	token := path.Base(link)

	// A boot, which clears stale setup links, keeps recovery links.
	_, err = f.users.PrepareSetup(f.ctx, "")
	f.must(err)
	if recovery, err := f.users.CheckSetupToken(f.ctx, token); err != nil || !recovery {
		t.Fatalf("CheckSetupToken(recovery link) = %v, %v; want a recovery link", recovery, err)
	}

	if _, _, err := f.signup(Signup{SetupToken: token, Email: "eve@example.com"}); !errors.Is(err, crm.ErrInvalid) {
		t.Fatalf("recovery link with another email: %v, want ErrInvalid", err)
	}
	newPasskey, newCookie, err := f.signup(Signup{SetupToken: token, Email: "ada@example.com"})
	f.must(err)

	if n := f.count(`SELECT count(*) FROM users`); n != 1 {
		t.Fatalf("%d users after recovery, want the one account", n)
	}
	if n := f.count(`SELECT count(*) FROM passkeys`); n != 2 {
		t.Fatalf("%d passkeys after recovery, want the old one and the new one", n)
	}
	if f.alive(oldCookie) || !f.alive(newCookie) {
		t.Fatalf("after recovery: old session alive %v, new session alive %v; want only the new one", f.alive(oldCookie), f.alive(newCookie))
	}
	if _, err := f.passkeyLogin(oldPasskey); err != nil {
		t.Fatalf("the old passkey stopped working after recovery: %v", err)
	}
	if _, err := f.passkeyLogin(newPasskey); err != nil {
		t.Fatalf("the recovered passkey does not sign in: %v", err)
	}

	if _, _, err := f.signup(Signup{SetupToken: token, Email: "ada@example.com"}); !errors.Is(err, crm.ErrNotFound) {
		t.Fatalf("reusing a recovery link: %v, want ErrNotFound", err)
	}

	expired, err := IssueRecovery(f.ctx, f.pool, f.cfg.PublicURL, "ada@example.com")
	f.must(err)
	f.exec(`UPDATE setup_tokens SET expires_at = now() - interval '1 second' WHERE user_id IS NOT NULL`)
	if _, err := f.users.CheckSetupToken(f.ctx, path.Base(expired)); !errors.Is(err, crm.ErrNotFound) {
		t.Fatalf("expired recovery link: %v, want ErrNotFound", err)
	}
}

func TestTOTPSeedsAreSealedAtRest(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	_, cookie := f.firstUser("ada@example.com")
	secret, _ := f.enrolTOTP(cookie)
	userID := f.current(cookie).UserID

	var stored string
	f.must(f.pool.QueryRow(f.ctx, `SELECT secret FROM totp WHERE user_id = $1`, userID).Scan(&stored))
	if !strings.HasPrefix(stored, "enc:v1:") || strings.Contains(stored, secret) {
		t.Fatalf("totp.secret = %q, want it sealed", stored)
	}

	// A seed stored before encryption is sealed on the next start and keeps
	// working.
	f.exec(`UPDATE totp SET secret = $2, last_step = 0 WHERE user_id = $1`, userID, secret)
	n, err := f.users.SealTOTPSecrets(f.ctx)
	if err != nil || n != 1 {
		t.Fatalf("SealTOTPSecrets = %d, %v; want 1 sealed", n, err)
	}
	if n, _ := f.users.SealTOTPSecrets(f.ctx); n != 0 {
		t.Fatalf("a second SealTOTPSecrets resealed %d seeds", n)
	}
	method, err := checkCode(f.ctx, f.pool, f.cfg.Secrets, userID, code(t, secret, time.Now()))
	if err != nil || method != MethodTOTP {
		t.Fatalf("code from the sealed seed: %q, %v", method, err)
	}
}
