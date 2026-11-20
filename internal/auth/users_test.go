package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/uncrm/uncrm/internal/crm"
)

func TestSetupLinkIsSingleUse(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	open, err := f.users.SetupOpen(f.ctx)
	f.must(err)
	if !open {
		t.Fatal("an empty instance must be open for setup")
	}
	bootA, err := f.users.PrepareSetup(f.ctx, "")
	f.must(err)
	bootB, err := f.replica().PrepareSetup(f.ctx, "")
	f.must(err)
	if bootA == "" || bootB == "" || bootA == bootB {
		t.Fatalf("two boots gave setup tokens %q and %q, want two distinct tokens", bootA, bootB)
	}
	f.must(f.users.CheckSetupToken(f.ctx, bootA))

	if _, _, err := f.signup(Signup{SetupToken: "guessed-token", Email: "ada@example.com"}); !errors.Is(err, crm.ErrNotFound) {
		t.Fatalf("unknown setup token: %v, want ErrNotFound", err)
	}
	pk, cookie, err := f.signup(Signup{SetupToken: bootB, Email: " Ada@Example.com ", Name: "Ada"})
	f.must(err)
	if a := f.actor(cookie); a.Label != "Ada" {
		t.Fatalf("first user's session is %+v", a)
	}

	for _, token := range []string{bootA, bootB} {
		if _, _, err := f.signup(Signup{SetupToken: token, Email: "eve@example.com"}); !errors.Is(err, crm.ErrNotFound) {
			t.Fatalf("setup token after setup: %v, want ErrNotFound", err)
		}
		if err := f.users.CheckSetupToken(f.ctx, token); !errors.Is(err, crm.ErrNotFound) {
			t.Fatalf("CheckSetupToken after setup: %v", err)
		}
	}
	if again, err := f.users.PrepareSetup(f.ctx, ""); err != nil || again != "" {
		t.Fatalf("a boot after setup returned token %q, %v; want none", again, err)
	}
	if n := f.count(`SELECT count(*) FROM setup_tokens`); n != 0 {
		t.Fatalf("%d setup tokens left after setup", n)
	}
	if _, err := f.passkeyLogin(pk); err != nil {
		t.Fatalf("the setup passkey cannot sign in: %v", err)
	}
}

func TestSetupTokenFromEnvironment(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	fixed := strings.Repeat("s", MinSetupTokenLen)
	for range 2 {
		token, err := f.users.PrepareSetup(f.ctx, fixed)
		f.must(err)
		if token != fixed {
			t.Fatalf("PrepareSetup with SETUP_TOKEN returned %q", token)
		}
	}
	if _, _, err := f.signup(Signup{SetupToken: fixed, Email: "ada@example.com"}); err != nil {
		t.Fatal(err)
	}
}

func TestSetupClaimsAnUpgradedPasswordAccount(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	hash, err := hashPassword("legacy-password-123")
	f.must(err)
	f.exec(`INSERT INTO users (email, name, password_hash) VALUES ('admin@example.com', 'Admin', $1)`, hash)

	if _, err := f.users.PasswordLogin(f.ctx, "admin@example.com", "legacy-password-123", browser); !errors.Is(err, errNoSecondFactor) {
		t.Fatalf("legacy password without a second factor: %v, want errNoSecondFactor", err)
	}
	token, err := f.users.PrepareSetup(f.ctx, "")
	f.must(err)
	if token == "" {
		t.Fatal("an instance where nobody can sign in must offer setup, even with users")
	}
	_, cookie, err := f.signup(Signup{SetupToken: token, Email: "admin@example.com", Name: "Someone else"})
	f.must(err)
	if a := f.actor(cookie); a.Label != "Admin" {
		t.Fatalf("setup with an existing email signed in %+v, want the existing Admin account", a)
	}
	if n := f.count(`SELECT count(*) FROM users`); n != 1 {
		t.Fatalf("%d users after claiming the existing account, want 1", n)
	}
}

func TestPasskeySignInIsCompleteAlone(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	pk, cookie := f.firstUser("ada@example.com")
	f.enrolTOTP(cookie)

	session, err := f.passkeyLogin(pk)
	if err != nil || session == "" {
		t.Fatalf("passkey sign-in with an authenticator app enrolled: %q, %v; want a session", session, err)
	}
	var method string
	f.must(f.pool.QueryRow(f.ctx, `SELECT method FROM sessions WHERE token_hash = $1`, hashToken(session)).Scan(&method))
	if method != MethodPasskey {
		t.Fatalf("session method %q, want passkey", method)
	}

	stranger := *pk
	stranger.UserHandle = []byte("someone-else")
	if _, err := f.passkeyLogin(&stranger); err == nil {
		t.Fatal("a passkey with an unknown user handle signed in")
	}
}

