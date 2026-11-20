package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/oauthex"

	"github.com/uncrm/uncrm/internal/crm"
)

// OAuthServer is the OAuth 2.1 authorization server behind /mcp, as the MCP
// authorization spec lays it out. An MCP client registers itself (RFC 7591),
// sends a person to Authorize, the person approves on the consent page
// (Decide), and the client trades the code for tokens (Exchange) with PKCE.
// Access tokens are good for the MCP resource only and act as the person
// who approved, through the client: an ActorApp.
//
// Every client is public and unverified: anyone can register any name. The
// consent page shows the redirect target for that reason, and a person can
// revoke a connection from Settings at any time.
type OAuthServer struct {
	db       *pgxpool.Pool
	issuer   string
	resource string
}

const (
	oauthRequestTTL = 10 * time.Minute // long enough to sign in on the way
	oauthCodeTTL    = 5 * time.Minute
	accessTokenTTL  = time.Hour
	refreshTokenTTL = 30 * 24 * time.Hour
	// unapprovedClientTTL is how long a registration nobody approved is kept.
	unapprovedClientTTL = 30 * 24 * time.Hour

	accessTokenPrefix  = "uncrm_at_"
	refreshTokenPrefix = "uncrm_rt_"
	maxClientName      = 80
	maxRedirectURIs    = 10
	maxRedirectURILen  = 2000
)

// ErrInvalidToken is an access token /mcp does not accept: unknown,
// expired, revoked, or issued for another resource.
var ErrInvalidToken = errors.New("invalid access token")

// OAuthError is a refusal in the RFC 6749 shape. Code is the "error" value
// clients branch on.
type OAuthError struct {
	Code        string
	Description string
}

func (e *OAuthError) Error() string { return e.Code + ": " + e.Description }

func oauthErr(code, description string) *OAuthError {
	return &OAuthError{Code: code, Description: description}
}

func NewOAuthServer(db *pgxpool.Pool, publicURL *url.URL) *OAuthServer {
	o := origin(publicURL)
	return &OAuthServer{db: db, issuer: o, resource: o + "/mcp"}
}

// Issuer is the authorization server's identifier: PUBLIC_URL's origin.
func (o *OAuthServer) Issuer() string { return o.issuer }

// Resource is the MCP server's resource identifier, PUBLIC_URL/mcp.
func (o *OAuthServer) Resource() string { return o.resource }

// ResourceMetadataURL is where RFC 9728 puts the metadata for Resource.
func (o *OAuthServer) ResourceMetadataURL() string {
	return o.issuer + "/.well-known/oauth-protected-resource/mcp"
}

func (o *OAuthServer) isResource(r string) bool {
	return strings.TrimSuffix(r, "/") == o.resource
}

