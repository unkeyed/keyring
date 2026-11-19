package auth

import (
	"context"
	"errors"
	"net/http"
	"time"

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
// "pbkdf2-sha256$600000$<salt b64>$<hash b64>" (stdlib crypto/pbkdf2).
// Cookie: "uncrm_session", 32 random bytes, HttpOnly, Secure (unless the
// request is plain-HTTP localhost), SameSite=Lax, 30 days. Only its SHA-256
// is stored. CSRF: httpapi wraps cookie routes in http.CrossOriginProtection.
type Users struct {
	db *pgxpool.Pool
}

const SessionCookie = "uncrm_session"

func NewUsers(db *pgxpool.Pool) *Users { return &Users{db: db} }

// SeedAdmin creates the first user. Both empty is a no-op; the login page
// then explains which env vars to set when the users table is empty.
func (u *Users) SeedAdmin(ctx context.Context, email, password string) error {
	return errors.New("not implemented")
}

// Login checks the password and returns a new session cookie value.
// Wrong email and wrong password return the same ErrUnauthorized after the
// same PBKDF2 work.
func (u *Users) Login(ctx context.Context, email, password string) (cookie string, expires time.Time, err error) {
	return "", time.Time{}, errors.New("not implemented")
}

// Session resolves a cookie value to its user, or ErrUnauthorized.
func (u *Users) Session(ctx context.Context, cookie string) (crm.Actor, error) {
	return crm.Actor{}, errors.New("not implemented")
}

func (u *Users) Logout(ctx context.Context, cookie string) error {
	return errors.New("not implemented")
}

// Invite returns a one-time token valid for 7 days. The UI builds the link.
func (u *Users) Invite(ctx context.Context, by crm.Actor, email string) (token string, err error) {
	return "", errors.New("not implemented")
}

// AcceptInvite creates the user and logs them in.
func (u *Users) AcceptInvite(ctx context.Context, token, name, password string) (cookie string, expires time.Time, err error) {
	return "", time.Time{}, errors.New("not implemented")
}

func (u *Users) ChangePassword(ctx context.Context, who crm.Actor, oldPassword, newPassword string) error {
	return errors.New("not implemented")
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
// Neither present, or either invalid, is ErrUnauthorized.
func (a *Authenticator) Caller(r *http.Request) (crm.Actor, Via, error) {
	return crm.Actor{}, 0, errors.New("not implemented")
}
