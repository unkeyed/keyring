package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/unkeyed/keyring/internal/crm"
)

// Setup is open while nobody can sign in: no passkey, no linked identity
// and no authenticator app exists. That covers an empty users table and an
// instance upgraded from password-only logins. While it is open, each boot
// adds a setup token and main logs its link. The holder of a link registers
// a passkey or signs in with a provider and becomes the first user, or
// claims the existing account with their email. That closes setup and
// deletes every token.

const MinSetupTokenLen = 32

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
			_, err := tx.Exec(ctx, `DELETE FROM setup_tokens`)
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

func (u *Users) CheckSetupToken(ctx context.Context, token string) error {
	return u.checkSetup(ctx, u.db, hashToken(token))
}

func (u *Users) checkSetup(ctx context.Context, db dbtx, tokenHash []byte) error {
	var ok bool
	if err := db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM setup_tokens WHERE token_hash = $1)`, tokenHash).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return errSetupClosed
	}
	open, err := u.setupOpen(ctx, db)
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
func (u *Users) claimSetup(ctx context.Context, tx pgx.Tx, tokenHash []byte, email, name string, handle []byte) (int64, error) {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('keyring.setup'))`); err != nil {
		return 0, err
	}
	if err := u.checkSetup(ctx, tx, tokenHash); err != nil {
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
