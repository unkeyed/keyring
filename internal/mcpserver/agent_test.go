package mcpserver

import (
	"context"
	"fmt"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/uncrm/uncrm/internal/auth"
	"github.com/uncrm/uncrm/internal/crm"
	"github.com/uncrm/uncrm/internal/testdb"
)

// session connects an MCP client to the real /mcp handler with an access
// token that needs no database, so a test can take the CRM's away and
// still get past authentication.
func session(t *testing.T) (*mcp.ClientSession, *crm.Service, *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	pool := testdb.New(t)
	svc := crm.New(pool, nil)
	srv := httptest.NewServer(Handler(svc, auth.NewKeyStore(pool, auth.UnkeyConfig{}), app, "test"))
	t.Cleanup(srv.Close)
	cs, err := connect(ctx, srv.URL, app.token)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs, svc, pool
}

// callErr runs a tool that must fail and returns the error text the agent
// sees.
func callErr(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) string {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	var text string
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			text += tc.Text
		}
	}
	if !res.IsError {
		t.Fatalf("%s(%v) succeeded: %s", name, args, text)
	}
	return text
}

func wantText(t *testing.T, what, text string, parts ...string) {
	t.Helper()
	for _, p := range parts {
		if !strings.Contains(text, p) {
			t.Errorf("%s = %q, want it to contain %q", what, text, p)
		}
	}
}

func TestErrorsSayWhatToDoNext(t *testing.T) {
	cs, _, _ := session(t)
	var c crm.Contact
	call(t, cs, "create_contact", map[string]any{"email": "ada@stripe.com"}, &c)

	text := callErr(t, cs, "create_contact", map[string]any{"email": "Ada@Stripe.com"})
	wantText(t, "duplicate create_contact", text, fmt.Sprintf(`contact %d already has ada@stripe.com; use get_contact {"id":%d} or update_contact`, c.ID, c.ID))
	text = callErr(t, cs, "create_company", map[string]any{"domain": "stripe.com"})
	wantText(t, "duplicate create_company", text, fmt.Sprintf(`company %d already has stripe.com; use get_company {"id":%d} or update_company`, c.Company.ID, c.Company.ID))

	for _, tc := range []struct {
		tool string
		args map[string]any
	}{
		{"set_stage", map[string]any{"id": c.ID, "stage": "won"}},
		{"search_contacts", map[string]any{"stage": "New"}},
		{"bulk_update_contacts", map[string]any{"ids": []int{int(c.ID)}, "stage": "won"}},
	} {
		wantText(t, tc.tool+" with an unknown stage", callErr(t, cs, tc.tool, tc.args), "new, qualified, customer, churned")
	}

	wantText(t, "get_contact 999", callErr(t, cs, "get_contact", map[string]any{"id": 999}), "no contact 999; find contacts with search_contacts")
	wantText(t, "get_contact by email", callErr(t, cs, "get_contact", map[string]any{"email": "bob@stripe.com"}), "search_contacts")
	wantText(t, "get_company 999", callErr(t, cs, "get_company", map[string]any{"id": 999}), "no company 999; find companies with search_companies")
	wantText(t, "delete_note 999", callErr(t, cs, "delete_note", map[string]any{"id": 999}), "no note 999; get_contact lists")
	wantText(t, "search_contacts with a bad date", callErr(t, cs, "search_contacts", map[string]any{"signed_up_before": "last week"}), "2026-01-02")
}

func TestInternalErrorsDoNotLeak(t *testing.T) {
	cs, _, pool := session(t)
	pool.Close()
	text := callErr(t, cs, "get_status", nil)
	wantText(t, "get_status with the database gone", text, "request_id")
	if strings.Contains(text, "closed") || strings.Contains(text, "pool") || strings.Contains(text, "SELECT") {
		t.Fatalf("error text %q leaks the internal cause", text)
	}
}

func TestBulkUpdateDeleteAndActivityPaging(t *testing.T) {
	cs, svc, _ := session(t)
	ctx := context.Background()
	api := crm.Actor{Kind: crm.ActorAPIKey, ID: "seed", Label: "seed"}
	var ids []crm.ContactID
	for _, email := range []string{"ada@stripe.com", "bob@stripe.com", "cy@stripe.com"} {
		c, err := svc.CreateContact(ctx, api, crm.NewContact{Email: email})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, c.ID)
	}

	var bulk bulkOut
	call(t, cs, "bulk_update_contacts", map[string]any{"ids": []any{ids[0], 999, ids[1]}, "stage": "qualified", "add_tags": []string{"webinar"}}, &bulk)
	if len(bulk.Results) != 3 || !bulk.Results[0].OK || bulk.Results[1].OK || bulk.Results[1].Error == "" || !bulk.Results[2].OK {
		t.Fatalf("bulk_update_contacts = %+v", bulk.Results)
	}
	var page crm.Page[ContactSummary]
	call(t, cs, "search_contacts", map[string]any{"tag": "webinar", "stage": "qualified"}, &page)
	if len(page.Items) != 2 {
		t.Fatalf("after bulk update, qualified webinar contacts = %+v", page.Items)
	}

	co := page.Items[0].Company.ID
	var seen []int64
	cursor := ""
	for range 10 {
		args := map[string]any{"company_id": co, "limit": 2}
		if cursor != "" {
			args["cursor"] = cursor
		}
		var p crm.Page[crm.Activity]
		text := call(t, cs, "list_activity", args, &p)
		for _, a := range p.Items {
			seen = append(seen, a.ID)
		}
		if p.NextCursor == "" {
			break
		}
		wantText(t, "list_activity with more pages", text, p.NextCursor)
		cursor = p.NextCursor
	}
	all, err := svc.ListActivity(ctx, crm.ActivityQuery{CompanyID: co})
	if err != nil {
		t.Fatal(err)
	}
	var want []int64
	for _, a := range all.Items {
		want = append(want, a.ID)
	}
	if len(want) != 5 || !slices.Equal(seen, want) {
		t.Fatalf("paged %v, want the 5 stripe.com entries %v (3 created, 2 updated)", seen, want)
	}
	var kinds crm.Page[crm.Activity]
	call(t, cs, "list_activity", map[string]any{"contact_id": ids[0], "kind": "contact_updated"}, &kinds)
	if len(kinds.Items) != 1 || kinds.Items[0].Actor != app.actor {
		t.Fatalf("ada's contact_updated = %+v, want one by the app", kinds.Items)
	}

	var gone deletedOut
	call(t, cs, "delete_contact", map[string]any{"id": ids[2]}, &gone)
	wantText(t, "get_contact after delete", callErr(t, cs, "get_contact", map[string]any{"id": ids[2]}), "no contact")
	wantText(t, "delete_contact twice", callErr(t, cs, "delete_contact", map[string]any{"id": ids[2]}), "no contact")
}

