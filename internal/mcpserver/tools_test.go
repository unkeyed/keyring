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

	"github.com/uncrm/uncrm/internal/auth"
	"github.com/uncrm/uncrm/internal/crm"
	"github.com/uncrm/uncrm/internal/signup"
	"github.com/uncrm/uncrm/internal/testdb"
)

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
// an API key, the way an agent's MCP client does.
func TestEveryToolOverHTTP(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool := testdb.New(t)
	svc := crm.New(pool, nil)
	keys := auth.NewKeyStore(pool, auth.UnkeyConfig{})
	key, _, err := keys.Create(ctx, crm.Actor{}, "sales agent")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(Handler(svc, keys, "test"))
	defer srv.Close()

	if _, err := connect(ctx, srv.URL, ""); err == nil {
		t.Fatal("connected without an API key")
	}
	if _, err := connect(ctx, srv.URL, "uncrm_wrong"); err == nil {
		t.Fatal("connected with a wrong API key")
	}
	cs, err := connect(ctx, srv.URL, key)
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
	want := []string{"add_note", "enrich_contact", "get_company", "get_contact", "get_status", "list_recent_signups", "list_stages", "list_tags",
		"merge_contacts", "reset_contact_field", "search_companies", "search_contacts", "set_stage", "update_contact"}
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

	var page crm.Page[crm.Contact]
	text := call(t, cs, "search_contacts", map[string]any{"query": "stripe"}, &page)
	if len(page.Items) != 1 || page.Items[0].ID != ada.ContactID || !strings.HasPrefix(text, "1 contacts.") {
		t.Fatalf("search_contacts stripe = %+v, text %q", page.Items, text)
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
	if note.Author.Kind != crm.ActorAPIKey || note.Author.Label != "sales agent" {
		t.Fatalf("note author = %+v, want the API key", note.Author)
	}

	var signups signupsOut
	text = call(t, cs, "list_recent_signups", map[string]any{"limit": 10}, &signups)
	if len(signups.Signups) != 2 || !strings.Contains(text, "ada@stripe.com") {
		t.Fatalf("list_recent_signups = %d, text %q", len(signups.Signups), text)
	}

	var company crm.CompanyDetail
	call(t, cs, "get_company", map[string]any{"domain": "stripe.com"}, &company)
	if len(company.Contacts) != 1 {
		t.Fatalf("get_company contacts = %d", len(company.Contacts))
	}
	var companies crm.Page[crm.Company]
	call(t, cs, "search_companies", map[string]any{"query": "stripe"}, &companies)
	if len(companies.Items) != 1 || companies.Items[0].ID != company.ID {
		t.Fatalf("search_companies = %+v", companies.Items)
	}

	var job enrichOut
	call(t, cs, "enrich_contact", map[string]any{"id": ada.ContactID}, &job)
	if job.State != crm.JobPending {
		t.Fatalf("enrich_contact = %q", job.State)
	}

	var stages stagesOut
	call(t, cs, "list_stages", nil, &stages)
	if len(stages.Stages) == 0 || stages.Stages[0] != "new" {
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

	var st statusOut
	text = call(t, cs, "get_status", nil, &st)
	if st.Contacts != 1 || st.KeyStore != "builtin" || !strings.HasPrefix(text, "1 contacts, 1 companies.") {
		t.Fatalf("get_status = %+v, text %q", st, text)
	}
}
