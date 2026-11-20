package auth

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"errors"
	"fmt"
	"image/png"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	"github.com/unkeyed/keyring/internal/crm"
)

// Authenticator apps: RFC 6238 TOTP, SHA-1, 6 digits, 30-second steps, the
// defaults every app supports. A code is accepted for the current step or
// one step either side, and only for a step newer than the last accepted
// one, so a code works once even across replicas.
//
// Recovery codes: 10 per enrolment, 80 random bits each, shown once and
// stored as SHA-256. Each works once in place of an authenticator code.

const (
	totpPeriod        = 30
	recoveryCodeCount = 10
)

var (
	errBadCode     = fmt.Errorf("%w: that code is wrong or was already used", crm.ErrInvalid)
	errNoTOTP      = fmt.Errorf("%w: no authenticator app is set up", crm.ErrNotFound)
	errTOTPEnabled = fmt.Errorf("%w: an authenticator app is already set up; remove it first", crm.ErrConflict)
)

var totpOpts = totp.ValidateOpts{Period: totpPeriod, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1}

func totpStep(secret, code string, now time.Time) int64 {
	step := now.Unix() / totpPeriod
	for _, s := range []int64{step + 1, step, step - 1} {
		want, err := totp.GenerateCodeCustom(secret, time.Unix(s*totpPeriod, 0), totpOpts)
		if err == nil && subtle.ConstantTimeCompare([]byte(want), []byte(code)) == 1 {
			return s
		}
	}
	return 0
}

type TOTPEnrolment struct {
	Handle string `json:"handle"`
	Secret string `json:"secret"`
	URI    string `json:"uri"`
	QR     string `json:"qr"`
}

type totpEnrolment struct {
	UserID int64  `json:"user_id"`
	Secret string `json:"secret"`
}

// BeginTOTP generates a secret for the user's authenticator app. It is
// stored once FinishTOTP sees a code from it.
func (u *Users) BeginTOTP(ctx context.Context, cur Current) (TOTPEnrolment, error) {
	if !cur.Recent {
		return TOTPEnrolment{}, ErrReauth
	}
	var email string
	var enrolled bool
	if err := u.db.QueryRow(ctx, `SELECT email, EXISTS (SELECT 1 FROM totp WHERE user_id = $1) FROM users WHERE id = $1`,
		cur.UserID).Scan(&email, &enrolled); err != nil {
		return TOTPEnrolment{}, err
	}
	if enrolled {
		return TOTPEnrolment{}, errTOTPEnabled
	}
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer: "Keyring (" + u.cfg.PublicURL.Host + ")", AccountName: email,
		Period: totpPeriod, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1,
	})
	if err != nil {
		return TOTPEnrolment{}, err
	}
	img, err := key.Image(240, 240)
	if err != nil {
		return TOTPEnrolment{}, err
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return TOTPEnrolment{}, err
	}
	handle, err := putChallenge(ctx, u.db, kindTOTPEnrol, totpEnrolment{UserID: cur.UserID, Secret: key.Secret()})
	if err != nil {
		return TOTPEnrolment{}, err
	}
	return TOTPEnrolment{
		Handle: handle, Secret: key.Secret(), URI: key.URL(),
		QR: "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()),
	}, nil
}

// FinishTOTP stores the secret once code proves the app has it, and returns
// fresh recovery codes. Other sessions end: how the user signs in changed.
func (u *Users) FinishTOTP(ctx context.Context, cur Current, handle, code string) ([]string, error) {
	if !cur.Recent {
		return nil, ErrReauth
	}
	var e totpEnrolment
	if err := peekChallenge(ctx, u.db, kindTOTPEnrol, handle, &e); err != nil {
		return nil, err
	}
	if e.UserID != cur.UserID {
		return nil, errChallenge
	}
	step := totpStep(e.Secret, strings.TrimSpace(code), time.Now())
	if step == 0 {
		return nil, errBadCode
	}
	var codes []string
	err := pgx.BeginFunc(ctx, u.db, func(tx pgx.Tx) error {
		if ok, err := dropChallenge(ctx, tx, handle); err != nil || !ok {
			return errOr(err, errChallenge)
		}
		tag, err := tx.Exec(ctx, `INSERT INTO totp (user_id, secret, last_step) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`,
			cur.UserID, e.Secret, step)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return errTOTPEnabled
		}
		if codes, err = replaceRecoveryCodes(ctx, tx, cur.UserID); err != nil {
			return err
		}
		return revokeOthers(ctx, tx, cur)
	})
	return codes, err
}

