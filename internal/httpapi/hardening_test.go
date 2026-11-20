package httpapi

import (
	"bytes"
	"io"
	"net/http"
	"strconv"
	"testing"
	"time"
)

// send posts an arbitrary body, which client.do cannot.
func (c client) send(method, path string, header http.Header, body io.Reader) reply {
	c.t.Helper()
	req, err := http.NewRequest(method, c.base+path, body)
	if err != nil {
		c.t.Fatal(err)
	}
	for k, v := range header {
		req.Header[k] = v
	}
	hc := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := hc.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return reply{code: res.StatusCode, header: res.Header, body: b}
}

type createdKeyReply struct {
	Key       string     `json:"key"`
	ID        string     `json:"id"`
	ReadOnly  bool       `json:"read_only"`
	ExpiresAt *time.Time `json:"expires_at"`
}

func TestReadOnlyKeysOnlyRead(t *testing.T) {
	c := newServer(t)
	person, writer := login(t, c)
	var contact struct{ ID int64 }
	writer.call("POST", "/v1/contacts", map[string]string{"email": "ada@stripe.com"}, http.StatusCreated, &contact)

	var k createdKeyReply
	person.call("POST", "/v1/keys", map[string]any{"name": "reporting", "read_only": true}, http.StatusCreated, &k)
	if !k.ReadOnly {
		t.Fatalf("created key %+v, want read_only", k)
	}
	reader := c
	reader.key = k.Key

	reader.call("GET", "/v1/contacts", nil, http.StatusOK, nil)
	reader.call("GET", "/v1/status", nil, http.StatusOK, nil)
	for _, tc := range []struct {
		method, path string
		body         any
	}{
		{"POST", "/v1/contacts", map[string]string{"email": "bob@stripe.com"}},
		{"PATCH", "/v1/contacts/1", map[string]string{"name": "x"}},
		{"DELETE", "/v1/contacts/1", nil},
		{"POST", "/v1/contacts/1/enrich", nil},
		{"POST", "/v1/signups", map[string]string{"email": "eve@stripe.com"}},
	} {
		r := reader.do(tc.method, tc.path, tc.body)
		if r.code != http.StatusForbidden || !bytes.Contains(r.body, []byte("read-only")) {
			t.Errorf("read-only key %s %s = %d %s, want 403 naming read-only", tc.method, tc.path, r.code, r.body)
		}
	}

	// A read-only key can still revoke itself, and then stops working.
	person.call("DELETE", "/v1/keys/current", nil, http.StatusBadRequest, nil)
	reader.call("DELETE", "/v1/keys/current", nil, http.StatusNoContent, nil)
	reader.call("GET", "/v1/contacts", nil, http.StatusUnauthorized, nil)
	writer.call("GET", "/v1/contacts", nil, http.StatusOK, nil)
}

func TestKeysExpire(t *testing.T) {
	c := newServer(t)
	person, _ := login(t, c)
	past := time.Now().Add(-time.Hour)
	person.call("POST", "/v1/keys", map[string]any{"name": "old", "expires_at": past}, http.StatusBadRequest, nil)
	person.call("POST", "/v1/keys", map[string]any{"name": "forever", "expires_at": time.Now().AddDate(5, 0, 0)}, http.StatusBadRequest, nil)

	var k createdKeyReply
	soon := time.Now().Add(time.Hour)
	person.call("POST", "/v1/keys", map[string]any{"name": "ci", "expires_at": soon}, http.StatusCreated, &k)
	if k.ExpiresAt == nil || k.ExpiresAt.Sub(soon).Abs() > time.Second {
		t.Fatalf("created key expires %v, want %v", k.ExpiresAt, soon)
	}
	agent := c
	agent.key = k.Key
	agent.call("GET", "/v1/contacts", nil, http.StatusOK, nil)

	if _, err := c.db.Exec(t.Context(), `UPDATE api_keys SET expires_at = now() - interval '1 second' WHERE id = $1::bigint`, k.ID); err != nil {
		t.Fatal(err)
	}
	agent.call("GET", "/v1/contacts", nil, http.StatusUnauthorized, nil)

	var keys []createdKeyReply
	person.call("GET", "/v1/keys", nil, http.StatusOK, &keys)
	listed := false
	for _, l := range keys {
		listed = listed || (l.ID == k.ID && l.ExpiresAt != nil)
	}
	if !listed {
		t.Fatalf("expired key missing from the list, or listed without expires_at: %+v", keys)
	}
}