// Register records a public client. Whatever authentication method or
// grant types the client asked for, it gets none and the authorization code
// and refresh grants, and the response says so (RFC 7591 lets the server
// replace requested values). Every registration counts against the caller's
// address, so one address cannot fill the table.
func (o *OAuthServer) Register(ctx context.Context, c Client, m *oauthex.ClientRegistrationMetadata) (*oauthex.ClientRegistrationResponse, error) {
	name, err := clientName(m.ClientName)
	if err != nil {
		return nil, err
	}
	if len(m.RedirectURIs) == 0 || len(m.RedirectURIs) > maxRedirectURIs {
		return nil, oauthErr("invalid_redirect_uri", fmt.Sprintf("register 1 to %d redirect_uris", maxRedirectURIs))
	}
	for _, u := range m.RedirectURIs {
		if err := checkRedirectURI(u); err != nil {
			return nil, err
		}
	}
	id := newToken()
	var created time.Time
	keys := []string{"register:" + c.IP}
	err = pgx.BeginFunc(ctx, o.db, func(tx pgx.Tx) error {
		if err := lockKeys(ctx, tx, keys); err != nil {
			return err
		}
		if err := throttled(ctx, tx, keys); err != nil {
			return err
		}
		if err := fail(ctx, tx, keys); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			DELETE FROM oauth_clients c WHERE created_at < now() - make_interval(secs => $1)
			AND NOT EXISTS (SELECT 1 FROM oauth_grants g WHERE g.client_id = c.id)`, unapprovedClientTTL.Seconds()); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `INSERT INTO oauth_clients (id, name, redirect_uris) VALUES ($1, $2, $3) RETURNING created_at`,
			id, name, m.RedirectURIs).Scan(&created)
	})
	if err != nil {
		return nil, err
	}
	return &oauthex.ClientRegistrationResponse{
		ClientRegistrationMetadata: oauthex.ClientRegistrationMetadata{
			RedirectURIs:            m.RedirectURIs,
			TokenEndpointAuthMethod: "none",
			GrantTypes:              []string{"authorization_code", "refresh_token"},
			ResponseTypes:           []string{"code"},
			ClientName:              name,
		},
		ClientID:         id,
		ClientIDIssuedAt: created,
	}, nil
}

func clientName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "Unnamed MCP client", nil
	}
	if utf8.RuneCountInString(name) > maxClientName || strings.ContainsFunc(name, func(r rune) bool { return unicode.IsControl(r) || r == utf8.RuneError }) {
		return "", oauthErr("invalid_client_metadata", fmt.Sprintf("client_name must be at most %d characters with no control characters", maxClientName))
	}
	return name, nil
}

// blockedSchemes can run code or read local data when a browser follows
// them, so no client may redirect there.
var blockedSchemes = []string{"javascript", "data", "file", "vbscript", "blob", "about", "ftp", "ws", "wss"}

// checkRedirectURI accepts https anywhere, http only to a loopback address
// (RFC 8252 section 7.3), and a private-use scheme such as cursor://
// (section 7.1).
func checkRedirectURI(raw string) error {
	u, err := url.Parse(raw)
	bad := func(why string) error {
		return oauthErr("invalid_redirect_uri", fmt.Sprintf("redirect_uri %q %s", raw, why))
	}
	switch {
	case err != nil || !u.IsAbs() || u.Opaque != "":
		return bad("is not an absolute URI")
	case len(raw) > maxRedirectURILen:
		return bad(fmt.Sprintf("is longer than %d characters", maxRedirectURILen))
	case strings.Contains(raw, "#") || u.User != nil:
		return bad("must not have a fragment or user info")
	case u.Scheme == "https":
		if u.Host == "" {
			return bad("has no host")
		}
	case u.Scheme == "http":
		if !loopback(u.Hostname()) {
			return bad("uses http to a host other than 127.0.0.1, [::1] or localhost; use https")
		}
	case slices.Contains(blockedSchemes, u.Scheme):
		return bad("uses a scheme a browser must not be sent to")
	}
	return nil
}

func loopback(host string) bool {
	return host == "127.0.0.1" || host == "::1" || host == "localhost"
}

// redirectMatches compares a requested redirect_uri with a registered one
// exactly, except that a loopback http URI may use any port, because a
// native app picks a free port each time it listens (RFC 8252 section 7.3).
func redirectMatches(registered, requested string) bool {
	if registered == requested {
		return true
	}
	r, err1 := url.Parse(registered)
	q, err2 := url.Parse(requested)
	return err1 == nil && err2 == nil && r.Scheme == "http" && q.Scheme == "http" && loopback(r.Hostname()) &&
		r.Hostname() == q.Hostname() && r.Path == q.Path && r.RawQuery == q.RawQuery && !strings.Contains(requested, "#") && q.User == nil
}

// AuthorizeRequest is the query of GET /oauth/authorize.
type AuthorizeRequest struct {
	ClientID, RedirectURI, ResponseType, CodeChallenge, CodeChallengeMethod, State, Resource string
}

// Authorize checks an authorization request and keeps it for the consent
// page, returning the handle that page loads it by. A refusal is an
// *OAuthError to show the person, never a redirect: anyone can register
// any redirect_uri, so redirecting a request nobody approved would make
// uncrm an open redirector (RFC 9700 section 4.11.2).
func (o *OAuthServer) Authorize(ctx context.Context, q AuthorizeRequest) (string, error) {
	var uris []string
	err := o.db.QueryRow(ctx, `SELECT redirect_uris FROM oauth_clients WHERE id = $1`, q.ClientID).Scan(&uris)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", oauthErr("invalid_client", "this app is not registered with uncrm; reconnect it from the app")
	}
	if err != nil {
		return "", err
	}
	if !slices.ContainsFunc(uris, func(u string) bool { return redirectMatches(u, q.RedirectURI) }) {
		return "", oauthErr("invalid_redirect_uri", "the app asked to return to an address it did not register")
	}
	switch {
	case q.ResponseType != "code":
		return "", oauthErr("unsupported_response_type", "response_type must be code")
	case q.CodeChallengeMethod != "S256" || len(q.CodeChallenge) != 43:
		return "", oauthErr("invalid_request", "PKCE is required: send a code_challenge with code_challenge_method S256")
	case q.Resource != "" && !o.isResource(q.Resource):
		return "", oauthErr("invalid_target", "resource must be "+o.resource)
	}
	handle := newToken()
	if _, err := o.db.Exec(ctx, `DELETE FROM oauth_requests WHERE expires_at < now()`); err != nil {
		return "", err
	}
	_, err = o.db.Exec(ctx, `
		INSERT INTO oauth_requests (handle_hash, client_id, redirect_uri, code_challenge, state, resource, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, now() + make_interval(secs => $7))`,
		hashToken(handle), q.ClientID, q.RedirectURI, q.CodeChallenge, q.State, o.resource, oauthRequestTTL.Seconds())
	return handle, err
}

// withParams adds the non-empty values to u's query.
func withParams(u string, kv ...string) string {
	parsed, err := url.Parse(u)
	if err != nil {
		return u
	}
	q := parsed.Query()
	for i := 0; i+1 < len(kv); i += 2 {
		if kv[i+1] != "" {
			q.Set(kv[i], kv[i+1])
		}
	}
	parsed.RawQuery = q.Encode()
	return parsed.String()
}

// ConsentRequest is what the consent page shows about a pending request.
type ConsentRequest struct {
	ClientName         string    `json:"client_name"`
	RedirectURI        string    `json:"redirect_uri"`
	ClientRegisteredAt time.Time `json:"client_registered_at"`
}

var errRequestGone = fmt.Errorf("%w: this sign-in request expired or was already answered; start again from your app", crm.ErrNotFound)

func (o *OAuthServer) Consent(ctx context.Context, handle string) (ConsentRequest, error) {
	var c ConsentRequest
	err := o.db.QueryRow(ctx, `
		SELECT c.name, r.redirect_uri, c.created_at FROM oauth_requests r JOIN oauth_clients c ON c.id = r.client_id
		WHERE r.handle_hash = $1 AND r.expires_at > now()`, hashToken(handle)).Scan(&c.ClientName, &c.RedirectURI, &c.ClientRegisteredAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, errRequestGone
	}
	return c, err
}

// Decide answers a pending request as the signed-in person by and returns
// where to send their browser: the client's redirect_uri with a code bound
// to by, or with error=access_denied. A request is answered once.
func (o *OAuthServer) Decide(ctx context.Context, handle string, by crm.Actor, approve bool) (string, error) {
	user := userID(by)
	if user == nil {
		return "", fmt.Errorf("%w: only a signed-in person can approve an app", ErrUnauthorized)
	}
	var redirect string
	err := pgx.BeginFunc(ctx, o.db, func(tx pgx.Tx) error {
		var clientID, redirectURI, challenge, state, resource string
		err := tx.QueryRow(ctx, `
			DELETE FROM oauth_requests WHERE handle_hash = $1 AND expires_at > now()
			RETURNING client_id, redirect_uri, code_challenge, state, resource`, hashToken(handle)).
			Scan(&clientID, &redirectURI, &challenge, &state, &resource)
		if errors.Is(err, pgx.ErrNoRows) {
			return errRequestGone
		}
		if err != nil {
			return err
		}
		if !approve {
			redirect = withParams(redirectURI, "error", "access_denied", "error_description", "the person declined", "state", state)
			return nil
		}
		code := newToken()
		if _, err := tx.Exec(ctx, `DELETE FROM oauth_codes WHERE expires_at < now()`); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO oauth_codes (code_hash, client_id, user_id, redirect_uri, code_challenge, resource, expires_at)
			VALUES ($1, $2, $3, $4, $5, $6, now() + make_interval(secs => $7))`,
			hashToken(code), clientID, *user, redirectURI, challenge, resource, oauthCodeTTL.Seconds()); err != nil {
			return err
		}
		redirect = withParams(redirectURI, "code", code, "state", state)
		return nil
	})
	return redirect, err
}

