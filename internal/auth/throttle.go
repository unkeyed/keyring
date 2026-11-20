package auth

import (
	"context"
	"strings"
	"time"
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

func (u *Users) throttled(ctx context.Context, keys ...string) error {
	rows, err := u.db.Query(ctx, `
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

func (u *Users) fail(ctx context.Context, keys ...string) error {
	if _, err := u.db.Exec(ctx, `
		INSERT INTO login_failures (key, failures, last_failed_at) SELECT k, 1, now() FROM unnest($1::text[]) k
		ON CONFLICT (key) DO UPDATE SET
			failures = CASE WHEN login_failures.last_failed_at < now() - make_interval(secs => $2)
			           THEN 1 ELSE login_failures.failures + 1 END,
			last_failed_at = now()`, keys, loginFailureWindow.Seconds()); err != nil {
		return err
	}
	_, err := u.db.Exec(ctx, `DELETE FROM login_failures WHERE last_failed_at < now() - interval '1 day'`)
	return err
}

// clear forgets keys after a success. Callers never clear an IP key, so one
// valid account cannot reset the count for guesses against others.
func (u *Users) clear(ctx context.Context, keys ...string) error {
	_, err := u.db.Exec(ctx, `DELETE FROM login_failures WHERE key = ANY($1)`, keys)
	return err
}
