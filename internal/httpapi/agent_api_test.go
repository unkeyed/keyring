package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/unkeyed/keyring/internal/crm"
)

func TestDeleteBulkAndLookups(t *testing.T) {
	c := newServer(t)
	_, agent := login(t, c)

	var ada, bob, sam crm.Contact
	agent.call("POST", "/v1/contacts", map[string]any{"email": "ada@stripe.com"}, http.StatusCreated, &ada)
	agent.call("POST", "/v1/contacts", map[string]any{"email": "bob@stripe.com"}, http.StatusCreated, &bob)
	agent.call("POST", "/v1/contacts", map[string]any{"email": "sam@gmail.com"}, http.StatusCreated, &sam)

	var page crm.Page[crm.Contact]
	agent.call("GET", "/v1/contacts?email=ADA@stripe.com", nil, http.StatusOK, &page)
	if len(page.Items) != 1 || page.Items[0].ID != ada.ID {
		t.Fatalf("?email= = %+v, want only ada", page.Items)
	}
	var companies crm.Page[crm.Company]
	agent.call("GET", "/v1/companies?domain=stripe.com", nil, http.StatusOK, &companies)
	if len(companies.Items) != 1 || companies.Items[0].ID != ada.Company.ID {
		t.Fatalf("?domain= = %+v", companies.Items)
	}

	var bulk struct{ Results []crm.BulkResult }
	agent.call("POST", "/v1/contacts/bulk", map[string]any{"ids": []any{ada.ID, 999, bob.ID}, "stage": "qualified", "add_tags": []string{"webinar"}},
		http.StatusOK, &bulk)
	if len(bulk.Results) != 3 || !bulk.Results[0].OK || bulk.Results[1].OK || bulk.Results[1].Error == "" || !bulk.Results[2].OK {
		t.Fatalf("bulk results = %+v", bulk.Results)
	}
	var counts []crm.StageCount
	agent.call("GET", "/v1/stages/counts", nil, http.StatusOK, &counts)
	if want := []crm.StageCount{{Name: "new", ContactCount: 1}, {Name: "qualified", ContactCount: 2}, {Name: "customer"}, {Name: "churned"}}; !slices.Equal(counts, want) {
		t.Fatalf("stage counts = %v, want %v", counts, want)
	}
	var names []crm.Stage
	agent.call("GET", "/v1/stages", nil, http.StatusOK, &names)
	if !slices.Equal(names, []crm.Stage{"new", "qualified", "customer", "churned"}) {
		t.Fatalf("stages = %v, want the names alone as before", names)
	}

	var job struct{ State crm.JobState }
	agent.call("POST", fmt.Sprintf("/v1/contacts/%d/enrich", sam.ID), nil, http.StatusOK, &job)
	if job.State != crm.JobSkipped {
		t.Fatalf("free-mail enrich = %q, want skipped with 200", job.State)
	}

	var first, rest crm.Page[crm.Activity]
	agent.call("GET", fmt.Sprintf("/v1/activity?company_id=%d&limit=3&since=2020-01-01", ada.Company.ID), nil, http.StatusOK, &first)
	agent.call("GET", fmt.Sprintf("/v1/activity?company_id=%d&limit=3&cursor=%s", ada.Company.ID, first.NextCursor), nil, http.StatusOK, &rest)
	if len(first.Items) != 3 || first.NextCursor == "" || len(rest.Items) != 1 || rest.NextCursor != "" {
		t.Fatalf("stripe.com activity pages = %d (cursor %q) then %d, want 3 then 1", len(first.Items), first.NextCursor, len(rest.Items))
	}

	agent.call("DELETE", fmt.Sprintf("/v1/companies/%d", ada.Company.ID), nil, http.StatusNoContent, nil)
	var detail crm.ContactDetail
	agent.call("GET", fmt.Sprintf("/v1/contacts/%d", ada.ID), nil, http.StatusOK, &detail)
	if detail.Company != nil {
		t.Fatalf("after deleting its company, ada's company = %+v", detail.Company)
	}
	agent.call("DELETE", fmt.Sprintf("/v1/contacts/%d", ada.ID), nil, http.StatusNoContent, nil)
	agent.call("GET", fmt.Sprintf("/v1/contacts/%d", ada.ID), nil, http.StatusNotFound, nil)
	agent.call("DELETE", fmt.Sprintf("/v1/contacts/%d", ada.ID), nil, http.StatusNotFound, nil)
}

