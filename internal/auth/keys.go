// Package auth answers "who is calling?" for every inbound request.
// Agents present API keys (KeyStore) to REST, or OAuth access tokens
// (OAuthServer) to MCP. Humans present a session cookie (Users). Each
// resolves to a crm.Actor, which handlers pass to crm.Service.
// auth knows nothing about contacts; crm knows nothing about passwords.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/unkeyed/keyring/internal/crm"
)

var (
	ErrInvalidKey   = errors.New("invalid api key")
	ErrUnauthorized = errors.New("unauthorized")
)

// KeyStore issues and verifies API keys for REST. Exactly one
// implementation is live per process, chosen once in NewKeyStore.
type KeyStore interface {
	// Verify returns the actor for a presented key, or ErrInvalidKey.
	// Any other error means the store could not decide (Unkey down, database
	// gone), and callers must treat it as a failure, never as a pass.
	Verify(ctx context.Context, token string) (crm.Actor, error)
	// Create returns the plaintext once. It is never retrievable again.
	Create(ctx context.Context, by crm.Actor, opts KeyOptions) (plaintext string, k Key, err error)
	List(ctx context.Context) ([]Key, error)
	// Revoke stops a key verifying. An id this store did not issue is
	// crm.ErrNotFound.
	Revoke(ctx context.Context, id string) error
	// RevokeCreatedBy stops every key the user created verifying. Remove
	// calls it inside the transaction that deletes the user, before the
	// delete clears api_keys.created_by.
	RevokeCreatedBy(ctx context.Context, tx pgx.Tx, userID int64) error
	// Mode is "builtin" or "unkey", shown on the Settings page.
	Mode() string
}

