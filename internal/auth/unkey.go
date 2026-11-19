package auth

import (
	"context"
	"errors"

	"github.com/uncrm/uncrm/internal/crm"
)

// unkeyKeys calls Unkey's v2 API with net/http (POST
// https://api.unkey.com/v2/{procedure}, Bearer root key). The Go SDK v3
// requires go >= 1.26.10; four JSON calls do not justify forcing that.
//
//	Verify -> keys.verifyKey {key}; valid=false -> ErrInvalidKey.
//	          Rejects keys whose keyspaceId is not this API's keyspace, since
//	          v2 verify has no apiId filter (see RATIONALE open questions).
//	Create -> keys.createKey {apiId, prefix: "uncrm", name}
//	List   -> apis.listKeys {apiId}
//	Revoke -> keys.deleteKey {keyId}
type unkeyKeys struct {
	cfg UnkeyConfig
}

func (k *unkeyKeys) Verify(ctx context.Context, token string) (crm.Actor, error) {
	return crm.Actor{}, errors.New("not implemented")
}

func (k *unkeyKeys) Create(ctx context.Context, by crm.Actor, name string) (string, Key, error) {
	return "", Key{}, errors.New("not implemented")
}

func (k *unkeyKeys) List(ctx context.Context) ([]Key, error) {
	return nil, errors.New("not implemented")
}

func (k *unkeyKeys) Revoke(ctx context.Context, id string) error {
	return errors.New("not implemented")
}

func (k *unkeyKeys) Mode() string { return "unkey" }
