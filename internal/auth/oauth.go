package auth

import (
	"cmp"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/oauth2"

	"github.com/unkeyed/keyring/internal/crm"
)

// Social and OIDC sign-in: the authorization code flow with PKCE, and a
// nonce for OIDC. The state parameter is an oauth challenge handle, also set
// as a cookie, so a callback only completes in the browser that started it.
//
// Linking rules for a sign-in, in order (decideSocial):
//  1. An identity already linked signs in its user.
//  2. Otherwise the provider must have verified the email.
//  3. A user with that email gets the identity linked and signs in, after
//     their passkey or authenticator code if they have one.
//  4. A live invite for that email is accepted.
//  5. An email at an ALLOWED_EMAIL_DOMAINS domain joins, but only through a
//     provider that proves membership of the organization: Google with a
//     matching Workspace domain (hd claim), or the configured OIDC issuer.
//  6. Anyone else is turned away. Social sign-in never creates an account
//     for a stranger.

type ExternalIdentity struct {
	Provider      string
	Subject       string
	Email         string
	EmailVerified bool
	Name          string
	HostedDomain  string // Google's hd claim
}

type provider interface {
	// authCodeURL asks the provider to check the person again when reauth
	// is set, where the provider supports it.
	authCodeURL(ctx context.Context, state, nonce, verifier string, reauth bool) (string, error)
	identify(ctx context.Context, code, nonce, verifier string) (ExternalIdentity, error)
}

func newProvider(p ProviderConfig, redirect string) provider {
	conf := oauth2.Config{ClientID: p.ClientID, ClientSecret: p.ClientSecret, RedirectURL: redirect}
	if p.ID == "github" {
		web, api := cmp.Or(p.GitHubURL, "https://github.com"), cmp.Or(p.GitHubAPIURL, "https://api.github.com")
		conf.Endpoint = oauth2.Endpoint{AuthURL: web + "/login/oauth/authorize", TokenURL: web + "/login/oauth/access_token"}
		conf.Scopes = []string{"read:user", "user:email"}
		return &githubProvider{conf: conf, api: api}
	}
	conf.Scopes = []string{oidc.ScopeOpenID, "email", "profile"}
	return &oidcProvider{id: p.ID, issuer: p.Issuer, conf: conf}
}

var httpClient = &http.Client{Timeout: 15 * time.Second}

func withClient(ctx context.Context) context.Context {
	return context.WithValue(ctx, oauth2.HTTPClient, httpClient)
}

// oidcProvider is Google or a generic OIDC issuer. It discovers the
// issuer's endpoints on first use and retries discovery until it works, so
// an issuer that is down at boot does not stop Keyring from starting.
type oidcProvider struct {
	id, issuer string
	conf       oauth2.Config
	mu         sync.Mutex
	discovered *oidc.Provider
}

func (p *oidcProvider) discover(ctx context.Context) (*oidc.Provider, oauth2.Config, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.discovered == nil {
		d, err := oidc.NewProvider(withClient(context.WithoutCancel(ctx)), p.issuer)
		if err != nil {
			return nil, p.conf, fmt.Errorf("discover OIDC issuer %s: %w", p.issuer, err)
		}
		p.discovered = d
		p.conf.Endpoint = d.Endpoint()
	}
	return p.discovered, p.conf, nil
}

func (p *oidcProvider) authCodeURL(ctx context.Context, state, nonce, verifier string, reauth bool) (string, error) {
	_, conf, err := p.discover(ctx)
	if err != nil {
		return "", err
	}
	opts := []oauth2.AuthCodeOption{oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)}
	if reauth {
		opts = append(opts, oauth2.SetAuthURLParam("prompt", "login"), oauth2.SetAuthURLParam("max_age", "0"))
	}
	return conf.AuthCodeURL(state, opts...), nil
}

// flexBool reads a claim that some issuers send as "true" instead of true.
type flexBool bool

func (b *flexBool) UnmarshalJSON(data []byte) error {
	*b = flexBool(strings.Trim(string(data), `"`) == "true")
	return nil
}

type oidcClaims struct {
	Subject       string   `json:"sub"`
	Email         string   `json:"email"`
	EmailVerified flexBool `json:"email_verified"`
	Name          string   `json:"name"`
	HostedDomain  string   `json:"hd"`
}