func TestWrongMethodIs405WithAllow(t *testing.T) {
	c := newServer(t)
	_, agent := login(t, c)
	for _, tc := range []struct{ method, path, allow string }{
		{"PUT", "/v1/contacts", "GET, POST"},
		{"POST", "/v1/status", "GET"},
		{"PUT", "/v1/contacts/1", "GET, PATCH, DELETE"},
	} {
		r := agent.do(tc.method, tc.path, nil)
		if r.code != http.StatusMethodNotAllowed || r.header.Get("Allow") != tc.allow {
			t.Errorf("%s %s = %d Allow %q, want 405 Allow %q", tc.method, tc.path, r.code, r.header.Get("Allow"), tc.allow)
		}
	}
}

// mcpExclusions are API-key REST operations with no MCP tool, and why.
var mcpExclusions = map[string]string{
	"POST /v1/signups":        "records a sign-up from an auth system the team runs; agents add people with create_contact",
	"DELETE /v1/keys/current": "revokes the calling API key; MCP clients sign in with OAuth and are disconnected under Settings > Apps",
}

// mcpTools maps each API-key REST operation to the MCP tool that does the
// same, so the two surfaces cannot drift apart unnoticed.
var mcpTools = map[string]string{
	"GET /v1/contacts":                                "search_contacts",
	"POST /v1/contacts":                               "create_contact",
	"GET /v1/contacts/{id}":                           "get_contact",
	"PATCH /v1/contacts/{id}":                         "update_contact",
	"DELETE /v1/contacts/{id}":                        "delete_contact",
	"POST /v1/contacts/bulk":                          "bulk_update_contacts",
	"DELETE /v1/contacts/{id}/manual_fields/{field}":  "reset_contact_field",
	"POST /v1/contacts/{id}/merge":                    "merge_contacts",
	"POST /v1/contacts/{id}/notes":                    "add_note",
	"DELETE /v1/notes/{id}":                           "delete_note",
	"POST /v1/contacts/{id}/enrich":                   "enrich_contact",
	"GET /v1/companies":                               "search_companies",
	"POST /v1/companies":                              "create_company",
	"GET /v1/companies/{id}":                          "get_company",
	"PATCH /v1/companies/{id}":                        "update_company",
	"DELETE /v1/companies/{id}":                       "delete_company",
	"DELETE /v1/companies/{id}/manual_fields/{field}": "reset_company_field",
	"POST /v1/companies/{id}/enrich":                  "enrich_company",
	"GET /v1/activity":                                "list_activity",
	"GET /v1/tags":                                    "list_tags",
	"GET /v1/stages":                                  "list_stages",
	"GET /v1/stages/counts":                           "list_stages",
	"GET /v1/status":                                  "get_status",
}

// TestEveryAPIKeyRouteHasAnMCPTool fails when a route an API key can call
// has neither an MCP tool nor a stated reason to lack one, or names a tool
// the MCP server does not have.
func TestEveryAPIKeyRouteHasAnMCPTool(t *testing.T) {
	anon, person := newLocalServer(t)
	ctx := context.Background()
	_, tok := connect(t, anon, person, "parity")
	cs, err := mcpSession(anon, tok.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	list, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	var tools []string
	for _, tool := range list.Tools {
		tools = append(tools, tool.Name)
	}

	for _, rt := range routes {
		if rt.access != anyone {
			continue
		}
		tool, mapped := mcpTools[rt.pattern]
		reason := mcpExclusions[rt.pattern]
		switch {
		case mapped && !slices.Contains(tools, tool):
			t.Errorf("%s maps to %s, which the MCP server does not have", rt.pattern, tool)
		case !mapped && reason == "":
			t.Errorf("%s has no MCP tool; add one, or add it to mcpExclusions with the reason", rt.pattern)
		}
	}
	for _, ops := range []map[string]string{mcpTools, mcpExclusions} {
		for op := range ops {
			if !slices.ContainsFunc(routes, func(rt route) bool { return rt.pattern == op && rt.access == anyone }) {
				t.Errorf("%s is listed for MCP parity but is not an API-key route", op)
			}
		}
	}
}

type bearer string

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(b)))
	return http.DefaultTransport.RoundTrip(r)
}
