// Package signup turns auth-provider webhooks into normalized sign-ups.
//
// Each provider is one file in this package that calls register from init.
// Adding a provider means adding one file: the webhook route, the Settings
// page entry and the dispatch all read the registry. Nothing else lists
// providers.
//
// This package does no I/O. It verifies bytes and parses bytes. Storing the
// result is crm.Service's job.
package signup

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"time"
)

// Kind is what happened to the user at the provider.
type Kind string

const (
	Created Kind = "created"
	Updated Kind = "updated"
	Deleted Kind = "deleted"
)

// Signup is one provider fact about one user, in uncrm's vocabulary.
//
// Email is passed through as the provider sent it; crm.Ingest normalizes it,
// so no adapter can forget to. Email may be empty only for Deleted.
// Source is overwritten with the provider's registered Name by the caller,
// so an adapter cannot claim to be another provider.
type Signup struct {
	Source        string
	Kind          Kind
	ExternalID    string
	Email         string
	EmailVerified bool
	Name          string
	AvatarURL     string
	OccurredAt    time.Time
	Raw           json.RawMessage
}

// Delivery is one inbound webhook request: exactly the bytes and headers
// the provider signed.
type Delivery struct {
	Header     http.Header
	Body       []byte
	ReceivedAt time.Time
}

// ErrIgnored means the delivery verified but carries nothing uncrm stores
// (an organization event, a phone-only user, an anonymous Supabase user).
// The webhook still answers 200 so the provider stops retrying.
var ErrIgnored = errors.New("signup: event ignored")

// ErrBadSignature means Verify rejected the delivery.
var ErrBadSignature = errors.New("signup: bad signature")

// Provider is one registered adapter. It is data plus two functions, so the
// whole adapter is readable top to bottom in its own file.
type Provider struct {
	// Name is the URL segment (/webhooks/{Name}) and the identities.provider
	// value. Lowercase, stable forever once shipped.
	Name  string
	Title string

	// IssueSecret is true when uncrm generates the secret and the user pastes
	// it into the provider (Auth0, Supabase, Better Auth). False when the
	// provider issues it and the user pastes it into uncrm (Clerk, WorkOS, Stytch).
	IssueSecret bool

	// Setup is Markdown for the Settings page. "{{webhook_url}}" and
	// "{{secret}}" are substituted by the UI.
	Setup string

	// Verify authenticates the delivery with the stored secret. It must not
	// parse the body beyond what the signature scheme needs.
	Verify func(secret string, d Delivery) error

	// Parse maps a verified delivery to a Signup, or returns ErrIgnored.
	// It is only ever called after Verify returned nil.
	Parse func(d Delivery) (Signup, error)
}

var registry = map[string]Provider{}

// register is called from each provider file's init. A duplicate or
// incomplete registration panics at startup, before the server listens.
func register(p Provider) {
	if p.Name == "" || p.Verify == nil || p.Parse == nil {
		panic("signup: incomplete provider " + p.Name)
	}
	if _, dup := registry[p.Name]; dup {
		panic("signup: duplicate provider " + p.Name)
	}
	registry[p.Name] = p
}

func Lookup(name string) (Provider, bool) {
	p, ok := registry[name]
	return p, ok
}

// All returns every registered provider sorted by Name.
func All() []Provider {
	out := make([]Provider, 0, len(registry))
	for _, p := range registry {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
