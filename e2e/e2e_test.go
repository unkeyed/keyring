//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var (
	baseURL       = strings.TrimRight(envOr("BASE_URL", "http://localhost:8080"), "/")
	adminEmail    = envOr("E2E_ADMIN_EMAIL", "admin@example.com")
	adminPassword = envOr("E2E_ADMIN_PASSWORD", "uncrm-dev-password")
)

func TestHealthz(t *testing.T) {
	res := do(t, request{method: "GET", path: "/healthz"})
	res.expect(t, http.StatusOK)
}

func TestClerkWebhookCreatesOneContact(t *testing.T) {
	inst := bootstrap(t)
	run := unique()
	email := "clerk-" + run + "@uncrm-e2e.test"
	body := clerkUserCreated(t, "user_e2e_"+run, email)

	forged := do(t, request{method: "POST", path: "/webhooks/clerk", body: body,
		header: svixHeaders(randomBytes(32), "msg_forged_"+run, time.Now(), body)})
	forged.expect(t, http.StatusUnauthorized)

	msgID := "msg_e2e_" + run
	for attempt := range 2 {
		res := do(t, request{method: "POST", path: "/webhooks/clerk", body: body,
			header: svixHeaders(inst.clerkSecret, msgID, time.Now(), body)})
		res.expect(t, http.StatusOK, http.StatusNoContent)
		t.Logf("delivery %d accepted", attempt+1)
	}
	replay := do(t, request{method: "POST", path: "/webhooks/clerk", body: body,
		header: svixHeaders(inst.clerkSecret, "msg_replay_"+run, time.Now(), body)})
	replay.expect(t, http.StatusOK, http.StatusNoContent)

	found := searchContacts(t, inst, email)
	if len(found) != 1 {
		t.Fatalf("after a delivery, its retry and a dashboard replay, search for %s found %d contacts, want 1", email, len(found))
	}
}

func TestGenericSignupIsIdempotent(t *testing.T) {
	inst := bootstrap(t)
	email := "api-" + unique() + "@uncrm-e2e.test"

	first := postSignup(t, inst, email)
	first.res.expect(t, http.StatusCreated)
	if !first.Created || first.Duplicate || first.ContactID <= 0 {
		t.Fatalf("first POST /v1/signups = %+v, want created with a contact id", first)
	}

	second := postSignup(t, inst, email)
	second.res.expect(t, http.StatusOK)
	if second.Created || !second.Duplicate || second.ContactID != first.ContactID {
		t.Fatalf("repeated POST /v1/signups = %+v, want duplicate of contact %d", second, first.ContactID)
	}

	if found := searchContacts(t, inst, email); len(found) != 1 {
		t.Fatalf("search for %s found %d contacts, want 1", email, len(found))
	}
}

func TestContactsListAndSearch(t *testing.T) {
	inst := bootstrap(t)
	email := "list-" + unique() + "@uncrm-e2e.test"
	created := postSignup(t, inst, email)
	created.res.expect(t, http.StatusCreated)

	do(t, request{method: "GET", path: "/v1/contacts"}).expect(t, http.StatusUnauthorized)

	list := do(t, request{method: "GET", path: "/v1/contacts", key: inst.apiKey})
	list.expect(t, http.StatusOK)
	var page contactPage
	list.decode(t, &page)
	if !slices.ContainsFunc(page.Items, func(c contact) bool { return c.Email == email }) {
		t.Fatalf("GET /v1/contacts first page (newest first) does not include %s", email)
	}

	found := searchContacts(t, inst, email)
	if len(found) != 1 || found[0].ID != created.ContactID {
		t.Fatalf("search for %s = %+v, want contact %d only", email, found, created.ContactID)
	}

	one := do(t, request{method: "GET", path: fmt.Sprintf("/v1/contacts/%d", created.ContactID), key: inst.apiKey})
	one.expect(t, http.StatusOK)
	var c contact
	one.decode(t, &c)
	if c.Email != email {
		t.Fatalf("GET /v1/contacts/%d email = %q, want %q", created.ContactID, c.Email, email)
	}
}

