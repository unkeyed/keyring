package auth

import (
	"errors"
	"strings"
	"testing"

	"github.com/uncrm/uncrm/internal/authtest"
	"github.com/uncrm/uncrm/internal/crm"
)

func TestDecideSocial(t *testing.T) {
	verified := func(email string) ExternalIdentity {
		return ExternalIdentity{Provider: "oidc", Subject: "s", Email: email, EmailVerified: true}
	}
	allowed := []string{"acme.com"}
	for _, c := range []struct {
		name  string
		facts socialFacts
		want  socialAction
		err   error
	}{
		{"linked identity signs in even unverified", socialFacts{ident: ExternalIdentity{Provider: "oidc", Subject: "s"}, linkedUser: 7}, socialSignIn, nil},
		{"unverified email is refused", socialFacts{ident: ExternalIdentity{Provider: "oidc", Email: "ada@acme.com"}, emailUser: 3}, 0, errUnverifiedEmail},
		{"verified email of a user links", socialFacts{ident: verified("ada@acme.com"), emailUser: 3}, socialLink, nil},
		{"verified email with an invite", socialFacts{ident: verified("bob@other.com"), invited: true}, socialAcceptInvite, nil},
		{"allowed domain joins", socialFacts{ident: verified("eve@acme.com")}, socialJoinDomain, nil},
		{"google needs a matching hd", socialFacts{ident: ExternalIdentity{Provider: "google", Email: "eve@acme.com", EmailVerified: true}}, 0, errStranger},
		{"google with hd joins", socialFacts{ident: ExternalIdentity{Provider: "google", Email: "eve@acme.com", EmailVerified: true, HostedDomain: "acme.com"}}, socialJoinDomain, nil},
		{"stranger is refused", socialFacts{ident: verified("mallory@evil.com")}, 0, errStranger},
		{"personal google account with a company address", socialFacts{ident: ExternalIdentity{Provider: "google", Email: "ada@acme.com", EmailVerified: true}, emailUser: 3}, 0, errNotWorkspace},
		{"google workspace account links", socialFacts{ident: ExternalIdentity{Provider: "google", Email: "ada@acme.com", EmailVerified: true, HostedDomain: "acme.com"}, emailUser: 3}, socialLink, nil},
		{"personal google account outside the allowed domains links", socialFacts{ident: ExternalIdentity{Provider: "google", Email: "ada@gmail.com", EmailVerified: true}, emailUser: 3}, socialLink, nil},
	} {
		got, err := decideSocial(c.facts, allowed)
		if got != c.want || !errors.Is(err, c.err) {
			t.Errorf("%s: got %v, %v; want %v, %v", c.name, got, err, c.want, c.err)
		}
	}
}

func socialFixture(t *testing.T, iss *authtest.Issuer, domains ...string) *fixture {
	return newFixture(t, func(c *Config) {
		c.AllowedEmailDomains = domains
		c.Providers = []ProviderConfig{
			{ID: "oidc", Name: "Acme SSO", ClientID: "uncrm", ClientSecret: "s3cret", Issuer: iss.URL},
			{ID: "google", Name: "Google", ClientID: "uncrm-google", ClientSecret: "s3cret", Issuer: iss.URL},
			{ID: "github", Name: "GitHub", ClientID: "uncrm-gh", ClientSecret: "s3cret", GitHubURL: iss.URL, GitHubAPIURL: iss.URL + "/api"},
		}
	})
}

