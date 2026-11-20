package auth

import (
	"context"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/uncrm/uncrm/internal/crm"
)

type LinkedIdentity struct {
	Provider   string     `json:"provider"`
	Email      string     `json:"email"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
}

type Account struct {
	User              User             `json:"user"`
	Passkeys          []Passkey        `json:"passkeys"`
	Identities        []LinkedIdentity `json:"identities"`
	TOTP              bool             `json:"totp"`
	RecoveryCodesLeft int              `json:"recovery_codes_left"`
	Password          bool             `json:"password"`
	Recent            bool             `json:"recent"`
	Sessions          []Session        `json:"sessions"`
}

func (u *Users) Account(ctx context.Context, cur Current) (Account, error) {
	a := Account{Recent: cur.Recent}
	err := u.db.QueryRow(ctx, `
		SELECT id, email, name, created_at, password_hash IS NOT NULL,
			EXISTS (SELECT 1 FROM totp WHERE user_id = $1),
			(SELECT count(*) FROM recovery_codes WHERE user_id = $1)
		FROM users WHERE id = $1`, cur.UserID).Scan(
		&a.User.ID, &a.User.Email, &a.User.Name, &a.User.CreatedAt, &a.Password, &a.TOTP, &a.RecoveryCodesLeft)
	if err != nil {
		return a, err
	}
	rows, err := u.db.Query(ctx, `SELECT id, name, created_at, last_used_at FROM passkeys WHERE user_id = $1 ORDER BY created_at`, cur.UserID)
	if err != nil {
		return a, err
	}
	a.Passkeys, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (Passkey, error) {
		var p Passkey
		var id []byte
		err := row.Scan(&id, &p.Name, &p.CreatedAt, &p.LastUsedAt)
		p.ID = base64.RawURLEncoding.EncodeToString(id)
		return p, err
	})
	if err != nil {
		return a, err
	}
	rows, err = u.db.Query(ctx, `SELECT provider, email, created_at, last_used_at FROM user_identities WHERE user_id = $1 ORDER BY created_at`, cur.UserID)
	if err != nil {
		return a, err
	}
	if a.Identities, err = pgx.CollectRows(rows, pgx.RowToStructByPos[LinkedIdentity]); err != nil {
		return a, err
	}
	a.Sessions, err = u.Sessions(ctx, cur)
	return a, err
}

var errLastMethod = fmt.Errorf("%w: that is your last way to sign in; add a passkey or connect a login first", crm.ErrConflict)

// changeMethods runs change in a transaction that holds the user's row, so
// two concurrent removals cannot both pass the last-method check.
func (u *Users) changeMethods(ctx context.Context, cur Current, change func(tx pgx.Tx) error) error {
	if !cur.Recent {
		return ErrReauth
	}
	return pgx.BeginFunc(ctx, u.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT 1 FROM users WHERE id = $1 FOR UPDATE`, cur.UserID); err != nil {
			return err
		}
		if err := change(tx); err != nil {
			return err
		}
		var n int
		err := tx.QueryRow(ctx, `SELECT
			(SELECT count(*) FROM passkeys WHERE user_id = $1)
			+ (SELECT count(*) FROM user_identities WHERE user_id = $1 AND provider = ANY($2))
			+ (SELECT count(*) FROM users WHERE id = $1 AND $3 AND password_hash IS NOT NULL
			     AND EXISTS (SELECT 1 FROM totp WHERE user_id = $1))`,
			cur.UserID, u.providerIDs(), u.cfg.PasswordLogin).Scan(&n)
		if err != nil {
			return err
		}
		if n == 0 {
			return errLastMethod
		}
		return revokeOthers(ctx, tx, cur)
	})
}

func (u *Users) RemovePasskey(ctx context.Context, cur Current, id string) error {
	raw, err := base64.RawURLEncoding.DecodeString(id)
	if err != nil {
		return fmt.Errorf("%w: no passkey %q", crm.ErrNotFound, id)
	}
	return u.changeMethods(ctx, cur, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `DELETE FROM passkeys WHERE id = $1 AND user_id = $2`, raw, cur.UserID)
		return oneRow(tag, err, fmt.Errorf("%w: no passkey %q", crm.ErrNotFound, id))
	})
}

func (u *Users) Unlink(ctx context.Context, cur Current, provider string) error {
	return u.changeMethods(ctx, cur, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `DELETE FROM user_identities WHERE user_id = $1 AND provider = $2`, cur.UserID, provider)
		return oneRow(tag, err, fmt.Errorf("%w: no %s login is linked", crm.ErrNotFound, provider))
	})
}

var errNeedsSecondFactor = fmt.Errorf("%w: add a passkey or an authenticator app first; a password always needs a second factor", crm.ErrConflict)

// SetPassword sets or replaces the user's password. It needs a second
// factor to exist, since a password alone never signs anyone in.
func (u *Users) SetPassword(ctx context.Context, cur Current, password string) error {
	if !u.cfg.PasswordLogin {
		return ErrPasswordLoginOff
	}
	if err := checkPassword(password); err != nil {
		return err
	}
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}
	return u.changeMethods(ctx, cur, func(tx pgx.Tx) error {
		var ok bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM totp WHERE user_id = $1) OR EXISTS (SELECT 1 FROM passkeys WHERE user_id = $1)`,
			cur.UserID).Scan(&ok); err != nil {
			return err
		}
		if !ok {
			return errNeedsSecondFactor
		}
		_, err := tx.Exec(ctx, `UPDATE users SET password_hash = $2 WHERE id = $1`, cur.UserID, hash)
		return err
	})
}

func (u *Users) RemovePassword(ctx context.Context, cur Current) error {
	return u.changeMethods(ctx, cur, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE users SET password_hash = NULL WHERE id = $1 AND password_hash IS NOT NULL`, cur.UserID)
		return oneRow(tag, err, fmt.Errorf("%w: you have no password", crm.ErrNotFound))
	})
}

func oneRow(tag pgconn.CommandTag, err error, missing error) error {
	if err == nil && tag.RowsAffected() == 0 {
		return missing
	}
	return err
}
