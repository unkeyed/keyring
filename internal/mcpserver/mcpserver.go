// Package mcpserver exposes crm.Service as MCP tools over stateless
// Streamable HTTP at /mcp, authenticated with the same API keys as REST.
// Each tool is an input struct and a few lines that call one Service method;
// the output is the same domain type REST returns.
package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/google/jsonschema-go/jsonschema"

	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/uncrm/uncrm/internal/auth"
	"github.com/uncrm/uncrm/internal/crm"
)

const actorKey = "actor"

const instructions = `uncrm is this team's CRM. Every person who signed up through any auth provider is a contact, one per email, linked to a company by email domain. Start with search_contacts or get_contact (by email). Edits you make are pinned: sign-ups and enrichment never overwrite them until reset_contact_field. Use list_stages and list_tags to learn the names this team uses before filtering or setting them.`

// Handler returns the /mcp handler. Stateless, so any replica serves any
// request without sticky sessions.
func Handler(svc *crm.Service, keys auth.KeyStore, version string) http.Handler {
	srv := mcp.NewServer(&mcp.Implementation{Name: "uncrm", Version: version}, &mcp.ServerOptions{Instructions: instructions})
	addTools(srv, svc, keys)
	h := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv },
		&mcp.StreamableHTTPOptions{Stateless: true})

	verify := func(ctx context.Context, token string, _ *http.Request) (*mcpauth.TokenInfo, error) {
		a, err := keys.Verify(ctx, token)
		if errors.Is(err, auth.ErrInvalidKey) {
			return nil, fmt.Errorf("%w: %v", mcpauth.ErrInvalidToken, err)
		}
		if err != nil {
			return nil, err // the key store is down: a 500, so the client does not drop a good key
		}
		return &mcpauth.TokenInfo{UserID: a.ID, Extra: map[string]any{actorKey: a}}, nil
	}
	return mcpauth.RequireBearerToken(verify, &mcpauth.RequireBearerTokenOptions{AllowMissingExpiration: true})(h)
}

// actorOf reads the actor the bearer middleware stored for this request.
func actorOf(req *mcp.CallToolRequest) crm.Actor {
	if req.Extra == nil || req.Extra.TokenInfo == nil {
		return crm.Actor{}
	}
	a, _ := req.Extra.TokenInfo.Extra[actorKey].(crm.Actor)
	return a
}

// schemaOpts tells schema inference how types with custom JSON travel.
// Parse-only domain types are strings: a struct with only unexported fields
// would otherwise be inferred as an empty object. Activity.Data is a
// json.RawMessage object: inference sees []byte and expects an array. Either
// way every tool output holding one would fail validation.
var schemaOpts = &jsonschema.ForOptions{TypeSchemas: map[reflect.Type]*jsonschema.Schema{
	reflect.TypeFor[crm.Email]():       {Type: "string"},
	reflect.TypeFor[crm.Domain]():      {Type: "string"},
	reflect.TypeFor[json.RawMessage](): {Type: "object"},
}}

// addTool registers a tool whose result is out as structured content, and as
// text: summary(out) first, so an agent can skip the JSON, then the JSON
// for clients that ignore structured content. Every tool registers here.
func addTool[In, Out any](srv *mcp.Server, t *mcp.Tool, run func(context.Context, crm.Actor, In) (Out, error), summary func(Out) string) {
	in, err := jsonschema.For[In](schemaOpts)
	if err != nil {
		panic(err)
	}
	out, err := jsonschema.For[Out](schemaOpts)
	if err != nil {
		panic(err)
	}
	t.InputSchema, t.OutputSchema = in, out
	mcp.AddTool(srv, t, func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
		out, err := run(ctx, actorOf(req), in)
		if err != nil {
			return nil, out, err
		}
		j, err := json.Marshal(out)
		if err != nil {
			return nil, out, err
		}
		text := summary(out) + "\n\n" + string(j)
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}, out, nil
	})
}