func TestMCPSearchContacts(t *testing.T) {
	inst := bootstrap(t)
	email := "mcp-" + unique() + "@uncrm-e2e.test"
	postSignup(t, inst, email).res.expect(t, http.StatusCreated)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if _, err := connectMCP(ctx, ""); err == nil {
		t.Fatal("MCP accepted a session without an API key")
	}

	cs, err := connectMCP(ctx, inst.apiKey)
	if err != nil {
		t.Fatalf("connect to %s/mcp: %v", baseURL, err)
	}
	defer cs.Close()

	tools, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	var names []string
	for _, tool := range tools.Tools {
		names = append(names, tool.Name)
	}
	for _, want := range []string{"search_contacts", "get_contact", "update_contact", "add_note", "list_recent_signups", "get_company", "enrich_contact"} {
		if !slices.Contains(names, want) {
			t.Errorf("tools/list = %v, missing %s", names, want)
		}
	}

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "search_contacts", Arguments: map[string]any{"query": email}})
	if err != nil {
		t.Fatalf("search_contacts: %v", err)
	}
	if res.IsError {
		t.Fatalf("search_contacts returned a tool error: %s", toolText(res))
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var page contactPage
	if err := json.Unmarshal(raw, &page); err != nil {
		t.Fatalf("search_contacts structured content %s: %v", raw, err)
	}
	if len(page.Items) != 1 || page.Items[0].Email != email {
		t.Fatalf("search_contacts(%q) = %s, want that one contact", email, raw)
	}
}

// instance is what the checks share: an API key, and the signing secret of
// the Clerk connection they created.
type instance struct {
	apiKey      string
	clerkSecret []byte
}

var (
	setupOnce sync.Once
	setup     instance
	setupErr  error
)

// bootstrap logs in as the seeded admin, creates an API key and connects
// Clerk with a fresh secret, once per test binary.
func bootstrap(t *testing.T) instance {
	t.Helper()
	setupOnce.Do(func() { setup, setupErr = connect() })
	if setupErr != nil {
		t.Fatalf("bootstrap: %v", setupErr)
	}
	return setup
}

func connect() (instance, error) {
	login, err := send(request{method: "POST", path: "/auth/login", body: map[string]string{"email": adminEmail, "password": adminPassword}})
	if err != nil {
		return instance{}, err
	}
	if err := login.check(http.StatusOK, http.StatusNoContent); err != nil {
		return instance{}, err
	}
	var session string
	for _, c := range login.Cookies() {
		if c.Name == "uncrm_session" {
			session = c.Value
		}
	}
	if session == "" {
		return instance{}, fmt.Errorf("POST /auth/login set no uncrm_session cookie")
	}

	created, err := send(request{method: "POST", path: "/v1/keys", session: session, body: map[string]string{"name": "e2e " + unique()}})
	if err != nil {
		return instance{}, err
	}
	if err := created.check(http.StatusOK, http.StatusCreated); err != nil {
		return instance{}, err
	}
	var key struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(created.body, &key); err != nil || key.Key == "" {
		return instance{}, fmt.Errorf("POST /v1/keys returned no key: %s", created.body)
	}

	secret := randomBytes(32)
	conn, err := send(request{method: "PUT", path: "/v1/providers/clerk", session: session,
		body: map[string]string{"secret": "whsec_" + base64.StdEncoding.EncodeToString(secret)}})
	if err != nil {
		return instance{}, err
	}
	if err := conn.check(http.StatusOK, http.StatusNoContent); err != nil {
		return instance{}, err
	}
	return instance{apiKey: key.Key, clerkSecret: secret}, nil
}

type contact struct {
	ID    int64  `json:"id"`
	Email string `json:"email"`
}

type contactPage struct {
	Items []contact `json:"items"`
}

func searchContacts(t *testing.T, inst instance, q string) []contact {
	t.Helper()
	res := do(t, request{method: "GET", path: "/v1/contacts?q=" + url.QueryEscape(q), key: inst.apiKey})
	res.expect(t, http.StatusOK)
	var page contactPage
	res.decode(t, &page)
	return page.Items
}

type signupResult struct {
	ContactID int64 `json:"contact_id"`
	Created   bool  `json:"created"`
	Duplicate bool  `json:"duplicate"`
	res       response
}

func postSignup(t *testing.T, inst instance, email string) signupResult {
	t.Helper()
	res := do(t, request{method: "POST", path: "/v1/signups", key: inst.apiKey,
		body: map[string]string{"email": email, "name": "Ada E2E"}})
	out := signupResult{res: res}
	if res.StatusCode/100 == 2 {
		res.decode(t, &out)
	}
	return out
}

