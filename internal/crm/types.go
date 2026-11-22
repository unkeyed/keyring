package crm

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type (
	ContactID int64
	CompanyID int64
	NoteID    int64
)

// Tag is trimmed and lowercased by ParseTag. The schema CHECK enforces the same.
type Tag string

func ParseTag(s string) (Tag, error) {
	t := strings.ToLower(strings.TrimSpace(s))
	if t == "" {
		return "", fmt.Errorf("%w: empty tag", ErrInvalid)
	}
	return Tag(t), nil
}

// Stage names a row in the stages table. The list is user-editable, so it
// is checked against the table (checkStage), not a Go list.
type Stage string

// StageCount is a stage and how many contacts are in it.
type StageCount struct {
	Name         Stage `json:"name"`
	ContactCount int   `json:"contact_count"`
}

// Field names a column a person or agent can set, which then stays theirs
// until reset (see the package doc). Contacts accept FieldName, FieldTitle,
// FieldLocation, FieldBio, FieldLinkedInURL and FieldCompany. Companies accept
// FieldName, FieldDescription, FieldEmployeeRange, FieldIndustry,
// FieldCountry, FieldLinkedInURL, FieldTagline and FieldHeadquarters. The
// manual_fields CHECKs in the migrations list the same names.
type Field string

const (
	FieldName          Field = "name"
	FieldTitle         Field = "title"
	FieldLocation      Field = "location"
	FieldBio           Field = "bio"
	FieldLinkedInURL   Field = "linkedin_url"
	FieldCompany       Field = "company"
	FieldDescription   Field = "description"
	FieldEmployeeRange Field = "employee_range"
	FieldIndustry      Field = "industry"
	FieldCountry       Field = "country"
	FieldTagline       Field = "tagline"
	FieldHeadquarters  Field = "headquarters"
)

// Socials maps a profile type to its URL: "x", "github", "youtube",
// "website", and for people any other profile host as its host name.
// LinkedIn is not in it; contacts and companies keep it in LinkedInURL,
// which people can edit.
type Socials map[string]string

// Contact is one person. ManualFields lists the fields a person or agent
// set, which machine writers leave alone.
type Contact struct {
	ID           ContactID   `json:"id"`
	Email        Email       `json:"email"`
	Name         string      `json:"name"`
	AvatarURL    string      `json:"avatar_url"`
	Title        string      `json:"title"`
	Location     string      `json:"location"`
	Bio          string      `json:"bio"`
	LinkedInURL  string      `json:"linkedin_url"`
	Socials      Socials     `json:"socials"`
	Company      *CompanyRef `json:"company"` // nil for free-mail or unknown
	Stage        Stage       `json:"stage"`
	Tags         []Tag       `json:"tags"`
	CompanyTags  []Tag       `json:"company_tags"` // the company's tags, which apply to the contact too
	Providers    []string    `json:"providers"`    // distinct identities.provider
	Enrichment   JobState    `json:"enrichment"`
	ManualFields []Field     `json:"manual_fields"`
	FirstSeenAt  time.Time   `json:"first_seen_at"`
	LastSeenAt   time.Time   `json:"last_seen_at"`
	CreatedAt    time.Time   `json:"created_at"`
	UpdatedAt    time.Time   `json:"updated_at"`
}

type CompanyRef struct {
	ID     CompanyID `json:"id"`
	Domain Domain    `json:"domain"`
	Name   string    `json:"name"` // '' until enriched or edited; UIs show Domain then
}

type ContactDetail struct {
	Contact
	Identities []Identity `json:"identities"`
	Notes      []Note     `json:"notes"`
	Activity   []Activity `json:"activity"` // newest first, capped at 50
}

// Identity is one auth provider's latest view of the person.
type Identity struct {
	Provider      string     `json:"provider"`
	ExternalID    string     `json:"external_id"`
	Email         Email      `json:"email"`
	EmailVerified bool       `json:"email_verified"`
	Name          string     `json:"name"`
	AvatarURL     string     `json:"avatar_url"`
	OccurredAt    time.Time  `json:"occurred_at"`
	DeletedAt     *time.Time `json:"deleted_at,omitempty"`
}

