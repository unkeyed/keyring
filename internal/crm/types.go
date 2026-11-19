package crm

import (
	"encoding/json"
	"errors"
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
	return "", errors.New("not implemented")
}

// Stage names a row in the stages table. Validity is enforced by the
// foreign key, not in Go, because the list is user-editable.
type Stage string

// Field names a column a person or agent can set, which then stays theirs
// until reset (see the package doc). Contacts accept FieldName, FieldTitle,
// FieldLocation, FieldBio, FieldLinkedInURL and FieldCompany. Companies accept
// FieldName, FieldDescription, FieldEmployeeRange, FieldIndustry,
// FieldCountry and FieldLinkedInURL. The manual_fields CHECKs in
// 001_init.sql list the same names.
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
)

// Contact is one person. Display fields are plain columns. ManualFields
// lists the ones a person or agent set, which machine writers leave alone.
type Contact struct {
	ID           ContactID   `json:"id"`
	Email        Email       `json:"email"`
	Name         string      `json:"name"`
	AvatarURL    string      `json:"avatar_url"`
	Title        string      `json:"title"`
	Location     string      `json:"location"`
	Bio          string      `json:"bio"`
	LinkedInURL  string      `json:"linkedin_url"`
	Company      *CompanyRef `json:"company"` // nil for free-mail or unknown
	Stage        Stage       `json:"stage"`
	Tags         []Tag       `json:"tags"`
	Providers    []string    `json:"providers"` // distinct identities.provider
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

// ContactDetail is everything a contact page or get_contact needs in one read.
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
	ContactCount  int       `json:"contact_count"`
	Enrichment    JobState  `json:"enrichment"`
	ManualFields  []Field   `json:"manual_fields"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type CompanyDetail struct {
	Company
	Contacts []Contact `json:"contacts"`
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
	ActivitySignedUp        ActivityKind = "signed_up"        // data: {provider, external_id, raw}
	ActivityProviderDeleted ActivityKind = "provider_deleted" // data: {provider, external_id}
	ActivityContactUpdated  ActivityKind = "contact_updated"  // data: {field: {from, to}} or {reset: field}
	ActivityCompanyUpdated  ActivityKind = "company_updated"  // data: {field: {from, to}} or {reset: field}
	ActivityNoteAdded       ActivityKind = "note_added"       // data: {note_id}
	ActivityContactsMerged  ActivityKind = "contacts_merged"  // on keep; data: {drop_id, drop_email}
	ActivityEnriched        ActivityKind = "enriched"         // data: {changed: [field...]}
)

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
}

type ActorKind string

const (
	ActorUser     ActorKind = "user"
	ActorAPIKey   ActorKind = "api_key"
	ActorProvider ActorKind = "provider" // ID is the provider name
	ActorSystem   ActorKind = "system"   // enrichment worker
)

// ContactQuery filters SearchContacts. Zero values mean "no filter".
type ContactQuery struct {
	Text           string // case-insensitive substring of email, name, company name or domain
	Tag            Tag
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
	Cursor string
	Limit  int
}

type ActivityQuery struct {
	Kind      ActivityKind // optional
	ContactID ContactID    // optional
	Since     time.Time    // optional
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

type CompanyPatch struct {
	Name          *string `json:"name,omitempty"`
	Description   *string `json:"description,omitempty"`
	EmployeeRange *string `json:"employee_range,omitempty"`
	Industry      *string `json:"industry,omitempty"`
	Country       *string `json:"country,omitempty"`
	LinkedInURL   *string `json:"linkedin_url,omitempty"`
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
	EnrichmentEnabled bool             `json:"enrichment_enabled"`
	Jobs              map[JobState]int `json:"jobs"`
	Contacts          int              `json:"contacts"`
	Companies         int              `json:"companies"`
}