func TestSocialSignInAgainstFakeIssuer(t *testing.T) {
	t.Parallel()
	iss := authtest.NewIssuer()
	defer iss.Close()
	f := socialFixture(t, iss, "acme.com")
	_, adminCookie := f.firstUser("ada@example.com")

	iss.SetNext(authtest.Identity{Subject: "ada-sso", Email: "ADA@example.com", EmailVerified: false})
	if _, err := f.oauth("oidc", IntentLogin, "", nil); !errors.Is(err, errUnverifiedEmail) {
		t.Fatalf("unverified email matching a user: %v, want errUnverifiedEmail", err)
	}

	iss.SetNext(authtest.Identity{Subject: "ada-sso", Email: "ADA@example.com", EmailVerified: true, Name: "Ada"})
	res, err := f.oauth("oidc", IntentLogin, "", nil)
	f.must(err)
	if res.SignIn.Session == "" || res.Redirect != "/contacts" {
		t.Fatalf("verified email of an existing user: %+v, want a session landing on next", res)
	}
	if f.actor(res.SignIn.Session).ID != f.actor(adminCookie).ID {
		t.Fatal("social sign-in signed in someone other than the matching user")
	}

	iss.SetNext(authtest.Identity{Subject: "ada-sso", Email: "ada.new@elsewhere.com", EmailVerified: false})
	if res, err := f.oauth("oidc", IntentLogin, "", nil); err != nil || f.actor(res.SignIn.Session).ID != f.actor(adminCookie).ID {
		t.Fatalf("a linked identity whose email changed: %v; it must still sign in its user", err)
	}

	iss.SetNext(authtest.Identity{Subject: "mallory", Email: "mallory@evil.com", EmailVerified: true})
	if _, err := f.oauth("oidc", IntentLogin, "", nil); !errors.Is(err, errStranger) {
		t.Fatalf("stranger: %v, want errStranger", err)
	}

	iss.SetNext(authtest.Identity{Subject: "eve", Email: "eve@acme.com", EmailVerified: true, Name: "Eve"})
	if res, err := f.oauth("oidc", IntentLogin, "", nil); err != nil || f.actor(res.SignIn.Session).Label != "Eve" {
		t.Fatalf("allowed domain: %v, want a new user Eve", err)
	}

	iss.SetNext(authtest.Identity{Subject: "g-1", Email: "gus@acme.com", EmailVerified: true})
	if _, err := f.oauth("google", IntentLogin, "", nil); !errors.Is(err, errStranger) {
		t.Fatalf("Google account at an allowed domain without hd: %v, want errStranger", err)
	}
	iss.SetNext(authtest.Identity{Subject: "g-1", Email: "gus@acme.com", EmailVerified: true, HostedDomain: "acme.com"})
	if _, err := f.oauth("google", IntentLogin, "", nil); err != nil {
		t.Fatalf("Google Workspace account at an allowed domain: %v", err)
	}

	_, err = f.users.Invite(f.ctx, f.actor(adminCookie), "bob@other.com")
	f.must(err)
	iss.SetNext(authtest.Identity{Subject: "bob", Email: "bob@other.com", EmailVerified: true, Name: "Bob"})
	if res, err := f.oauth("oidc", IntentLogin, "", nil); err != nil || f.actor(res.SignIn.Session).Label != "Bob" {
		t.Fatalf("pending invite for the verified email: %v", err)
	}
	if n := f.count(`SELECT count(*) FROM invites WHERE email = 'bob@other.com' AND accepted_at IS NULL`); n != 0 {
		t.Fatal("social sign-in left the invite it accepted open")
	}
}

func TestSocialSignInAsksForTheAuthenticatorCode(t *testing.T) {
	t.Parallel()
	iss := authtest.NewIssuer()
	defer iss.Close()
	f := socialFixture(t, iss)
	_, cookie := f.firstUser("ada@example.com")
	_, codes := f.enrolTOTP(cookie)

	iss.SetNext(authtest.Identity{Subject: "ada", Email: "ada@example.com", EmailVerified: true})
	res, err := f.oauth("oidc", IntentLogin, "", nil)
	f.must(err)
	if res.SignIn.Session != "" || res.SignIn.Pending == "" || !strings.HasPrefix(res.Redirect, "/login?second_factor=1") {
		t.Fatalf("social sign-in with an app enrolled: %+v, want a pending login", res)
	}
	if got := strings.Join(res.SignIn.SecondFactors, ","); got != "totp,recovery_code" {
		t.Fatalf("second factors %q, want totp,recovery_code", got)
	}
	if n := f.count(`SELECT count(*) FROM user_identities`); n != 0 {
		t.Fatal("the provider login was linked before the second factor passed")
	}
	if _, err := f.users.SecondFactorCode(f.ctx, res.SignIn.Pending, "000000", browser); !errors.Is(err, errBadCode) {
		t.Fatalf("wrong code: %v", err)
	}
	if n := f.count(`SELECT count(*) FROM user_identities`); n != 0 {
		t.Fatal("a failed code left the provider login linked")
	}
	if _, err := f.users.SecondFactorCode(f.ctx, res.SignIn.Pending, codes[0], browser); err != nil {
		t.Fatalf("recovery code: %v", err)
	}
	if n := f.count(`SELECT count(*) FROM user_identities`); n != 1 {
		t.Fatalf("%d provider logins linked after the second factor passed, want 1", n)
	}
}