func TestSessionExpiresAndSlides(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	_, cookie := f.firstUser("ada@example.com")

	f.exec(`UPDATE sessions SET expires_at = now() + interval '1 day', last_seen_at = now() - interval '1 hour' WHERE token_hash = $1`, hashToken(cookie))
	if !f.alive(cookie) {
		t.Fatal("session with a day left was rejected")
	}
	left := time.Duration(f.count(`SELECT extract(epoch FROM expires_at - now())::int FROM sessions WHERE token_hash = $1`, hashToken(cookie))) * time.Second
	if left < 29*24*time.Hour {
		t.Fatalf("using a session with a day left extended it to %s, want 30 days", left)
	}
	if n := f.count(`SELECT count(*) FROM sessions WHERE token_hash = $1 AND last_seen_at > now() - interval '1 minute'`, hashToken(cookie)); n != 1 {
		t.Fatal("using a session did not update last_seen_at")
	}

	f.exec(`UPDATE sessions SET expires_at = now() - interval '1 second' WHERE token_hash = $1`, hashToken(cookie))
	if _, err := f.users.Session(f.ctx, cookie); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expired session: %v, want ErrUnauthorized", err)
	}
}

func TestPasswordAloneNeverSignsIn(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	pk, cookie := f.firstUser("ada@example.com")
	cur := f.current(cookie)

	if err := f.users.SetPassword(f.ctx, cur, "correct-horse-battery"); err != nil {
		t.Fatalf("set a password with a passkey as second factor: %v", err)
	}
	in, err := f.users.PasswordLogin(f.ctx, "ada@example.com", "correct-horse-battery", browser)
	f.must(err)
	if in.Session != "" || in.Pending == "" || strings.Join(in.SecondFactors, ",") != MethodPasskey {
		t.Fatalf("password sign-in gave %+v, want a pending login asking for a passkey", in)
	}
	if _, err := f.users.SecondFactorCode(f.ctx, in.Pending, "123456", browser); !errors.Is(err, errBadCode) {
		t.Fatalf("a code for a user without an authenticator app: %v, want errBadCode", err)
	}

	handle, opts, err := f.users.BeginSecondFactorPasskey(f.ctx, in.Pending)
	f.must(err)
	cred, err := pk.Sign(optionsJSON(t, opts))
	f.must(err)
	done, err := f.users.FinishSecondFactorPasskey(f.ctx, in.Pending, handle, cred, browser)
	f.must(err)
	var method string
	f.must(f.pool.QueryRow(f.ctx, `SELECT method FROM sessions WHERE token_hash = $1`, hashToken(done.Session)).Scan(&method))
	if method != "password+passkey" {
		t.Fatalf("session method %q, want password+passkey", method)
	}
	if _, err := f.users.SecondFactorCode(f.ctx, in.Pending, "123456", browser); !errors.Is(err, errChallenge) {
		t.Fatalf("a completed pending login used again: %v, want errChallenge", err)
	}

	_, mateCookie := f.teammate(cookie, "bob@example.com")
	f.exec(`DELETE FROM passkeys WHERE user_id = $1`, f.current(mateCookie).UserID)
	hash, err := hashPassword("bobs-password-123")
	f.must(err)
	f.exec(`UPDATE users SET password_hash = $1 WHERE email = 'bob@example.com'`, hash)
	if _, err := f.users.PasswordLogin(f.ctx, "bob@example.com", "bobs-password-123", browser); !errors.Is(err, errNoSecondFactor) {
		t.Fatalf("password of a user with no second factor: %v, want errNoSecondFactor", err)
	}
	if err := f.users.SetPassword(f.ctx, f.current(mateCookie), "another-password-1"); !errors.Is(err, errNeedsSecondFactor) {
		t.Fatalf("set a password without a second factor: %v, want errNeedsSecondFactor", err)
	}
}

func TestPasswordLoginIsOffByDefault(t *testing.T) {
	t.Parallel()
	f := newFixture(t, func(c *Config) { c.PasswordLogin = false })
	_, cookie := f.firstUser("ada@example.com")
	if _, err := f.users.PasswordLogin(f.ctx, "ada@example.com", "anything-at-all", browser); !errors.Is(err, ErrPasswordLoginOff) {
		t.Fatalf("password sign-in with PASSWORD_LOGIN off: %v", err)
	}
	if err := f.users.SetPassword(f.ctx, f.current(cookie), "correct-horse-battery"); !errors.Is(err, ErrPasswordLoginOff) {
		t.Fatalf("set a password with PASSWORD_LOGIN off: %v", err)
	}
}