func (p *oidcProvider) identify(ctx context.Context, code, nonce, verifier string) (ExternalIdentity, error) {
	d, conf, err := p.discover(ctx)
	if err != nil {
		return ExternalIdentity{}, err
	}
	ctx = withClient(ctx)
	tok, err := conf.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return ExternalIdentity{}, fmt.Errorf("exchange the authorization code: %w", err)
	}
	raw, ok := tok.Extra("id_token").(string)
	if !ok {
		return ExternalIdentity{}, errors.New("the token response has no id_token")
	}
	idt, err := d.Verifier(&oidc.Config{ClientID: conf.ClientID}).Verify(ctx, raw)
	if err != nil {
		return ExternalIdentity{}, fmt.Errorf("verify the ID token: %w", err)
	}
	if subtle.ConstantTimeCompare([]byte(idt.Nonce), []byte(nonce)) != 1 {
		return ExternalIdentity{}, errors.New("the ID token's nonce does not match")
	}
	var cl oidcClaims
	if err := idt.Claims(&cl); err != nil {
		return ExternalIdentity{}, err
	}
	if cl.Email == "" && d.UserInfoEndpoint() != "" {
		info, err := d.UserInfo(ctx, oauth2.StaticTokenSource(tok))
		if err != nil {
			return ExternalIdentity{}, fmt.Errorf("read userinfo: %w", err)
		}
		var more oidcClaims
		if err := info.Claims(&more); err != nil {
			return ExternalIdentity{}, err
		}
		if more.Subject == cl.Subject {
			cl.Email, cl.EmailVerified, cl.Name = more.Email, more.EmailVerified, cmp.Or(cl.Name, more.Name)
		}
	}
	return ExternalIdentity{
		Provider: p.id, Subject: idt.Subject, Email: normalizeEmail(cl.Email), EmailVerified: bool(cl.EmailVerified),
		Name: cl.Name, HostedDomain: strings.ToLower(cl.HostedDomain),
	}, nil
}

// githubProvider uses GitHub's OAuth app flow. GitHub has no ID token, so
// the identity comes from the API, and the email is the primary address
// only when GitHub marks it verified.
type githubProvider struct {
	conf oauth2.Config
	api  string
}

func (p *githubProvider) authCodeURL(_ context.Context, state, _, verifier string, _ bool) (string, error) {
	return p.conf.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier)), nil
}

func (p *githubProvider) identify(ctx context.Context, code, _, verifier string) (ExternalIdentity, error) {
	ctx = withClient(ctx)
	tok, err := p.conf.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return ExternalIdentity{}, fmt.Errorf("exchange the authorization code: %w", err)
	}
	client := p.conf.Client(ctx, tok)
	var user struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
		Name  string `json:"name"`
	}
	if err := getJSON(ctx, client, p.api+"/user", &user); err != nil {
		return ExternalIdentity{}, err
	}
	var emails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}
	if err := getJSON(ctx, client, p.api+"/user/emails", &emails); err != nil {
		return ExternalIdentity{}, err
	}
	id := ExternalIdentity{Provider: "github", Subject: strconv.FormatInt(user.ID, 10), Name: cmp.Or(user.Name, user.Login)}
	for _, e := range emails {
		if e.Primary && e.Verified {
			id.Email, id.EmailVerified = normalizeEmail(e.Email), true
		}
	}
	if user.ID == 0 {
		return ExternalIdentity{}, errors.New("GitHub returned no user id")
	}
	return id, nil
}

func getJSON(ctx context.Context, client *http.Client, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", url, res.Status)
	}
	return json.NewDecoder(res.Body).Decode(out)
}

const (
	IntentLogin  = "login"
	IntentLink   = "link"
	IntentReauth = "reauth"
	IntentSetup  = "setup"
	IntentInvite = "invite"
)

type oauthState struct {
	Provider  string `json:"provider"`
	Intent    string `json:"intent"`
	UserID    int64  `json:"user_id,omitempty"`
	TokenHash []byte `json:"token_hash,omitempty"`
	Nonce     string `json:"nonce"`
	Verifier  string `json:"verifier"`
	Next      string `json:"next,omitempty"`
}

type OAuthStart struct {
	Provider string
	Intent   string
	Token    string
	Next     string
}