// TokenRequest is the form body of POST /oauth/token.
type TokenRequest struct {
	GrantType, ClientID, Code, RedirectURI, CodeVerifier, RefreshToken, Resource string
}

// TokenResponse is RFC 6749's successful token response.
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
}

// Exchange runs the token endpoint: an authorization code with its PKCE
// verifier, or a refresh token, for a new access and refresh token. Each
// refusal is an *OAuthError and counts against the caller's address.
// Reusing a code or a spent refresh token revokes the grant it belongs to,
// since one of the two holders is not the client.
func (o *OAuthServer) Exchange(ctx context.Context, c Client, q TokenRequest) (TokenResponse, error) {
	var out TokenResponse
	isRefusal := func(err error) bool { _, ok := errors.AsType[*OAuthError](err); return ok }
	err := guarded(ctx, o.db, []string{"token:" + c.IP}, nil, isRefusal, func(tx pgx.Tx) error {
		if q.Resource != "" && !o.isResource(q.Resource) {
			return oauthErr("invalid_target", "resource must be "+o.resource)
		}
		var grant int64
		var err error
		switch q.GrantType {
		case "authorization_code":
			grant, err = o.redeemCode(ctx, tx, q)
		case "refresh_token":
			grant, err = o.rotate(ctx, tx, q)
		default:
			return oauthErr("unsupported_grant_type", "grant_type must be authorization_code or refresh_token")
		}
		if err != nil {
			return err
		}
		out, err = issueTokens(ctx, tx, grant)
		return err
	})
	return out, err
}

