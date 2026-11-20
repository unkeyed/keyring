package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/uncrm/uncrm/internal/crm"
)

// fakeUnkey models the slice of Unkey's v2 API the store uses. Like the real
// service, verifyKey has no apiId: it answers for any key in the workspace
// unless the request restricts keyspaces.
type fakeUnkey struct {
	t        *testing.T
	rootKey  string
	keyspace map[string]string // apiId -> keyspaceId

	mu       sync.Mutex
	keys     map[string]fakeKey // plaintext -> key
	verifies int
	down     bool
}

type fakeKey struct {
	id, api, name string
	meta          map[string]string
	expired       bool
}

func newFakeUnkey(t *testing.T) (*fakeUnkey, *httptest.Server) {
	f := &fakeUnkey{
		t:        t,
		rootKey:  "unkey_root",
		keyspace: map[string]string{"api_ours": "ks_ours", "api_other": "ks_other"},
		keys:     map[string]fakeKey{},
	}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, srv
}

func (f *fakeUnkey) add(plaintext, api, name string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := "key_" + strconv.Itoa(len(f.keys)+1)
	f.keys[plaintext] = fakeKey{id: id, api: api, name: name}
	return id
}

func (f *fakeUnkey) expire(plaintext string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	k := f.keys[plaintext]
	k.expired = true
	f.keys[plaintext] = k
}

func (f *fakeUnkey) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer "+f.rootKey {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if f.down {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"meta":{},"error":{"title":"Service Unavailable","detail":"try later","status":503}}`))
		return
	}
	var in struct {
		Key, KeyID, APIID, Name, Prefix string
		Keyspaces                       []string
		Meta                            map[string]string
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		f.t.Errorf("fake unkey: bad body: %v", err)
	}
	reply := func(v any) { _ = json.NewEncoder(w).Encode(v) }
	switch r.URL.Path {
	case "/v2/keys.verifyKey":
		f.verifies++
		k, ok := f.keys[in.Key]
		ks := f.keyspace[k.api]
		if !ok || (in.Keyspaces != nil && !slices.Contains(in.Keyspaces, ks)) {
			reply(map[string]any{"data": map[string]any{"valid": false, "code": "NOT_FOUND"}})
			return
		}
		if k.expired {
			reply(map[string]any{"data": map[string]any{"valid": false, "code": "EXPIRED", "keyId": k.id, "keyspaceId": ks, "name": k.name}})
			return
		}
		reply(map[string]any{"data": map[string]any{"valid": true, "code": "VALID", "keyId": k.id, "keyspaceId": ks, "name": k.name}})
	case "/v2/keys.createKey":
		plaintext := unkeyToken(in.Prefix + "_" + strconv.Itoa(len(f.keys)))
		id := "key_" + strconv.Itoa(len(f.keys)+1)
		f.keys[plaintext] = fakeKey{id: id, api: in.APIID, name: in.Name, meta: in.Meta}
		reply(map[string]any{"data": map[string]any{"keyId": id, "key": plaintext}})
	case "/v2/apis.listKeys":
		data := []map[string]any{}
		for plaintext, k := range f.keys {
			if k.api == in.APIID {
				data = append(data, map[string]any{"keyId": k.id, "start": plaintext[:8], "name": k.name, "meta": k.meta, "createdAt": 1701425400000})
			}
		}
		reply(map[string]any{"data": data, "pagination": map[string]any{"hasMore": false}})
	case "/v2/keys.deleteKey":
		for plaintext, k := range f.keys {
			if k.id == in.KeyID {
				delete(f.keys, plaintext)
			}
		}
		reply(map[string]any{"data": map[string]any{}})
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (f *fakeUnkey) verifyCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.verifies
}

// unkeyToken pads name to the shape of a real Unkey key, "uncrm_" and the
// base58 of 32 bytes.
func unkeyToken(name string) string {
	return name + strings.Repeat("Z", 44-len(name)+len(keyPrefix))
}

var (
	ours    = unkeyToken("uncrm_ours")
	nope    = unkeyToken("uncrm_nope")
	expired = unkeyToken("uncrm_expired")
	billing = unkeyToken("uncrm_billing") // another API's key in the same workspace
)

func newTestUnkeyStore(srv *httptest.Server) *unkeyKeys {
	return newUnkeyKeys(UnkeyConfig{RootKey: "unkey_root", APIID: "api_ours"}, srv.URL)
}

func TestUnkeyVerify(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fake, srv := newFakeUnkey(t)
	keys := newTestUnkeyStore(srv)
	oursID := fake.add(ours, "api_ours", "agent")
	fake.add(billing, "api_other", "billing")

	if _, err := keys.Verify(ctx, billing); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("key from another API in the workspace, before our keyspace is known: %v, want ErrInvalidKey", err)
	}
	actor, err := keys.Verify(ctx, ours)
	if err != nil {
		t.Fatal(err)
	}
	if want := (crm.Actor{Kind: crm.ActorAPIKey, ID: oursID, Label: "agent"}); actor != want {
		t.Fatalf("verify gave %+v, want %+v", actor, want)
	}
	if _, err := keys.Verify(ctx, billing); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("key from another API in the workspace, after our keyspace is known: %v, want ErrInvalidKey", err)
	}
	if _, err := keys.Verify(ctx, nope); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("unknown key: %v, want ErrInvalidKey", err)
	}
	fake.add(expired, "api_ours", "old")
	fake.expire(expired)
	if _, err := keys.Verify(ctx, expired); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("expired key of our API: %v, want ErrInvalidKey", err)
	}
}

