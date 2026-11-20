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
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/unkeyed/keyring/internal/crm"
)

// Human sign-in. A person signs in with one of:
//
//   - a passkey (discoverable, user verification required), complete alone;
//   - a linked Google, GitHub or OIDC identity, followed by an authenticator
//     code when the person enrolled one;
//   - email and password, only when Config.PasswordLogin is on, and always
//     followed by an authenticator code or a passkey.
//
// A password alone never yields a session. SignIn is the outcome of one step:
// a session, or a pending login that waits for its second factor.
//
// Every piece of state between two requests lives in Postgres
// (auth_challenges, setup_tokens, login_failures), so any replica can serve
// any step.
//
// Bootstrap: while nobody can sign in, each boot adds a setup link
// (PrepareSetup). Its holder registers a passkey or signs in with a
// configured provider and becomes the first user. Teammates join through
// one-time invite links.
//
// Sessions: the cookie holds 32 random bytes; only their SHA-256 is stored.
// A session expires after 30 days without use. Each use pushes expires_at
// and last_seen_at out again, written at most every five minutes. The
// browser keeps the cookie for the 400 days browsers allow, so the database
// alone decides expiry. CSRF: httpapi wraps cookie routes in
// http.CrossOriginProtection.
type Users struct {
	db        *pgxpool.Pool
	cfg       Config
	webauthn  *webauthn.WebAuthn
	providers map[string]provider
}

const (
	sessionTTL       = 30 * 24 * time.Hour
	sessionSlideStep = 5 * time.Minute
	cookieMaxAge     = 400 * 24 * time.Hour
	inviteTTL        = 7 * 24 * time.Hour
	minPasswordLen   = 12
)

var (
	errBadLogin = fmt.Errorf("%w: wrong email or password", ErrUnauthorized)
	// ErrInvalidInvite is a token that is unknown, expired or already used.
	ErrInvalidInvite = fmt.Errorf("invite link is invalid, expired or already used: %w", crm.ErrNotFound)
	// ErrReauth asks the person to prove a factor again before a change to
	// how they or the team sign in. Serve it as 403.
	ErrReauth           = errors.New("confirm it's you: this change needs a sign-in within the last 15 minutes. Confirm under Settings, Account, or sign in again")
	ErrPasswordLoginOff = fmt.Errorf("%w: password sign-in is turned off on this instance", crm.ErrNotFound)
)

// ThrottledError answers a sign-in attempt while its email, IP or user is
// backing off after repeated failures. Serve it as 429 with Retry-After.
type ThrottledError struct {
	RetryAfter time.Duration // whole seconds, at least 1s
}

func (e *ThrottledError) Error() string {
	return fmt.Sprintf("too many failed attempts, try again in %s", e.RetryAfter)
}