func TestTOTPRejectsReplay(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	_, cookie := f.firstUser("ada@example.com")
	f.must(f.users.SetPassword(f.ctx, f.current(cookie), "correct-horse-battery"))
	secret, _ := f.enrolTOTP(cookie)

	login := func() SignIn {
		t.Helper()
		in, err := f.users.PasswordLogin(f.ctx, "ada@example.com", "correct-horse-battery", browser)
		f.must(err)
		if in.Pending == "" || !strings.Contains(strings.Join(in.SecondFactors, ","), MethodTOTP) {
			t.Fatalf("password sign-in with an app enrolled gave %+v", in)
		}
		return in
	}

	enrolCode := code(t, secret, time.Now())
	in := login()
	if _, err := f.users.SecondFactorCode(f.ctx, in.Pending, enrolCode, browser); !errors.Is(err, errBadCode) {
		t.Fatalf("the code used to enrol, used again: %v, want errBadCode", err)
	}
	next := code(t, secret, time.Now().Add(totpPeriod*time.Second))
	done, err := f.users.SecondFactorCode(f.ctx, in.Pending, next, browser)
	if err != nil || done.Session == "" {
		t.Fatalf("a fresh code after a wrong one: %+v, %v", done, err)
	}
	if _, err := f.users.SecondFactorCode(f.ctx, login().Pending, next, browser); !errors.Is(err, errBadCode) {
		t.Fatalf("a code replayed on a second login: %v, want errBadCode", err)
	}
	far := code(t, secret, time.Now().Add(5*totpPeriod*time.Second))
	if _, err := f.users.SecondFactorCode(f.ctx, login().Pending, far, browser); !errors.Is(err, errBadCode) {
		t.Fatalf("a code five steps ahead: %v, want errBadCode", err)
	}
}

func TestRecoveryCodeWorksOnce(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	_, cookie := f.firstUser("ada@example.com")
	f.must(f.users.SetPassword(f.ctx, f.current(cookie), "correct-horse-battery"))
	_, codes := f.enrolTOTP(cookie)
	if len(codes) != recoveryCodeCount {
		t.Fatalf("%d recovery codes, want %d", len(codes), recoveryCodeCount)
	}
	if n := f.count(`SELECT count(*) FROM recovery_codes WHERE code_hash = $1`, hashToken(codes[0])); n != 0 {
		t.Fatal("recovery codes are stored in plain text")
	}

	pending := func() string {
		in, err := f.users.PasswordLogin(f.ctx, "ada@example.com", "correct-horse-battery", browser)
		f.must(err)
		return in.Pending
	}
	typed := strings.ToUpper(strings.ReplaceAll(codes[3], "-", " "))
	in, err := f.users.SecondFactorCode(f.ctx, pending(), typed, browser)
	if err != nil || in.Session == "" {
		t.Fatalf("recovery code typed in capitals with spaces: %+v, %v", in, err)
	}
	if _, err := f.users.SecondFactorCode(f.ctx, pending(), codes[3], browser); !errors.Is(err, errBadCode) {
		t.Fatalf("a used recovery code: %v, want errBadCode", err)
	}
	if n := f.count(`SELECT count(*) FROM recovery_codes`); n != recoveryCodeCount-1 {
		t.Fatalf("%d recovery codes left, want %d", n, recoveryCodeCount-1)
	}

	fresh, err := f.users.RegenerateRecoveryCodes(f.ctx, f.current(cookie))
	f.must(err)
	if _, err := f.users.SecondFactorCode(f.ctx, pending(), codes[4], browser); !errors.Is(err, errBadCode) {
		t.Fatalf("an old recovery code after regenerating: %v, want errBadCode", err)
	}
	if in, err := f.users.SecondFactorCode(f.ctx, pending(), fresh[0], browser); err != nil || in.Session == "" {
		t.Fatalf("a regenerated recovery code: %v", err)
	}
}

func TestThrottlePerEmailAcrossReplicas(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	_, cookie := f.firstUser("ada@example.com")
	f.must(f.users.SetPassword(f.ctx, f.current(cookie), "correct-horse-battery"))
	replicaA, replicaB := f.users, f.replica()

	for i := range 5 {
		client := Client{IP: "198.51.100." + string(rune('1'+i))}
		replica := []*Users{replicaA, replicaB}[i%2]
		if _, err := replica.PasswordLogin(f.ctx, "ada@example.com", "wrong-password-123", client); !errors.Is(err, ErrUnauthorized) {
			t.Fatalf("failure %d: %v, want ErrUnauthorized", i+1, err)
		}
	}
	_, err := replicaA.PasswordLogin(f.ctx, "ada@example.com", "correct-horse-battery", browser)
	var throttled *ThrottledError
	if !errors.As(err, &throttled) || throttled.RetryAfter < time.Second {
		t.Fatalf("right password after 5 failures from 5 IPs on 2 replicas: %v, want *ThrottledError", err)
	}

	f.exec(`UPDATE login_failures SET last_failed_at = now() - interval '2 seconds'`)
	if _, err := replicaB.PasswordLogin(f.ctx, "ada@example.com", "correct-horse-battery", browser); err != nil {
		t.Fatalf("sign-in after the backoff passed: %v", err)
	}
	for range 5 {
		if _, err := replicaA.PasswordLogin(f.ctx, "nobody@example.com", "whatever-password", Client{IP: "203.0.113.9"}); !errors.Is(err, ErrUnauthorized) {
			t.Fatalf("unknown email: %v", err)
		}
	}
	if _, err := replicaA.PasswordLogin(f.ctx, "nobody@example.com", "whatever-password", Client{IP: "203.0.113.10"}); !errors.As(err, &throttled) {
		t.Fatalf("unknown email after 5 failures: %v, want *ThrottledError like a real account", err)
	}
}

