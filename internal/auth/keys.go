// Package auth answers "who is calling?" for every inbound request.
// Agents present API keys (KeyStore). Humans present a session cookie
// (Users). Both resolve to a crm.Actor, which handlers pass to crm.Service.
// auth knows nothing about contacts; crm knows nothing about passwords.
package auth

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/uncrm/uncrm/internal/crm"
)

var (
	ErrInvalidKey   = errors.New("invalid api key")
	ErrUnauthorized = errors.New("unauthorized")
)

// KeyStore issues and verifies API keys for REST and MCP. Exactly one
// implementation is live per process, chosen once in NewKeyStore.
type KeyStore interface {
	// Verify returns the actor for a presented key, or ErrInvalidKey.
	Verify(ctx context.Context, token string) (crm.Actor, error)
	// Create returns the plaintext once. It is never retrievable again.
	Create(ctx context.Context, by crm.Actor, name string) (plaintext string, k Key, err error)
	List(ctx context.Context) ([]Key, error)
	Revoke(ctx context.Context, id string) error
	// Mode is "builtin" or "unkey", shown on the Settings page.
	Mode() string
}

type Key struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"` // "uncrm_ab12" so people can tell keys apart
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
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
		return &unkeyKeys{cfg: unkey}
	}
	return &dbKeys{db: db}
}

// dbKeys stores SHA-256(token) in api_keys. Tokens are "uncrm_" + base62 of
// 32 random bytes, so lookup by hash is safe and no slow hash is needed.
// Verify updates last_used_at at most once a minute per key.
type dbKeys struct {
	db *pgxpool.Pool
}

func (k *dbKeys) Verify(ctx context.Context, token string) (crm.Actor, error) {
	return crm.Actor{}, errors.New("not implemented")
}

func (k *dbKeys) Create(ctx context.Context, by crm.Actor, name string) (string, Key, error) {
	return "", Key{}, errors.New("not implemented")
}

func (k *dbKeys) List(ctx context.Context) ([]Key, error) {
	return nil, errors.New("not implemented")
}

func (k *dbKeys) Revoke(ctx context.Context, id string) error {
	return errors.New("not implemented")
}

func (k *dbKeys) Mode() string { return "builtin" }