func (o *OAuthServer) redeemCode(ctx context.Context, tx pgx.Tx, q TokenRequest) (int64, error) {
	var clientID, redirectURI, challenge, resource string
	var user int64
	var expired bool
	var spent *int64
	err := tx.QueryRow(ctx, `
		SELECT client_id, user_id, redirect_uri, code_challenge, resource, expires_at <= now(), grant_id
		FROM oauth_codes WHERE code_hash = $1 FOR UPDATE`, hashToken(q.Code)).
		Scan(&clientID, &user, &redirectURI, &challenge, &resource, &expired, &spent)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return 0, oauthErr("invalid_grant", "unknown authorization code")
	case err != nil:
		return 0, err
	case spent != nil:
		if err := revokeGrant(ctx, tx, *spent); err != nil {
			return 0, err
		}
		return 0, oauthErr("invalid_grant", "authorization code already used; the tokens it issued are revoked")
	case expired:
		return 0, oauthErr("invalid_grant", "authorization code expired")
	case clientID != q.ClientID:
		return 0, oauthErr("invalid_grant", "authorization code was issued to another client")
	case redirectURI != q.RedirectURI:
		return 0, oauthErr("invalid_grant", "redirect_uri does not match the authorization request")
	case !pkceMatches(q.CodeVerifier, challenge):
		return 0, oauthErr("invalid_grant", "code_verifier does not match the code_challenge")
	}
	var grant int64
	if err := tx.QueryRow(ctx, `INSERT INTO oauth_grants (client_id, user_id, resource) VALUES ($1, $2, $3) RETURNING id`,
		clientID, user, resource).Scan(&grant); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `UPDATE oauth_codes SET grant_id = $1 WHERE code_hash = $2`, grant, hashToken(q.Code)); err != nil {
		return 0, err
	}
	_, err = tx.Exec(ctx, `UPDATE oauth_clients SET last_used_at = now() WHERE id = $1`, clientID)
	return grant, err
}