func TestThrottleHoldsUnderConcurrentGuesses(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	_, cookie := f.firstUser("ada@example.com")
	f.must(f.users.SetPassword(f.ctx, f.current(cookie), "correct-horse-battery"))
	replica := f.replica()
	results := make(chan error, 20)
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Go(func() {
			_, err := []*Users{f.users, replica}[i%2].PasswordLogin(f.ctx, "ada@example.com", "wrong-password-123", Client{IP: fmt.Sprintf("198.51.100.%d", i)})
			results <- err
		})
	}
	wg.Wait()
	close(results)
	var wrong, throttled int
	for err := range results {
		var te *ThrottledError
		switch {
		case errors.Is(err, ErrUnauthorized):
			wrong++
		case errors.As(err, &te):
			throttled++
		default:
			t.Fatalf("unexpected result %v", err)
		}
	}
	if wrong > freeAttempts["email"]+1 {
		t.Fatalf("20 concurrent guesses got %d password checks, want at most %d; the rest must wait", wrong, freeAttempts["email"]+1)
	}
}

func TestSetupReopensWhenNoMethodIsUsable(t *testing.T) {
	t.Parallel()
	f := newFixture(t, func(c *Config) { c.PasswordLogin = false })
	hash, err := hashPassword("legacy-password-123")
	f.must(err)
	f.exec(`INSERT INTO users (email, password_hash) VALUES ('ada@example.com', $1)`, hash)
	f.exec(`INSERT INTO user_identities (provider, subject, user_id, email) SELECT 'github', '7', id, email FROM users`)
	f.exec(`INSERT INTO totp (user_id, secret) SELECT id, 'JBSWY3DPEHPK3PXP' FROM users`)
	if open, err := f.users.SetupOpen(f.ctx); err != nil || !open {
		t.Fatalf("a GitHub login with GitHub unconfigured and a password with PASSWORD_LOGIN off: setup open = %v, %v; want true", open, err)
	}
	withGitHub := newFixture(t, func(c *Config) { c.Providers = []ProviderConfig{{ID: "github", Name: "GitHub"}} })
	withGitHub.exec(`INSERT INTO users (email) VALUES ('ada@example.com')`)
	withGitHub.exec(`INSERT INTO user_identities (provider, subject, user_id, email) SELECT 'github', '7', id, email FROM users`)
	if open, err := withGitHub.users.SetupOpen(withGitHub.ctx); err != nil || open {
		t.Fatalf("a GitHub login with GitHub configured: setup open = %v, %v; want false", open, err)
	}
}

func TestThrottlePerIP(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	_, cookie := f.firstUser("ada@example.com")
	f.must(f.users.SetPassword(f.ctx, f.current(cookie), "correct-horse-battery"))
	attacker := Client{IP: "203.0.113.7"}

	for i := range freeAttempts["ip"] {
		email := "guess" + string(rune('a'+i%26)) + string(rune('a'+i/26)) + "@example.com"
		if _, err := f.users.PasswordLogin(f.ctx, email, "wrong-password-123", attacker); !errors.Is(err, ErrUnauthorized) {
			t.Fatalf("guess %d: %v", i+1, err)
		}
	}
	var throttled *ThrottledError
	if _, err := f.users.PasswordLogin(f.ctx, "ada@example.com", "correct-horse-battery", attacker); !errors.As(err, &throttled) {
		t.Fatalf("an IP after %d failures across many emails: %v, want *ThrottledError", freeAttempts["ip"], err)
	}
	if _, err := f.users.PasswordLogin(f.ctx, "ada@example.com", "correct-horse-battery", browser); err != nil {
		t.Fatalf("another IP is not throttled: %v", err)
	}
}

func TestSecondFactorThrottlesPerUser(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	_, cookie := f.firstUser("ada@example.com")
	f.must(f.users.SetPassword(f.ctx, f.current(cookie), "correct-horse-battery"))
	secret, _ := f.enrolTOTP(cookie)
	in, err := f.users.PasswordLogin(f.ctx, "ada@example.com", "correct-horse-battery", browser)
	f.must(err)
	for i := range 5 {
		if _, err := f.users.SecondFactorCode(f.ctx, in.Pending, "000000", Client{IP: "198.51.100." + string(rune('1'+i))}); !errors.Is(err, errBadCode) {
			t.Fatalf("wrong code %d: %v", i+1, err)
		}
	}
	var throttled *ThrottledError
	right := code(t, secret, time.Now().Add(totpPeriod*time.Second))
	if _, err := f.users.SecondFactorCode(f.ctx, in.Pending, right, browser); !errors.As(err, &throttled) {
		t.Fatalf("the right code after 5 wrong ones from 5 IPs: %v, want *ThrottledError", err)
	}
}

