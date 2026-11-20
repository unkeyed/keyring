package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/unkeyed/keyring/internal/crm"
)

// Cookie names. Over https each goes out with the __Host- prefix (see
// cookieName).
const (
	SessionCookie = "keyring_session"
	PendingCookie = "keyring_pending"
	// OAuthCookie binds an OAuth redirect to the browser that started it.
	OAuthCookie = "keyring_oauth"
	// OAuthBackCookie is the setup or invite page a failed OAuth flow
	// returns to. It holds that page's token, so it stays in the browser
	// instead of the challenge row.
	OAuthBackCookie = "keyring_oauth_back"

	recentAuth = 15 * time.Minute
)

const (
	MethodPasskey  = "passkey"
	MethodPassword = "password"
	MethodTOTP     = "totp"
	MethodRecovery = "recovery_code"
	MethodSetup    = "setup"
)

// SignIn is the outcome of one sign-in step: exactly one of Session (the
// cookie value of a new session) and Pending (the handle of a login that
// waits for one of SecondFactors).
type SignIn struct {
	Session       string
	Pending       string
	SecondFactors []string
}

type Session struct {
	ID         int64     `json:"id"`
	Method     string    `json:"method"`
	UserAgent  string    `json:"user_agent"`
	CreatedAt  time.Time `json:"created_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
	Current    bool      `json:"current"`
}

type Current struct {
	UserID    int64
	SessionID int64
	// Recent is whether the person proved a factor within recentAuth.
	Recent bool
}

func (u *Users) newSession(ctx context.Context, q dbtx, userID int64, method string, c Client) (string, error) {
	if _, err := q.Exec(ctx, `DELETE FROM sessions WHERE expires_at < now()`); err != nil {
		return "", err
	}
	token := newToken()
	_, err := q.Exec(ctx, `
		INSERT INTO sessions (token_hash, user_id, expires_at, method, user_agent)
		VALUES ($1, $2, now() + make_interval(secs => $3), $4, $5)`,
		hashToken(token), userID, sessionTTL.Seconds(), method, c.UserAgent)
	if err != nil {
		return "", err
	}
	return token, nil
}

func (u *Users) signIn(ctx context.Context, userID int64, method string, c Client) (SignIn, error) {
	token, err := u.newSession(ctx, u.db, userID, method, c)
	return SignIn{Session: token}, err
}

// Session resolves a cookie value to its user, or ErrUnauthorized, and
// slides the session's expiry and last-seen time.
func (u *Users) Session(ctx context.Context, cookie string) (crm.Actor, error) {
	if cookie == "" {
		return crm.Actor{}, ErrUnauthorized
	}
	var id int64
	var email, name string
	err := u.db.QueryRow(ctx, `
		WITH s AS (
			SELECT s.token_hash, s.last_seen_at, u.id, u.email, u.name
			FROM sessions s JOIN users u ON u.id = s.user_id
			WHERE s.token_hash = $1 AND s.expires_at > now()
		), slide AS (
			UPDATE sessions SET expires_at = now() + make_interval(secs => $2::float8), last_seen_at = now() FROM s
			WHERE sessions.token_hash = s.token_hash
			  AND s.last_seen_at < now() - make_interval(secs => $3::float8)
		)
		SELECT id, email, name FROM s`,
		hashToken(cookie), sessionTTL.Seconds(), sessionSlideStep.Seconds()).Scan(&id, &email, &name)
	if errors.Is(err, pgx.ErrNoRows) {
		return crm.Actor{}, ErrUnauthorized
	}
	if err != nil {
		return crm.Actor{}, err
	}
	return userActor(id, email, name), nil
}

func (u *Users) Current(ctx context.Context, cookie string) (Current, error) {
	var c Current
	err := u.db.QueryRow(ctx, `
		SELECT user_id, id, authenticated_at > now() - make_interval(secs => $2)
		FROM sessions WHERE token_hash = $1 AND expires_at > now()`,
		hashToken(cookie), recentAuth.Seconds()).Scan(&c.UserID, &c.SessionID, &c.Recent)
	if errors.Is(err, pgx.ErrNoRows) {
		return Current{}, ErrUnauthorized
	}
	return c, err
}

func userActor(id int64, email, name string) crm.Actor {
	label := name
	if label == "" {
		label = email
	}
	return crm.Actor{Kind: crm.ActorUser, ID: strconv.FormatInt(id, 10), Label: label}
}

// Logout ends the session. An unknown or empty cookie is already logged out.
func (u *Users) Logout(ctx context.Context, cookie string) error {
	if cookie == "" {
		return nil
	}
	_, err := u.db.Exec(ctx, `DELETE FROM sessions WHERE token_hash = $1`, hashToken(cookie))
	return err
}

func (u *Users) Sessions(ctx context.Context, cur Current) ([]Session, error) {
	rows, err := u.db.Query(ctx, `
		SELECT id, method, user_agent, created_at, last_seen_at, id = $2
		FROM sessions WHERE user_id = $1 AND expires_at > now()
		ORDER BY last_seen_at DESC, id DESC`, cur.UserID, cur.SessionID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Session])
}

func (u *Users) RevokeSession(ctx context.Context, cur Current, id int64) error {
	tag, err := u.db.Exec(ctx, `DELETE FROM sessions WHERE id = $1 AND user_id = $2`, id, cur.UserID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: no session %d", crm.ErrNotFound, id)
	}
	return nil
}

func revokeOthers(ctx context.Context, tx pgx.Tx, cur Current) error {
	_, err := tx.Exec(ctx, `DELETE FROM sessions WHERE user_id = $1 AND id <> $2`, cur.UserID, cur.SessionID)
	return err
}

func (u *Users) reauthenticated(ctx context.Context, cur Current) error {
	_, err := u.db.Exec(ctx, `UPDATE sessions SET authenticated_at = now() WHERE id = $1`, cur.SessionID)
	return err
}

func joinMethods(m ...string) string { return strings.Join(m, "+") }

func (u *Users) secure() bool { return u.cfg.PublicURL.Scheme == "https" }

// cookieName is the name a cookie goes by on this instance. Over https it
// carries the __Host- prefix, which browsers accept only from this exact
// host, Secure, with Path=/ and no Domain, so a sibling subdomain cannot
// plant a session or a sign-in in progress.
func (u *Users) cookieName(name string) string {
	if u.secure() {
		return "__Host-" + name
	}
	return name
}

// Cookie reads the named cookie from r, or "".
func (u *Users) Cookie(r *http.Request, name string) string {
	if c, err := r.Cookie(u.cookieName(name)); err == nil {
		return c.Value
	}
	return ""
}

func (u *Users) setCookie(w http.ResponseWriter, name, value string, maxAge time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name: u.cookieName(name), Value: value, Path: "/", MaxAge: int(maxAge.Seconds()),
		HttpOnly: true, Secure: u.secure(), SameSite: http.SameSiteLaxMode,
	})
}

func (u *Users) SetSessionCookie(w http.ResponseWriter, value string) {
	u.setCookie(w, SessionCookie, value, cookieMaxAge)
}

func (u *Users) ClearSessionCookie(w http.ResponseWriter) {
	u.setCookie(w, SessionCookie, "", -1)
}

func (u *Users) SetPendingCookie(w http.ResponseWriter, value string) {
	u.setCookie(w, PendingCookie, value, challengeTTL)
}

func (u *Users) ClearPendingCookie(w http.ResponseWriter) {
	u.setCookie(w, PendingCookie, "", -1)
}

func (u *Users) SetOAuthCookie(w http.ResponseWriter, state, back string) {
	u.setCookie(w, OAuthCookie, state, challengeTTL)
	if back != "" {
		u.setCookie(w, OAuthBackCookie, back, challengeTTL)
	}
}

func (u *Users) ClearOAuthCookie(w http.ResponseWriter) {
	u.setCookie(w, OAuthCookie, "", -1)
	u.setCookie(w, OAuthBackCookie, "", -1)
}
