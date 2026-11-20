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
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/uncrm/uncrm/internal/auth"
	"github.com/uncrm/uncrm/internal/authtest"
	"github.com/uncrm/uncrm/internal/crm"
	"github.com/uncrm/uncrm/internal/testdb"
)

const (
	adminEmail = "admin@example.com"
	testOrigin = "http://localhost:8080"
)

// client calls a test server as a person (session cookie) or an agent
// (API key), or anonymously when both are empty. pending carries a login
// waiting for its second factor.
type client struct {
	t          *testing.T
	base       string
	session    string
	pending    string
	key        string
	setupToken string
	passkey    *authtest.Passkey
	db         *pgxpool.Pool
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
	if c.pending != "" {
		req.AddCookie(&http.Cookie{Name: auth.PendingCookie, Value: c.pending})
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

func newServer(t *testing.T, mod ...func(*auth.Config)) client {
	t.Helper()
	pool := testdb.New(t)
	u, _ := url.Parse(testOrigin)
	cfg := auth.Config{PublicURL: u}
	for _, m := range mod {
		m(&cfg)
	}
	users, err := auth.NewUsers(pool, cfg)
	if err != nil {
		t.Fatal(err)
	}
	token, err := users.PrepareSetup(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	authn := &auth.Authenticator{Keys: auth.NewKeyStore(pool, auth.UnkeyConfig{}), Users: users}
	srv := httptest.NewServer(NewMux(crm.New(pool, nil), authn, slog.New(slog.DiscardHandler)))
	t.Cleanup(srv.Close)
	return client{t: t, base: srv.URL, setupToken: token, db: pool}
}

func cookieValue(r reply, name string) string {
	for _, ck := range (&http.Response{Header: r.header}).Cookies() {
		if ck.Name == name && ck.MaxAge >= 0 {
			return ck.Value
		}
	}
	return ""
}

type ceremony struct {
	Handle  string          `json:"handle"`
	Options json.RawMessage `json:"options"`
}

// signup registers a passkey through POST /auth/signup/passkey/* and
// returns c signed in as the new user.
func (c client) signup(body map[string]string) client {
	c.t.Helper()
	var begun ceremony
	c.call("POST", "/auth/signup/passkey/begin", body, http.StatusOK, &begun)
	pk, cred, err := authtest.NewPasskey(testOrigin, begun.Options)
	if err != nil {
		c.t.Fatal(err)
	}
	var res struct{ Status string }
	r := c.call("POST", "/auth/signup/passkey/finish", map[string]any{"handle": begun.Handle, "credential": cred}, http.StatusOK, &res)
	if res.Status != "signed_in" {
		c.t.Fatalf("signup answered %q, want signed_in", res.Status)
	}
	c.session, c.passkey = cookieValue(r, auth.SessionCookie), pk
	if c.session == "" {
		c.t.Fatal("signup set no session cookie")
	}
	return c
}

// passkeyLogin signs in with pk through POST /auth/passkey/*.
func (c client) passkeyLogin(pk *authtest.Passkey) client {
	c.t.Helper()
	var begun ceremony
	c.call("POST", "/auth/passkey/begin", nil, http.StatusOK, &begun)
	cred, err := pk.Sign(begun.Options)
	if err != nil {
		c.t.Fatal(err)
	}
	r := c.call("POST", "/auth/passkey/finish", map[string]any{"handle": begun.Handle, "credential": cred}, http.StatusOK, nil)
	c.session, c.passkey = cookieValue(r, auth.SessionCookie), pk
	return c
}

// login returns c as the first user, created through the setup link with a
// passkey, and an agent holding a fresh key.
func login(t *testing.T, c client) (person, agent client) {
	t.Helper()
	person = c.signup(map[string]string{"setup_token": c.setupToken, "email": adminEmail})
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

func TestSignInAndOut(t *testing.T) {
	c := newServer(t)
	var opts auth.Options
	c.call("GET", "/auth/options", nil, http.StatusOK, &opts)
	if !opts.SetupRequired || opts.PasswordLogin || len(opts.Providers) != 0 {
		t.Fatalf("options before setup = %+v", opts)
	}
	c.call("GET", "/auth/setup/"+c.setupToken, nil, http.StatusNoContent, nil)

	person, _ := login(t, c)
	c.call("GET", "/auth/options", nil, http.StatusOK, &opts)
	if opts.SetupRequired {
		t.Fatal("options still ask for setup after the first user")
	}
	c.call("GET", "/auth/setup/"+c.setupToken, nil, http.StatusNotFound, nil)
	c.call("POST", "/auth/signup/passkey/begin", map[string]string{"setup_token": c.setupToken, "email": "eve@example.com"}, http.StatusNotFound, nil)

	var me crm.Actor
	person.call("GET", "/v1/me", nil, http.StatusOK, &me)
	if me.Kind != crm.ActorUser || me.Label != adminEmail {
		t.Fatalf("me = %+v", me)
	}
	person.call("POST", "/auth/logout", nil, http.StatusNoContent, nil)
	person.call("GET", "/v1/me", nil, http.StatusUnauthorized, nil)

	again := c.passkeyLogin(person.passkey)
	again.call("GET", "/v1/me", nil, http.StatusOK, nil)
	c.call("POST", "/auth/password", map[string]string{"email": adminEmail, "password": "anything-at-all"}, http.StatusNotFound, nil)
}

func TestPasswordNeedsASecondFactor(t *testing.T) {
	c := newServer(t, func(cfg *auth.Config) { cfg.PasswordLogin = true })
	person, _ := login(t, c)
	person.call("PUT", "/v1/account/password", map[string]string{"password": "correct-horse-battery"}, http.StatusNoContent, nil)

	var res struct {
		Status        string   `json:"status"`
		SecondFactors []string `json:"second_factors"`
	}
	r := c.call("POST", "/auth/password", map[string]string{"email": adminEmail, "password": "correct-horse-battery"}, http.StatusOK, &res)
	if res.Status != "second_factor" || cookieValue(r, auth.SessionCookie) != "" {
		t.Fatalf("password sign-in answered %+v with a session cookie %q; a password alone must never sign in", res, cookieValue(r, auth.SessionCookie))
	}
	pending := c
	pending.pending = cookieValue(r, auth.PendingCookie)
	pending.call("GET", "/auth/second-factor", nil, http.StatusOK, &res)
	if !slices.Equal(res.SecondFactors, []string{"passkey"}) {
		t.Fatalf("second factors %v, want passkey", res.SecondFactors)
	}
	var begun ceremony
	pending.call("POST", "/auth/second-factor/passkey/begin", nil, http.StatusOK, &begun)
	cred, err := person.passkey.Sign(begun.Options)
	if err != nil {
		t.Fatal(err)
	}
	r = pending.call("POST", "/auth/second-factor/passkey/finish", map[string]any{"handle": begun.Handle, "credential": cred}, http.StatusOK, &res)
	if res.Status != "signed_in" || cookieValue(r, auth.SessionCookie) == "" {
		t.Fatalf("second factor answered %+v", res)
	}

	wrong := map[string]string{"email": adminEmail, "password": "wrong-password-123"}
	for range 5 {
		c.call("POST", "/auth/password", wrong, http.StatusUnauthorized, nil)
	}
	r = c.call("POST", "/auth/password", wrong, http.StatusTooManyRequests, nil)
	if r.header.Get("Retry-After") == "" || r.header.Get("Retry-After") == "0" {
		t.Fatalf("429 Retry-After = %q, want whole seconds", r.header.Get("Retry-After"))
	}
}

func TestInviteFlow(t *testing.T) {
	c := newServer(t)
	person, _ := login(t, c)
	var inv struct{ Token string }
	person.call("POST", "/v1/invites", map[string]string{"email": "grace@example.com"}, http.StatusCreated, &inv)
	var info struct{ Email string }
	c.call("GET", "/auth/invites/"+inv.Token, nil, http.StatusOK, &info)
	if info.Email != "grace@example.com" {
		t.Fatalf("invite email %q", info.Email)
	}

	grace := c.signup(map[string]string{"invite_token": inv.Token, "name": "Grace"})
	var me crm.Actor
	grace.call("GET", "/v1/me", nil, http.StatusOK, &me)
	if me.Label != "Grace" {
		t.Fatalf("invited user's session is %+v", me)
	}
	c.call("POST", "/auth/signup/passkey/begin", map[string]string{"invite_token": inv.Token, "name": "Grace"}, http.StatusNotFound, nil)
	c.call("GET", "/auth/invites/"+inv.Token, nil, http.StatusNotFound, nil)

	var users []auth.User
	person.call("GET", "/v1/users", nil, http.StatusOK, &users)
	if len(users) != 2 || users[1].Name != "Grace" {
		t.Fatalf("users = %+v, want the admin and Grace", users)
	}

	person.call("DELETE", fmt.Sprintf("/v1/users/%d", users[1].ID), nil, http.StatusNoContent, nil)
	grace.call("GET", "/v1/me", nil, http.StatusUnauthorized, nil)
	person.call("DELETE", fmt.Sprintf("/v1/users/%d", users[0].ID), nil, http.StatusConflict, nil)
}

func TestAccountChangesNeedARecentSignIn(t *testing.T) {
	c := newServer(t)
	person, _ := login(t, c)
	var account auth.Account
	person.call("GET", "/v1/account", nil, http.StatusOK, &account)
	if !account.Recent || len(account.Passkeys) != 1 || len(account.Sessions) != 1 || !account.Sessions[0].Current {
		t.Fatalf("account = %+v", account)
	}

	if _, err := c.db.Exec(context.Background(), `UPDATE sessions SET authenticated_at = now() - interval '1 hour'`); err != nil {
		t.Fatal(err)
	}
	r := person.call("POST", "/v1/account/totp/begin", nil, http.StatusForbidden, nil)
	var e struct{ Code string }
	if err := json.Unmarshal(r.body, &e); err != nil || e.Code != "reauth_required" {
		t.Fatalf("stale session changing sign-in methods: %s, want code reauth_required", r.body)
	}

	var begun ceremony
	person.call("POST", "/v1/account/reauth/passkey/begin", nil, http.StatusOK, &begun)
	cred, err := person.passkey.Sign(begun.Options)
	if err != nil {
		t.Fatal(err)
	}
	person.call("POST", "/v1/account/reauth/passkey", map[string]any{"handle": begun.Handle, "credential": cred}, http.StatusNoContent, nil)
	person.call("POST", "/v1/account/totp/begin", nil, http.StatusOK, nil)
}

func TestOAuthSignInSetsCookiesAndRedirects(t *testing.T) {
	iss := authtest.NewIssuer()
	defer iss.Close()
	c := newServer(t, func(cfg *auth.Config) {
		cfg.Providers = []auth.ProviderConfig{{ID: "oidc", Name: "Acme SSO", ClientID: "uncrm", ClientSecret: "s3cret", Issuer: iss.URL}}
	})
	login(t, c)
	jar, _ := cookiejar.New(nil)
	browser := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	get := func(u string) *http.Response {
		t.Helper()
		res, err := browser.Get(u)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res
	}
	base, _ := url.Parse(c.base)
	toServer := func(loc string) string {
		u, err := url.Parse(loc)
		if err != nil {
			t.Fatal(err)
		}
		u.Scheme, u.Host = base.Scheme, base.Host
		return u.String()
	}

	iss.SetNext(authtest.Identity{Subject: "mallory", Email: "mallory@evil.com", EmailVerified: true})
	res := get(c.base + "/auth/oauth/oidc/start?next=/contacts")
	res = get(res.Header.Get("Location"))
	res = get(toServer(res.Header.Get("Location")))
	if loc := res.Header.Get("Location"); !strings.HasPrefix(loc, "/login?error=") {
		t.Fatalf("a stranger's callback redirected to %q, want /login?error=", loc)
	}

	iss.SetNext(authtest.Identity{Subject: "admin", Email: adminEmail, EmailVerified: true})
	res = get(c.base + "/auth/oauth/oidc/start?next=/contacts")
	if res.StatusCode != http.StatusSeeOther || !strings.HasPrefix(res.Header.Get("Location"), iss.URL+"/authorize") {
		t.Fatalf("start = %d to %q", res.StatusCode, res.Header.Get("Location"))
	}
	callback := toServer(get(res.Header.Get("Location")).Header.Get("Location"))

	stranger := &http.Client{CheckRedirect: browser.CheckRedirect}
	other, err := stranger.Get(callback)
	if err != nil {
		t.Fatal(err)
	}
	other.Body.Close()
	if loc := other.Header.Get("Location"); !strings.HasPrefix(loc, "/login?error=") {
		t.Fatalf("callback in another browser redirected to %q, want an error", loc)
	}

	res = get(callback)
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/contacts" {
		t.Fatalf("callback = %d to %q, want 303 to /contacts", res.StatusCode, res.Header.Get("Location"))
	}
	var session string
	for _, ck := range jar.Cookies(base) {
		if ck.Name == auth.SessionCookie {
			session = ck.Value
		}
	}
	signedIn := c
	signedIn.session = session
	var me crm.Actor
	signedIn.call("GET", "/v1/me", nil, http.StatusOK, &me)
	if me.Label != adminEmail {
		t.Fatalf("OIDC sign-in is %+v", me)
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
	u, _ := url.Parse(testOrigin)
	users, err := auth.NewUsers(pool, auth.Config{PublicURL: u})
	if err != nil {
		t.Fatal(err)
	}
	authn := &auth.Authenticator{Keys: downKeys{}, Users: users}
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