func TestLoginWait(t *testing.T) {
	for _, c := range []struct {
		free, failures int
		since          time.Duration
		want           time.Duration
	}{
		{5, 0, 0, 0},
		{5, 4, 0, 0},
		{5, 5, 0, time.Second},
		{5, 5, 400 * time.Millisecond, 600 * time.Millisecond},
		{5, 5, 2 * time.Second, 0},
		{5, 6, 0, 2 * time.Second},
		{5, 8, time.Second, 7 * time.Second},
		{5, 40, 0, 15 * time.Minute},
		{5, 1000, time.Minute, 14 * time.Minute},
		{5, 1000, time.Hour, 0},
		{30, 29, 0, 0},
		{30, 30, 0, time.Second},
	} {
		if got := loginWait(c.free, c.failures, c.since); got != c.want {
			t.Errorf("loginWait(%d, %d, %s) = %s, want %s", c.free, c.failures, c.since, got, c.want)
		}
	}
}

func TestClientOf(t *testing.T) {
	trusting := &Users{cfg: Config{ClientIPHeader: "X-Forwarded-For"}}
	plain := &Users{}
	for _, c := range []struct {
		name   string
		users  *Users
		remote string
		xff    []string
		want   string
	}{
		{"peer address", plain, "192.0.2.1:5555", nil, "192.0.2.1"},
		{"header ignored unless trusted", plain, "192.0.2.1:5555", []string{"10.9.9.9"}, "192.0.2.1"},
		{"rightmost entry of a trusted header", trusting, "10.0.0.2:80", []string{"6.6.6.6, 198.51.100.4"}, "198.51.100.4"},
		{"last header line wins", trusting, "10.0.0.2:80", []string{"6.6.6.6", "198.51.100.5"}, "198.51.100.5"},
		{"trusted header absent", trusting, "192.0.2.3:1", nil, "192.0.2.3"},
		{"IPv6 by /64", plain, "[2001:db8:1:2:3:4:5:6]:443", nil, "2001:db8:1:2::/64"},
		{"IPv4-mapped IPv6", plain, "[::ffff:192.0.2.8]:443", nil, "192.0.2.8"},
	} {
		r := httptest.NewRequest(http.MethodPost, "/auth/password", nil)
		r.RemoteAddr = c.remote
		for _, v := range c.xff {
			r.Header.Add("X-Forwarded-For", v)
		}
		if got := c.users.ClientOf(r).IP; got != c.want {
			t.Errorf("%s: IP %q, want %q", c.name, got, c.want)
		}
	}
}

func TestInviteIsSingleUse(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	_, adminCookie := f.firstUser("admin@example.com")
	admin := f.actor(adminCookie)

	if _, err := f.users.Invite(f.ctx, crm.Actor{Kind: crm.ActorAPIKey, ID: "1"}, "ada@example.com"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("invite by an API key: %v, want ErrUnauthorized", err)
	}
	if _, err := f.users.Invite(f.ctx, admin, "not-an-email"); !errors.Is(err, crm.ErrInvalid) {
		t.Fatalf("invite a non-email: %v, want crm.ErrInvalid", err)
	}
	if _, err := f.users.Invite(f.ctx, admin, "admin@example.com"); !errors.Is(err, crm.ErrConflict) {
		t.Fatalf("invite an existing user: %v, want crm.ErrConflict", err)
	}
	token, err := f.users.Invite(f.ctx, admin, " Ada@Example.com ")
	f.must(err)
	if email, err := f.users.InviteEmail(f.ctx, token); err != nil || email != "ada@example.com" {
		t.Fatalf("InviteEmail = %q, %v", email, err)
	}

	type begun struct {
		handle string
		opts   []byte
	}
	var ceremonies []begun
	for range 2 {
		h, opts, err := f.users.BeginPasskeySignup(f.ctx, Signup{InviteToken: token, Name: "Ada Lovelace"})
		f.must(err)
		ceremonies = append(ceremonies, begun{h, optionsJSON(t, opts)})
	}
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, c := range ceremonies {
		wg.Go(func() {
			_, cred, err := newPasskey(c.opts)
			if err != nil {
				results <- err
				return
			}
			_, err = f.users.FinishPasskeySignup(f.ctx, c.handle, cred, browser)
			results <- err
		})
	}
	wg.Wait()
	close(results)
	var ok, rejected int
	for err := range results {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, ErrInvalidInvite):
			rejected++
		default:
			t.Fatal(err)
		}
	}
	if ok != 1 || rejected != 1 {
		t.Fatalf("two concurrent accepts: %d succeeded, %d rejected; want 1 and 1", ok, rejected)
	}
	list, err := f.users.List(f.ctx)
	f.must(err)
	if len(list) != 2 || list[1].Email != "ada@example.com" || list[1].Name != "Ada Lovelace" {
		t.Fatalf("users %+v, want admin then ada", list)
	}

	expired, err := f.users.Invite(f.ctx, admin, "bob@example.com")
	f.must(err)
	f.exec(`UPDATE invites SET expires_at = now() - interval '1 second' WHERE email = 'bob@example.com'`)
	if _, _, err := f.signup(Signup{InviteToken: expired, Name: "Bob"}); !errors.Is(err, ErrInvalidInvite) {
		t.Fatalf("expired invite: %v, want ErrInvalidInvite", err)
	}
}