type Company struct {
	ID            CompanyID `json:"id"`
	Domain        Domain    `json:"domain"`
	Name          string    `json:"name"`
	Description   string    `json:"description"`
	LogoURL       string    `json:"logo_url"`
	EmployeeRange string    `json:"employee_range"`
	Industry      string    `json:"industry"`
	Country       string    `json:"country"`
	LinkedInURL   string    `json:"linkedin_url"`
	Tagline       string    `json:"tagline"`
	Headquarters  string    `json:"headquarters"` // "South San Francisco, California"; the country is in Country
	Socials       Socials   `json:"socials"`
	Links         Links     `json:"links"`
	Segments      []string  `json:"segments"` // sub-industries, most relevant first
	Stock         string    `json:"stock"`    // "NASDAQ: XYZ", or ""
	Logos         []Logo    `json:"logos"`    // best first (see Logo); LogoURL is the first one's URL
	Tags          []Tag     `json:"tags"`     // apply to every contact at the company
	ContactCount  int       `json:"contact_count"`
	Enrichment    JobState  `json:"enrichment"`
	ManualFields  []Field   `json:"manual_fields"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// Links are a company's own pages, by kind: careers, pricing, blog, contact.
type Links map[string]string

// Logo is one company image, normalized from context.dev. A company's Logos
// are ordered icons before wordmarks; within each kind an image with its own
// background first, then one that reads on both light and dark backgrounds;
// then sharp images (SVG, or rasters at least 104px wide, twice the largest
// tile) before blurry ones; then the squarest. A UI picks the first logo
// that reads on its theme, and API consumers can take the first one.
type Logo struct {
	URL           string   `json:"url"`
	Kind          LogoKind `json:"kind"`
	OwnBackground bool     `json:"own_background" jsonschema:"the image is opaque and fills its own box"`
	OnLight       bool     `json:"on_light" jsonschema:"the art reads on a light background"`
	OnDark        bool     `json:"on_dark" jsonschema:"the art reads on a dark background"`
	Aspect        float64  `json:"aspect" jsonschema:"width over height; 0 when unknown"`
	Width         int      `json:"width" jsonschema:"pixels; 0 when unknown"`
}

type LogoKind string

const (
	LogoIcon     LogoKind = "icon"     // a mark, roughly square
	LogoWordmark LogoKind = "wordmark" // the full logo, usually wide
)

type CompanyDetail struct {
	Company
	Contacts []Contact  `json:"contacts"`
	Activity []Activity `json:"activity"` // the company's and its contacts', newest first, capped at 50
}

type Note struct {
	ID        NoteID    `json:"id"`
	ContactID ContactID `json:"contact_id"`
	Body      string    `json:"body"`
	Author    Actor     `json:"author"`
	CreatedAt time.Time `json:"created_at"`
}

// ActivityKind is the closed list of timeline entries. The activities.kind
// column has no CHECK; this list is the only one.
type ActivityKind string

const (
	ActivitySignedUp        ActivityKind = "signed_up"        // data: {provider, external_id}
	ActivityProviderDeleted ActivityKind = "provider_deleted" // data: {provider, external_id}
	ActivityContactUpdated  ActivityKind = "contact_updated"  // data: {field: {from, to}} or {reset: field}
	ActivityCompanyUpdated  ActivityKind = "company_updated"  // data: {field: {from, to}} or {reset: field}
	ActivityNoteAdded       ActivityKind = "note_added"       // data: {note_id}
	ActivityNoteDeleted     ActivityKind = "note_deleted"     // data: {note_id}
	ActivityContactsMerged  ActivityKind = "contacts_merged"  // on keep; data: {drop_id, drop_email}
	ActivityContactCreated  ActivityKind = "contact_created"  // by hand, not a sign-up; data: {}
	ActivityCompanyCreated  ActivityKind = "company_created"  // by hand; data: {}
	ActivityEnriched        ActivityKind = "enriched"         // data: {changed: [field...]}
)

var ActivityKinds = []ActivityKind{ActivitySignedUp, ActivityProviderDeleted, ActivityContactUpdated, ActivityCompanyUpdated,
	ActivityNoteAdded, ActivityNoteDeleted, ActivityContactsMerged, ActivityContactCreated, ActivityCompanyCreated, ActivityEnriched}

// Activity is one timeline row. It records what happened; it is never read
// to decide state.
type Activity struct {
	ID         int64           `json:"id"`
	Kind       ActivityKind    `json:"kind"`
	Contact    *ContactRef     `json:"contact,omitempty"`
	Company    *CompanyRef     `json:"company,omitempty"`
	Actor      Actor           `json:"actor"`
	Data       json.RawMessage `json:"data"`
	OccurredAt time.Time       `json:"occurred_at"`
}

type ContactRef struct {
	ID    ContactID `json:"id"`
	Email Email     `json:"email"`
	Name  string    `json:"name"`
}

// Actor is who caused a write. Transports build it from the authenticated
// caller and pass it explicitly; it is stored denormalized (label included)
// so the timeline survives a deleted user or revoked key.
type Actor struct {
	Kind  ActorKind `json:"kind"`
	ID    string    `json:"id"`
	Label string    `json:"label"`
	// ReadOnly is set for an API key created read-only. httpapi refuses it
	// every route but GET. It is never stored with the actor.
	ReadOnly bool `json:"-"`
}

type ActorKind string

const (
	ActorUser     ActorKind = "user"
	ActorAPIKey   ActorKind = "api_key"
	ActorApp      ActorKind = "app"      // an MCP client acting for the person who approved it; ID is the grant
	ActorProvider ActorKind = "provider" // ID is the provider name
	ActorSystem   ActorKind = "system"   // enrichment worker
)

// ContactQuery filters SearchContacts. Zero values mean "no filter".
type ContactQuery struct {
	Text           string // case-insensitive substring of email, name, company name or domain
	Email          string // exact after ParseEmail; matches the contact's email or any identity's
	Tag            Tag    // on the contact or on its company
	Stage          Stage
	Provider       string
	CompanyID      CompanyID
	SignedUpAfter  time.Time
	SignedUpBefore time.Time
	Cursor         string // opaque, from Page.NextCursor
	Limit          int    // default 50, max 200
}

type CompanyQuery struct {
	Text   string
	Domain string // exact after ParseDomain
	Tag    Tag
	Cursor string
	Limit  int
}

// ActivityQuery filters ListActivity. Zero values mean "no filter".
type ActivityQuery struct {
	Kind      ActivityKind
	ContactID ContactID
	CompanyID CompanyID // the company's own entries and its contacts'
	Since     time.Time // at or after
	Before    time.Time
	Cursor    string
	Limit     int
}

// Page is a keyset page ordered newest first.
type Page[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
}

// ContactPatch is a partial update. nil fields are left alone, and every
// non-nil field becomes manual (CompanyDomain as FieldCompany). Tag changes
// are add/remove sets, not a replacement list, so two agents tagging the same
// contact at once both win.
//
// It is also the REST PATCH body and the MCP update_contact "changes"
// argument, so all three transports share one shape.
type ContactPatch struct {
	Name          *string `json:"name,omitempty"`
	Title         *string `json:"title,omitempty"`
	Location      *string `json:"location,omitempty"`
	Bio           *string `json:"bio,omitempty"`
	LinkedInURL   *string `json:"linkedin_url,omitempty"`
	Stage         *Stage  `json:"stage,omitempty" jsonschema:"pipeline stage name"`
	CompanyDomain *Domain `json:"company_domain,omitempty" jsonschema:"move to this company domain; empty string detaches"`
	AddTags       []Tag   `json:"add_tags,omitempty"`
	RemoveTags    []Tag   `json:"remove_tags,omitempty"`
}

// CompanyPatch is ContactPatch for companies: the REST PATCH body and the MCP
// update_company "changes" argument.
type CompanyPatch struct {
	Name          *string `json:"name,omitempty"`
	Description   *string `json:"description,omitempty"`
	EmployeeRange *string `json:"employee_range,omitempty"`
	Industry      *string `json:"industry,omitempty"`
	Country       *string `json:"country,omitempty"`
	LinkedInURL   *string `json:"linkedin_url,omitempty"`
	Tagline       *string `json:"tagline,omitempty"`
	Headquarters  *string `json:"headquarters,omitempty"`
	AddTags       []Tag   `json:"add_tags,omitempty" jsonschema:"tags such as customer; they apply to every contact at the company"`
	RemoveTags    []Tag   `json:"remove_tags,omitempty"`
}

// NewContact is a contact added by hand rather than by a sign-up. Only Email
// is required. Every other non-empty field becomes manual, as an edit would.
// It is also the REST POST /v1/contacts body and the MCP create_contact
// input.
type NewContact struct {
	Email       string `json:"email" jsonschema:"work or personal email; one contact per email"`
	Name        string `json:"name,omitempty"`
	Title       string `json:"title,omitempty"`
	Location    string `json:"location,omitempty"`
	LinkedInURL string `json:"linkedin_url,omitempty"`
	Stage       Stage  `json:"stage,omitempty" jsonschema:"pipeline stage name; default the first stage"`
	Tags        []Tag  `json:"tags,omitempty"`
}

// NewCompany is a company added by hand. Name, when given, becomes manual.
type NewCompany struct {
	Domain string `json:"domain" jsonschema:"the company's web or email domain, e.g. stripe.com"`
	Name   string `json:"name,omitempty"`
}

// Record is the kind of row an error names, so a transport can say where to
// look next.
type Record string

const (
	RecordContact Record = "contact"
	RecordCompany Record = "company"
	RecordNote    Record = "note"
)

// ExistsError is ErrConflict from a create whose email or domain is already
// taken. ID names the existing contact or company, so a caller can go there.
type ExistsError struct {
	Record Record
	ID     int64
	Key    string // the taken email or domain
}

func (e *ExistsError) Error() string {
	return fmt.Sprintf("%s %d already has %s", e.Record, e.ID, e.Key)
}
func (e *ExistsError) Unwrap() error { return ErrConflict }

// NotFoundError is ErrNotFound naming the missing record.
type NotFoundError struct {
	Record Record
	Msg    string // "no contact 999"
}

func (e *NotFoundError) Error() string { return e.Msg }
func (e *NotFoundError) Unwrap() error { return ErrNotFound }

func notFound(r Record, format string, args ...any) error {
	return &NotFoundError{Record: r, Msg: fmt.Sprintf(format, args...)}
}

// BulkUpdate is the REST POST /v1/contacts/bulk body and the MCP
// bulk_update_contacts input: one change applied to up to 200 contacts.
type BulkUpdate struct {
	IDs        []ContactID `json:"ids" jsonschema:"up to 200 contact ids"`
	Stage      *Stage      `json:"stage,omitempty" jsonschema:"pipeline stage name from list_stages"`
	AddTags    []Tag       `json:"add_tags,omitempty"`
	RemoveTags []Tag       `json:"remove_tags,omitempty"`
}

// BulkResult is one contact's outcome in a bulk update. Error is set when OK
// is false: the contact does not exist, or the change was invalid for it.
type BulkResult struct {
	ID    ContactID `json:"id"`
	OK    bool      `json:"ok"`
	Error string    `json:"error,omitempty"`
}

// IngestResult tells the caller what an Ingest did. Duplicate is true when the
// sign-up was already recorded; the call still succeeded.
type IngestResult struct {
	ContactID ContactID `json:"contact_id"`
	Created   bool      `json:"created"`
	Duplicate bool      `json:"duplicate"`
}

// ProviderStatus is one row on the Settings page: every registered adapter,
// connected or not.
type ProviderStatus struct {
	Name           string     `json:"name"`
	Title          string     `json:"title"`
	WebhookPath    string     `json:"webhook_path"` // "/webhooks/clerk"; the UI prefixes its origin
	IssueSecret    bool       `json:"issue_secret"`
	Setup          string     `json:"setup"`
	Connected      bool       `json:"connected"`
	LastDeliveryAt *time.Time `json:"last_delivery_at,omitempty"`
	LastError      string     `json:"last_error,omitempty"`
	LastErrorAt    *time.Time `json:"last_error_at,omitempty"`
}

// Status is the instance health summary for Settings and agents.
type Status struct {
	EnrichmentEnabled bool            `json:"enrichment_enabled"`
	EnrichmentHalt    *EnrichmentHalt `json:"enrichment_halt,omitempty"` // nil unless paused now
	// EnrichmentDailyLimit is ENRICHMENT_DAILY_LIMIT, 0 for none. Once
	// EnrichmentCallsToday reaches it, jobs wait for the next UTC day.
	EnrichmentDailyLimit int              `json:"enrichment_daily_limit"`
	EnrichmentCallsToday int              `json:"enrichment_calls_today"`
	Jobs                 map[JobState]int `json:"jobs"`
	Contacts             int              `json:"contacts"`
	Companies            int              `json:"companies"`
}

// EnrichmentHalt is why enrichment is paused (context.dev rejected the key or
// the account) and until when.
type EnrichmentHalt struct {
	Reason string    `json:"reason"`
	Until  time.Time `json:"until"`
}