func (o *OAuthServer) rotate(ctx context.Context, tx pgx.Tx, q TokenRequest) (int64, error) {
	var grant int64
	var clientID string
	var spent, expired bool
	err := tx.QueryRow(ctx, `
		SELECT t.grant_id, g.client_id, t.used_at IS NOT NULL, t.expires_at <= now()
		FROM oauth_tokens t JOIN oauth_grants g ON g.id = t.grant_id
		WHERE t.token_hash = $1 AND t.kind = 'refresh' AND g.revoked_at IS NULL
		FOR UPDATE OF t`, hashToken(q.RefreshToken)).Scan(&grant, &clientID, &spent, &expired)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return 0, oauthErr("invalid_grant", "unknown or revoked refresh token")
	case err != nil:
		return 0, err
	case clientID != q.ClientID:
		return 0, oauthErr("invalid_grant", "refresh token was issued to another client")
	case spent:
		if err := revokeGrant(ctx, tx, grant); err != nil {
			return 0, err
		}
		return 0, oauthErr("invalid_grant", "refresh token already used; this connection is revoked, so sign in again")
	case expired:
		return 0, oauthErr("invalid_grant", "refresh token expired")
	}
	if _, err := tx.Exec(ctx, `UPDATE oauth_tokens SET used_at = now() WHERE token_hash = $1`, hashToken(q.RefreshToken)); err != nil {
		return 0, err
	}
	_, err = tx.Exec(ctx, `UPDATE oauth_clients SET last_used_at = now() WHERE id = $1`, clientID)
	return grant, err
}

// pkceMatches is the S256 check of RFC 7636 section 4.6.
func pkceMatches(verifier, challenge string) bool {
	if len(verifier) < 43 || len(verifier) > 128 {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:]) == challenge
}