func TestSafeNext(t *testing.T) {
	for next, want := range map[string]string{
		"/contacts":           "/contacts",
		"/contacts?q=ada#top": "/contacts?q=ada#top",
		"":                    "/",
		"contacts":            "/",
		"//evil.com":          "/",
		"/\\evil.com":         "/",
		"/\t/evil.com":        "/",
		"/\n/evil.com":        "/",
		"/%09/evil.com":       "/%09/evil.com",
		"https://evil.com":    "/",
		"/ok\\..":             "/",
	} {
		if got := SafeNext(next); got != want {
			t.Errorf("SafeNext(%q) = %q, want %q", next, got, want)
		}
	}
}

func TestProviderReauthOnlyWithoutStrongerFactors(t *testing.T) {
	t.Parallel()
	iss := authtest.NewIssuer()
	defer iss.Close()
	f := socialFixture(t, iss)
	token, err := f.users.PrepareSetup(f.ctx, "")
	f.must(err)
	iss.SetNext(authtest.Identity{Subject: "ada", Email: "ada@example.com", EmailVerified: true})
	res, err := f.oauth("oidc", IntentSetup, token, nil)
	f.must(err)
	cookie := res.SignIn.Session
	cur := f.current(cookie)
	to, _, err := f.users.StartOAuth(f.ctx, OAuthStart{Provider: "oidc", Intent: IntentReauth}, &cur)
	f.must(err)
	if !strings.Contains(to, "prompt=login") || !strings.Contains(to, "max_age=0") {
		t.Fatalf("reauth URL %q does not ask the provider to check the person again", to)
	}
	f.enrolTOTP(cookie)
	cur = f.current(cookie)
	if _, _, err := f.users.StartOAuth(f.ctx, OAuthStart{Provider: "oidc", Intent: IntentReauth}, &cur); !errors.Is(err, errStrongerReauth) {
		t.Fatalf("provider reauth with an authenticator app enrolled: %v, want errStrongerReauth", err)
	}
}

func TestOAuthStateIsBoundAndSingleUse(t *testing.T) {
	t.Parallel()
	iss := authtest.NewIssuer()
	defer iss.Close()
	f := socialFixture(t, iss)
	f.firstUser("ada@example.com")
	iss.SetNext(authtest.Identity{Subject: "ada", Email: "ada@example.com", EmailVerified: true})

	to, state, err := f.users.StartOAuth(f.ctx, OAuthStart{Provider: "oidc", Intent: IntentLogin}, nil)
	f.must(err)
	cb, err := authtest.CallbackFor(to)
	f.must(err)
	q := cb.Query()
	attempt := OAuthCallback{Provider: "oidc", State: q.Get("state"), CookieState: "another-browser", Code: q.Get("code")}
	if _, err := f.users.FinishOAuth(f.ctx, attempt, nil, browser); !errors.Is(err, crm.ErrInvalid) {
		t.Fatalf("callback without the starting browser's cookie: %v, want ErrInvalid", err)
	}
	attempt.CookieState = state
	if _, err := f.users.FinishOAuth(f.ctx, attempt, nil, browser); err != nil {
		t.Fatalf("callback in the starting browser: %v", err)
	}
	if _, err := f.users.FinishOAuth(f.ctx, attempt, nil, browser); !errors.Is(err, errChallenge) {
		t.Fatalf("replayed callback: %v, want errChallenge", err)
	}
	if _, _, err := f.users.StartOAuth(f.ctx, OAuthStart{Provider: "gitlab", Intent: IntentLogin}, nil); !errors.Is(err, errNoProvider) {
		t.Fatalf("unconfigured provider: %v", err)
	}
}

