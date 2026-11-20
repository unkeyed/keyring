package mcpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/unkeyed/keyring/internal/auth"
	"github.com/unkeyed/keyring/internal/crm"
	"github.com/unkeyed/keyring/internal/signup"
	"github.com/unkeyed/keyring/internal/testdb"
)

// oneToken accepts one access token, standing in for auth.OAuthServer,
// whose own tests drive the real OAuth flow into /mcp.
type oneToken struct {
	token string
	actor crm.Actor
}

func (o oneToken) VerifyAccessToken(_ context.Context, token string) (crm.Actor, time.Time, error) {
	if token != o.token {
		return crm.Actor{}, time.Time{}, auth.ErrInvalidToken
	}
	return o.actor, time.Now().Add(time.Hour), nil
}

func (oneToken) ResourceMetadataURL() string {
	return "http://localhost/.well-known/oauth-protected-resource/mcp"
}

var app = oneToken{"keyring_at_test", crm.Actor{Kind: crm.ActorApp, ID: "1", Label: "Claude Code (via Ada)"}}

type bearer struct{ key string }

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	if b.key != "" {
		r.Header.Set("Authorization", "Bearer "+b.key)
	}
	return http.DefaultTransport.RoundTrip(r)
}

func connect(ctx context.Context, url, key string) (*mcp.ClientSession, error) {
	return mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil).Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint:             url,
		HTTPClient:           &http.Client{Transport: bearer{key}},
		DisableStandaloneSSE: true,
		MaxRetries:           -1,
	}, nil)
}

// call runs a tool, fails on a tool error, and decodes its structured
// content into out. It returns the text content.
func call(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any, out any) string {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	text := ""
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			text += tc.Text
		}
	}
	if res.IsError {
		t.Fatalf("%s(%v) returned a tool error: %s", name, args, text)
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			t.Fatalf("%s: structured content %s: %v", name, raw, err)
		}
	}
	return text
}