func issueTokens(ctx context.Context, tx pgx.Tx, grant int64) (TokenResponse, error) {
	access, refresh := accessTokenPrefix+newToken(), refreshTokenPrefix+newToken()
	if _, err := tx.Exec(ctx, `DELETE FROM oauth_tokens WHERE expires_at < now()`); err != nil {
		return TokenResponse{}, err
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO oauth_tokens (token_hash, grant_id, kind, expires_at) VALUES
		($1, $3, 'access', now() + make_interval(secs => $4)),
		($2, $3, 'refresh', now() + make_interval(secs => $5))`,
		hashToken(access), hashToken(refresh), grant, accessTokenTTL.Seconds(), refreshTokenTTL.Seconds())
	return TokenResponse{AccessToken: access, TokenType: "Bearer", ExpiresIn: int(accessTokenTTL.Seconds()), RefreshToken: refresh}, err
}

func revokeGrant(ctx context.Context, db dbtx, grant int64) error {
	if _, err := db.Exec(ctx, `UPDATE oauth_grants SET revoked_at = coalesce(revoked_at, now()) WHERE id = $1`, grant); err != nil {
		return err
	}
	_, err := db.Exec(ctx, `DELETE FROM oauth_tokens WHERE grant_id = $1`, grant)
	return err
}

// Revoke is RFC 7009 revocation: any token of a grant ends the whole grant.
// An unknown token is already revoked.
func (o *OAuthServer) Revoke(ctx context.Context, token string) error {
	return pgx.BeginFunc(ctx, o.db, func(tx pgx.Tx) error {
		var grant int64
		err := tx.QueryRow(ctx, `SELECT grant_id FROM oauth_tokens WHERE token_hash = $1`, hashToken(token)).Scan(&grant)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		return revokeGrant(ctx, tx, grant)
	})
}

// VerifyAccessToken returns the app actor behind an access token for the
// MCP resource and when the token expires, or ErrInvalidToken. Any other
// error means the database could not decide.
func (o *OAuthServer) VerifyAccessToken(ctx context.Context, token string) (crm.Actor, time.Time, error) {
	if wellFormedKey(token) {
		return crm.Actor{}, time.Time{}, fmt.Errorf("%w: API keys do not work on /mcp. Remove the Authorization header from your MCP client's config and connect again; it signs you in through the browser", ErrInvalidToken)
	}
	var grant, user int64
	var client, email, name string
	var expires time.Time
	err := o.db.QueryRow(ctx, `
		WITH t AS (
			SELECT g.id, g.last_used_at, c.name AS client, u.id AS user_id, u.email, u.name, t.expires_at
			FROM oauth_tokens t
			JOIN oauth_grants g ON g.id = t.grant_id
			JOIN oauth_clients c ON c.id = g.client_id
			JOIN users u ON u.id = g.user_id
			WHERE t.token_hash = $1 AND t.kind = 'access' AND t.expires_at > now()
			  AND g.revoked_at IS NULL AND g.resource = $2
		), touch AS (
			UPDATE oauth_grants SET last_used_at = now() FROM t
			WHERE oauth_grants.id = t.id
			  AND (t.last_used_at IS NULL OR t.last_used_at < now() - interval '1 minute')
		)
		SELECT id, client, user_id, email, name, expires_at FROM t`, hashToken(token), o.resource).
		Scan(&grant, &client, &user, &email, &name, &expires)
	if errors.Is(err, pgx.ErrNoRows) {
		return crm.Actor{}, time.Time{}, ErrInvalidToken
	}
	if err != nil {
		return crm.Actor{}, time.Time{}, err
	}
	person := userActor(user, email, name).Label
	return crm.Actor{Kind: crm.ActorApp, ID: strconv.FormatInt(grant, 10), Label: client + " (via " + person + ")"}, expires, nil
}

// App is a live connection: a client a person approved whose refresh
// token has not expired or been revoked.
type App struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	ApprovedBy string     `json:"approved_by"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
}

// Apps lists the team's live connections, newest first.
func (o *OAuthServer) Apps(ctx context.Context) ([]App, error) {
	rows, err := o.db.Query(ctx, `
		SELECT g.id::text, c.name, coalesce(nullif(u.name, ''), u.email), g.created_at, g.last_used_at
		FROM oauth_grants g
		JOIN oauth_clients c ON c.id = g.client_id
		JOIN users u ON u.id = g.user_id
		WHERE g.revoked_at IS NULL
		  AND EXISTS (SELECT 1 FROM oauth_tokens t WHERE t.grant_id = g.id AND t.kind = 'refresh' AND t.used_at IS NULL AND t.expires_at > now())
		ORDER BY g.created_at DESC, g.id DESC`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[App])
}

// RevokeApp ends a connection: its tokens stop working at once. Revoking
// a revoked connection succeeds.
func (o *OAuthServer) RevokeApp(ctx context.Context, id string) error {
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return fmt.Errorf("%w: no connected app %s", crm.ErrNotFound, id)
	}
	return pgx.BeginFunc(ctx, o.db, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `SELECT FROM oauth_grants WHERE id = $1`, n)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return fmt.Errorf("%w: no connected app %s", crm.ErrNotFound, id)
		}
		return revokeGrant(ctx, tx, n)
	})
}