func TestRemoveMember(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	_, adminCookie := f.firstUser("admin@example.com")
	admin := f.actor(adminCookie)
	_, mateCookie := f.teammate(adminCookie, "bob@example.com")
	mate := f.current(mateCookie)
	f.enrolTOTP(mateCookie)
	keys := NewKeyStore(f.pool, UnkeyConfig{})
	adminKey, _, err := keys.Create(f.ctx, admin, "admin's agent")
	f.must(err)
	mateKey, _, err := keys.Create(f.ctx, f.actor(mateCookie), "bob's agent")
	f.must(err)

	f.must(f.users.Remove(f.ctx, admin, mate.UserID, keys))
	if f.alive(mateCookie) {
		t.Fatal("a removed teammate's session still works")
	}
	if _, err := keys.Verify(f.ctx, mateKey); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("a removed teammate's API key: %v, want ErrInvalidKey", err)
	}
	for _, table := range []string{"sessions", "passkeys", "user_identities", "totp", "recovery_codes"} {
		if n := f.count(`SELECT count(*) FROM `+table+` WHERE user_id = $1`, mate.UserID); n != 0 {
			t.Errorf("%d %s rows left for the removed teammate", n, table)
		}
	}
	if err := f.users.Remove(f.ctx, admin, f.current(adminCookie).UserID, keys); !errors.Is(err, crm.ErrConflict) {
		t.Fatalf("removing the last user: %v, want crm.ErrConflict", err)
	}
	if err := f.users.Remove(f.ctx, admin, 999999, keys); !errors.Is(err, crm.ErrNotFound) {
		t.Fatalf("removing an unknown user: %v, want crm.ErrNotFound", err)
	}
	if _, err := keys.Verify(f.ctx, adminKey); err != nil {
		t.Fatalf("the remaining user's key after removals: %v", err)
	}
}

// failingKeys cannot revoke, like Unkey during an outage.
type failingKeys struct{ KeyStore }

func (failingKeys) RevokeCreatedBy(context.Context, pgx.Tx, int64) error {
	return errors.New("unkey keys.deleteKey: HTTP 503")
}

func TestRemoveKeepsTheTeammateWhenTheirKeysStay(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	_, adminCookie := f.firstUser("admin@example.com")
	_, mateCookie := f.teammate(adminCookie, "bob@example.com")

	if err := f.users.Remove(f.ctx, f.actor(adminCookie), f.current(mateCookie).UserID, failingKeys{}); err == nil {
		t.Fatal("removal succeeded although the key store could not revoke the teammate's keys")
	}
	if !f.alive(mateCookie) {
		t.Fatal("a removal that failed still ended the teammate's session; a retry could no longer find their keys")
	}
}

func TestChangingSignInMethodsEndsOtherSessions(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name   string
		change func(f *fixture, cur Current, pk string) error
	}{
		{"set password", func(f *fixture, cur Current, _ string) error {
			return f.users.SetPassword(f.ctx, cur, "a-new-password-12")
		}},
		{"remove password", func(f *fixture, cur Current, _ string) error { return f.users.RemovePassword(f.ctx, cur) }},
		{"disable two-factor", func(f *fixture, cur Current, _ string) error { return f.users.DisableTOTP(f.ctx, cur) }},
		{"remove passkey", func(f *fixture, cur Current, pk string) error { return f.users.RemovePasskey(f.ctx, cur, pk) }},
		{"unlink identity", func(f *fixture, cur Current, _ string) error { return f.users.Unlink(f.ctx, cur, "oidc") }},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t, func(cfg *Config) {
				cfg.Providers = []ProviderConfig{{ID: "oidc", Name: "SSO", Issuer: "http://127.0.0.1:1"}}
			})
			pk, here := f.firstUser("ada@example.com")
			cur := f.current(here)
			f.must(f.users.SetPassword(f.ctx, cur, "correct-horse-battery"))
			f.enrolTOTP(here)
			f.exec(`INSERT INTO user_identities (provider, subject, user_id, email) VALUES ('oidc', 'sub-1', $1, 'ada@example.com')`, cur.UserID)
			_, second := f.teammate(here, "bob@example.com")
			elsewhere, err := f.passkeyLogin(pk)
			f.must(err)
			passkeyID := f.mustAccount(cur).Passkeys[0].ID
			if c.name == "remove passkey" {
				_, _, err := f.addPasskey(cur)
				f.must(err)
			}

			f.must(c.change(f, cur, passkeyID))
			if f.alive(elsewhere) {
				t.Error("another session of the user survived")
			}
			if !f.alive(here) {
				t.Error("the session that made the change was ended")
			}
			if !f.alive(second) {
				t.Error("a teammate's session was ended")
			}
		})
	}
}

