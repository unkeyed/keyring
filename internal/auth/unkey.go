package auth

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/unkeyed/keyring/internal/crm"
)

const unkeyBaseURL = "https://api.unkey.com"

// unkeyCacheTTL bounds how long a revoked key keeps working on a replica
// that did not revoke it. Only valid results are cached; a rejected key is
// asked about again on its next request.
const unkeyCacheTTL = 30 * time.Second

// unkeyKeys calls Unkey's v2 API with net/http (POST
// https://api.unkey.com/v2/{procedure}, Bearer root key). The Go SDK v3
// requires go >= 1.26.10; four JSON calls do not justify forcing that.
//
//	Verify          -> keys.verifyKey {key, keyspaces: [our keyspace id]},
//	                   only for a token shaped like an Keyring key
//	Create          -> keys.createKey {apiId, prefix: "Keyring", name, meta: {created_by}}
//	List            -> apis.listKeys {apiId}
//	Revoke          -> keys.deleteKey {keyId}, after checking apis.listKeys has it
//	RevokeCreatedBy -> keys.deleteKey for each key whose meta.created_by
//	                   matches. A key created before Keyring wrote that meta
//	                   has no creator and stays until revoked by hand.
//
// Cross-API keys. keys.verifyKey takes no apiId; a key from any keyspace in
// the workspace verifies unless the request restricts it with
// `keyspaces`, which matches keyspace ids (ks_...), not API ids (api_...).
// No endpoint maps UNKEY_API_ID to its keyspace id (apis.getApi returns
// only id and name), so the store learns it: the first valid key whose
// keyId apis.listKeys {apiId} also returns proves its keyspaceId is ours.
// From then on every verify sends keyspaces: [that id], Unkey answers
// NOT_FOUND for any other keyspace, and a mismatched keyspaceId in a valid
// answer is still rejected here.
type unkeyKeys struct {
	cfg     UnkeyConfig
	baseURL string
	client  *http.Client

	mu       sync.Mutex
	keyspace string // "" until learned
	cache    map[[sha256.Size]byte]cachedActor
}

type cachedActor struct {
	actor   crm.Actor
	expires time.Time
}

func newUnkeyKeys(cfg UnkeyConfig, baseURL string) *unkeyKeys {
	return &unkeyKeys{
		cfg:     cfg,
		baseURL: baseURL,
		client:  &http.Client{Timeout: 10 * time.Second},
		cache:   map[[sha256.Size]byte]cachedActor{},
	}
}

