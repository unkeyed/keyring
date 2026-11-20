package auth

import (
	"context"
	"encoding/json"
	"net/url"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pquerna/otp/totp"
	"github.com/uncrm/uncrm/internal/authtest"
	"github.com/uncrm/uncrm/internal/crm"
	"github.com/uncrm/uncrm/internal/testdb"
)

const testOrigin = "http://localhost:8080"

var browser = Client{IP: "192.0.2.1", UserAgent: "Mozilla/5.0 test"}

type fixture struct {
	t     *testing.T
	ctx   context.Context
	pool  *pgxpool.Pool
	users *Users
	cfg   Config
}

func newFixture(t *testing.T, mod ...func(*Config)) *fixture {
	t.Helper()
	u, _ := url.Parse(testOrigin)
	cfg := Config{PublicURL: u, PasswordLogin: true}
	for _, m := range mod {
		m(&cfg)
	}
	pool := testdb.New(t)
	users, err := NewUsers(pool, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{t: t, ctx: context.Background(), pool: pool, users: users, cfg: cfg}
}

// replica is a second server on the same database.
func (f *fixture) replica() *Users {
	f.t.Helper()
	u, err := NewUsers(f.pool, f.cfg)
	if err != nil {
		f.t.Fatal(err)
	}
	return u
}

func (f *fixture) must(err error) {
	f.t.Helper()
	if err != nil {
		f.t.Fatal(err)
	}
}

func optionsJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// signup registers a passkey through a setup link or an invite and returns
// it with the new session's cookie.
func (f *fixture) signup(s Signup) (*authtest.Passkey, string, error) {
	f.t.Helper()
	handle, opts, err := f.users.BeginPasskeySignup(f.ctx, s)
	if err != nil {
		return nil, "", err
	}
	pk, cred, err := authtest.NewPasskey(testOrigin, optionsJSON(f.t, opts))
	if err != nil {
		f.t.Fatal(err)
	}
	in, err := f.users.FinishPasskeySignup(f.ctx, handle, cred, browser)
	return pk, in.Session, err
}

// firstUser runs setup with a passkey.
func (f *fixture) firstUser(email string) (*authtest.Passkey, string) {
	f.t.Helper()
	token, err := f.users.PrepareSetup(f.ctx, "")
	f.must(err)
	pk, cookie, err := f.signup(Signup{SetupToken: token, Email: email, Name: "First"})
	f.must(err)
	return pk, cookie
}

// teammate invites email and accepts with a passkey.
func (f *fixture) teammate(inviterCookie, email string) (*authtest.Passkey, string) {
	f.t.Helper()
	token, err := f.users.Invite(f.ctx, f.actor(inviterCookie), email)
	f.must(err)
	pk, cookie, err := f.signup(Signup{InviteToken: token, Name: "Mate"})
	f.must(err)
	return pk, cookie
}

func (f *fixture) passkeyLogin(pk *authtest.Passkey) (string, error) {
	f.t.Helper()
	handle, opts, err := f.users.BeginPasskeyLogin(f.ctx)
	f.must(err)
	cred, err := pk.Sign(optionsJSON(f.t, opts))
	f.must(err)
	in, err := f.users.FinishPasskeyLogin(f.ctx, handle, cred, browser)
	return in.Session, err
}

func (f *fixture) actor(cookie string) crm.Actor {
	f.t.Helper()
	a, err := f.users.Session(f.ctx, cookie)
	f.must(err)
	return a
}

func (f *fixture) current(cookie string) Current {
	f.t.Helper()
	c, err := f.users.Current(f.ctx, cookie)
	f.must(err)
	return c
}

func (f *fixture) alive(cookie string) bool {
	_, err := f.users.Session(f.ctx, cookie)
	return err == nil
}

func code(t *testing.T, secret string, at time.Time) string {
	t.Helper()
	c, err := totp.GenerateCodeCustom(secret, at, totpOpts)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// enrolTOTP sets up an authenticator app with a code for the current step
// and returns its secret and recovery codes.
func (f *fixture) enrolTOTP(cookie string) (string, []string) {
	f.t.Helper()
	cur := f.current(cookie)
	e, err := f.users.BeginTOTP(f.ctx, cur)
	f.must(err)
	codes, err := f.users.FinishTOTP(f.ctx, cur, e.Handle, code(f.t, e.Secret, time.Now()))
	f.must(err)
	return e.Secret, codes
}

func (f *fixture) count(sql string, args ...any) int {
	f.t.Helper()
	var n int
	f.must(f.pool.QueryRow(f.ctx, sql, args...).Scan(&n))
	return n
}

func (f *fixture) exec(sql string, args ...any) {
	f.t.Helper()
	_, err := f.pool.Exec(f.ctx, sql, args...)
	f.must(err)
}

// oauth runs a provider flow end to end against a fake issuer.
func (f *fixture) oauth(provider, intent, token string, cur *Current) (OAuthResult, error) {
	f.t.Helper()
	to, state, err := f.users.StartOAuth(f.ctx, OAuthStart{Provider: provider, Intent: intent, Token: token, Next: "/contacts"}, cur)
	if err != nil {
		return OAuthResult{}, err
	}
	cb, err := authtest.Follow(to)
	f.must(err)
	q := cb.Query()
	return f.users.FinishOAuth(f.ctx, OAuthCallback{Provider: provider, State: q.Get("state"), CookieState: state, Code: q.Get("code")}, cur, browser)
}

func newPasskey(options []byte) (*authtest.Passkey, json.RawMessage, error) {
	return authtest.NewPasskey(testOrigin, options)
}
