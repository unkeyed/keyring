package crm

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/unkeyed/keyring/internal/signup"
	"github.com/unkeyed/keyring/internal/testdb"
)

var (
	clerk = Actor{Kind: ActorProvider, ID: "clerk", Label: "Clerk"}
	auth0 = Actor{Kind: ActorProvider, ID: "auth0", Label: "Auth0"}
	agent = Actor{Kind: ActorAPIKey, ID: "key_1", Label: "sales agent"}
	human = Actor{Kind: ActorUser, ID: "1", Label: "Admin"}

	t0 = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
)

func newService(t *testing.T, e Enricher) (*Service, *pgxpool.Pool) {
	t.Helper()
	pool := testdb.New(t)
	return New(pool, e), pool
}

// created is a provider's user.created fact for email.
func created(a Actor, externalID, email string) signup.Signup {
	return signup.Signup{Source: a.ID, Kind: signup.Created, ExternalID: externalID, Email: email, OccurredAt: t0}
}

func ingest(t *testing.T, s *Service, a Actor, su signup.Signup) IngestResult {
	t.Helper()
	r, err := s.Ingest(context.Background(), a, su)
	if err != nil {
		t.Fatalf("Ingest(%s %s %s): %v", su.Source, su.Kind, su.Email, err)
	}
	return r
}

func contact(t *testing.T, s *Service, id ContactID) ContactDetail {
	t.Helper()
	d, err := s.GetContact(context.Background(), id)
	if err != nil {
		t.Fatalf("GetContact(%d): %v", id, err)
	}
	return d
}

func status(t *testing.T, s *Service) Status {
	t.Helper()
	st, err := s.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func activities(t *testing.T, s *Service, q ActivityQuery) []Activity {
	t.Helper()
	a, err := s.ListActivity(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	return a.Items
}

func ptr[T any](v T) *T { return &v }

func wantErr(t *testing.T, err, target error) {
	t.Helper()
	if !errors.Is(err, target) {
		t.Fatalf("err = %v, want %v", err, target)
	}
}

// fakeEnricher answers with fixed functions; a nil function finds nothing.
type fakeEnricher struct {
	person  func(Email, string) (PersonFacts, error)
	company func(Domain) (CompanyFacts, error)
}

func (f fakeEnricher) Person(_ context.Context, e Email, linkedInURL string) (PersonFacts, error) {
	if f.person == nil {
		return PersonFacts{}, nil
	}
	return f.person(e, linkedInURL)
}

func (f fakeEnricher) Company(_ context.Context, d Domain) (CompanyFacts, error) {
	if f.company == nil {
		return CompanyFacts{}, nil
	}
	return f.company(d)
}

// drain runs the worker until no job is due.
func drain(t *testing.T, s *Service) {
	t.Helper()
	for {
		ran, err := s.enrichOnce(context.Background())
		if err != nil {
			t.Fatalf("enrichOnce: %v", err)
		}
		if !ran {
			return
		}
	}
}
