package auth

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Throttling counts failed attempts per key in login_failures, so every
// replica sees the same count. Keys are "email:<address>" for passwords,
// "user:<id>" for second-factor and re-verification codes, and "ip:<addr>"
// for both. A key's row exists for unknown emails too, so throttling never
// reveals whether an account exists.

// freeAttempts is how many failures a key gets before backoff starts. An IP
// gets more, because an office or a carrier NAT puts many people behind one.
var freeAttempts = map[string]int{"email": 5, "user": 5, "ip": 30}

const (
	maxLoginBackoff    = 15 * time.Minute
	loginFailureWindow = time.Hour
)

func loginWait(free, failures int, sinceLast time.Duration) time.Duration {
	if sinceLast >= loginFailureWindow || failures < free {
		return 0
	}
	backoff := min(time.Second<<min(failures-free, 20), maxLoginBackoff)
	return max(backoff-sinceLast, 0)
}

func freeFor(key string) int {
	kind, _, _ := strings.Cut(key, ":")
	return freeAttempts[kind]
}

// guarded runs check as one attempt on keys. Advisory locks per key make
// concurrent attempts on a key take turns, so a burst of guesses cannot all
// read the count from before any of them failed. check runs in the same
// transaction, so an attempt holds one connection however many queue. A
// check error that failed reports true counts against every key; success
// clears onSuccess.
func (u *Users) guarded(ctx context.Context, keys, onSuccess []string, failed func(error) bool, check func(tx pgx.Tx) error) error {
	tx, err := u.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('uncrm.throttle:' || k)) FROM (SELECT unnest($1::text[]) AS k ORDER BY 1) s`, keys); err != nil {
		return err
	}
	if err := throttled(ctx, tx, keys); err != nil {
		return err
	}
	checkErr := check(tx)
	switch {
	case checkErr == nil:
		err = clearKeys(ctx, tx, onSuccess)
	case failed(checkErr):
		err = fail(ctx, tx, keys)
	default:
		return checkErr
	}
	if err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	return checkErr
}

func throttled(ctx context.Context, db dbtx, keys []string) error {
	rows, err := db.Query(ctx, `
		SELECT key, failures, extract(epoch FROM now() - last_failed_at)::float8
		FROM login_failures WHERE key = ANY($1)`, keys)
	if err != nil {
		return err
	}
	defer rows.Close()
	var wait time.Duration
	for rows.Next() {
		var key string
		var failures int
		var since float64
		if err := rows.Scan(&key, &failures, &since); err != nil {
			return err
		}
		wait = max(wait, loginWait(freeFor(key), failures, time.Duration(since*float64(time.Second))))
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if wait > 0 {
		return &ThrottledError{RetryAfter: wait.Truncate(time.Second) + time.Second}
	}
	return nil
}

func fail(ctx context.Context, db dbtx, keys []string) error {
	if _, err := db.Exec(ctx, `
		INSERT INTO login_failures (key, failures, last_failed_at) SELECT k, 1, now() FROM unnest($1::text[]) k
		ON CONFLICT (key) DO UPDATE SET
			failures = CASE WHEN login_failures.last_failed_at < now() - make_interval(secs => $2)
			           THEN 1 ELSE login_failures.failures + 1 END,
			last_failed_at = now()`, keys, loginFailureWindow.Seconds()); err != nil {
		return err
	}
	_, err := db.Exec(ctx, `DELETE FROM login_failures WHERE last_failed_at < now() - interval '1 day'`)
	return err
}

// clearKeys forgets keys after a success. Callers never clear an IP key, so
// one valid account cannot reset the count for guesses against others.
func clearKeys(ctx context.Context, db dbtx, keys []string) error {
	_, err := db.Exec(ctx, `DELETE FROM login_failures WHERE key = ANY($1)`, keys)
	return err
}