type searchContactsIn struct {
	Query         string        `json:"query,omitempty" jsonschema:"case-insensitive substring of email, name, company name or company domain"`
	Tag           crm.Tag       `json:"tag,omitempty" jsonschema:"only contacts with this tag"`
	Stage         crm.Stage     `json:"stage,omitempty" jsonschema:"only contacts in this pipeline stage"`
	Provider      string        `json:"provider,omitempty" jsonschema:"only contacts who signed up through this auth provider, e.g. clerk, or api"`
	CompanyID     crm.CompanyID `json:"company_id,omitempty" jsonschema:"only contacts at this company"`
	SignedUpAfter time.Time     `json:"signed_up_after,omitzero" jsonschema:"only contacts first seen at or after this RFC 3339 time"`
	Cursor        string        `json:"cursor,omitempty" jsonschema:"next_cursor from the previous call, for the next page"`
	Limit         int           `json:"limit,omitempty" jsonschema:"page size, default 50, max 200"`
}

type getContactIn struct {
	ID    crm.ContactID `json:"id,omitempty" jsonschema:"contact id; give this or email"`
	Email string        `json:"email,omitempty" jsonschema:"any email the person signed up with; give this or id"`
}

type updateContactIn struct {
	ID      crm.ContactID    `json:"id"`
	Changes crm.ContactPatch `json:"changes" jsonschema:"only the fields to change; each one becomes pinned"`
}

type resetContactFieldIn struct {
	ID    crm.ContactID `json:"id"`
	Field crm.Field     `json:"field" jsonschema:"name, title, location, bio, linkedin_url or company"`
}

type setStageIn struct {
	ID    crm.ContactID `json:"id"`
	Stage crm.Stage     `json:"stage" jsonschema:"a stage name from list_stages"`
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
	Since time.Time `json:"since,omitzero" jsonschema:"only sign-ups at or after this RFC 3339 time"`
	Limit int       `json:"limit,omitempty" jsonschema:"how many, default 50, max 200"`
}

type getCompanyIn struct {
	ID     crm.CompanyID `json:"id,omitempty" jsonschema:"company id; give this or domain"`
	Domain string        `json:"domain,omitempty" jsonschema:"email domain, e.g. stripe.com; give this or id"`
}

type searchCompaniesIn struct {
	Query  string `json:"query,omitempty" jsonschema:"case-insensitive substring of the company name or domain"`
	Cursor string `json:"cursor,omitempty" jsonschema:"next_cursor from the previous call, for the next page"`
	Limit  int    `json:"limit,omitempty" jsonschema:"page size, default 50, max 200"`
}

type enrichContactIn struct {
	ID crm.ContactID `json:"id"`
}

type noInput struct{}

type enrichOut struct {
	State crm.JobState `json:"state"`
}

type signupsOut struct {
	Signups []crm.Activity `json:"signups"`
}

type stagesOut struct {
	Stages []crm.Stage `json:"stages"`
}

type tagsOut struct {
	Tags map[crm.Tag]int `json:"tags" jsonschema:"tag name to number of contacts"`
}

type statusOut struct {
	crm.Status
	KeyStore string `json:"key_store" jsonschema:"builtin or unkey"`
}

