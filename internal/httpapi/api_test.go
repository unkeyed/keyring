package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/uncrm/uncrm/internal/auth"
	"github.com/uncrm/uncrm/internal/crm"
	"github.com/uncrm/uncrm/internal/testdb"
)

const (
	adminEmail    = "admin@example.com"
	adminPassword = "correct-horse-battery"
)

// client calls a test server as a person (session cookie) or an agent
// (API key), or anonymously when both are empty.
type client struct {
	t       *testing.T
	base    string
	session string
	key     string
}

type reply struct {
	code   int
	header http.Header
	body   []byte
}

func (c client) do(method, path string, body any) reply {
	c.t.Helper()
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			c.t.Fatal(err)
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.base+path, r)
	if err != nil {
		c.t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.key != "" {
		req.Header.Set("Authorization", "Bearer "+c.key)
	}
	if c.session != "" {
		req.AddCookie(&http.Cookie{Name: auth.SessionCookie, Value: c.session})
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		c.t.Fatal(err)
	}
	return reply{code: res.StatusCode, header: res.Header, body: b}
}

// call fails the test unless the status is want, then decodes into out.
func (c client) call(method, path string, body any, want int, out any) reply {
	c.t.Helper()
	r := c.do(method, path, body)
	if r.code != want {
		c.t.Fatalf("%s %s = %d %s, want %d", method, path, r.code, bytes.TrimSpace(r.body), want)
	}
	if out != nil {
		if err := json.Unmarshal(r.body, out); err != nil {
			c.t.Fatalf("%s %s: decode %s: %v", method, path, r.body, err)
		}
	}
	noNullLists(c.t, method+" "+path, r.body)
	return r
}

// listKeys are the JSON fields that hold arrays. None may be null.
var listKeys = []string{"items", "tags", "providers", "manual_fields", "identities", "notes", "activity", "contacts", "stages"}

func noNullLists(t *testing.T, what string, body []byte) {
	t.Helper()
	if len(bytes.TrimSpace(body)) == 0 {
		return
	}
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		return // not JSON (openapi.yaml)
	}
	if v == nil {
		t.Errorf("%s: body is null, want [] or an object", what)
	}
	var walk func(path string, v any)
	walk = func(path string, v any) {
		switch v := v.(type) {
		case map[string]any:
			for k, child := range v {
				if child == nil && slices.Contains(listKeys, k) {
					t.Errorf("%s: %s.%s is null, want []", what, path, k)
				}
				walk(path+"."+k, child)
			}
		case []any:
			for i, child := range v {
				walk(fmt.Sprintf("%s[%d]", path, i), child)
			}
		}
	}
	walk("$", v)
}

func newServer(t *testing.T) client {
	t.Helper()
	pool := testdb.New(t)
	users := auth.NewUsers(pool)
	if err := users.SeedAdmin(context.Background(), adminEmail, adminPassword); err != nil {
		t.Fatal(err)
	}
	authn := &auth.Authenticator{Keys: auth.NewKeyStore(pool, auth.UnkeyConfig{}), Users: users}
	srv := httptest.NewServer(NewMux(crm.New(pool, nil), authn, slog.New(slog.DiscardHandler)))
	t.Cleanup(srv.Close)
	return client{t: t, base: srv.URL}
}

// login returns c as the seeded admin and an agent holding a fresh key.
func login(t *testing.T, c client) (person, agent client) {
	t.Helper()
	r := c.call("POST", "/auth/login", map[string]string{"email": adminEmail, "password": adminPassword}, http.StatusNoContent, nil)
	person = c
	for _, ck := range (&http.Response{Header: r.header}).Cookies() {
		if ck.Name == auth.SessionCookie {
			person.session = ck.Value
		}
	}
	if person.session == "" {
		t.Fatal("login set no session cookie")
	}
	var key struct {
		Key    string `json:"key"`
		Prefix string `json:"prefix"`
	}
	person.call("POST", "/v1/keys", map[string]string{"name": "test agent"}, http.StatusCreated, &key)
	if !strings.HasPrefix(key.Key, key.Prefix) || key.Prefix == "" {
		t.Fatalf("created key %q with prefix %q", key.Key, key.Prefix)
	}
	agent = c
	agent.key = key.Key
	return person, agent
}