// User is a person who can sign into the UI.
type User struct {
	ID        int64     `json:"id"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

func NewUsers(db *pgxpool.Pool, cfg Config) (*Users, error) {
	wa, err := webauthn.New(&webauthn.Config{
		RPID:          cfg.PublicURL.Hostname(),
		RPDisplayName: "Keyring",
		RPOrigins:     []string{origin(cfg.PublicURL)},
	})
	if err != nil {
		return nil, fmt.Errorf("PUBLIC_URL %s cannot be a WebAuthn relying party: %w", cfg.PublicURL, err)
	}
	u := &Users{db: db, cfg: cfg, webauthn: wa, providers: map[string]provider{}}
	for _, p := range cfg.Providers {
		u.providers[p.ID] = newProvider(p, u.callbackURL(p.ID))
	}
	return u, nil
}

func origin(u *url.URL) string { return u.Scheme + "://" + u.Host }

func (u *Users) callbackURL(provider string) string {
	return u.cfg.PublicURL.JoinPath("auth", "oauth", provider, "callback").String()
}

const pbkdf2Iterations = 600_000

// hashPassword: PBKDF2-HMAC-SHA256, 600,000 iterations, 16-byte salt,
// stored as "pbkdf2-sha256$600000$<salt b64>$<hash b64>".
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
	h, _ := hashPassword("Keyring timing equalizer")
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

func checkEmail(email string) error {
	if local, domain, ok := strings.Cut(email, "@"); !ok || local == "" || domain == "" || strings.Contains(domain, "@") {
		return fmt.Errorf("%w: %q is not an email address", crm.ErrInvalid, email)
	}
	return nil
}

// PasswordLogin checks an email and password. Success is never a session:
// it is a pending login that needs an authenticator code or a passkey.
// Wrong email and wrong password return the same ErrUnauthorized after the
// same PBKDF2 work. An email or IP in backoff gets a *ThrottledError before
// any password check.
func (u *Users) PasswordLogin(ctx context.Context, email, password string, c Client) (SignIn, error) {
	if !u.cfg.PasswordLogin {
		return SignIn{}, ErrPasswordLoginOff
	}
	email = normalizeEmail(email)
	key := "email:" + email
	var id int64
	err := guarded(ctx, u.db, []string{key, c.ipKey()}, []string{key}, func(err error) bool { return errors.Is(err, errBadLogin) }, func(tx pgx.Tx) error {
		var stored *string
		err := tx.QueryRow(ctx, `SELECT id, password_hash FROM users WHERE email = $1`, email).Scan(&id, &stored)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		hash := dummyHash()
		if stored != nil {
			hash = *stored
		}
		if !verifyPassword(hash, password) || stored == nil {
			return errBadLogin
		}
		return nil
	})
	if err != nil {
		return SignIn{}, err
	}
	return u.afterFirstFactor(ctx, id, MethodPassword, nil, c)
}

// Invite returns a one-time token valid for 7 days. The UI builds the link.
func (u *Users) Invite(ctx context.Context, by crm.Actor, email string) (token string, err error) {
	inviter := userID(by)
	if inviter == nil {
		return "", fmt.Errorf("%w: only a signed-in person can invite", ErrUnauthorized)
	}
	email = normalizeEmail(email)
	if err := checkEmail(email); err != nil {
		return "", err
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

func (u *Users) InviteEmail(ctx context.Context, token string) (string, error) {
	var email string
	err := u.db.QueryRow(ctx, `SELECT email FROM invites WHERE token_hash = $1 AND accepted_at IS NULL AND expires_at > now()`,
		hashToken(token)).Scan(&email)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrInvalidInvite
	}
	return email, err
}

// claimInvite marks the invite used inside tx and returns its email. Of two
// concurrent claims exactly one succeeds.
func claimInvite(ctx context.Context, tx pgx.Tx, tokenHash []byte) (string, error) {
	var email string
	err := tx.QueryRow(ctx, `
		UPDATE invites SET accepted_at = now()
		WHERE token_hash = $1 AND accepted_at IS NULL AND expires_at > now()
		RETURNING email`, tokenHash).Scan(&email)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrInvalidInvite
	}
	return email, err
}

// claimPendingInvite marks any live invite for email used, so social sign-in
// can accept an invite without its link.
func claimPendingInvite(ctx context.Context, tx pgx.Tx, email string) (bool, error) {
	tag, err := tx.Exec(ctx, `UPDATE invites SET accepted_at = now() WHERE email = $1 AND accepted_at IS NULL AND expires_at > now()`, email)
	return tag.RowsAffected() > 0, err
}

func createUser(ctx context.Context, tx pgx.Tx, email, name string, handle []byte) (int64, error) {
	var id int64
	err := tx.QueryRow(ctx, `
		INSERT INTO users (email, name, webauthn_handle) VALUES ($1, $2, coalesce($3, uuid_send(gen_random_uuid())))
		RETURNING id`, email, strings.TrimSpace(name), handle).Scan(&id)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return 0, fmt.Errorf("%w: %s already has an account, sign in instead", crm.ErrConflict, email)
	}
	return id, err
}

// List returns every user, oldest first.
func (u *Users) List(ctx context.Context) ([]User, error) {
	rows, err := u.db.Query(ctx, `SELECT id, email, name, created_at FROM users ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[User])
}

// Remove deletes a teammate with their sessions, passkeys, identities,
// authenticator and the apps they approved for MCP, whose tokens go with
// them, and revokes the API keys they created. The last user cannot
// be removed; the advisory lock keeps two people removing each other from
// emptying the team. The keys go first, in the same transaction, so a key
// store that cannot revoke them keeps the teammate for a retry.
func (u *Users) Remove(ctx context.Context, by crm.Actor, id int64, keys KeyStore) error {
	if userID(by) == nil {
		return fmt.Errorf("%w: only a signed-in person can remove a teammate", ErrUnauthorized)
	}
	return pgx.BeginFunc(ctx, u.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('keyring.users'))`); err != nil {
			return err
		}
		// The row lock waits out a key the teammate is creating right now and
		// holds off the next one, so revoking cannot miss a key.
		tag, err := tx.Exec(ctx, `SELECT FROM users WHERE id = $1 FOR UPDATE`, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return fmt.Errorf("%w: no user %d", crm.ErrNotFound, id)
		}
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&n); err != nil {
			return err
		}
		if n <= 1 {
			return fmt.Errorf("%w: the last person on the team cannot be removed", crm.ErrConflict)
		}
		if err := keys.RevokeCreatedBy(ctx, tx, id); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `DELETE FROM users WHERE id = $1`, id)
		return err
	})
}