// SafeNext keeps a post-login redirect on this origin. Browsers drop tabs and newlines from URLs and read a backslash as a
// slash, so "/\t/evil.com" would leave the site; any such character fails.
func SafeNext(next string) string {
	u, err := url.Parse(next)
	if err != nil || u.Scheme != "" || u.Host != "" || !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") ||
		strings.ContainsFunc(next, func(r rune) bool { return r < 0x20 || r == 0x7f || r == '\\' }) {
		return "/"
	}
	return next
}

var (
	errNoProvider = fmt.Errorf("%w: that sign-in provider is not configured", crm.ErrNotFound)
	// A provider may sign someone in again without asking anything, so it
	// cannot vouch for a person who has a passkey or an authenticator app.
	errStrongerReauth = fmt.Errorf("%w: confirm it's you with your passkey or authenticator code instead", crm.ErrInvalid)
)

func (u *Users) StartOAuth(ctx context.Context, s OAuthStart, cur *Current) (redirect, state string, err error) {
	p, ok := u.providers[s.Provider]
	if !ok {
		return "", "", errNoProvider
	}
	st := oauthState{Provider: s.Provider, Intent: s.Intent, Nonce: newToken(), Verifier: oauth2.GenerateVerifier(), Next: SafeNext(s.Next)}
	switch s.Intent {
	case IntentLogin:
	case IntentLink:
		if cur == nil {
			return "", "", ErrUnauthorized
		}
		if !cur.Recent {
			return "", "", ErrReauth
		}
		st.UserID = cur.UserID
	case IntentReauth:
		if cur == nil {
			return "", "", ErrUnauthorized
		}
		f, err := u.factorsOf(ctx, cur.UserID)
		if err != nil {
			return "", "", err
		}
		if f.passkey || f.totp {
			return "", "", errStrongerReauth
		}
		st.UserID = cur.UserID
	case IntentSetup:
		st.TokenHash = hashToken(s.Token)
		if _, err := u.checkSetup(ctx, u.db, st.TokenHash); err != nil {
			return "", "", err
		}
	case IntentInvite:
		if _, err := u.InviteEmail(ctx, s.Token); err != nil {
			return "", "", err
		}
		st.TokenHash = hashToken(s.Token)
	default:
		return "", "", fmt.Errorf("%w: unknown intent %q", crm.ErrInvalid, s.Intent)
	}
	state, err = putChallenge(ctx, u.db, kindOAuth, st)
	if err != nil {
		return "", "", err
	}
	redirect, err = p.authCodeURL(ctx, state, st.Nonce, st.Verifier, s.Intent == IntentReauth)
	return redirect, state, err
}

type OAuthCallback struct {
	Provider    string
	State       string
	CookieState string
	Code        string
	Error       string // the provider's error parameter, e.g. access_denied
}

type OAuthResult struct {
	SignIn   SignIn
	Redirect string
}