func TestEmptyInstanceListsAreArrays(t *testing.T) {
	c := newServer(t)
	person, agent := login(t, c)
	for _, path := range []string{"/v1/contacts", "/v1/companies", "/v1/activity", "/v1/stages", "/v1/tags", "/v1/status"} {
		agent.call("GET", path, nil, http.StatusOK, nil)
	}
	for _, path := range []string{"/v1/providers", "/v1/keys", "/v1/users"} {
		person.call("GET", path, nil, http.StatusOK, nil)
	}
	var page struct{ Items []any }
	r := agent.call("GET", "/v1/contacts", nil, http.StatusOK, &page)
	if !bytes.Contains(r.body, []byte(`"items":[]`)) {
		t.Fatalf("empty contacts page = %s, want items []", r.body)
	}
	if r := agent.call("GET", "/v1/activity", nil, http.StatusOK, nil); string(bytes.TrimSpace(r.body)) != "[]" {
		t.Fatalf("empty activity = %s, want []", r.body)
	}
}

func TestContactLifecycle(t *testing.T) {
	c := newServer(t)
	_, agent := login(t, c)

	var first, again crm.IngestResult
	agent.call("POST", "/v1/signups", map[string]any{"email": "Ada@Stripe.com", "name": "Ada"}, http.StatusCreated, &first)
	agent.call("POST", "/v1/signups", map[string]any{"email": "ada@stripe.com"}, http.StatusOK, &again)
	if !again.Duplicate || again.ContactID != first.ContactID {
		t.Fatalf("repeat sign-up = %+v, want a duplicate of %d", again, first.ContactID)
	}
	id := fmt.Sprint(first.ContactID)

	var page crm.Page[crm.Contact]
	agent.call("GET", "/v1/contacts?q=stripe&limit=10", nil, http.StatusOK, &page)
	if len(page.Items) != 1 || page.Items[0].Company == nil || page.Items[0].Company.Domain.String() != "stripe.com" {
		t.Fatalf("search stripe = %+v, want Ada at stripe.com", page.Items)
	}

	var edited crm.Contact
	agent.call("PATCH", "/v1/contacts/"+id, map[string]any{"name": "Ada Lovelace", "add_tags": []string{"VIP"}, "stage": "new"}, http.StatusOK, &edited)
	if edited.Name != "Ada Lovelace" || !slices.Contains(edited.ManualFields, crm.FieldName) || !slices.Contains(edited.Tags, "vip") {
		t.Fatalf("after PATCH: %+v", edited)
	}
	var reset crm.Contact
	agent.call("DELETE", "/v1/contacts/"+id+"/manual_fields/name", nil, http.StatusOK, &reset)
	if slices.Contains(reset.ManualFields, crm.FieldName) {
		t.Fatalf("after reset manual_fields = %v", reset.ManualFields)
	}

	var note crm.Note
	agent.call("POST", "/v1/contacts/"+id+"/notes", map[string]string{"body": "met at **Stripe Sessions**"}, http.StatusCreated, &note)
	if note.Author.Kind != crm.ActorAPIKey || note.Author.Label != "test agent" {
		t.Fatalf("note author = %+v, want the API key", note.Author)
	}
	var detail crm.ContactDetail
	agent.call("GET", "/v1/contacts/"+id, nil, http.StatusOK, &detail)
	if len(detail.Notes) != 1 || len(detail.Activity) == 0 {
		t.Fatalf("detail notes %d activity %d", len(detail.Notes), len(detail.Activity))
	}
	agent.call("DELETE", fmt.Sprintf("/v1/notes/%d", note.ID), nil, http.StatusNoContent, nil)
	agent.call("DELETE", fmt.Sprintf("/v1/notes/%d", note.ID), nil, http.StatusNotFound, nil)

	var tags map[string]int
	agent.call("GET", "/v1/tags", nil, http.StatusOK, &tags)
	if tags["vip"] != 1 {
		t.Fatalf("tags = %v", tags)
	}
	var signups []crm.Activity
	agent.call("GET", "/v1/activity?kind=signed_up&contact_id="+id, nil, http.StatusOK, &signups)
	if len(signups) != 1 {
		t.Fatalf("signed_up activity = %d rows, want 1", len(signups))
	}

	var other crm.IngestResult
	agent.call("POST", "/v1/signups", map[string]any{"email": "ada@gmail.com"}, http.StatusCreated, &other)
	var merged crm.Contact
	agent.call("POST", "/v1/contacts/"+id+"/merge", map[string]any{"drop": other.ContactID}, http.StatusOK, &merged)
	agent.call("GET", fmt.Sprintf("/v1/contacts/%d", other.ContactID), nil, http.StatusNotFound, nil)

	var job struct{ State crm.JobState }
	agent.call("POST", "/v1/contacts/"+id+"/enrich", nil, http.StatusAccepted, &job)
	if job.State != crm.JobPending {
		t.Fatalf("enrich state = %q, want pending", job.State)
	}

	var companies crm.Page[crm.Company]
	agent.call("GET", "/v1/companies?q=stripe", nil, http.StatusOK, &companies)
	if len(companies.Items) != 1 {
		t.Fatalf("companies = %+v", companies.Items)
	}
	co := fmt.Sprint(companies.Items[0].ID)
	var company crm.Company
	agent.call("PATCH", "/v1/companies/"+co, map[string]string{"name": "Stripe"}, http.StatusOK, &company)
	agent.call("DELETE", "/v1/companies/"+co+"/manual_fields/name", nil, http.StatusOK, &company)
	var codetail crm.CompanyDetail
	agent.call("GET", "/v1/companies/"+co, nil, http.StatusOK, &codetail)
	if len(codetail.Contacts) != 1 {
		t.Fatalf("company contacts = %d, want 1", len(codetail.Contacts))
	}
	agent.call("POST", "/v1/companies/"+co+"/enrich", nil, http.StatusAccepted, nil)

	var st struct {
		Contacts int
		KeyStore string `json:"key_store"`
	}
	agent.call("GET", "/v1/status", nil, http.StatusOK, &st)
	if st.Contacts != 1 || st.KeyStore != "builtin" {
		t.Fatalf("status = %+v, want 1 contact and the builtin key store", st)
	}
}