func TestGetCompanyCapsItsContacts(t *testing.T) {
	cs, svc, _ := session(t)
	ctx := context.Background()
	api := crm.Actor{Kind: crm.ActorAPIKey, ID: "seed", Label: "seed"}
	for i := range companyContacts + 1 {
		if _, err := svc.CreateContact(ctx, api, crm.NewContact{Email: fmt.Sprintf("p%d@stripe.com", i)}); err != nil {
			t.Fatal(err)
		}
	}
	var d crm.CompanyDetail
	text := call(t, cs, "get_company", map[string]any{"domain": "stripe.com"}, &d)
	if len(d.Contacts) != companyContacts || d.ContactCount != companyContacts+1 {
		t.Fatalf("get_company embedded %d of %d contacts, want %d", len(d.Contacts), d.ContactCount, companyContacts)
	}
	wantText(t, "get_company text", text, fmt.Sprintf(`search_contacts {"company_id": %d}`, d.ID))
}

// TestToolSchemasNameTheClosedSets checks what an agent's client reads from
// tools/list: closed sets as enums, times as date-time, and hints that let
// a client ask before an irreversible call.
func TestToolSchemasNameTheClosedSets(t *testing.T) {
	ctx := context.Background()
	srv := mcp.NewServer(&mcp.Implementation{Name: "uncrm", Version: "test"}, nil)
	addTools(srv, nil, nil)
	st, ct := mcp.NewInMemoryTransports()
	if _, err := srv.Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "test"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	res, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	tools := map[string]*mcp.Tool{}
	for _, tool := range res.Tools {
		tools[tool.Name] = tool
	}
	prop := func(tool, schema string, path ...string) map[string]any {
		var v any = tools[tool].InputSchema
		if schema == "output" {
			v = tools[tool].OutputSchema
		}
		for _, p := range path {
			v = v.(map[string]any)[p]
		}
		return v.(map[string]any)
	}
	enumOf := func(m map[string]any) []string {
		var out []string
		for _, v := range m["enum"].([]any) {
			out = append(out, v.(string))
		}
		return out
	}
	names := func(fs []crm.Field) []string {
		out := make([]string, len(fs))
		for i, f := range fs {
			out[i] = string(f)
		}
		return out
	}
	if got := enumOf(prop("reset_contact_field", "input", "properties", "field")); !slices.Equal(got, names(crm.ContactFields)) {
		t.Errorf("reset_contact_field field enum = %v", got)
	}
	if got := enumOf(prop("reset_company_field", "input", "properties", "field")); !slices.Equal(got, names(crm.CompanyFields)) {
		t.Errorf("reset_company_field field enum = %v", got)
	}
	if got := enumOf(prop("list_activity", "input", "properties", "kind")); !slices.Contains(got, "note_deleted") || len(got) != len(crm.ActivityKinds) {
		t.Errorf("list_activity kind enum = %v", got)
	}
	if got := enumOf(prop("enrich_contact", "output", "properties", "state")); len(got) != len(crm.JobStates) {
		t.Errorf("enrich_contact state enum = %v", got)
	}
	if f := prop("get_contact", "output", "properties", "created_at")["format"]; f != "date-time" {
		t.Errorf("get_contact created_at format = %v, want date-time", f)
	}

	for name, tool := range tools {
		a := tool.Annotations
		if a == nil {
			t.Errorf("%s has no annotations", name)
			continue
		}
		readOnly := strings.HasPrefix(name, "get_") || strings.HasPrefix(name, "list_") || strings.HasPrefix(name, "search_")
		destructive := strings.HasPrefix(name, "delete_") || name == "merge_contacts"
		if a.ReadOnlyHint != readOnly {
			t.Errorf("%s readOnlyHint = %v, want %v", name, a.ReadOnlyHint, readOnly)
		}
		if !readOnly && (a.DestructiveHint == nil || *a.DestructiveHint != destructive) {
			t.Errorf("%s destructiveHint = %v, want %v", name, a.DestructiveHint, destructive)
		}
	}
}