func (k *unkeyKeys) Verify(ctx context.Context, token string) (crm.Actor, error) {
	if !wellFormedKey(token) {
		return crm.Actor{}, ErrInvalidKey
	}
	sum := sha256.Sum256([]byte(token))
	k.mu.Lock()
	hit, cached := k.cache[sum]
	keyspace := k.keyspace
	k.mu.Unlock()
	if cached && time.Now().Before(hit.expires) {
		return hit.actor, nil
	}

	req := struct {
		Key       string   `json:"key"`
		Keyspaces []string `json:"keyspaces,omitempty"`
	}{Key: token}
	if keyspace != "" {
		req.Keyspaces = []string{keyspace}
	}
	var res struct {
		Data struct {
			Valid      bool      `json:"valid"`
			KeyID      string    `json:"keyId"`
			KeyspaceID string    `json:"keyspaceId"`
			Name       string    `json:"name"`
			Meta       unkeyMeta `json:"meta"`
			Expires    int64     `json:"expires"`
		} `json:"data"`
	}
	if err := k.call(ctx, "keys.verifyKey", req, &res); err != nil {
		return crm.Actor{}, err
	}
	d := res.Data
	if !d.Valid {
		return crm.Actor{}, ErrInvalidKey
	}
	if keyspace == "" {
		ours, err := k.hasKey(ctx, d.KeyID)
		if err != nil {
			return crm.Actor{}, err
		}
		if !ours {
			return crm.Actor{}, ErrInvalidKey
		}
		keyspace = d.KeyspaceID
	}
	if d.KeyspaceID != keyspace {
		return crm.Actor{}, ErrInvalidKey
	}

	actor := crm.Actor{Kind: crm.ActorAPIKey, ID: d.KeyID, Label: d.Name, ReadOnly: d.Meta.ReadOnly == "true"}
	if actor.Label == "" {
		actor.Label = d.KeyID
	}
	now := time.Now()
	cacheUntil := now.Add(unkeyCacheTTL)
	if d.Expires > 0 {
		cacheUntil = minTime(cacheUntil, time.UnixMilli(d.Expires))
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	k.keyspace = keyspace
	if len(k.cache) >= 1024 {
		for s, c := range k.cache {
			if now.After(c.expires) {
				delete(k.cache, s)
			}
		}
	}
	k.cache[sum] = cachedActor{actor: actor, expires: cacheUntil}
	return actor, nil
}

func minTime(a, b time.Time) time.Time {
	if b.Before(a) {
		return b
	}
	return a
}

func (k *unkeyKeys) Create(ctx context.Context, by crm.Actor, opts KeyOptions) (string, Key, error) {
	opts, err := opts.check(time.Now())
	if err != nil {
		return "", Key{}, err
	}
	req := struct {
		APIID      string     `json:"apiId"`
		Prefix     string     `json:"prefix"`
		Name       string     `json:"name"`
		ByteLength int        `json:"byteLength"`
		Expires    int64      `json:"expires,omitempty"`
		Meta       *unkeyMeta `json:"meta,omitempty"`
	}{APIID: k.cfg.APIID, Prefix: "keyring", Name: opts.Name, ByteLength: 32}
	var meta unkeyMeta
	if id := userID(by); id != nil {
		meta.CreatedBy = strconv.FormatInt(*id, 10)
	}
	if opts.ReadOnly {
		meta.ReadOnly = "true"
	}
	if meta != (unkeyMeta{}) {
		req.Meta = &meta
	}
	if opts.ExpiresAt != nil {
		req.Expires = opts.ExpiresAt.UnixMilli()
	}
	var res struct {
		Data struct {
			KeyID string `json:"keyId"`
			Key   string `json:"key"`
		} `json:"data"`
	}
	if err := k.call(ctx, "keys.createKey", req, &res); err != nil {
		return "", Key{}, err
	}
	return res.Data.Key, Key{
		ID:        res.Data.KeyID,
		Name:      opts.Name,
		ReadOnly:  opts.ReadOnly,
		ExpiresAt: opts.ExpiresAt,
		Prefix:    displayPrefix(res.Data.Key),
		CreatedAt: time.Now().UTC(),
	}, nil
}

func (k *unkeyKeys) List(ctx context.Context) ([]Key, error) {
	keys, err := k.listKeys(ctx, false)
	if err != nil {
		return nil, err
	}
	out := make([]Key, len(keys))
	for i, u := range keys {
		out[i] = u.key()
	}
	return out, nil
}

func (k *unkeyKeys) Revoke(ctx context.Context, id string) error {
	ours, err := k.hasKey(ctx, id)
	if err != nil {
		return err
	}
	if !ours {
		return crm.ErrNotFound
	}
	req := struct {
		KeyID string `json:"keyId"`
	}{id}
	if err := k.call(ctx, "keys.deleteKey", req, &struct{}{}); err != nil {
		return err
	}
	k.forget(id)
	return nil
}

// RevokeCreatedBy deletes in Unkey; the transaction only orders it before
// the user's removal.
func (k *unkeyKeys) RevokeCreatedBy(ctx context.Context, _ pgx.Tx, userID int64) error {
	keys, err := k.listKeys(ctx, true)
	if err != nil {
		return err
	}
	creator := strconv.FormatInt(userID, 10)
	for _, key := range keys {
		if key.Meta.CreatedBy != creator {
			continue
		}
		req := struct {
			KeyID string `json:"keyId"`
		}{key.KeyID}
		if err := k.call(ctx, "keys.deleteKey", req, &struct{}{}); err != nil {
			return err
		}
		k.forget(key.KeyID)
	}
	return nil
}

// forget drops a revoked key from this replica's cache.
func (k *unkeyKeys) forget(id string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	for s, c := range k.cache {
		if c.actor.ID == id {
			delete(k.cache, s)
		}
	}
}

func (k *unkeyKeys) Mode() string { return "unkey" }

// hasKey reports whether keyID belongs to the configured API. It reads
// Unkey's primary database so a key created a moment ago is found.
func (k *unkeyKeys) hasKey(ctx context.Context, keyID string) (bool, error) {
	keys, err := k.listKeys(ctx, true)
	if err != nil {
		return false, err
	}
	for _, key := range keys {
		if key.KeyID == keyID {
			return true, nil
		}
	}
	return false, nil
}

type unkeyMeta struct {
	CreatedBy string `json:"created_by,omitempty"` // users.id
	ReadOnly  string `json:"read_only,omitempty"`  // "true" for a read-only key
}

type unkeyKey struct {
	KeyID      string    `json:"keyId"`
	Start      string    `json:"start"`
	Name       string    `json:"name"`
	Meta       unkeyMeta `json:"meta"`
	Expires    int64     `json:"expires"`
	CreatedAt  int64     `json:"createdAt"`
	LastUsedAt int64     `json:"lastUsedAt"`
}

func (u unkeyKey) key() Key {
	key := Key{ID: u.KeyID, Name: u.Name, Prefix: displayPrefix(u.Start), ReadOnly: u.Meta.ReadOnly == "true",
		CreatedAt: time.UnixMilli(u.CreatedAt).UTC()}
	if u.Expires > 0 {
		t := time.UnixMilli(u.Expires).UTC()
		key.ExpiresAt = &t
	}
	if u.LastUsedAt > 0 {
		t := time.UnixMilli(u.LastUsedAt).UTC()
		key.LastUsedAt = &t
	}
	return key
}

func (k *unkeyKeys) listKeys(ctx context.Context, fresh bool) ([]unkeyKey, error) {
	req := struct {
		APIID               string `json:"apiId"`
		Limit               int    `json:"limit"`
		Cursor              string `json:"cursor,omitempty"`
		RevalidateKeysCache bool   `json:"revalidateKeysCache,omitempty"`
	}{APIID: k.cfg.APIID, Limit: 100, RevalidateKeysCache: fresh}
	keys := []unkeyKey{}
	for {
		var res struct {
			Data       []unkeyKey `json:"data"`
			Pagination struct {
				Cursor  string `json:"cursor"`
				HasMore bool   `json:"hasMore"`
			} `json:"pagination"`
		}
		if err := k.call(ctx, "apis.listKeys", req, &res); err != nil {
			return nil, err
		}
		keys = append(keys, res.Data...)
		if !res.Pagination.HasMore || res.Pagination.Cursor == "" {
			return keys, nil
		}
		req.Cursor = res.Pagination.Cursor
	}
}

// call POSTs one v2 procedure. Any non-200 answer is an error, so an Unkey
// outage or a rate limit fails the request instead of passing it.
func (k *unkeyKeys) call(ctx context.Context, procedure string, in, out any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, k.baseURL+"/v2/"+procedure, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+k.cfg.RootKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := k.client.Do(req)
	if err != nil {
		return fmt.Errorf("unkey %s: %w", procedure, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Error struct {
				Title  string `json:"title"`
				Detail string `json:"detail"`
			} `json:"error"`
		}
		_ = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&e)
		return fmt.Errorf("unkey %s: HTTP %d %s: %s", procedure, resp.StatusCode, e.Error.Title, e.Error.Detail)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("unkey %s: %w", procedure, err)
	}
	return nil
}