func TestErrorStatuses(t *testing.T) {
	c := newServer(t)
	person, agent := login(t, c)

	cases := []struct {
		name   string
		as     client
		method string
		path   string
		body   any
		want   int
	}{
		{"no credentials", c, "GET", "/v1/contacts", nil, http.StatusUnauthorized},
		{"unknown key", client{t: t, base: c.base, key: "uncrm_nope"}, "GET", "/v1/contacts", nil, http.StatusUnauthorized},
		{"unknown session", client{t: t, base: c.base, session: "nope"}, "GET", "/v1/contacts", nil, http.StatusUnauthorized},
		{"agent on a human route", agent, "GET", "/v1/keys", nil, http.StatusForbidden},
		{"agent mints a key", agent, "POST", "/v1/keys", map[string]string{"name": "x"}, http.StatusForbidden},
		{"bad id", agent, "GET", "/v1/contacts/abc", nil, http.StatusBadRequest},
		{"missing contact", agent, "GET", "/v1/contacts/999", nil, http.StatusNotFound},
		{"typo in a query parameter", agent, "GET", "/v1/contacts?query=ada", nil, http.StatusBadRequest},
		{"bad timestamp", agent, "GET", "/v1/contacts?signed_up_after=yesterday", nil, http.StatusBadRequest},
		{"bad cursor", agent, "GET", "/v1/contacts?cursor=zzz", nil, http.StatusBadRequest},
		{"unknown body field", agent, "POST", "/v1/signups", map[string]string{"emial": "a@b.co"}, http.StatusBadRequest},
		{"empty body", agent, "POST", "/v1/signups", nil, http.StatusBadRequest},
		{"bad email", agent, "POST", "/v1/signups", map[string]string{"email": "nope"}, http.StatusBadRequest},
		{"reset an unknown field", agent, "DELETE", "/v1/contacts/1/manual_fields/shoe_size", nil, http.StatusBadRequest},
		{"unknown provider", person, "PUT", "/v1/providers/myspace", map[string]string{"secret": "x"}, http.StatusNotFound},
		{"webhook for an unconnected provider", c, "POST", "/webhooks/clerk", map[string]string{}, http.StatusNotFound},
		{"invite an existing user", person, "POST", "/v1/invites", map[string]string{"email": adminEmail}, http.StatusConflict},
		{"unknown /v1 route", agent, "GET", "/v1/nope", nil, http.StatusNotFound},
	}
	for _, tc := range cases {
		r := tc.as.do(tc.method, tc.path, tc.body)
		if r.code != tc.want {
			t.Errorf("%s: %s %s = %d %s, want %d", tc.name, tc.method, tc.path, r.code, bytes.TrimSpace(r.body), tc.want)
			continue
		}
		var e struct{ Error string }
		if err := json.Unmarshal(r.body, &e); err != nil || e.Error == "" {
			t.Errorf("%s: body %s, want {\"error\": ...}", tc.name, r.body)
		}
	}
}