// FinishOAuth completes a flow. On failure the result still says where to
// show the error: the account page for link and reauth, the sign-in page
// otherwise. cur is the signed-in session, or nil.
func (u *Users) FinishOAuth(ctx context.Context, cb OAuthCallback, cur *Current, c Client) (OAuthResult, error) {
	fail := OAuthResult{Redirect: "/login"}
	if cb.State == "" || subtle.ConstantTimeCompare([]byte(cb.State), []byte(cb.CookieState)) != 1 {
		return fail, fmt.Errorf("%w: this sign-in was started in another browser or expired; start again", crm.ErrInvalid)
	}
	var st oauthState
	if err := takeChallenge(ctx, u.db, kindOAuth, cb.State, &st); err != nil {
		return fail, err
	}
	if st.Intent == IntentLink || st.Intent == IntentReauth {
		fail.Redirect = "/settings/account"
	}
	p, ok := u.providers[cb.Provider]
	if !ok || st.Provider != cb.Provider {
		return fail, errNoProvider
	}
	if cb.Error != "" {
		return fail, fmt.Errorf("%w: the provider did not sign you in (%s)", crm.ErrInvalid, cb.Error)
	}
	ident, err := p.identify(ctx, cb.Code, st.Nonce, st.Verifier)
	if err != nil {
		return fail, fmt.Errorf("%w: %s sign-in failed: %v", crm.ErrInvalid, u.providerName(cb.Provider), err)
	}
	switch st.Intent {
	case IntentLink, IntentReauth:
		if cur == nil || cur.UserID != st.UserID {
			return fail, ErrUnauthorized
		}
		if st.Intent == IntentLink {
			if err := pgx.BeginFunc(ctx, u.db, func(tx pgx.Tx) error { return u.insertIdentity(ctx, tx, st.UserID, ident) }); err != nil {
				return fail, err
			}
			return OAuthResult{Redirect: "/settings/account?linked=" + url.QueryEscape(cb.Provider)}, nil
		}
		if err := u.reauthIdentity(ctx, *cur, ident); err != nil {
			return fail, err
		}
		return OAuthResult{Redirect: "/settings/account?reauth=1"}, nil
	case IntentSetup, IntentInvite:
		in, err := u.joinWithIdentity(ctx, st, ident, c)
		if err != nil {
			return fail, err
		}
		return OAuthResult{SignIn: in, Redirect: "/"}, nil
	}
	userID, link, err := u.socialUser(ctx, ident)
	if err != nil {
		return fail, err
	}
	in, err := u.afterFirstFactor(ctx, userID, cb.Provider, link, c)
	if err != nil {
		return fail, err
	}
	if in.Pending != "" {
		return OAuthResult{SignIn: in, Redirect: "/login?second_factor=1&next=" + url.QueryEscape(st.Next)}, nil
	}
	return OAuthResult{SignIn: in, Redirect: st.Next}, nil
}

func (u *Users) providerName(id string) string {
	for _, p := range u.cfg.Providers {
		if p.ID == id {
			return p.Name
		}
	}
	return id
}

var (
	errUnverifiedEmail = fmt.Errorf("%w: your provider has not verified your email address, so it cannot be matched to a teammate", crm.ErrInvalid)
	errNotWorkspace    = fmt.Errorf("%w: sign in with your Google Workspace account for this address", crm.ErrInvalid)
	errStranger        = fmt.Errorf("%w: there is no Keyring account for this email. Ask a teammate for an invite", crm.ErrInvalid)
)

type socialAction int

const (
	socialSignIn socialAction = iota + 1
	socialLink
	socialAcceptInvite
	socialJoinDomain
)

type socialFacts struct {
	ident      ExternalIdentity
	linkedUser int64 // the user the identity is linked to, or 0
	emailUser  int64 // the user with the identity's email, or 0
	invited    bool  // a live invite exists for the email
}

func decideSocial(f socialFacts, allowed []string) (socialAction, error) {
	switch {
	case f.linkedUser != 0:
		return socialSignIn, nil
	case !f.ident.EmailVerified || f.ident.Email == "":
		return 0, errUnverifiedEmail
	case f.emailUser != 0 && !workspaceMatches(f.ident, allowed):
		return 0, errNotWorkspace
	case f.emailUser != 0:
		return socialLink, nil
	case f.invited:
		return socialAcceptInvite, nil
	case domainAllowed(f.ident, allowed):
		return socialJoinDomain, nil
	}
	return 0, errStranger
}

// domainAllowed is whether the identity may join through its email domain.
// A GitHub account never may: it only shows that someone once verified the
// address, which a teammate's personal account keeps after they leave.
func domainAllowed(id ExternalIdentity, allowed []string) bool {
	_, domain, _ := strings.Cut(id.Email, "@")
	if !slices.Contains(allowed, domain) {
		return false
	}
	switch id.Provider {
	case "google":
		return id.HostedDomain == domain
	case "oidc":
		return true
	}
	return false
}

// workspaceMatches is false for a Google account with an address at an
// allowed domain that is not that domain's Workspace account: a personal
// Google account can carry a company address as verified long after the
// company gave the address to someone else.
func workspaceMatches(id ExternalIdentity, allowed []string) bool {
	_, domain, _ := strings.Cut(id.Email, "@")
	return id.Provider != "google" || !slices.Contains(allowed, domain) || id.HostedDomain == domain
}

