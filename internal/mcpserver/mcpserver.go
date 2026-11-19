// Package mcpserver exposes crm.Service as MCP tools over stateless
// Streamable HTTP at /mcp, authenticated with the same API keys as REST.
// Each tool is an input struct and a few lines that call one Service method;
// the output is the same domain type REST returns.
package mcpserver

import (
	"context"
	"fmt"
	"net/http"
	"reflect"

	"github.com/google/jsonschema-go/jsonschema"

	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/uncrm/uncrm/internal/auth"
	"github.com/uncrm/uncrm/internal/crm"
)

const actorKey = "actor"

// Handler returns the /mcp handler. Stateless, so any replica serves any
// request without sticky sessions.
func Handler(svc *crm.Service, keys auth.KeyStore, version string) http.Handler {
	srv := mcp.NewServer(&mcp.Implementation{Name: "uncrm", Version: version}, nil)
	addTools(srv, svc)
	h := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv },
		&mcp.StreamableHTTPOptions{Stateless: true})

	verify := func(ctx context.Context, token string, _ *http.Request) (*mcpauth.TokenInfo, error) {
		a, err := keys.Verify(ctx, token)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", mcpauth.ErrInvalidToken, err)
		}
		return &mcpauth.TokenInfo{UserID: a.ID, Extra: map[string]any{actorKey: a}}, nil
	}
	return mcpauth.RequireBearerToken(verify, &mcpauth.RequireBearerTokenOptions{AllowMissingExpiration: true})(h)
}

// actorOf reads the actor the bearer middleware stored for this request.
func actorOf(req *mcp.CallToolRequest) crm.Actor {
	a, _ := req.Extra.TokenInfo.Extra[actorKey].(crm.Actor)
	return a
}

type searchContactsIn struct {
	Query    string    `json:"query,omitempty" jsonschema:"substring of email, name, or company"`
	Tag      crm.Tag   `json:"tag,omitempty"`
	Stage    crm.Stage `json:"stage,omitempty"`
	Provider string    `json:"provider,omitempty" jsonschema:"auth provider name, e.g. clerk"`
	Cursor   string    `json:"cursor,omitempty"`
	Limit    int       `json:"limit,omitempty"`
}

type getContactIn struct {
	ID    crm.ContactID `json:"id,omitempty" jsonschema:"contact id; give this or email"`
	Email string        `json:"email,omitempty"`
}

type updateContactIn struct {
	ID      crm.ContactID    `json:"id"`
	Changes crm.ContactPatch `json:"changes"`
}

type resetContactFieldIn struct {
	ID    crm.ContactID `json:"id"`
	Field crm.Field     `json:"field" jsonschema:"name, title, location, bio, linkedin_url or company"`
}

type mergeContactsIn struct {
	Keep crm.ContactID `json:"keep" jsonschema:"contact that survives"`
	Drop crm.ContactID `json:"drop" jsonschema:"contact folded into keep and deleted"`
}

type addNoteIn struct {
	ContactID crm.ContactID `json:"contact_id"`
	Body      string        `json:"body" jsonschema:"markdown note text"`
}

type recentSignupsIn struct {
	Limit int `json:"limit,omitempty"`
}

type getCompanyIn struct {
	ID     crm.CompanyID `json:"id,omitempty"`
	Domain string        `json:"domain,omitempty"`
}

type enrichContactIn struct {
	ID crm.ContactID `json:"id"`
}

type enrichOut struct {
	State crm.JobState `json:"state"`
}

type signupsOut struct {
	Signups []crm.Activity `json:"signups"`
}

// schemaOpts tells schema inference that parse-only domain types travel as
// strings. Without it, a struct with only unexported fields is inferred as an
// empty object, and every tool output holding one fails validation.
var schemaOpts = &jsonschema.ForOptions{TypeSchemas: map[reflect.Type]*jsonschema.Schema{
	reflect.TypeFor[crm.Email]():  {Type: "string"},
	reflect.TypeFor[crm.Domain](): {Type: "string"},
}}

// addTool is mcp.AddTool with input and output schemas inferred under
// schemaOpts. Every tool registers through it.
func addTool[In, Out any](srv *mcp.Server, t *mcp.Tool, h mcp.ToolHandlerFor[In, Out]) {
	in, err := jsonschema.For[In](schemaOpts)
	if err != nil {
		panic(err)
	}
	out, err := jsonschema.For[Out](schemaOpts)
	if err != nil {
		panic(err)
	}
	t.InputSchema, t.OutputSchema = in, out
	mcp.AddTool(srv, t, h)
}