func addTools(srv *mcp.Server, svc *crm.Service, keys auth.KeyStore) {
	addTool(srv, &mcp.Tool{Name: "search_contacts", Description: "Find contacts. Filters combine with AND; with no filters it lists everyone, newest sign-up first. Returns one page of contacts and, when more exist, a next_cursor to pass back as cursor. Use get_contact for notes and history."},
		func(ctx context.Context, _ crm.Actor, in searchContactsIn) (crm.Page[crm.Contact], error) {
			return svc.SearchContacts(ctx, crm.ContactQuery{Text: in.Query, Tag: in.Tag, Stage: in.Stage, Provider: in.Provider,
				CompanyID: in.CompanyID, SignedUpAfter: in.SignedUpAfter, Cursor: in.Cursor, Limit: in.Limit})
		}, contactPageSummary)

	addTool(srv, &mcp.Tool{Name: "get_contact", Description: "Read one contact in full: fields, company, stage, tags, every auth provider identity, notes and the newest 50 timeline entries. Look up by id, or by any email the person used."},
		func(ctx context.Context, _ crm.Actor, in getContactIn) (crm.ContactDetail, error) {
			switch {
			case in.Email != "":
				return svc.FindContactByEmail(ctx, in.Email)
			case in.ID != 0:
				return svc.GetContact(ctx, in.ID)
			}
			return crm.ContactDetail{}, fmt.Errorf("%w: give id or email", crm.ErrInvalid)
		}, func(c crm.ContactDetail) string {
			return fmt.Sprintf("%s. %d identities, %d notes, %d timeline entries.", contactLine(c.Contact), len(c.Identities), len(c.Notes), len(c.Activity))
		})

	addTool(srv, &mcp.Tool{Name: "update_contact", Description: "Change a contact. Send only the fields to change: name, title, location, bio, linkedin_url, stage, company_domain (\"\" detaches), add_tags, remove_tags. Changed fields are pinned so later sign-ups and enrichment leave them alone. Returns the updated contact."},
		func(ctx context.Context, a crm.Actor, in updateContactIn) (crm.Contact, error) {
			return svc.UpdateContact(ctx, a, in.ID, in.Changes)
		}, func(c crm.Contact) string { return "Updated " + contactLine(c) + "." })

	addTool(srv, &mcp.Tool{Name: "set_stage", Description: "Move a contact to a pipeline stage, such as from new to qualified. Call list_stages for the valid names. Returns the updated contact."},
		func(ctx context.Context, a crm.Actor, in setStageIn) (crm.Contact, error) {
			return svc.UpdateContact(ctx, a, in.ID, crm.ContactPatch{Stage: &in.Stage})
		}, func(c crm.Contact) string { return "Moved " + contactLine(c) + "." })

	addTool(srv, &mcp.Tool{Name: "reset_contact_field", Description: "Unpin a contact field that a person or agent set, so the next value from the auth provider or enrichment shows. The current value stays until then. Returns the contact."},
		func(ctx context.Context, a crm.Actor, in resetContactFieldIn) (crm.Contact, error) {
			return svc.ResetContactField(ctx, a, in.ID, in.Field)
		}, func(c crm.Contact) string {
			return fmt.Sprintf("Unpinned. %s; pinned fields now %v.", contactLine(c), c.ManualFields)
		})

	addTool(srv, &mcp.Tool{Name: "merge_contacts", Description: "Merge two contacts that are the same person, such as a work and a personal email. Moves drop's identities, notes, tags and history to keep, fills keep's empty fields from drop, then deletes drop. Cannot be undone; confirm with get_contact first. Returns keep."},
		func(ctx context.Context, a crm.Actor, in mergeContactsIn) (crm.Contact, error) {
			return svc.MergeContacts(ctx, a, in.Keep, in.Drop)
		}, func(c crm.Contact) string { return "Merged into " + contactLine(c) + "." })

	addTool(srv, &mcp.Tool{Name: "add_note", Description: "Add a Markdown note to a contact's timeline, attributed to this API key. Use it to record calls, context or follow-ups. Returns the note."},
		func(ctx context.Context, a crm.Actor, in addNoteIn) (crm.Note, error) {
			return svc.AddNote(ctx, a, in.ContactID, in.Body)
		}, func(n crm.Note) string { return fmt.Sprintf("Added note %d to contact %d.", n.ID, n.ContactID) })

	addTool(srv, &mcp.Tool{Name: "list_recent_signups", Description: "List the newest sign-ups across every auth provider, newest first. Each entry names the contact, the provider and when. Use it to answer \"who signed up this week\"."},
		func(ctx context.Context, _ crm.Actor, in recentSignupsIn) (signupsOut, error) {
			a, err := svc.ListActivity(ctx, crm.ActivityQuery{Kind: crm.ActivitySignedUp, Since: in.Since, Limit: in.Limit})
			return signupsOut{Signups: a}, err
		}, func(o signupsOut) string {
			var b strings.Builder
			fmt.Fprintf(&b, "%d sign-ups.", len(o.Signups))
			for _, a := range o.Signups {
				if a.Contact != nil {
					fmt.Fprintf(&b, "\n- %s %s (contact %d) via %s", a.OccurredAt.Format(time.DateOnly), a.Contact.Email, a.Contact.ID, a.Actor.Label)
				}
			}
			return b.String()
		})

	addTool(srv, &mcp.Tool{Name: "get_company", Description: "Read one company and all its contacts, by id or by email domain."},
		func(ctx context.Context, _ crm.Actor, in getCompanyIn) (crm.CompanyDetail, error) {
			switch {
			case in.Domain != "":
				return svc.GetCompanyByDomain(ctx, in.Domain)
			case in.ID != 0:
				return svc.GetCompany(ctx, in.ID)
			}
			return crm.CompanyDetail{}, fmt.Errorf("%w: give id or domain", crm.ErrInvalid)
		}, func(c crm.CompanyDetail) string {
			return fmt.Sprintf("%s with %d contacts.", companyLine(c.Company), len(c.Contacts))
		})

	addTool(srv, &mcp.Tool{Name: "search_companies", Description: "Find companies by name or domain, newest first. Companies come from business email domains; free-mail addresses have none. Returns one page and, when more exist, a next_cursor."},
		func(ctx context.Context, _ crm.Actor, in searchCompaniesIn) (crm.Page[crm.Company], error) {
			return svc.SearchCompanies(ctx, crm.CompanyQuery{Text: in.Query, Cursor: in.Cursor, Limit: in.Limit})
		}, func(p crm.Page[crm.Company]) string {
			lines := make([]string, len(p.Items))
			for i, c := range p.Items {
				lines[i] = "- " + companyLine(c)
			}
			return pageSummary(len(p.Items), "companies", p.NextCursor, lines)
		})

	addTool(srv, &mcp.Tool{Name: "enrich_contact", Description: "Queue a fresh context.dev lookup for a contact and its company (title, location, bio, LinkedIn, company size and industry). It runs in the background; get_contact shows the result. Pinned fields are never overwritten. Returns the job state: pending, or skipped for free-mail addresses."},
		func(ctx context.Context, a crm.Actor, in enrichContactIn) (enrichOut, error) {
			st, err := svc.EnrichContact(ctx, a, in.ID)
			return enrichOut{State: st}, err
		}, func(o enrichOut) string { return "Enrichment " + string(o.State) + "." })

	addTool(srv, &mcp.Tool{Name: "list_stages", Description: "List the pipeline stages this team uses, in order. New contacts start in the first."},
		func(ctx context.Context, _ crm.Actor, _ noInput) (stagesOut, error) {
			s, err := svc.ListStages(ctx)
			return stagesOut{Stages: s}, err
		}, func(o stagesOut) string { return fmt.Sprintf("Stages: %v.", o.Stages) })

	addTool(srv, &mcp.Tool{Name: "list_tags", Description: "List every tag in use, with how many contacts carry it."},
		func(ctx context.Context, _ crm.Actor, _ noInput) (tagsOut, error) {
			t, err := svc.ListTags(ctx)
			return tagsOut{Tags: t}, err
		}, func(o tagsOut) string { return fmt.Sprintf("%d tags.", len(o.Tags)) })

	addTool(srv, &mcp.Tool{Name: "get_status", Description: "Summarize this CRM: how many contacts and companies, whether enrichment is on or paused and why, enrichment jobs per state, and where API keys live."},
		func(ctx context.Context, _ crm.Actor, _ noInput) (statusOut, error) {
			st, err := svc.Status(ctx)
			return statusOut{st, keys.Mode()}, err
		}, func(o statusOut) string {
			enrich := "off (no CONTEXT_DEV_API_KEY)"
			switch {
			case o.EnrichmentHalt != nil:
				enrich = "paused until " + o.EnrichmentHalt.Until.Format(time.RFC3339) + ": " + o.EnrichmentHalt.Reason
			case o.EnrichmentEnabled:
				enrich = "on"
			}
			return fmt.Sprintf("%d contacts, %d companies. Enrichment %s.", o.Contacts, o.Companies, enrich)
		})
}