// socialUser finds or creates the user a provider sign-in is for. A login
// matched by email comes back as link, to be linked once any second factor
// passes.
func (u *Users) socialUser(ctx context.Context, ident ExternalIdentity) (id int64, link *ExternalIdentity, err error) {
	err = pgx.BeginFunc(ctx, u.db, func(tx pgx.Tx) error {
		f := socialFacts{ident: ident}
		err := tx.QueryRow(ctx, `
			UPDATE user_identities SET last_used_at = now(), email = $3 WHERE provider = $1 AND subject = $2 RETURNING user_id`,
			ident.Provider, ident.Subject, ident.Email).Scan(&f.linkedUser)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if f.linkedUser == 0 && ident.Email != "" {
			err := tx.QueryRow(ctx, `SELECT
				coalesce((SELECT id FROM users WHERE email = $1), 0),
				EXISTS (SELECT 1 FROM invites WHERE email = $1 AND accepted_at IS NULL AND expires_at > now())`,
				ident.Email).Scan(&f.emailUser, &f.invited)
			if err != nil {
				return err
			}
		}
		action, err := decideSocial(f, u.cfg.AllowedEmailDomains)
		if err != nil {
			return err
		}
		switch action {
		case socialSignIn:
			id = f.linkedUser
			return nil
		case socialLink:
			id, link = f.emailUser, &ident
			return nil
		case socialAcceptInvite:
			if _, err := claimPendingInvite(ctx, tx, ident.Email); err != nil {
				return err
			}
			fallthrough
		case socialJoinDomain:
			if id, err = createUser(ctx, tx, ident.Email, ident.Name, nil); err != nil {
				return err
			}
		}
		return u.insertIdentity(ctx, tx, id, ident)
	})
	return id, link, err
}

func (u *Users) joinWithIdentity(ctx context.Context, st oauthState, ident ExternalIdentity, c Client) (SignIn, error) {
	if !ident.EmailVerified || ident.Email == "" {
		return SignIn{}, errUnverifiedEmail
	}
	var token string
	err := pgx.BeginFunc(ctx, u.db, func(tx pgx.Tx) error {
		var id int64
		var err error
		if st.Intent == IntentSetup {
			id, err = u.claimSetup(ctx, tx, st.TokenHash, ident.Email, ident.Name, nil)
		} else {
			var email string
			if email, err = claimInvite(ctx, tx, st.TokenHash); err == nil {
				if email != ident.Email {
					return fmt.Errorf("%w: this invite is for %s, but %s signed you in as %s", crm.ErrInvalid, email, u.providerName(ident.Provider), ident.Email)
				}
				id, err = createUser(ctx, tx, email, ident.Name, nil)
			}
		}
		if err != nil {
			return err
		}
		if err := u.insertIdentity(ctx, tx, id, ident); err != nil {
			return err
		}
		token, err = u.newSession(ctx, tx, id, ident.Provider, c)
		return err
	})
	return SignIn{Session: token}, err
}

func (u *Users) insertIdentity(ctx context.Context, tx pgx.Tx, userID int64, ident ExternalIdentity) error {
	name := u.providerName(ident.Provider)
	var owner int64
	err := tx.QueryRow(ctx, `SELECT user_id FROM user_identities WHERE provider = $1 AND subject = $2`, ident.Provider, ident.Subject).Scan(&owner)
	switch {
	case err == nil && owner == userID:
		return nil
	case err == nil:
		return fmt.Errorf("%w: this %s account is already linked to another teammate", crm.ErrConflict, name)
	case !errors.Is(err, pgx.ErrNoRows):
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO user_identities (provider, subject, user_id, email, last_used_at) VALUES ($1, $2, $3, $4, now())`,
		ident.Provider, ident.Subject, userID, ident.Email)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return fmt.Errorf("%w: a different %s account is already linked to this Keyring account; unlink it first", crm.ErrConflict, name)
	}
	return err
}

func (u *Users) reauthIdentity(ctx context.Context, cur Current, ident ExternalIdentity) error {
	var owner int64
	err := u.db.QueryRow(ctx, `SELECT user_id FROM user_identities WHERE provider = $1 AND subject = $2`, ident.Provider, ident.Subject).Scan(&owner)
	if errors.Is(err, pgx.ErrNoRows) || owner != cur.UserID {
		return fmt.Errorf("%w: that %s account is not linked to you", crm.ErrInvalid, u.providerName(ident.Provider))
	}
	if err != nil {
		return err
	}
	return u.reauthenticated(ctx, cur)
}