func addTools(srv *mcp.Server, svc *crm.Service) {
	addTool(srv, &mcp.Tool{Name: "search_contacts", Description: "Search contacts by text, tag, stage or provider."},
		func(ctx context.Context, req *mcp.CallToolRequest, in searchContactsIn) (*mcp.CallToolResult, crm.Page[crm.Contact], error) {
			p, err := svc.SearchContacts(ctx, crm.ContactQuery{Text: in.Query, Tag: in.Tag, Stage: in.Stage, Provider: in.Provider, Cursor: in.Cursor, Limit: in.Limit})
			return nil, p, err
		})

	addTool(srv, &mcp.Tool{Name: "get_contact", Description: "Get one contact with identities, notes and recent activity, by id or email."},
		func(ctx context.Context, req *mcp.CallToolRequest, in getContactIn) (*mcp.CallToolResult, crm.ContactDetail, error) {
			if in.Email != "" {
				c, err := svc.FindContactByEmail(ctx, in.Email)
				return nil, c, err
			}
			c, err := svc.GetContact(ctx, in.ID)
			return nil, c, err
		})

	addTool(srv, &mcp.Tool{Name: "update_contact", Description: "Change a contact's fields, stage, company or tags."},
		func(ctx context.Context, req *mcp.CallToolRequest, in updateContactIn) (*mcp.CallToolResult, crm.Contact, error) {
			c, err := svc.UpdateContact(ctx, actorOf(req), in.ID, in.Changes)
			return nil, c, err
		})

	addTool(srv, &mcp.Tool{Name: "reset_contact_field", Description: "Hand a field a person or agent set back to the auth provider and enrichment, so their next value shows."},
		func(ctx context.Context, req *mcp.CallToolRequest, in resetContactFieldIn) (*mcp.CallToolResult, crm.Contact, error) {
			c, err := svc.ResetContactField(ctx, actorOf(req), in.ID, in.Field)
			return nil, c, err
		})

	addTool(srv, &mcp.Tool{Name: "merge_contacts", Description: "Merge two contacts that are the same person. Moves drop's identities, notes, tags and history to keep, then deletes drop."},
		func(ctx context.Context, req *mcp.CallToolRequest, in mergeContactsIn) (*mcp.CallToolResult, crm.Contact, error) {
			c, err := svc.MergeContacts(ctx, actorOf(req), in.Keep, in.Drop)
			return nil, c, err
		})

	addTool(srv, &mcp.Tool{Name: "add_note", Description: "Add a note to a contact's timeline."},
		func(ctx context.Context, req *mcp.CallToolRequest, in addNoteIn) (*mcp.CallToolResult, crm.Note, error) {
			n, err := svc.AddNote(ctx, actorOf(req), in.ContactID, in.Body)
			return nil, n, err
		})

	addTool(srv, &mcp.Tool{Name: "list_recent_signups", Description: "List the newest sign-ups across all providers."},
		func(ctx context.Context, req *mcp.CallToolRequest, in recentSignupsIn) (*mcp.CallToolResult, signupsOut, error) {
			a, err := svc.ListActivity(ctx, crm.ActivityQuery{Kind: crm.ActivitySignedUp, Limit: in.Limit})
			return nil, signupsOut{Signups: a}, err
		})

	addTool(srv, &mcp.Tool{Name: "get_company", Description: "Get a company and its contacts, by id or domain."},
		func(ctx context.Context, req *mcp.CallToolRequest, in getCompanyIn) (*mcp.CallToolResult, crm.CompanyDetail, error) {
			if in.Domain != "" {
				c, err := svc.GetCompanyByDomain(ctx, in.Domain)
				return nil, c, err
			}
			c, err := svc.GetCompany(ctx, in.ID)
			return nil, c, err
		})

	addTool(srv, &mcp.Tool{Name: "enrich_contact", Description: "Queue a fresh enrichment for a contact and its company."},
		func(ctx context.Context, req *mcp.CallToolRequest, in enrichContactIn) (*mcp.CallToolResult, enrichOut, error) {
			st, err := svc.EnrichContact(ctx, actorOf(req), in.ID)
			return nil, enrichOut{State: st}, err
		})
}
