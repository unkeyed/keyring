package crm

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"
)

func TestCreateContact(t *testing.T) {
	var lookedUp []string
	s, _ := newService(t, fakeEnricher{
		person: func(e Email, linkedIn string) (PersonFacts, error) {
			lookedUp = append(lookedUp, e.String()+" "+linkedIn)
			return PersonFacts{Found: true, Name: "Grace B. Hopper", Title: "Rear Admiral", Bio: "COBOL",
				Socials: Socials{"github": "https://github.com/grace"}}, nil
		},
		company: func(Domain) (CompanyFacts, error) { return CompanyFacts{Found: true, Name: "Navy"}, nil },
	})
	ctx := context.Background()

	c, err := s.CreateContact(ctx, human, NewContact{Email: " Grace@Navy.mil ", Name: "Grace Hopper", Title: " ",
		LinkedInURL: "https://linkedin.com/in/grace", Stage: "qualified", Tags: []Tag{"Event"}})
	if err != nil {
		t.Fatal(err)
	}
	if c.Email.String() != "grace@navy.mil" || c.Name != "Grace Hopper" || c.Stage != "qualified" || !slices.Equal(c.Tags, []Tag{"event"}) ||
		c.Company == nil || c.Company.Domain.String() != "navy.mil" || len(c.Providers) != 0 {
		t.Fatalf("created %+v", c)
	}
	if !slices.Equal(c.ManualFields, []Field{FieldName, FieldLinkedInURL}) {
		t.Fatalf("manual fields = %v, want only the non-blank ones given", c.ManualFields)
	}
	if st := status(t, s); st.Jobs[JobPending] != 2 {
		t.Fatalf("jobs = %v, want the contact and its company queued", st.Jobs)
	}

	drain(t, s)
	got := contact(t, s, c.ID)
	if got.Name != "Grace Hopper" || got.Title != "Rear Admiral" || got.Socials["github"] != "https://github.com/grace" {
		t.Fatalf("after enrichment: %+v, want the typed name kept and the rest filled", got.Contact)
	}
	if !slices.Equal(lookedUp, []string{"grace@navy.mil https://linkedin.com/in/grace"}) {
		t.Fatalf("person lookups = %q, want the known LinkedIn URL sent along", lookedUp)
	}
	if a := got.Activity[len(got.Activity)-1]; a.Kind != ActivityContactCreated || a.Actor != human {
		t.Fatalf("first activity = %+v, want contact_created by the person", a)
	}

	_, err = s.CreateContact(ctx, agent, NewContact{Email: "grace@navy.mil"})
	var exists *ExistsError
	if !errors.As(err, &exists) || exists.ID != int64(c.ID) || !errors.Is(err, ErrConflict) {
		t.Fatalf("second create: %v, want ErrConflict naming contact %d", err, c.ID)
	}

	signedUp := ingest(t, s, clerk, created(clerk, "user_9", "alan@bletchley.uk")).ContactID
	if _, err := s.db.Exec(ctx, `UPDATE contacts SET email = 'alan.turing@bletchley.uk' WHERE id = $1`, signedUp); err != nil {
		t.Fatal(err)
	}
	_, err = s.CreateContact(ctx, agent, NewContact{Email: "alan@bletchley.uk"})
	if !errors.As(err, &exists) || exists.ID != int64(signedUp) {
		t.Fatalf("create with an identity's email: %v, want ErrConflict naming contact %d", err, signedUp)
	}

	free, err := s.CreateContact(ctx, agent, NewContact{Email: "someone@gmail.com"})
	if err != nil || free.Company != nil || free.Enrichment != JobSkipped || free.Stage != "new" {
		t.Fatalf("free-mail create = %+v, %v; want no company, skipped, first stage", free, err)
	}
	_, err = s.CreateContact(ctx, agent, NewContact{Email: "x@acme.com", Stage: "nope"})
	wantErr(t, err, ErrInvalid)
	_, err = s.CreateContact(ctx, agent, NewContact{Email: "not an email"})
	wantErr(t, err, ErrInvalid)
}

