package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/uncrm/uncrm/internal/crm"
)

type dbtx interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// A challenge carries one sign-in step across two requests. The browser
// holds a random handle; auth_challenges holds its hash, the kind and a JSON
// payload. Each kind has one payload type, below.
type challengeKind string

const (
	kindOAuth        challengeKind = "oauth"         // oauthState
	kindPasskey      challengeKind = "passkey"       // ceremony
	kindSecondFactor challengeKind = "second_factor" // pendingLogin
	kindTOTPEnrol    challengeKind = "totp_enrol"    // totpEnrolment
)

const challengeTTL = 10 * time.Minute

var errChallenge = fmt.Errorf("%w: this sign-in step expired or was already used; start again", crm.ErrInvalid)

func putChallenge(ctx context.Context, db dbtx, kind challengeKind, payload any) (string, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	if _, err := db.Exec(ctx, `DELETE FROM auth_challenges WHERE expires_at < now()`); err != nil {
		return "", err
	}
	handle := newToken()
	_, err = db.Exec(ctx, `
		INSERT INTO auth_challenges (handle_hash, kind, data, expires_at)
		VALUES ($1, $2, $3, now() + make_interval(secs => $4))`,
		hashToken(handle), kind, data, challengeTTL.Seconds())
	return handle, err
}

// takeChallenge deletes the challenge and decodes its payload into out, so a
// handle works once even when two replicas race for it.
func takeChallenge(ctx context.Context, db dbtx, kind challengeKind, handle string, out any) error {
	return scanChallenge(db.QueryRow(ctx, `
		DELETE FROM auth_challenges WHERE handle_hash = $1 AND kind = $2 AND expires_at > now()
		RETURNING data`, hashToken(handle), kind), out)
}

// peekChallenge reads a challenge without using it up, for steps where a
// typo may be retried.
func peekChallenge(ctx context.Context, db dbtx, kind challengeKind, handle string, out any) error {
	return scanChallenge(db.QueryRow(ctx, `
		SELECT data FROM auth_challenges WHERE handle_hash = $1 AND kind = $2 AND expires_at > now()`,
		hashToken(handle), kind), out)
}

func scanChallenge(row pgx.Row, out any) error {
	var data []byte
	err := row.Scan(&data)
	if errors.Is(err, pgx.ErrNoRows) {
		return errChallenge
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

// dropChallenge deletes a peeked challenge once its step succeeded. It
// reports false when another request used it first.
func dropChallenge(ctx context.Context, db dbtx, handle string) (bool, error) {
	tag, err := db.Exec(ctx, `DELETE FROM auth_challenges WHERE handle_hash = $1`, hashToken(handle))
	return tag.RowsAffected() == 1, err
}