func (f *fixture) mustAccount(cur Current) Account {
	f.t.Helper()
	a, err := f.users.Account(f.ctx, cur)
	f.must(err)
	return a
}

func (f *fixture) addPasskey(cur Current) (Passkey, []byte, error) {
	f.t.Helper()
	handle, opts, err := f.users.BeginAddPasskey(f.ctx, cur)
	if err != nil {
		return Passkey{}, nil, err
	}
	_, cred, err := newPasskey(optionsJSON(f.t, opts))
	f.must(err)
	p, err := f.users.FinishAddPasskey(f.ctx, cur, handle, cred, "Laptop")
	return p, cred, err
}

func TestChangesNeedARecentSignIn(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	_, cookie := f.firstUser("ada@example.com")
	f.exec(`UPDATE sessions SET authenticated_at = now() - interval '16 minutes'`)
	cur := f.current(cookie)
	if cur.Recent {
		t.Fatal("a session that signed in 16 minutes ago counts as recent")
	}
	account := f.mustAccount(cur)
	for name, err := range map[string]error{
		"remove passkey":  f.users.RemovePasskey(f.ctx, cur, account.Passkeys[0].ID),
		"set password":    f.users.SetPassword(f.ctx, cur, "correct-horse-battery"),
		"remove password": f.users.RemovePassword(f.ctx, cur),
		"disable TOTP":    f.users.DisableTOTP(f.ctx, cur),
		"unlink":          f.users.Unlink(f.ctx, cur, "google"),
	} {
		if !errors.Is(err, ErrReauth) {
			t.Errorf("%s on a stale session: %v, want ErrReauth", name, err)
		}
	}
	if _, err := f.users.BeginTOTP(f.ctx, cur); !errors.Is(err, ErrReauth) {
		t.Errorf("BeginTOTP on a stale session: %v", err)
	}
	if _, _, err := f.users.BeginAddPasskey(f.ctx, cur); !errors.Is(err, ErrReauth) {
		t.Errorf("BeginAddPasskey on a stale session: %v", err)
	}
}

func TestReauthenticate(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	pk, cookie := f.firstUser("ada@example.com")
	secret, _ := f.enrolTOTP(cookie)
	stale := func() Current {
		f.exec(`UPDATE sessions SET authenticated_at = now() - interval '1 hour'`)
		return f.current(cookie)
	}

	cur := stale()
	handle, opts, err := f.users.BeginReauthPasskey(f.ctx, cur)
	f.must(err)
	cred, err := pk.Sign(optionsJSON(t, opts))
	f.must(err)
	f.must(f.users.FinishReauthPasskey(f.ctx, cur, handle, cred))
	if !f.current(cookie).Recent {
		t.Fatal("a passkey re-verification did not make the session recent")
	}

	cur = stale()
	if err := f.users.ReauthCode(f.ctx, cur, "000000", browser); !errors.Is(err, errBadCode) {
		t.Fatalf("wrong re-verification code: %v", err)
	}
	f.must(f.users.ReauthCode(f.ctx, cur, code(t, secret, time.Now().Add(totpPeriod*time.Second)), browser))
	if !f.current(cookie).Recent {
		t.Fatal("a code re-verification did not make the session recent")
	}
}

func TestLastSignInMethodStays(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	_, cookie := f.firstUser("ada@example.com")
	cur := f.current(cookie)
	f.must(f.users.SetPassword(f.ctx, cur, "correct-horse-battery"))
	only := f.mustAccount(cur).Passkeys[0].ID

	if err := f.users.RemovePasskey(f.ctx, cur, only); !errors.Is(err, errLastMethod) {
		t.Fatalf("removing the only passkey, with a password but no app: %v, want errLastMethod", err)
	}
	f.enrolTOTP(cookie)
	if err := f.users.RemovePasskey(f.ctx, cur, only); err != nil {
		t.Fatalf("removing the passkey while password and app remain: %v", err)
	}
	if err := f.users.DisableTOTP(f.ctx, cur); !errors.Is(err, errLastMethod) {
		t.Fatalf("disabling the app that makes the password usable: %v, want errLastMethod", err)
	}
	if err := f.users.RemovePassword(f.ctx, cur); !errors.Is(err, errLastMethod) {
		t.Fatalf("removing the last password: %v, want errLastMethod", err)
	}
}