// clerkUserCreated is the committed Clerk fixture with a fresh user id,
// email and timestamp.
func clerkUserCreated(t *testing.T, userID, email string) []byte {
	t.Helper()
	raw, err := os.ReadFile("../internal/signup/testdata/clerk/user_created.json")
	if err != nil {
		t.Fatal(err)
	}
	var ev map[string]any
	if err := json.Unmarshal(raw, &ev); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	ev["timestamp"] = now
	data := ev["data"].(map[string]any)
	data["id"] = userID
	data["created_at"] = now
	data["updated_at"] = now
	data["email_addresses"].([]any)[0].(map[string]any)["email_address"] = email
	body, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

// svixHeaders signs body the way Clerk does: base64 HMAC-SHA256 of
// "<id>.<unix seconds>.<body>" keyed with the secret's decoded bytes.
func svixHeaders(secret []byte, msgID string, at time.Time, body []byte) http.Header {
	ts := strconv.FormatInt(at.Unix(), 10)
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(msgID + "." + ts + "."))
	mac.Write(body)
	h := http.Header{}
	h.Set("Content-Type", "application/json")
	h.Set("svix-id", msgID)
	h.Set("svix-timestamp", ts)
	h.Set("svix-signature", "v1,"+base64.StdEncoding.EncodeToString(mac.Sum(nil)))
	return h
}

func connectMCP(ctx context.Context, key string) (*mcp.ClientSession, error) {
	client := mcp.NewClient(&mcp.Implementation{Name: "uncrm-e2e", Version: "0"}, nil)
	transport := &mcp.StreamableClientTransport{
		Endpoint:             baseURL + "/mcp",
		HTTPClient:           &http.Client{Transport: bearer{key}, Timeout: 30 * time.Second},
		DisableStandaloneSSE: true,
		MaxRetries:           -1,
	}
	return client.Connect(ctx, transport, nil)
}

type bearer struct{ key string }

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	if b.key != "" {
		r = r.Clone(r.Context())
		r.Header.Set("Authorization", "Bearer "+b.key)
	}
	return http.DefaultTransport.RoundTrip(r)
}

func toolText(res *mcp.CallToolResult) string {
	var parts []string
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			parts = append(parts, tc.Text)
		}
	}
	return strings.Join(parts, " ")
}

type request struct {
	method, path string
	key          string // sent as a bearer token
	session      string // sent as the uncrm_session cookie
	header       http.Header
	body         any // []byte is sent as is; anything else as JSON
}

type response struct {
	*http.Response
	req  request
	body []byte
}

var httpClient = &http.Client{Timeout: 30 * time.Second}

func do(t *testing.T, r request) response {
	t.Helper()
	res, err := send(r)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func send(r request) (response, error) {
	var body io.Reader
	switch b := r.body.(type) {
	case nil:
	case []byte:
		body = bytes.NewReader(b)
	default:
		j, err := json.Marshal(b)
		if err != nil {
			return response{}, err
		}
		body = bytes.NewReader(j)
	}
	req, err := http.NewRequest(r.method, baseURL+r.path, body)
	if err != nil {
		return response{}, err
	}
	for k, v := range r.header {
		req.Header[k] = v
	}
	if r.body != nil && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if r.key != "" {
		req.Header.Set("Authorization", "Bearer "+r.key)
	}
	if r.session != "" {
		req.AddCookie(&http.Cookie{Name: "uncrm_session", Value: r.session})
	}
	res, err := httpClient.Do(req)
	if err != nil {
		return response{}, fmt.Errorf("%s %s: %w", r.method, r.path, err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		return response{}, fmt.Errorf("%s %s: read body: %w", r.method, r.path, err)
	}
	return response{Response: res, req: r, body: b}, nil
}

func (r response) check(codes ...int) error {
	if slices.Contains(codes, r.StatusCode) {
		return nil
	}
	return fmt.Errorf("%s %s: got %d %s, want %v", r.req.method, r.req.path, r.StatusCode, bytes.TrimSpace(r.body), codes)
}

func (r response) expect(t *testing.T, codes ...int) {
	t.Helper()
	if err := r.check(codes...); err != nil {
		t.Fatal(err)
	}
}

func (r response) decode(t *testing.T, v any) {
	t.Helper()
	if err := json.Unmarshal(r.body, v); err != nil {
		t.Fatalf("%s %s: decode %s: %v", r.req.method, r.req.path, r.body, err)
	}
}

func unique() string {
	return strconv.FormatInt(time.Now().UnixNano(), 36) + hex.EncodeToString(randomBytes(3))
}

func randomBytes(n int) []byte {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return b
}

func envOr(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}
