package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/uncrm/uncrm/internal/crm"
	"github.com/uncrm/uncrm/internal/testdb"
)

func TestSeedAdminIsIdempotent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testdb.New(t)
	users := NewUsers(pool)

	if err := users.SeedAdmin(ctx, "", ""); err != nil {
		t.Fatalf("no ADMIN_EMAIL should be a no-op: %v", err)
	}
	for range 2 {
		if err := users.SeedAdmin(ctx, " Admin@Example.com ", "first-password"); err != nil {
			t.Fatal(err)
		}
	}
	if err := users.SeedAdmin(ctx, "admin@example.com", "changed-in-env"); err != nil {
		t.Fatal(err)
	}

	var n int
	var hash string
	if err := pool.QueryRow(ctx, `SELECT count(*), min(password_hash) FROM users WHERE email = 'admin@example.com'`).Scan(&n, &hash); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("%d admin rows after three boots, want 1", n)
	}
	if !strings.HasPrefix(hash, "pbkdf2-sha256$600000$") {
		t.Fatalf("stored hash %q is not in the documented format", hash)
	}
	if _, _, err := users.Login(ctx, "admin@example.com", "first-password"); err != nil {
		t.Fatalf("the first seeded password must keep working: %v", err)
	}
}

func TestSeedAdminRejectsShortPassword(t *testing.T) {
	t.Parallel()
	users := NewUsers(testdb.New(t))
	err := users.SeedAdmin(context.Background(), "admin@example.com", "elevenchars")
	if err == nil || !strings.Contains(err.Error(), "ADMIN_PASSWORD") {
		t.Fatalf("11-character ADMIN_PASSWORD gave %v, want an error naming ADMIN_PASSWORD", err)
	}
}

const adminPassword = "correct-horse-battery"

func seededUsers(t *testing.T) (*pgxpool.Pool, *Users) {
	t.Helper()
	pool := testdb.New(t)
	users := NewUsers(pool)
	if err := users.SeedAdmin(context.Background(), "admin@example.com", adminPassword); err != nil {
		t.Fatal(err)
	}
	return pool, users
}

func TestLoginAndSession(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, users := seededUsers(t)

	for _, bad := range []struct{ email, password string }{
		{"admin@example.com", "wrong-password-123"},
		{"nobody@example.com", adminPassword},
	} {
		if _, _, err := users.Login(ctx, bad.email, bad.password); !errors.Is(err, ErrUnauthorized) {
			t.Fatalf("login %s/%s: %v, want ErrUnauthorized", bad.email, bad.password, err)
		}
	}

	cookie, expires, err := users.Login(ctx, " ADMIN@example.com", adminPassword)
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Until(expires); d < 29*24*time.Hour || d > 31*24*time.Hour {
		t.Fatalf("session expires in %s, want 30 days", d)
	}
	actor, err := users.Session(ctx, cookie)
	if err != nil {
		t.Fatal(err)
	}
	if actor.Kind != crm.ActorUser || actor.Label != "admin@example.com" || actor.ID == "" {
		t.Fatalf("session actor %+v", actor)
	}
	for _, bad := range []string{"", "x", cookie + "x"} {
		if _, err := users.Session(ctx, bad); !errors.Is(err, ErrUnauthorized) {
			t.Fatalf("session %q: %v, want ErrUnauthorized", bad, err)
		}
	}

	if err := users.Logout(ctx, cookie); err != nil {
		t.Fatal(err)
	}
	if _, err := users.Session(ctx, cookie); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("session after logout: %v, want ErrUnauthorized", err)
	}
}