func TestUnkeyCachesValidKeysOnly(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fake, srv := newFakeUnkey(t)
	keys := newTestUnkeyStore(srv)
	fake.add(ours, "api_ours", "agent")

	for range 3 {
		if _, err := keys.Verify(ctx, ours); err != nil {
			t.Fatal(err)
		}
	}
	if n := fake.verifyCalls(); n != 1 {
		t.Fatalf("three verifies of one valid key made %d Unkey calls, want 1", n)
	}

	before := fake.verifyCalls()
	for range 2 {
		if _, err := keys.Verify(ctx, nope); !errors.Is(err, ErrInvalidKey) {
			t.Fatalf("unknown key: %v", err)
		}
	}
	if n := fake.verifyCalls() - before; n != 2 {
		t.Fatalf("two verifies of an invalid key made %d Unkey calls, want 2 (no negative cache)", n)
	}
	fake.add(nope, "api_ours", "late")
	if _, err := keys.Verify(ctx, nope); err != nil {
		t.Fatalf("a key rejected a moment ago and then created must verify at once: %v", err)
	}
}

func TestUnkeyOutageIsAnErrorNotAPass(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fake, srv := newFakeUnkey(t)
	keys := newTestUnkeyStore(srv)
	fake.add(ours, "api_ours", "agent")
	fake.mu.Lock()
	fake.down = true
	fake.mu.Unlock()

	actor, err := keys.Verify(ctx, ours)
	if err == nil || errors.Is(err, ErrInvalidKey) {
		t.Fatalf("verify during a 503 gave (%+v, %v), want a non-ErrInvalidKey error", actor, err)
	}
	if actor != (crm.Actor{}) {
		t.Fatalf("verify during a 503 returned actor %+v", actor)
	}
	if _, _, err := keys.Create(ctx, crm.Actor{}, "x"); err == nil {
		t.Fatal("create during a 503 succeeded")
	}
}

func TestUnkeyCreateListRevoke(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fake, srv := newFakeUnkey(t)
	keys := newTestUnkeyStore(srv)
	other := fake.add(billing, "api_other", "billing")

	token, k, err := keys.Create(ctx, crm.Actor{Kind: crm.ActorUser, ID: "1"}, "zapier")
	if err != nil {
		t.Fatal(err)
	}
	if k.Name != "zapier" || k.ID == "" || k.Prefix != token[:10] {
		t.Fatalf("created %+v from token %q", k, token)
	}
	if _, err := keys.Verify(ctx, token); err != nil {
		t.Fatalf("a key just created must verify: %v", err)
	}

	list, err := keys.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != k.ID || list[0].Name != "zapier" {
		t.Fatalf("list %+v, want only our API's key %s", list, k.ID)
	}

	if err := keys.Revoke(ctx, other); !errors.Is(err, crm.ErrNotFound) {
		t.Fatalf("revoking another API's key: %v, want crm.ErrNotFound", err)
	}
	if err := keys.Revoke(ctx, k.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := keys.Verify(ctx, token); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("revoked key still verifies on the replica that revoked it: %v", err)
	}
}

func TestUnkeyRefusesMalformedKeysWithoutACall(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fake, srv := newFakeUnkey(t)
	keys := newTestUnkeyStore(srv)
	for _, token := range []string{"x", "uncrm_short", "sk_live_" + strings.Repeat("a", 44), "uncrm_" + strings.Repeat("a", 43) + "!", "uncrm_" + strings.Repeat("a", 200)} {
		if _, err := keys.Verify(ctx, token); !errors.Is(err, ErrInvalidKey) {
			t.Errorf("verify %q: %v, want ErrInvalidKey", token, err)
		}
	}
	if n := fake.verifyCalls(); n != 0 {
		t.Fatalf("malformed tokens made %d Unkey calls, want 0", n)
	}
	fake.add(ours, "api_ours", "agent")
	if _, err := keys.Verify(ctx, ours); err != nil || fake.verifyCalls() != 1 {
		t.Fatalf("a well-formed key: %v after %d Unkey calls, want valid after 1", err, fake.verifyCalls())
	}
}

func TestUnkeyRevokeCreatedBy(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, srv := newFakeUnkey(t)
	keys := newTestUnkeyStore(srv)
	bobs, _, err := keys.Create(ctx, crm.Actor{Kind: crm.ActorUser, ID: "7"}, "bob's agent")
	if err != nil {
		t.Fatal(err)
	}
	adas, _, err := keys.Create(ctx, crm.Actor{Kind: crm.ActorUser, ID: "1"}, "ada's agent")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := keys.Verify(ctx, bobs); err != nil {
		t.Fatal(err)
	}

	if err := keys.RevokeCreatedBy(ctx, nil, 7); err != nil {
		t.Fatal(err)
	}
	if _, err := keys.Verify(ctx, bobs); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("a key whose creator was removed, cached a moment before: %v, want ErrInvalidKey", err)
	}
	if _, err := keys.Verify(ctx, adas); err != nil {
		t.Fatalf("another person's key after the removal: %v", err)
	}
}