func TestLoginThrottleAndSessions(t *testing.T) {
	c := newServer(t)
	var setup struct {
		HasUsers bool `json:"has_users"`
	}
	c.call("GET", "/auth/setup", nil, http.StatusOK, &setup)
	if !setup.HasUsers {
		t.Fatal("setup says no users after SeedAdmin")
	}

	person, _ := login(t, c)
	var me crm.Actor
	person.call("GET", "/v1/me", nil, http.StatusOK, &me)
	if me.Kind != crm.ActorUser || me.Label != adminEmail {
		t.Fatalf("me = %+v", me)
	}
	person.call("POST", "/auth/logout", nil, http.StatusNoContent, nil)
	person.call("GET", "/v1/me", nil, http.StatusUnauthorized, nil)

	wrong := map[string]string{"email": adminEmail, "password": "wrong-password-123"}
	for range 5 {
		c.call("POST", "/auth/login", wrong, http.StatusUnauthorized, nil)
	}
	r := c.call("POST", "/auth/login", wrong, http.StatusTooManyRequests, nil)
	if r.header.Get("Retry-After") == "" || r.header.Get("Retry-After") == "0" {
		t.Fatalf("429 Retry-After = %q, want whole seconds", r.header.Get("Retry-After"))
	}
}

func TestInviteFlow(t *testing.T) {
	c := newServer(t)
	person, _ := login(t, c)
	var inv struct{ Token string }
	person.call("POST", "/v1/invites", map[string]string{"email": "grace@example.com"}, http.StatusCreated, &inv)

	r := c.call("POST", "/auth/invites/accept", map[string]string{"token": inv.Token, "name": "Grace", "password": "cobol-forever-1959"}, http.StatusNoContent, nil)
	if !strings.Contains(r.header.Get("Set-Cookie"), auth.SessionCookie+"=") {
		t.Fatal("accepting an invite set no session cookie")
	}
	c.call("POST", "/auth/invites/accept", map[string]string{"token": inv.Token, "name": "Grace", "password": "cobol-forever-1959"}, http.StatusNotFound, nil)

	var users []auth.User
	person.call("GET", "/v1/users", nil, http.StatusOK, &users)
	if len(users) != 2 || users[1].Name != "Grace" {
		t.Fatalf("users = %+v, want the admin and Grace", users)
	}
}

// downKeys is a key store that cannot reach its backend, like Unkey during
// an outage.
type downKeys struct{ auth.KeyStore }

func (downKeys) Verify(context.Context, string) (crm.Actor, error) {
	return crm.Actor{}, errors.New("unkey keys.verifyKey: HTTP 502")
}

func TestKeyStoreOutageIs503(t *testing.T) {
	pool := testdb.New(t)
	authn := &auth.Authenticator{Keys: downKeys{}, Users: auth.NewUsers(pool)}
	srv := httptest.NewServer(NewMux(crm.New(pool, nil), authn, slog.New(slog.DiscardHandler)))
	t.Cleanup(srv.Close)
	r := client{t: t, base: srv.URL, key: "uncrm_whatever"}.do("GET", "/v1/contacts", nil)
	var e struct {
		Error     string `json:"error"`
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal(r.body, &e); err != nil || r.code != http.StatusServiceUnavailable || e.RequestID == "" {
		t.Fatalf("key store down: %d %s, want 503 with a request_id", r.code, r.body)
	}
}

func TestSettings(t *testing.T) {
	c := newServer(t)
	person, agent := login(t, c)

	var issued struct{ Secret string }
	person.call("PUT", "/v1/providers/better-auth", map[string]string{"secret": ""}, http.StatusOK, &issued)
	if !strings.HasPrefix(issued.Secret, "whsec_") {
		t.Fatalf("issued secret = %q", issued.Secret)
	}
	var providers []crm.ProviderStatus
	person.call("GET", "/v1/providers", nil, http.StatusOK, &providers)
	i := slices.IndexFunc(providers, func(p crm.ProviderStatus) bool { return p.Name == "better-auth" })
	if i < 0 || !providers[i].Connected {
		t.Fatalf("better-auth not connected in %+v", providers)
	}
	person.call("DELETE", "/v1/providers/better-auth", nil, http.StatusNoContent, nil)

	person.call("PUT", "/v1/stages", map[string]any{"stages": []string{"lead", "customer"}, "renames": map[string]string{"new": "lead"}}, http.StatusNoContent, nil)
	var stages []string
	agent.call("GET", "/v1/stages", nil, http.StatusOK, &stages)
	if !slices.Equal(stages, []string{"lead", "customer"}) {
		t.Fatalf("stages = %v", stages)
	}

	var keys []auth.Key
	person.call("GET", "/v1/keys", nil, http.StatusOK, &keys)
	if len(keys) != 1 {
		t.Fatalf("keys = %+v", keys)
	}
	person.call("DELETE", "/v1/keys/"+keys[0].ID, nil, http.StatusNoContent, nil)
	agent.call("GET", "/v1/contacts", nil, http.StatusUnauthorized, nil)

	r := c.call("GET", "/v1/openapi.yaml", nil, http.StatusOK, nil)
	if !bytes.HasPrefix(r.body, []byte("openapi: 3.1")) {
		t.Fatalf("openapi.yaml starts %q", r.body[:min(len(r.body), 20)])
	}
}