// TestEveryToolOverHTTP drives each tool through the real /mcp handler with
// an access token, the way an agent's MCP client does.
func TestEveryToolOverHTTP(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool := testdb.New(t)
	svc := crm.New(pool, nil)
	srv := httptest.NewServer(Handler(svc, auth.NewKeyStore(pool, auth.UnkeyConfig{}), app, "test"))
	defer srv.Close()

	if _, err := connect(ctx, srv.URL, ""); err == nil {
		t.Fatal("connected without a token")
	}
	if _, err := connect(ctx, srv.URL, "keyring_at_wrong"); err == nil {
		t.Fatal("connected with a wrong token")
	}
	cs, err := connect(ctx, srv.URL, app.token)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()

	tools, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range tools.Tools {
		names = append(names, tool.Name)
	}
	want := []string{"add_note", "bulk_update_contacts", "create_company", "create_contact", "delete_company", "delete_contact", "delete_note",
		"enrich_company", "enrich_contact", "get_company", "get_contact", "get_status", "list_activity", "list_recent_signups", "list_stages", "list_tags",
		"merge_contacts", "reset_company_field", "reset_contact_field", "search_companies", "search_contacts", "set_stage", "update_company", "update_contact"}
	if slices.Sort(names); !slices.Equal(names, want) {
		t.Fatalf("tools = %v, want %v", names, want)
	}

	api := crm.Actor{Kind: crm.ActorAPIKey, ID: "seed", Label: "seed"}
	ada, err := svc.Ingest(ctx, api, signup.Signup{Kind: signup.Created, Email: "ada@stripe.com", Name: "Ada"})
	if err != nil {
		t.Fatal(err)
	}
	personal, err := svc.Ingest(ctx, api, signup.Signup{Kind: signup.Created, Email: "ada@gmail.com"})
	if err != nil {
		t.Fatal(err)
	}

	var page crm.Page[ContactSummary]
	call(t, cs, "search_contacts", map[string]any{"query": "stripe"}, &page)
	if len(page.Items) != 1 || page.Items[0].ID != ada.ContactID || page.Items[0].Company == nil || page.Items[0].SignedUpAt.IsZero() {
		t.Fatalf("search_contacts stripe = %+v", page.Items)
	}
	call(t, cs, "search_contacts", map[string]any{"email": "ADA@stripe.com", "signed_up_after": "2020-01-01"}, &page)
	if len(page.Items) != 1 || page.Items[0].ID != ada.ContactID {
		t.Fatalf("search_contacts by email and date = %+v", page.Items)
	}

	var detail crm.ContactDetail
	call(t, cs, "get_contact", map[string]any{"email": "ADA@stripe.com"}, &detail)
	if detail.ID != ada.ContactID || len(detail.Identities) != 1 {
		t.Fatalf("get_contact by email = %+v", detail)
	}
	if res, _ := cs.CallTool(ctx, &mcp.CallToolParams{Name: "get_contact", Arguments: map[string]any{}}); !res.IsError {
		t.Fatal("get_contact with neither id nor email succeeded")
	}

	var c crm.Contact
	call(t, cs, "update_contact", map[string]any{"id": ada.ContactID, "changes": map[string]any{"title": "Analyst", "add_tags": []string{"VIP"}}}, &c)
	if c.Title != "Analyst" || !slices.Contains(c.Tags, "vip") || !slices.Contains(c.ManualFields, crm.FieldTitle) {
		t.Fatalf("update_contact = %+v", c)
	}
	call(t, cs, "set_stage", map[string]any{"id": ada.ContactID, "stage": "qualified"}, &c)
	if c.Stage != "qualified" {
		t.Fatalf("set_stage = %q", c.Stage)
	}
	if res, _ := cs.CallTool(ctx, &mcp.CallToolParams{Name: "set_stage", Arguments: map[string]any{"id": ada.ContactID, "stage": "nope"}}); !res.IsError {
		t.Fatal("set_stage to an unknown stage succeeded")
	}
	call(t, cs, "reset_contact_field", map[string]any{"id": ada.ContactID, "field": "title"}, &c)
	if slices.Contains(c.ManualFields, crm.FieldTitle) {
		t.Fatalf("reset_contact_field left title pinned: %v", c.ManualFields)
	}

	var note crm.Note
	call(t, cs, "add_note", map[string]any{"contact_id": ada.ContactID, "body": "Asked for a demo."}, &note)
	if note.Author != app.actor {
		t.Fatalf("note author = %+v, want the app %+v", note.Author, app.actor)
	}

	var signups signupsOut
	text := call(t, cs, "list_recent_signups", map[string]any{"limit": 1}, &signups)
	if len(signups.Signups) != 1 || signups.NextCursor == "" || !strings.Contains(text, signups.NextCursor) {
		t.Fatalf("list_recent_signups page 1 = %+v, text %q", signups, text)
	}
	var rest signupsOut
	call(t, cs, "list_recent_signups", map[string]any{"limit": 1, "cursor": signups.NextCursor}, &rest)
	if len(rest.Signups) != 1 || rest.Signups[0].ID == signups.Signups[0].ID || rest.NextCursor != "" {
		t.Fatalf("list_recent_signups page 2 = %+v", rest)
	}

	var company crm.CompanyDetail
	call(t, cs, "get_company", map[string]any{"domain": "stripe.com"}, &company)
	if len(company.Contacts) != 1 {
		t.Fatalf("get_company contacts = %d", len(company.Contacts))
	}
	var companies crm.Page[CompanySummary]
	call(t, cs, "search_companies", map[string]any{"domain": "stripe.com"}, &companies)
	if len(companies.Items) != 1 || companies.Items[0].ID != company.ID || companies.Items[0].ContactCount != 1 {
		t.Fatalf("search_companies = %+v", companies.Items)
	}
	var co crm.Company
	call(t, cs, "update_company", map[string]any{"id": company.ID, "changes": map[string]any{"name": "Stripe"}}, &co)
	if co.Name != "Stripe" || !slices.Contains(co.ManualFields, crm.FieldName) {
		t.Fatalf("update_company = %+v", co)
	}
	call(t, cs, "reset_company_field", map[string]any{"id": company.ID, "field": "name"}, &co)
	if slices.Contains(co.ManualFields, crm.FieldName) {
		t.Fatalf("reset_company_field left name pinned: %v", co.ManualFields)
	}

	var job enrichOut
	call(t, cs, "enrich_contact", map[string]any{"id": ada.ContactID}, &job)
	if job.State != crm.JobPending {
		t.Fatalf("enrich_contact = %q", job.State)
	}
	call(t, cs, "enrich_company", map[string]any{"id": company.ID}, &job)
	if job.State != crm.JobPending {
		t.Fatalf("enrich_company = %q", job.State)
	}

	var stages stagesOut
	call(t, cs, "list_stages", nil, &stages)
	if len(stages.Stages) != 4 || stages.Stages[0] != (crm.StageCount{Name: "new", ContactCount: 1}) || stages.Stages[1] != (crm.StageCount{Name: "qualified", ContactCount: 1}) {
		t.Fatalf("list_stages = %v", stages.Stages)
	}
	var tags tagsOut
	call(t, cs, "list_tags", nil, &tags)
	if tags.Tags["vip"] != 1 {
		t.Fatalf("list_tags = %v", tags.Tags)
	}

	call(t, cs, "merge_contacts", map[string]any{"keep": ada.ContactID, "drop": personal.ContactID}, &c)
	call(t, cs, "get_contact", map[string]any{"email": "ada@gmail.com"}, &detail)
	if detail.ID != ada.ContactID {
		t.Fatalf("after merge, ada@gmail.com resolves to %d, want %d", detail.ID, ada.ContactID)
	}

	var made crm.Contact
	call(t, cs, "create_contact", map[string]any{"email": "grace@navy.mil", "title": "Rear Admiral"}, &made)
	if made.Company == nil || made.Company.Domain.String() != "navy.mil" || made.Title != "Rear Admiral" {
		t.Fatalf("create_contact = %+v", made)
	}
	if res, _ := cs.CallTool(ctx, &mcp.CallToolParams{Name: "create_contact", Arguments: map[string]any{"email": "grace@navy.mil"}}); !res.IsError {
		t.Fatal("create_contact with an existing email succeeded")
	}
	var madeCo crm.Company
	call(t, cs, "create_company", map[string]any{"domain": "linear.app"}, &madeCo)
	if madeCo.Domain.String() != "linear.app" || madeCo.Enrichment != crm.JobPending || madeCo.Logos == nil {
		t.Fatalf("create_company = %+v", madeCo)
	}

	var st statusOut
	call(t, cs, "get_status", nil, &st)
	if st.Contacts != 2 || st.Companies != 3 || st.KeyStore != "builtin" {
		t.Fatalf("get_status = %+v", st)
	}

	var gone deletedOut
	call(t, cs, "delete_company", map[string]any{"id": madeCo.ID}, &gone)
	call(t, cs, "delete_note", map[string]any{"id": note.ID}, &gone)
	call(t, cs, "delete_contact", map[string]any{"id": made.ID}, &gone)
	if gone.ID != int64(made.ID) || !gone.Deleted {
		t.Fatalf("delete_contact = %+v", gone)
	}
	call(t, cs, "get_status", nil, &st)
	if st.Contacts != 1 || st.Companies != 2 {
		t.Fatalf("after deletes, get_status = %+v, want 1 contact and 2 companies", st)
	}
}