func TestSessionExpiresAndSlides(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool, users := seededUsers(t)
	cookie, _, err := users.Login(ctx, "admin@example.com", adminPassword)
	if err != nil {
		t.Fatal(err)
	}
	setExpiry := func(sql string) {
		t.Helper()
		if _, err := pool.Exec(ctx, `UPDATE sessions SET expires_at = `+sql+` WHERE token_hash = $1`, hashToken(cookie)); err != nil {
			t.Fatal(err)
		}
	}

	setExpiry(`now() + interval '1 day'`)
	if _, err := users.Session(ctx, cookie); err != nil {
		t.Fatalf("session with a day left: %v", err)
	}
	var left time.Duration
	if err := pool.QueryRow(ctx, `SELECT extract(epoch FROM expires_at - now())::bigint * 1000000000 FROM sessions WHERE token_hash = $1`,
		hashToken(cookie)).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left < 29*24*time.Hour {
		t.Fatalf("using a session with a day left extended it to %s, want 30 days", left)
	}

	setExpiry(`now() - interval '1 second'`)
	if _, err := users.Session(ctx, cookie); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expired session: %v, want ErrUnauthorized", err)
	}
}

func TestLoginThrottlesAcrossReplicas(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool, replicaA := seededUsers(t)
	replicaB := NewUsers(pool)

	for i := range 5 {
		replica := []*Users{replicaA, replicaB}[i%2]
		if _, _, err := replica.Login(ctx, "admin@example.com", "wrong-password-123"); !errors.Is(err, ErrUnauthorized) {
			t.Fatalf("failure %d: %v, want ErrUnauthorized", i+1, err)
		}
	}
	_, _, err := replicaA.Login(ctx, "admin@example.com", adminPassword)
	var throttled *ThrottledError
	if !errors.As(err, &throttled) || throttled.RetryAfter < time.Second {
		t.Fatalf("right password after 5 failures split across replicas: %v, want *ThrottledError", err)
	}

	if _, err := pool.Exec(ctx, `UPDATE login_failures SET last_failed_at = now() - interval '2 seconds'`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := replicaB.Login(ctx, "admin@example.com", adminPassword); err != nil {
		t.Fatalf("login after the backoff passed: %v", err)
	}
	for range 5 {
		if _, _, err := replicaA.Login(ctx, "admin@example.com", "wrong-password-123"); !errors.Is(err, ErrUnauthorized) {
			t.Fatalf("a success must reset the count, got %v", err)
		}
	}
}

func TestLoginThrottlesUnknownEmailsAlike(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, users := seededUsers(t)
	for range 5 {
		_, _, _ = users.Login(ctx, "nobody@example.com", "whatever-password")
	}
	_, _, err := users.Login(ctx, "nobody@example.com", "whatever-password")
	var throttled *ThrottledError
	if !errors.As(err, &throttled) {
		t.Fatalf("unknown email after 5 failures: %v, want *ThrottledError like a real account", err)
	}
}

func TestLoginWait(t *testing.T) {
	for _, c := range []struct {
		failures int
		since    time.Duration
		want     time.Duration
	}{
		{0, 0, 0},
		{4, 0, 0},
		{5, 0, time.Second},
		{5, 400 * time.Millisecond, 600 * time.Millisecond},
		{5, 2 * time.Second, 0},
		{6, 0, 2 * time.Second},
		{8, time.Second, 7 * time.Second},
		{40, 0, 15 * time.Minute},
		{1000, time.Minute, 14 * time.Minute},
		{1000, time.Hour, 0},
	} {
		if got := loginWait(c.failures, c.since); got != c.want {
			t.Errorf("loginWait(%d, %s) = %s, want %s", c.failures, c.since, got, c.want)
		}
	}
}

func TestInviteIsSingleUse(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool, users := seededUsers(t)
	adminCookie, _, err := users.Login(ctx, "admin@example.com", adminPassword)
	if err != nil {
		t.Fatal(err)
	}
	admin, err := users.Session(ctx, adminCookie)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := users.Invite(ctx, crm.Actor{Kind: crm.ActorAPIKey, ID: "1"}, "ada@example.com"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("invite by an API key: %v, want ErrUnauthorized", err)
	}
	if _, err := users.Invite(ctx, admin, "not-an-email"); !errors.Is(err, crm.ErrInvalid) {
		t.Fatalf("invite a non-email: %v, want crm.ErrInvalid", err)
	}
	if _, err := users.Invite(ctx, admin, "admin@example.com"); !errors.Is(err, crm.ErrConflict) {
		t.Fatalf("invite an existing user: %v, want crm.ErrConflict", err)
	}

	token, err := users.Invite(ctx, admin, " Ada@Example.com ")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := users.AcceptInvite(ctx, token, "Ada", "short"); !errors.Is(err, crm.ErrInvalid) {
		t.Fatalf("short password: %v, want crm.ErrInvalid", err)
	}

	results := make(chan error, 2)
	cookies := make(chan string, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			cookie, _, err := users.AcceptInvite(ctx, token, "Ada Lovelace", "analytical-engine")
			results <- err
			cookies <- cookie
		})
	}
	wg.Wait()
	close(results)
	close(cookies)
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
	for cookie := range cookies {
		if cookie == "" {
			continue
		}
		actor, err := users.Session(ctx, cookie)
		if err != nil || actor.Label != "Ada Lovelace" {
			t.Fatalf("new user's session: %+v, %v", actor, err)
		}
	}
	if _, _, err := users.Login(ctx, "ada@example.com", "analytical-engine"); err != nil {
		t.Fatalf("invited user cannot log in: %v", err)
	}

	list, err := users.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Email != "admin@example.com" || list[1].Email != "ada@example.com" {
		t.Fatalf("users %+v, want admin then ada", list)
	}

	expired, err := users.Invite(ctx, admin, "bob@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE invites SET expires_at = now() - interval '1 second' WHERE email = 'bob@example.com'`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := users.AcceptInvite(ctx, expired, "Bob", "long-enough-password"); !errors.Is(err, ErrInvalidInvite) {
		t.Fatalf("expired invite: %v, want ErrInvalidInvite", err)
	}
}