func TestKeyEnrichmentIsThrottled(t *testing.T) {
	c := newServer(t)
	_, agent := login(t, c)
	var contact struct{ ID int64 }
	agent.call("POST", "/v1/contacts", map[string]string{"email": "ada@stripe.com"}, http.StatusCreated, &contact)
	path := "/v1/contacts/" + strconv.FormatInt(contact.ID, 10) + "/enrich"
	for i := range 30 {
		if r := agent.do("POST", path, nil); r.code == http.StatusTooManyRequests {
			t.Fatalf("request %d throttled, want 30 free", i+1)
		}
	}
	r := agent.do("POST", path, nil)
	if r.code != http.StatusTooManyRequests || r.header.Get("Retry-After") == "" {
		t.Fatalf("31st enrich = %d %s, want 429 with Retry-After", r.code, r.body)
	}
}

func TestWebhookAnswersDoNotRevealProviders(t *testing.T) {
	c := newServer(t)
	person, _ := login(t, c)
	person.call("PUT", "/v1/providers/clerk", map[string]string{"secret": "whsec_c2VjcmV0"}, http.StatusOK, nil)

	var first []byte
	for _, provider := range []string{"clerk", "workos", "myspace"} {
		r := c.do("POST", "/webhooks/"+provider, map[string]string{"type": "user.created"})
		if r.code != http.StatusUnauthorized {
			t.Fatalf("POST /webhooks/%s = %d %s, want 401", provider, r.code, r.body)
		}
		if first == nil {
			first = r.body
		} else if !bytes.Equal(r.body, first) {
			t.Fatalf("POST /webhooks/%s answered %s, but clerk answered %s", provider, r.body, first)
		}
	}

	big := c.send("POST", "/webhooks/clerk", http.Header{"Content-Type": {"application/json"}}, bytes.NewReader(make([]byte, maxWebhookBody+1)))
	if big.code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized delivery = %d %s, want 413", big.code, big.body)
	}

	// Three failures above, so 57 more reach the 60 free ones.
	for i := range 57 {
		if r := c.do("POST", "/webhooks/workos", map[string]string{}); r.code != http.StatusUnauthorized {
			t.Fatalf("failure %d = %d, want 401", i+4, r.code)
		}
	}
	if r := c.do("POST", "/webhooks/workos", map[string]string{}); r.code != http.StatusTooManyRequests {
		t.Fatalf("delivery after 60 failures = %d %s, want 429", r.code, r.body)
	}
}

func TestAuthorizeIsThrottledPerAddress(t *testing.T) {
	c := newServer(t)
	path := "/oauth/authorize?client_id=nope&response_type=code"
	for i := range 30 {
		if r := c.raw("GET", path, nil, nil); r.code != http.StatusFound {
			t.Fatalf("authorize %d = %d %s, want a redirect to the consent page", i+1, r.code, r.body)
		}
	}
	if r := c.raw("GET", path, nil, nil); r.code != http.StatusTooManyRequests {
		t.Fatalf("31st authorize = %d %s, want 429", r.code, r.body)
	}
}

func TestUnknownV1RoutesNeedCredentials(t *testing.T) {
	c := newServer(t)
	_, agent := login(t, c)
	c.call("GET", "/v1/nope", nil, http.StatusUnauthorized, nil)
	c.call("GET", "/v1/contacts/", nil, http.StatusUnauthorized, nil)
	agent.call("GET", "/v1/nope", nil, http.StatusNotFound, nil)
}

// openapi.yaml is the same for everyone, so unlike every other response it
// may be cached, and a client holding the current ETag gets a 304.
func TestOpenAPIRevalidates(t *testing.T) {
	c := newServer(t)
	r := c.raw("GET", "/v1/openapi.yaml", nil, nil)
	etag := r.header.Get("ETag")
	if r.code != http.StatusOK || etag == "" || r.header.Get("Cache-Control") != "public, no-cache" || r.header.Get("Vary") != "" {
		t.Fatalf("openapi.yaml = %d, ETag %q, Cache-Control %q, Vary %q", r.code, etag, r.header.Get("Cache-Control"), r.header.Get("Vary"))
	}
	if r := c.raw("GET", "/v1/openapi.yaml", nil, map[string]string{"If-None-Match": etag}); r.code != http.StatusNotModified {
		t.Fatalf("openapi.yaml with its ETag = %d, want 304", r.code)
	}
}