func TestGitHubUsesTheVerifiedPrimaryEmail(t *testing.T) {
	t.Parallel()
	iss := authtest.NewIssuer()
	defer iss.Close()
	f := socialFixture(t, iss)
	_, cookie := f.firstUser("ada@example.com")

	iss.SetNextGitHub(authtest.GitHubUser{ID: 42, Login: "ada", Emails: []authtest.GitHubEmail{
		{Email: "ada@example.com", Primary: false, Verified: true},
		{Email: "ada@personal.dev", Primary: true, Verified: false},
	}})
	if _, err := f.oauth("github", IntentLogin, "", nil); !errors.Is(err, errUnverifiedEmail) {
		t.Fatalf("GitHub with an unverified primary email: %v, want errUnverifiedEmail", err)
	}
	iss.SetNextGitHub(authtest.GitHubUser{ID: 42, Login: "ada", Emails: []authtest.GitHubEmail{
		{Email: "ada@personal.dev", Primary: false, Verified: true},
		{Email: "Ada@Example.com", Primary: true, Verified: true},
	}})
	res, err := f.oauth("github", IntentLogin, "", nil)
	f.must(err)
	if f.actor(res.SignIn.Session).ID != f.actor(cookie).ID {
		t.Fatal("GitHub sign-in with the verified primary email did not sign in its user")
	}
}

func TestSocialSetupInviteLinkAndReauth(t *testing.T) {
	t.Parallel()
	iss := authtest.NewIssuer()
	defer iss.Close()
	f := socialFixture(t, iss)

	token, err := f.users.PrepareSetup(f.ctx, "")
	f.must(err)
	iss.SetNext(authtest.Identity{Subject: "ada", Email: "ada@example.com", EmailVerified: true, Name: "Ada"})
	res, err := f.oauth("oidc", IntentSetup, token, nil)
	f.must(err)
	admin := res.SignIn.Session
	if f.actor(admin).Label != "Ada" {
		t.Fatal("setup through OIDC did not create the first user")
	}
	if _, err := f.oauth("oidc", IntentSetup, token, nil); !errors.Is(err, crm.ErrNotFound) {
		t.Fatalf("setup link after setup: %v, want ErrNotFound", err)
	}

	invite, err := f.users.Invite(f.ctx, f.actor(admin), "bob@example.com")
	f.must(err)
	iss.SetNext(authtest.Identity{Subject: "bob-x", Email: "bobby@example.com", EmailVerified: true})
	if _, err := f.oauth("oidc", IntentInvite, invite, nil); !errors.Is(err, crm.ErrInvalid) {
		t.Fatalf("invite accepted by a different verified email: %v, want ErrInvalid", err)
	}
	iss.SetNext(authtest.Identity{Subject: "bob", Email: "bob@example.com", EmailVerified: true, Name: "Bob"})
	res, err = f.oauth("oidc", IntentInvite, invite, nil)
	f.must(err)
	if f.actor(res.SignIn.Session).Label != "Bob" {
		t.Fatal("invite through OIDC did not create Bob")
	}

	cur := f.current(admin)
	iss.SetNextGitHub(authtest.GitHubUser{ID: 7, Login: "ada-gh"})
	res, err = f.oauth("github", IntentLink, "", &cur)
	f.must(err)
	if res.Redirect != "/settings/account?linked=github" {
		t.Fatalf("link redirect %q", res.Redirect)
	}
	if n := f.count(`SELECT count(*) FROM user_identities WHERE user_id = $1`, cur.UserID); n != 2 {
		t.Fatalf("%d identities after linking GitHub, want 2", n)
	}

	f.exec(`UPDATE sessions SET authenticated_at = now() - interval '1 hour'`)
	stale := f.current(admin)
	if _, _, err := f.users.StartOAuth(f.ctx, OAuthStart{Provider: "github", Intent: IntentLink}, &stale); !errors.Is(err, ErrReauth) {
		t.Fatalf("linking on a stale session: %v, want ErrReauth", err)
	}
	iss.SetNext(authtest.Identity{Subject: "bob", Email: "bob@example.com", EmailVerified: true})
	if _, err := f.oauth("oidc", IntentReauth, "", &stale); !errors.Is(err, crm.ErrInvalid) {
		t.Fatalf("re-verifying with someone else's identity: %v, want ErrInvalid", err)
	}
	iss.SetNext(authtest.Identity{Subject: "ada", Email: "ada@example.com", EmailVerified: true})
	stale = f.current(admin)
	if _, err := f.oauth("oidc", IntentReauth, "", &stale); err != nil {
		t.Fatalf("re-verifying with a linked identity: %v", err)
	}
	if !f.current(admin).Recent {
		t.Fatal("re-verifying did not make the session recent")
	}
}