func errOr(err, fallback error) error {
	if err != nil {
		return err
	}
	return fallback
}

func (u *Users) DisableTOTP(ctx context.Context, cur Current) error {
	return u.changeMethods(ctx, cur, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `DELETE FROM totp WHERE user_id = $1`, cur.UserID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return errNoTOTP
		}
		_, err = tx.Exec(ctx, `DELETE FROM recovery_codes WHERE user_id = $1`, cur.UserID)
		return err
	})
}

func (u *Users) RegenerateRecoveryCodes(ctx context.Context, cur Current) ([]string, error) {
	if !cur.Recent {
		return nil, ErrReauth
	}
	var codes []string
	err := pgx.BeginFunc(ctx, u.db, func(tx pgx.Tx) error {
		var enrolled bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM totp WHERE user_id = $1)`, cur.UserID).Scan(&enrolled); err != nil {
			return err
		}
		if !enrolled {
			return errNoTOTP
		}
		var err error
		codes, err = replaceRecoveryCodes(ctx, tx, cur.UserID)
		return err
	})
	return codes, err
}

var recoveryEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

func newRecoveryCode() string {
	b := make([]byte, 10)
	_, _ = rand.Read(b)
	s := strings.ToLower(recoveryEncoding.EncodeToString(b))
	return s[0:4] + "-" + s[4:8] + "-" + s[8:12] + "-" + s[12:16]
}

// normalizeRecoveryCode lets people type a code with or without dashes,
// spaces or capitals.
func normalizeRecoveryCode(code string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '2' && r <= '7':
			return r
		case r >= 'A' && r <= 'Z':
			return r + ('a' - 'A')
		}
		return -1
	}, code)
}

func replaceRecoveryCodes(ctx context.Context, tx pgx.Tx, userID int64) ([]string, error) {
	if _, err := tx.Exec(ctx, `DELETE FROM recovery_codes WHERE user_id = $1`, userID); err != nil {
		return nil, err
	}
	codes := make([]string, recoveryCodeCount)
	hashes := make([][]byte, recoveryCodeCount)
	for i := range codes {
		codes[i] = newRecoveryCode()
		hashes[i] = hashToken(normalizeRecoveryCode(codes[i]))
	}
	_, err := tx.Exec(ctx, `INSERT INTO recovery_codes (user_id, code_hash) SELECT $1, h FROM unnest($2::bytea[]) h`, userID, hashes)
	return codes, err
}

func (u *Users) useCode(ctx context.Context, userID int64, code string, c Client) (string, error) {
	user := fmt.Sprintf("user:%d", userID)
	var method string
	err := guarded(ctx, u.db, []string{user, c.ipKey()}, []string{user}, func(err error) bool { return errors.Is(err, errBadCode) }, func(tx pgx.Tx) error {
		var err error
		method, err = checkCode(ctx, tx, userID, strings.TrimSpace(code))
		return err
	})
	return method, err
}

func checkCode(ctx context.Context, db dbtx, userID int64, code string) (string, error) {
	if len(code) == 6 {
		var secret string
		err := db.QueryRow(ctx, `SELECT secret FROM totp WHERE user_id = $1`, userID).Scan(&secret)
		if errors.Is(err, pgx.ErrNoRows) {
			return "", errBadCode
		}
		if err != nil {
			return "", err
		}
		step := totpStep(secret, code, time.Now())
		if step == 0 {
			return "", errBadCode
		}
		tag, err := db.Exec(ctx, `UPDATE totp SET last_step = $2 WHERE user_id = $1 AND last_step < $2`, userID, step)
		if err != nil {
			return "", err
		}
		if tag.RowsAffected() == 0 {
			return "", errBadCode
		}
		return MethodTOTP, nil
	}
	tag, err := db.Exec(ctx, `DELETE FROM recovery_codes WHERE user_id = $1 AND code_hash = $2`,
		userID, hashToken(normalizeRecoveryCode(code)))
	if err != nil {
		return "", err
	}
	if tag.RowsAffected() == 0 {
		return "", errBadCode
	}
	return MethodRecovery, nil
}