func TestSessionsListAndRevoke(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	pk, here := f.firstUser("ada@example.com")
	other, err := f.passkeyLogin(pk)
	f.must(err)
	cur := f.current(here)
	list, err := f.users.Sessions(f.ctx, cur)
	f.must(err)
	if len(list) != 2 {
		t.Fatalf("%d sessions, want 2", len(list))
	}
	var otherID int64
	for _, s := range list {
		if s.UserAgent != browser.UserAgent || s.Method != MethodPasskey {
			t.Errorf("session %+v lacks its user agent or method", s)
		}
		if !s.Current {
			otherID = s.ID
		}
	}
	f.must(f.users.RevokeSession(f.ctx, cur, otherID))
	if f.alive(other) || !f.alive(here) {
		t.Fatal("revoking one session ended the wrong one")
	}
	_, mate := f.teammate(here, "bob@example.com")
	if err := f.users.RevokeSession(f.ctx, f.current(mate), cur.SessionID); !errors.Is(err, crm.ErrNotFound) {
		t.Fatalf("revoking someone else's session: %v, want ErrNotFound", err)
	}
}

func TestCaller(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	_, cookie := f.firstUser("ada@example.com")
	authn := &Authenticator{Keys: NewKeyStore(f.pool, UnkeyConfig{}), Users: f.users}
	key, _, err := authn.Keys.Create(f.ctx, crm.Actor{}, "agent")
	f.must(err)
	request := func(authorization, session string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
		if authorization != "" {
			r.Header.Set("Authorization", authorization)
		}
		if session != "" {
			r.AddCookie(&http.Cookie{Name: SessionCookie, Value: session})
		}
		return r
	}

	if a, via, err := authn.Caller(request("bearer "+key, "")); err != nil || via != ViaAPIKey || a.Kind != crm.ActorAPIKey {
		t.Fatalf("bearer key: %+v %v %v", a, via, err)
	}
	if a, via, err := authn.Caller(request("", cookie)); err != nil || via != ViaSession || a.Kind != crm.ActorUser {
		t.Fatalf("session cookie: %+v %v %v", a, via, err)
	}
	for name, r := range map[string]*http.Request{
		"nothing":                    request("", ""),
		"bad key beside good cookie": request("Bearer uncrm_wrong", cookie),
		"basic auth":                 request("Basic "+key, ""),
		"bad cookie":                 request("", "nope"),
		"empty bearer beside cookie": request("Bearer ", cookie),
	} {
		_, _, err := authn.Caller(r)
		if !errors.Is(err, ErrUnauthorized) && !errors.Is(err, ErrInvalidKey) {
			t.Errorf("%s: %v, want ErrUnauthorized or ErrInvalidKey", name, err)
		}
	}
}

func TestSessionCookieAttributes(t *testing.T) {
	for _, c := range []struct {
		publicURL string
		name      string
		secure    bool
	}{{"http://localhost:8080", "uncrm_session", false}, {"https://crm.example.com", "__Host-uncrm_session", true}} {
		u, _ := url.Parse(c.publicURL)
		users := &Users{cfg: Config{PublicURL: u}}
		w := httptest.NewRecorder()
		users.SetSessionCookie(w, "token")
		users.SetPendingCookie(w, "pending")
		users.SetOAuthCookie(w, "state", "/setup/x")
		got := w.Result().Cookies()
		if len(got) != 4 {
			t.Fatalf("%s: %d cookies, want 4", c.publicURL, len(got))
		}
		ck := got[0]
		if ck.Name != c.name || ck.Value != "token" || !ck.HttpOnly || ck.SameSite != http.SameSiteLaxMode || ck.Secure != c.secure || ck.MaxAge < 30*24*3600 {
			t.Errorf("%s: cookie %+v", c.publicURL, ck)
		}
		for _, ck := range got {
			if c.secure && (!strings.HasPrefix(ck.Name, "__Host-") || ck.Path != "/" || ck.Domain != "" || !ck.Secure) {
				t.Errorf("%s: cookie %s with Path %q, Domain %q, Secure %v breaks the __Host- rules", c.publicURL, ck.Name, ck.Path, ck.Domain, ck.Secure)
			}
		}
	}
}

func TestHTTPSIgnoresAnUnprefixedSessionCookie(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	_, session := f.firstUser("ada@example.com")
	cfg := f.cfg
	cfg.PublicURL, _ = url.Parse("https://crm.example.com")
	users, err := NewUsers(f.pool, cfg)
	f.must(err)
	authn := &Authenticator{Keys: NewKeyStore(f.pool, UnkeyConfig{}), Users: users}
	withCookie := func(name string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
		r.AddCookie(&http.Cookie{Name: name, Value: session})
		return r
	}
	if _, _, err := authn.Caller(withCookie("uncrm_session")); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("an unprefixed session cookie over https, as a sibling subdomain could plant: %v, want ErrUnauthorized", err)
	}
	if _, via, err := authn.Caller(withCookie("__Host-uncrm_session")); err != nil || via != ViaSession {
		t.Fatalf("the __Host- session cookie over https: %v", err)
	}
}