func TestCreateCompany(t *testing.T) {
	s, _ := newService(t, fakeEnricher{
		company: func(Domain) (CompanyFacts, error) {
			return CompanyFacts{Found: true, Name: "Linear Orbit Inc", Tagline: "Plan and build"}, nil
		},
	})
	ctx := context.Background()

	co, err := s.CreateCompany(ctx, agent, NewCompany{Domain: "Linear.app", Name: "Linear"})
	if err != nil {
		t.Fatal(err)
	}
	if co.Domain.String() != "linear.app" || co.Name != "Linear" || !slices.Equal(co.ManualFields, []Field{FieldName}) || co.Enrichment != JobPending {
		t.Fatalf("created %+v", co)
	}
	drain(t, s)
	got, _ := s.GetCompany(ctx, co.ID)
	if got.Name != "Linear" || got.Tagline != "Plan and build" {
		t.Fatalf("after enrichment name %q tagline %q, want the typed name kept and the tagline filled", got.Name, got.Tagline)
	}
	created := activities(t, s, ActivityQuery{Kind: ActivityCompanyCreated})
	if len(created) != 1 || created[0].Actor != agent || created[0].Company == nil || created[0].Company.ID != co.ID {
		t.Fatalf("company_created activity = %+v", created)
	}

	_, err = s.CreateCompany(ctx, agent, NewCompany{Domain: "linear.app"})
	var exists *ExistsError
	if !errors.As(err, &exists) || exists.ID != int64(co.ID) {
		t.Fatalf("second create: %v, want ErrConflict naming company %d", err, co.ID)
	}
	_, err = s.CreateCompany(ctx, agent, NewCompany{Domain: "gmail.com"})
	wantErr(t, err, ErrInvalid)
	_, err = s.CreateCompany(ctx, agent, NewCompany{Domain: "nodot"})
	wantErr(t, err, ErrInvalid)
}

func TestEnrichmentStoresCompanyDetails(t *testing.T) {
	facts := CompanyFacts{
		Found: true, Name: "Stripe", Tagline: "Financial infrastructure", Headquarters: "South San Francisco, California",
		Socials:  Socials{"x": "https://x.com/stripe"},
		Links:    Links{"careers": "https://stripe.com/careers"},
		Segments: []string{"Payments", "Developer Tools"},
		Stock:    "NASDAQ: STRP",
		Logos:    []Logo{{URL: "https://cdn/icon.svg", Kind: LogoIcon, OnDark: true, Aspect: 1, Width: 512}},
		LogoURL:  "https://cdn/icon.svg",
	}
	s, _ := newService(t, fakeEnricher{company: func(Domain) (CompanyFacts, error) { return facts, nil }})
	ctx := context.Background()
	co, err := s.CreateCompany(ctx, human, NewCompany{Domain: "stripe.com"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateCompany(ctx, human, co.ID, CompanyPatch{Headquarters: ptr("Dublin")}); err != nil {
		t.Fatal(err)
	}
	drain(t, s)

	got, _ := s.GetCompany(ctx, co.ID)
	want := got.Company
	want.Tagline, want.Headquarters, want.Stock = facts.Tagline, "Dublin", facts.Stock
	want.Socials, want.Links, want.Segments, want.Logos, want.LogoURL = facts.Socials, facts.Links, facts.Segments, facts.Logos, facts.LogoURL
	if !reflect.DeepEqual(got.Company, want) {
		t.Fatalf("company = %+v\nwant      %+v", got.Company, want)
	}

	enriched := activities(t, s, ActivityQuery{Kind: ActivityEnriched})
	if len(enriched) != 1 {
		t.Fatalf("enriched activities = %d", len(enriched))
	}
	if _, err := s.EnrichCompany(ctx, human, co.ID); err != nil {
		t.Fatal(err)
	}
	drain(t, s)
	if n := len(activities(t, s, ActivityQuery{Kind: ActivityEnriched})); n != 1 {
		t.Fatalf("re-enriching unchanged facts wrote %d enriched activities, want still 1", n)
	}

	reset, err := s.ResetCompanyField(ctx, human, co.ID, FieldHeadquarters)
	if err != nil || slices.Contains(reset.ManualFields, FieldHeadquarters) {
		t.Fatalf("reset headquarters = %v, %v", reset.ManualFields, err)
	}
}