func TestChangePassword(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, users := seededUsers(t)
	cookie, _, err := users.Login(ctx, "admin@example.com", adminPassword)
	if err != nil {
		t.Fatal(err)
	}
	admin, err := users.Session(ctx, cookie)
	if err != nil {
		t.Fatal(err)
	}
	if err := users.ChangePassword(ctx, admin, "wrong-password-123", "a-brand-new-password"); !errors.Is(err, crm.ErrInvalid) {
		t.Fatalf("wrong current password: %v, want crm.ErrInvalid", err)
	}
	if err := users.ChangePassword(ctx, admin, adminPassword, "a-brand-new-password"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := users.Login(ctx, "admin@example.com", adminPassword); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("old password still works: %v", err)
	}
	if _, _, err := users.Login(ctx, "admin@example.com", "a-brand-new-password"); err != nil {
		t.Fatalf("new password: %v", err)
	}
}

func TestCaller(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool, users := seededUsers(t)
	authn := &Authenticator{Keys: NewKeyStore(pool, UnkeyConfig{}), Users: users}
	key, _, err := authn.Keys.Create(ctx, crm.Actor{}, "agent")
	if err != nil {
		t.Fatal(err)
	}
	cookie, _, err := users.Login(ctx, "admin@example.com", adminPassword)
	if err != nil {
		t.Fatal(err)
	}
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
		name   string
		https  bool
		secure bool
	}{{"plain http", false, false}, {"https via proxy", true, true}} {
		r := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
		if c.https {
			r.Header.Set("X-Forwarded-Proto", "https")
		}
		w := httptest.NewRecorder()
		SetSessionCookie(w, r, "token")
		got := w.Result().Cookies()
		if len(got) != 1 {
			t.Fatalf("%s: %d cookies", c.name, len(got))
		}
		ck := got[0]
		if ck.Name != SessionCookie || ck.Value != "token" || !ck.HttpOnly || ck.SameSite != http.SameSiteLaxMode || ck.Secure != c.secure || ck.MaxAge < 30*24*3600 {
			t.Errorf("%s: cookie %+v", c.name, ck)
		}
	}
}