type Key struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"` // "keyring_ab12" so people can tell keys apart
	ReadOnly   bool       `json:"read_only"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
}

// KeyOptions is what a person chooses when creating a key.
type KeyOptions struct {
	Name string `json:"name"`
	// ReadOnly limits the key to GET routes: reading contacts, companies
	// and activity, never changing or enriching them.
	ReadOnly bool `json:"read_only"`
	// ExpiresAt, when set, is when the key stops verifying.
	ExpiresAt *time.Time `json:"expires_at"`
}

// maxKeyLifetime bounds ExpiresAt, so "expires" always means within a few
// years rather than effectively never.
const maxKeyLifetime = 2 * 366 * 24 * time.Hour

// check validates and normalizes the options both stores accept.
func (o KeyOptions) check(now time.Time) (KeyOptions, error) {
	name, err := keyName(o.Name)
	if err != nil {
		return o, err
	}
	o.Name = name
	if o.ExpiresAt != nil {
		t := o.ExpiresAt.UTC().Truncate(time.Second)
		if !t.After(now.Add(time.Minute)) || t.After(now.Add(maxKeyLifetime)) {
			return o, fmt.Errorf("%w: expires_at must be in the future and within two years", crm.ErrInvalid)
		}
		o.ExpiresAt = &t
	}
	return o, nil
}

type UnkeyConfig struct {
	RootKey string
	APIID   string
}

// NewKeyStore picks Unkey when both UNKEY_ROOT_KEY and UNKEY_API_ID are set,
// otherwise the built-in Postgres store. Setting only one of the two is a
// config error raised by main before this is called.
func NewKeyStore(db *pgxpool.Pool, unkey UnkeyConfig) KeyStore {
	if unkey.RootKey != "" {
		return newUnkeyKeys(unkey, unkeyBaseURL)
	}
	return &dbKeys{db: db}
}

const keyPrefix = "keyring_"

// keyName validates the human label both stores require.
func keyName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 255 {
		return "", fmt.Errorf("%w: key name must be 1 to 255 characters", crm.ErrInvalid)
	}
	return name, nil
}

// displayPrefix is the part of a key shown in lists: "keyring_" plus four
// characters, enough to tell keys apart and far too short to guess from.
func displayPrefix(key string) string {
	return key[:min(len(key), len(keyPrefix)+4)]
}

// wellFormedKey is whether token could be a key Keyring issued: "keyring_"
// then 52 base32 characters (built-in) or about 44 base58 characters
// (Unkey, 32 bytes).
func wellFormedKey(token string) bool {
	rest, ok := strings.CutPrefix(token, keyPrefix)
	return ok && len(rest) >= 32 && len(rest) <= 64 && !strings.ContainsFunc(rest, func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9')
	})
}

// tokenEncoding is lowercase RFC 4648 base32 without padding, so tokens are
// safe in URLs, headers and cookies.
var tokenEncoding = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

// newToken returns 32 random bytes, encoded. API keys, session cookies and
// invite tokens all use it, and each is stored only as hashToken(token).
func newToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b) // never fails since Go 1.24
	return tokenEncoding.EncodeToString(b)
}

// hashToken is the stored form of a token. Tokens carry 256 random bits, so
// a plain SHA-256 is enough and no slow hash is needed.
func hashToken(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}

// dbKeys stores SHA-256(token) in api_keys. Verify looks a key up by its
// hash alone, so no secret byte is ever compared in Go.
type dbKeys struct {
	db *pgxpool.Pool
}

func (k *dbKeys) Verify(ctx context.Context, token string) (crm.Actor, error) {
	// The UPDATE only fires when last_used_at is a minute stale, so a busy
	// key costs one write a minute, not one per request.
	var id int64
	var name string
	var readOnly bool
	err := k.db.QueryRow(ctx, `
		WITH k AS (
			SELECT id, name, read_only, last_used_at FROM api_keys
			WHERE token_hash = $1 AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > now())
		), touch AS (
			UPDATE api_keys SET last_used_at = now() FROM k
			WHERE api_keys.id = k.id
			  AND (k.last_used_at IS NULL OR k.last_used_at < now() - interval '1 minute')
		)
		SELECT id, name, read_only FROM k`, hashToken(token)).Scan(&id, &name, &readOnly)
	if errors.Is(err, pgx.ErrNoRows) {
		return crm.Actor{}, ErrInvalidKey
	}
	if err != nil {
		return crm.Actor{}, err
	}
	return crm.Actor{Kind: crm.ActorAPIKey, ID: strconv.FormatInt(id, 10), Label: name, ReadOnly: readOnly}, nil
}

func (k *dbKeys) Create(ctx context.Context, by crm.Actor, opts KeyOptions) (string, Key, error) {
	opts, err := opts.check(time.Now())
	if err != nil {
		return "", Key{}, err
	}
	token := keyPrefix + newToken()
	key := Key{Name: opts.Name, Prefix: displayPrefix(token), ReadOnly: opts.ReadOnly, ExpiresAt: opts.ExpiresAt}
	var id int64
	err = k.db.QueryRow(ctx, `
		INSERT INTO api_keys (name, prefix, token_hash, created_by, read_only, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING id, created_at`,
		key.Name, key.Prefix, hashToken(token), userID(by), key.ReadOnly, key.ExpiresAt).Scan(&id, &key.CreatedAt)
	if err != nil {
		return "", Key{}, err
	}
	key.ID = strconv.FormatInt(id, 10)
	return token, key, nil
}

func (k *dbKeys) List(ctx context.Context) ([]Key, error) {
	rows, err := k.db.Query(ctx, `
		SELECT id::text, name, prefix, read_only, expires_at, created_at, last_used_at FROM api_keys
		WHERE revoked_at IS NULL ORDER BY created_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Key, error) {
		var key Key
		err := row.Scan(&key.ID, &key.Name, &key.Prefix, &key.ReadOnly, &key.ExpiresAt, &key.CreatedAt, &key.LastUsedAt)
		return key, err
	})
}

// Revoke is idempotent: revoking a revoked key succeeds and keeps the first
// revocation time.
func (k *dbKeys) Revoke(ctx context.Context, id string) error {
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return crm.ErrNotFound
	}
	tag, err := k.db.Exec(ctx, `UPDATE api_keys SET revoked_at = coalesce(revoked_at, now()) WHERE id = $1`, n)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return crm.ErrNotFound
	}
	return nil
}

func (k *dbKeys) RevokeCreatedBy(ctx context.Context, tx pgx.Tx, userID int64) error {
	_, err := tx.Exec(ctx, `UPDATE api_keys SET revoked_at = coalesce(revoked_at, now()) WHERE created_by = $1`, userID)
	return err
}

func (k *dbKeys) Mode() string { return "builtin" }

// userID is the users.id behind a user actor, or nil for any other actor.
func userID(a crm.Actor) *int64 {
	if a.Kind != crm.ActorUser {
		return nil
	}
	id, err := strconv.ParseInt(a.ID, 10, 64)
	if err != nil {
		return nil
	}
	return &id
}
