package auth

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/unkeyed/keyring/internal/crm"
)

// Setup is open while nobody can sign in: no passkey, no linked identity
// and no authenticator app exists. That covers an empty users table and an
// instance upgraded from password-only logins. While it is open, each boot
// adds a setup token and main logs its link. The holder of a link registers
// a passkey or signs in with a provider and becomes the first user, or
// claims the existing account with their email. That closes setup and
// deletes every token.
//
// A recovery link is a setup token bound to one existing user (user_id),
// issued by `keyring recover <email>` for someone who lost every sign-in
// method. It works while setup is closed, only for that user's email, for
// recoveryTTL, and only once. Using it adds a passkey and ends the user's
// other sessions.

const MinSetupTokenLen = 32

const recoveryTTL = time.Hour

var errSetupClosed = fmt.Errorf("%w: this setup link is invalid, or setup is already done; sign in instead", crm.ErrNotFound)

func (u *Users) SetupOpen(ctx context.Context) (bool, error) {
	return u.setupOpen(ctx, u.db)
}

// setupOpen counts the sign-in methods changeMethods counts, so removing a
// provider's variables or PASSWORD_LOGIN cannot lock everyone out without
// a setup link.
func (u *Users) setupOpen(ctx context.Context, db dbtx) (bool, error) {
	var open bool
	err := db.QueryRow(ctx, `SELECT NOT EXISTS (SELECT 1 FROM passkeys)
		AND NOT EXISTS (SELECT 1 FROM user_identities WHERE provider = ANY($1))
		AND NOT ($2 AND EXISTS (SELECT 1 FROM users JOIN totp ON totp.user_id = users.id WHERE users.password_hash IS NOT NULL))`,
		u.providerIDs(), u.cfg.PasswordLogin).Scan(&open)
	return open, err
}

func (u *Users) PrepareSetup(ctx context.Context, fixed string) (string, error) {
	token := fixed
	if token == "" {
		token = newToken()
	}
	err := pgx.BeginFunc(ctx, u.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('keyring.setup'))`); err != nil {
			return err
		}
		open, err := u.setupOpen(ctx, tx)
		if err != nil {
			return err
		}
		if !open {
			token = ""
			_, err := tx.Exec(ctx, `DELETE FROM setup_tokens WHERE user_id IS NULL`)
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO setup_tokens (token_hash) VALUES ($1) ON CONFLICT DO NOTHING`, hashToken(token))
		return err
	})
	return token, err
}

func (u *Users) SetupURL(token string) string {
	return u.cfg.PublicURL.JoinPath("setup", token).String()
}

// CheckSetupToken reports whether token is a recovery link rather than the
// first-account setup link, or errSetupClosed when it is neither.
func (u *Users) CheckSetupToken(ctx context.Context, token string) (recovery bool, err error) {
	g, err := u.checkSetup(ctx, u.db, hashToken(token))
	return g.userID != 0, err
}

// setupGrant is what a valid setup token allows: creating or claiming the
// first account, or, when userID is set, recovering that one user.
type setupGrant struct {
	userID int64
	email  string
	handle []byte // the user's WebAuthn handle, nil before their first passkey
}

func (u *Users) checkSetup(ctx context.Context, db dbtx, tokenHash []byte) (setupGrant, error) {
	var g setupGrant
	var userID *int64
	var email *string
	err := db.QueryRow(ctx, `
		SELECT t.user_id, u.email, u.webauthn_handle FROM setup_tokens t LEFT JOIN users u ON u.id = t.user_id
		WHERE t.token_hash = $1 AND (t.expires_at IS NULL OR t.expires_at > now())`, tokenHash).Scan(&userID, &email, &g.handle)
	if errors.Is(err, pgx.ErrNoRows) {
		return g, errSetupClosed
	}
	if err != nil {
		return g, err
	}
	if userID != nil {
		g.userID, g.email = *userID, *email
		return g, nil
	}
	open, err := u.setupOpen(ctx, db)
	if err != nil {
		return g, err
	}
	if !open {
		return g, errSetupClosed
	}
	return g, nil
}

// IssueRecovery returns a one-time link that lets the user with email
// register a new passkey, for `keyring recover`. Whoever runs it holds the
// database credentials, which already control every account.
func IssueRecovery(ctx context.Context, db *pgxpool.Pool, publicURL *url.URL, email string) (string, error) {
	email = normalizeEmail(email)
	var id int64
	err := db.QueryRow(ctx, `SELECT id FROM users WHERE email = $1`, email).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("%w: no account has the email %s", crm.ErrNotFound, email)
	}
	if err != nil {
		return "", err
	}
	token := newToken()
	err = pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM setup_tokens WHERE expires_at < now()`); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO setup_tokens (token_hash, user_id, expires_at) VALUES ($1, $2, now() + make_interval(secs => $3))`,
			hashToken(token), id, recoveryTTL.Seconds())
		return err
	})
	return publicURL.JoinPath("setup", token).String(), err
}

// claimSetup uses up setup inside tx and returns the first user: the
// existing account with email, or a new one. handle, when set, becomes the
// user's WebAuthn handle for the passkey being registered with it. The
// caller must give the user a sign-in method in the same transaction.
func (u *Users) claimSetup(ctx context.Context, tx pgx.Tx, tokenHash []byte, email, name string, handle []byte) (int64, error) {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('keyring.setup'))`); err != nil {
		return 0, err
	}
	g, err := u.checkSetup(ctx, tx, tokenHash)
	if err != nil {
		return 0, err
	}
	if g.userID != 0 {
		return u.claimRecovery(ctx, tx, tokenHash, g, email, handle)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM setup_tokens WHERE user_id IS NULL`); err != nil {
		return 0, err
	}
	var id int64
	err = tx.QueryRow(ctx, `
		UPDATE users SET webauthn_handle = coalesce($2, webauthn_handle), name = CASE WHEN name = '' THEN $3 ELSE name END
		WHERE email = $1 RETURNING id`, email, handle, strings.TrimSpace(name)).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return createUser(ctx, tx, email, name, handle)
	}
	return id, err
}

// claimRecovery uses up a recovery link for g.userID. The caller adds the
// new sign-in method in the same transaction. Every existing session ends,
// since whoever lost the old method may have lost a signed-in device too.
func (u *Users) claimRecovery(ctx context.Context, tx pgx.Tx, tokenHash []byte, g setupGrant, email string, handle []byte) (int64, error) {
	if email != g.email {
		return 0, fmt.Errorf("%w: this recovery link is for another account; sign in with the email it was issued for", crm.ErrInvalid)
	}
	tag, err := tx.Exec(ctx, `DELETE FROM setup_tokens WHERE token_hash = $1 AND user_id = $2`, tokenHash, g.userID)
	if err != nil {
		return 0, err
	}
	if tag.RowsAffected() == 0 {
		return 0, errSetupClosed
	}
	if _, err := tx.Exec(ctx, `UPDATE users SET webauthn_handle = coalesce(webauthn_handle, $2) WHERE id = $1`, g.userID, handle); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM sessions WHERE user_id = $1`, g.userID); err != nil {
		return 0, err
	}
	return g.userID, nil
}
