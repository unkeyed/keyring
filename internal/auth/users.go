package auth

import (
	"context"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/uncrm/uncrm/internal/crm"
)

// Human login: email + password, DB-backed session cookie.
//
// Bootstrap: on every boot SeedAdmin inserts ADMIN_EMAIL / ADMIN_PASSWORD
// ON CONFLICT (email) DO NOTHING. Idempotent across replicas and restarts,
// needs no shell (Unkey Deploy has none) and no log scraping. Once the admin
// changes their password in the UI the env value stops mattering.
// Teammates join through one-time invite links the admin creates.
//
// Passwords: PBKDF2-HMAC-SHA256, 600,000 iterations, 16-byte salt, stored as
// "pbkdf2-sha256$600000$<salt b64>$<hash b64>" (stdlib crypto/pbkdf2), at
// least 12 characters.
//
// Sessions: the cookie holds 32 random bytes; only their SHA-256 is stored.
// A session expires after 30 days without use. Each use pushes expires_at
// out again, written at most once an hour. The browser keeps the cookie for
// the 400 days browsers allow, so the database alone decides expiry and an
// active user is never logged out. Cookie attributes: SetSessionCookie.
// CSRF: httpapi wraps cookie routes in http.CrossOriginProtection.
//
// Throttling: login_failures counts failures per email in Postgres, so
// every replica sees the same count. See loginWait for the policy.
type Users struct {
	db *pgxpool.Pool
}

const SessionCookie = "uncrm_session"

const (
	sessionTTL       = 30 * 24 * time.Hour
	sessionSlideStep = time.Hour
	cookieMaxAge     = 400 * 24 * time.Hour
	inviteTTL        = 7 * 24 * time.Hour
	minPasswordLen   = 12
)

var (
	errBadLogin = fmt.Errorf("%w: wrong email or password", ErrUnauthorized)
	// ErrInvalidInvite is a token that is unknown, expired or already used.
	ErrInvalidInvite = fmt.Errorf("invite link is invalid, expired or already used: %w", crm.ErrNotFound)
)

// ThrottledError is Login's answer while an email is backing off after
// repeated failures. Serve it as 429 with a Retry-After header.
type ThrottledError struct {
	RetryAfter time.Duration // whole seconds, at least 1s
}

func (e *ThrottledError) Error() string {
	return fmt.Sprintf("too many failed logins, try again in %s", e.RetryAfter)
}