func contactPageSummary(p crm.Page[crm.Contact]) string {
	lines := make([]string, len(p.Items))
	for i, c := range p.Items {
		lines[i] = "- " + contactLine(c)
	}
	return pageSummary(len(p.Items), "contacts", p.NextCursor, lines)
}

func pageSummary(n int, noun, next string, lines []string) string {
	s := fmt.Sprintf("%d %s", n, noun)
	if next != "" {
		s += fmt.Sprintf(", more after cursor %q", next)
	}
	return s + "." + strings.Join(append([]string{""}, lines...), "\n")
}

// contactLine is "#42 Ada Lovelace <ada@stripe.com>, stripe.com, stage new, tags [vip]".
func contactLine(c crm.Contact) string {
	s := fmt.Sprintf("#%d %s <%s>", c.ID, c.Name, c.Email)
	if c.Company != nil {
		s += ", " + c.Company.Domain.String()
	}
	s += ", stage " + string(c.Stage)
	if len(c.Tags) > 0 {
		s += fmt.Sprintf(", tags %v", c.Tags)
	}
	return s
}

func companyLine(c crm.Company) string {
	name := c.Name
	if name == "" {
		name = c.Domain.String()
	}
	return fmt.Sprintf("#%d %s (%s)", c.ID, name, c.Domain)
}
