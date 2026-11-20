package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/uncrm/uncrm/internal/crm"
)

// Setup is open while nobody can sign in: no passkey, no linked identity
// and no authenticator app exists. That covers an empty users table and an
// instance upgraded from password-only logins. While it is open, each boot
// adds a setup token and main logs its link. The holder of a link registers
// a passkey or signs in with a provider and becomes the first user, or
// claims the existing account with their email. That closes setup and
// deletes every token.

// MinSetupTokenLen is the shortest SETUP_TOKEN main accepts.
const MinSetupTokenLen = 32

var errSetupClosed = fmt.Errorf("%w: this setup link is invalid, or setup is already done; sign in instead", crm.ErrNotFound)

// SetupOpen reports whether nobody can sign in yet.
func (u *Users) SetupOpen(ctx context.Context) (bool, error) {
	return setupOpen(ctx, u.db)
}

func setupOpen(ctx context.Context, db dbtx) (bool, error) {
	var open bool
	err := db.QueryRow(ctx, `SELECT NOT EXISTS (SELECT 1 FROM passkeys)
		AND NOT EXISTS (SELECT 1 FROM user_identities) AND NOT EXISTS (SELECT 1 FROM totp)`).Scan(&open)
	return open, err
}

// PrepareSetup returns a setup token to log, or "" when setup is closed.
// fixed, from SETUP_TOKEN, is used instead of a random token when set.
func (u *Users) PrepareSetup(ctx context.Context, fixed string) (string, error) {
	token := fixed
	if token == "" {
		token = newToken()
	}
	err := pgx.BeginFunc(ctx, u.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('uncrm.setup'))`); err != nil {
			return err
		}
		open, err := setupOpen(ctx, tx)
		if err != nil {
			return err
		}
		if !open {
			token = ""
			_, err := tx.Exec(ctx, `DELETE FROM setup_tokens`)
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO setup_tokens (token_hash) VALUES ($1) ON CONFLICT DO NOTHING`, hashToken(token))
		return err
	})
	return token, err
}

// SetupURL is the link that opens the setup page for token.
func (u *Users) SetupURL(token string) string {
	return u.cfg.PublicURL.JoinPath("setup", token).String()
}

// CheckSetupToken reports errSetupClosed unless token can still set up.
func (u *Users) CheckSetupToken(ctx context.Context, token string) error {
	return checkSetup(ctx, u.db, hashToken(token))
}

func checkSetup(ctx context.Context, db dbtx, tokenHash []byte) error {
	var ok bool
	if err := db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM setup_tokens WHERE token_hash = $1)`, tokenHash).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return errSetupClosed
	}
	open, err := setupOpen(ctx, db)
	if err != nil {
		return err
	}
	if !open {
		return errSetupClosed
	}
	return nil
}

// claimSetup uses up setup inside tx and returns the first user: the
// existing account with email, or a new one. handle, when set, becomes the
// user's WebAuthn handle for the passkey being registered with it. The
// caller must give the user a sign-in method in the same transaction.
func claimSetup(ctx context.Context, tx pgx.Tx, tokenHash []byte, email, name string, handle []byte) (int64, error) {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('uncrm.setup'))`); err != nil {
		return 0, err
	}
	if err := checkSetup(ctx, tx, tokenHash); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM setup_tokens`); err != nil {
		return 0, err
	}
	var id int64
	err := tx.QueryRow(ctx, `
		UPDATE users SET webauthn_handle = coalesce($2, webauthn_handle), name = CASE WHEN name = '' THEN $3 ELSE name END
		WHERE email = $1 RETURNING id`, email, handle, strings.TrimSpace(name)).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return createUser(ctx, tx, email, name, handle)
	}
	return id, err
}