// User is a person who can log into the UI.
type User struct {
	ID        int64     `json:"id"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

func NewUsers(db *pgxpool.Pool) *Users { return &Users{db: db} }

// SeedAdmin creates the first user. Both empty is a no-op; the login page
// then explains which env vars to set when the users table is empty.
// A short ADMIN_PASSWORD fails every boot, even once the admin exists, so
// the weak value cannot sit unnoticed in the environment.
func (u *Users) SeedAdmin(ctx context.Context, email, password string) error {
	if email == "" {
		return nil
	}
	if utf8.RuneCountInString(password) < minPasswordLen {
		return fmt.Errorf("ADMIN_PASSWORD must be at least %d characters", minPasswordLen)
	}
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}
	_, err = u.db.Exec(ctx, `INSERT INTO users (email, password_hash) VALUES ($1, $2) ON CONFLICT (email) DO NOTHING`,
		normalizeEmail(email), hash)
	return err
}

const pbkdf2Iterations = 600_000

// hashPassword returns "pbkdf2-sha256$600000$<salt b64>$<hash b64>".
func hashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, pbkdf2Iterations, 32)
	if err != nil {
		return "", err
	}
	b64 := base64.RawStdEncoding.EncodeToString
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s", pbkdf2Iterations, b64(salt), b64(key)), nil
}

// verifyPassword reports whether password matches a hashPassword result,
// comparing the derived keys in constant time.
func verifyPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil || iter < 1 {
		return false
	}
	salt, err1 := base64.RawStdEncoding.DecodeString(parts[2])
	want, err2 := base64.RawStdEncoding.DecodeString(parts[3])
	if err1 != nil || err2 != nil {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, password, salt, iter, len(want))
	return err == nil && subtle.ConstantTimeCompare(got, want) == 1
}

// dummyHash gives an unknown email the same PBKDF2 work as a known one.
var dummyHash = sync.OnceValue(func() string {
	h, _ := hashPassword("uncrm timing equalizer")
	return h
})

func checkPassword(password string) error {
	if utf8.RuneCountInString(password) < minPasswordLen {
		return fmt.Errorf("%w: password must be at least %d characters", crm.ErrInvalid, minPasswordLen)
	}
	return nil
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

const (
	freeLoginAttempts  = 5
	maxLoginBackoff    = 15 * time.Minute
	loginFailureWindow = time.Hour
)

// loginWait is how long an email must still wait before its next attempt,
// given its failure count and the time since the last failure. The first
// five failures cost nothing. After that each failure doubles the wait,
// from 1s up to 15 minutes. An hour without failures forgets the count.
func loginWait(failures int, sinceLast time.Duration) time.Duration {
	if sinceLast >= loginFailureWindow || failures < freeLoginAttempts {
		return 0
	}
	backoff := min(time.Second<<min(failures-freeLoginAttempts, 20), maxLoginBackoff)
	return max(backoff-sinceLast, 0)
}

// Login checks the password and returns a new session cookie value and the
// session's current expiry. Wrong email and wrong password return the same
// ErrUnauthorized after the same PBKDF2 work. An email in backoff gets a
// *ThrottledError before any password check.
func (u *Users) Login(ctx context.Context, email, password string) (cookie string, expires time.Time, err error) {
	email = normalizeEmail(email)
	var failures int
	var sinceLast float64
	err = u.db.QueryRow(ctx, `SELECT failures, extract(epoch FROM now() - last_failed_at)::float8 FROM login_failures WHERE email = $1`,
		email).Scan(&failures, &sinceLast)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", time.Time{}, err
	}
	if wait := loginWait(failures, time.Duration(sinceLast*float64(time.Second))); wait > 0 {
		return "", time.Time{}, &ThrottledError{RetryAfter: wait.Truncate(time.Second) + time.Second}
	}

	var id int64
	hash := dummyHash()
	err = u.db.QueryRow(ctx, `SELECT id, password_hash FROM users WHERE email = $1`, email).Scan(&id, &hash)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", time.Time{}, err
	}
	if !verifyPassword(hash, password) || id == 0 {
		if err := u.recordFailure(ctx, email); err != nil {
			return "", time.Time{}, err
		}
		return "", time.Time{}, errBadLogin
	}
	if _, err := u.db.Exec(ctx, `DELETE FROM login_failures WHERE email = $1`, email); err != nil {
		return "", time.Time{}, err
	}
	return u.newSession(ctx, id)
}

func (u *Users) recordFailure(ctx context.Context, email string) error {
	window := loginFailureWindow.Seconds()
	if _, err := u.db.Exec(ctx, `
		INSERT INTO login_failures (email, failures, last_failed_at) VALUES ($1, 1, now())
		ON CONFLICT (email) DO UPDATE SET
			failures = CASE WHEN login_failures.last_failed_at < now() - make_interval(secs => $2)
			           THEN 1 ELSE login_failures.failures + 1 END,
			last_failed_at = now()`, email, window); err != nil {
		return err
	}
	_, err := u.db.Exec(ctx, `DELETE FROM login_failures WHERE last_failed_at < now() - interval '1 day'`)
	return err
}

func (u *Users) newSession(ctx context.Context, userID int64) (string, time.Time, error) {
	if _, err := u.db.Exec(ctx, `DELETE FROM sessions WHERE expires_at < now()`); err != nil {
		return "", time.Time{}, err
	}
	token := newToken()
	var expires time.Time
	err := u.db.QueryRow(ctx, `
		INSERT INTO sessions (token_hash, user_id, expires_at)
		VALUES ($1, $2, now() + make_interval(secs => $3)) RETURNING expires_at`,
		hashToken(token), userID, sessionTTL.Seconds()).Scan(&expires)
	if err != nil {
		return "", time.Time{}, err
	}
	return token, expires, nil
}

// Session resolves a cookie value to its user, or ErrUnauthorized, and
// slides the session's expiry.
func (u *Users) Session(ctx context.Context, cookie string) (crm.Actor, error) {
	if cookie == "" {
		return crm.Actor{}, ErrUnauthorized
	}
	var id int64
	var email, name string
	err := u.db.QueryRow(ctx, `
		WITH s AS (
			SELECT s.token_hash, s.expires_at, u.id, u.email, u.name
			FROM sessions s JOIN users u ON u.id = s.user_id
			WHERE s.token_hash = $1 AND s.expires_at > now()
		), slide AS (
			UPDATE sessions SET expires_at = now() + make_interval(secs => $2::float8) FROM s
			WHERE sessions.token_hash = s.token_hash
			  AND s.expires_at < now() + make_interval(secs => $2::float8 - $3::float8)
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

// SetSessionCookie writes the cookie Login and AcceptInvite return:
// HttpOnly, SameSite=Lax, Secure when the request arrived over HTTPS
// (directly or via a proxy's X-Forwarded-Proto).
func SetSessionCookie(w http.ResponseWriter, r *http.Request, value string) {
	http.SetCookie(w, &http.Cookie{
		Name: SessionCookie, Value: value, Path: "/",
		MaxAge: int(cookieMaxAge.Seconds()), HttpOnly: true, Secure: isHTTPS(r), SameSite: http.SameSiteLaxMode,
	})
}

// ClearSessionCookie tells the browser to drop the session cookie.
func ClearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: SessionCookie, Path: "/",
		MaxAge: -1, HttpOnly: true, Secure: isHTTPS(r), SameSite: http.SameSiteLaxMode,
	})
}

func isHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// Invite returns a one-time token valid for 7 days. The UI builds the link.
func (u *Users) Invite(ctx context.Context, by crm.Actor, email string) (token string, err error) {
	inviter := userID(by)
	if inviter == nil {
		return "", fmt.Errorf("%w: only a signed-in person can invite", ErrUnauthorized)
	}
	email = normalizeEmail(email)
	if local, domain, ok := strings.Cut(email, "@"); !ok || local == "" || domain == "" || strings.Contains(domain, "@") {
		return "", fmt.Errorf("%w: %q is not an email address", crm.ErrInvalid, email)
	}
	var exists bool
	if err := u.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE email = $1)`, email).Scan(&exists); err != nil {
		return "", err
	}
	if exists {
		return "", fmt.Errorf("%w: %s already has an account", crm.ErrConflict, email)
	}
	token = newToken()
	_, err = u.db.Exec(ctx, `
		INSERT INTO invites (token_hash, email, created_by, expires_at)
		VALUES ($1, $2, $3, now() + make_interval(secs => $4))`,
		hashToken(token), email, *inviter, inviteTTL.Seconds())
	if err != nil {
		return "", err
	}
	return token, nil
}

// AcceptInvite creates the user and logs them in. The token works once:
// claiming it and creating the user commit together, so of two concurrent
// accepts exactly one succeeds.
func (u *Users) AcceptInvite(ctx context.Context, token, name, password string) (cookie string, expires time.Time, err error) {
	if err := checkPassword(password); err != nil {
		return "", time.Time{}, err
	}
	hash, err := hashPassword(password)
	if err != nil {
		return "", time.Time{}, err
	}
	var id int64
	err = pgx.BeginFunc(ctx, u.db, func(tx pgx.Tx) error {
		var email string
		err := tx.QueryRow(ctx, `
			UPDATE invites SET accepted_at = now()
			WHERE token_hash = $1 AND accepted_at IS NULL AND expires_at > now()
			RETURNING email`, hashToken(token)).Scan(&email)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrInvalidInvite
		}
		if err != nil {
			return err
		}
		err = tx.QueryRow(ctx, `
			INSERT INTO users (email, name, password_hash) VALUES ($1, $2, $3)
			ON CONFLICT (email) DO NOTHING RETURNING id`,
			email, strings.TrimSpace(name), hash).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: %s already has an account, log in instead", crm.ErrConflict, email)
		}
		return err
	})
	if err != nil {
		return "", time.Time{}, err
	}
	return u.newSession(ctx, id)
}

func (u *Users) ChangePassword(ctx context.Context, who crm.Actor, oldPassword, newPassword string) error {
	id := userID(who)
	if id == nil {
		return ErrUnauthorized
	}
	if err := checkPassword(newPassword); err != nil {
		return err
	}
	var current string
	err := u.db.QueryRow(ctx, `SELECT password_hash FROM users WHERE id = $1`, *id).Scan(&current)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrUnauthorized
	}
	if err != nil {
		return err
	}
	if !verifyPassword(current, oldPassword) {
		return fmt.Errorf("%w: current password is wrong", crm.ErrInvalid)
	}
	hash, err := hashPassword(newPassword)
	if err != nil {
		return err
	}
	_, err = u.db.Exec(ctx, `UPDATE users SET password_hash = $1 WHERE id = $2`, hash, *id)
	return err
}

// List returns every user, oldest first.
func (u *Users) List(ctx context.Context) ([]User, error) {
	rows, err := u.db.Query(ctx, `SELECT id, email, name, created_at FROM users ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[User])
}

// Via says how a request authenticated, which decides the routes it may use.
type Via int

const (
	ViaAPIKey Via = iota + 1
	ViaSession
)

// Authenticator resolves an inbound request to an actor.
type Authenticator struct {
	Keys  KeyStore
	Users *Users
}

// Caller reads "Authorization: Bearer <key>" first, then the session cookie.
// A present but invalid credential never falls through to the other one.
// Neither present is ErrUnauthorized, an invalid key ErrInvalidKey, an
// invalid session ErrUnauthorized. Any other error means the key store or
// database failed, and the request must fail with it.
func (a *Authenticator) Caller(r *http.Request) (crm.Actor, Via, error) {
	if h := r.Header.Get("Authorization"); h != "" {
		scheme, token, ok := strings.Cut(h, " ")
		if !ok || !strings.EqualFold(scheme, "Bearer") || strings.TrimSpace(token) == "" {
			return crm.Actor{}, 0, ErrUnauthorized
		}
		actor, err := a.Keys.Verify(r.Context(), strings.TrimSpace(token))
		if err != nil {
			return crm.Actor{}, 0, err
		}
		return actor, ViaAPIKey, nil
	}
	c, err := r.Cookie(SessionCookie)
	if err != nil {
		return crm.Actor{}, 0, ErrUnauthorized
	}
	actor, err := a.Users.Session(r.Context(), c.Value)
	if err != nil {
		return crm.Actor{}, 0, err
	}
	return actor, ViaSession, nil
}
