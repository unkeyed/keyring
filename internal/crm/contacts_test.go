package crm

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"testing"
	"time"

	"github.com/uncrm/uncrm/internal/signup"
)

func TestManualEditSurvivesIngestAndEnrichment(t *testing.T) {
	s, _ := newService(t, fakeEnricher{
		person: func(Email, string) (PersonFacts, error) {
			return PersonFacts{Found: true, Name: "Ada Lovelace", Title: "Countess", Raw: json.RawMessage(`{"match":{}}`)}, nil
		},
	})
	ctx := context.Background()
	su := created(clerk, "user_1", "ada@stripe.com")
	su.Name = "Ada King"
	id := ingest(t, s, clerk, su).ContactID

	if _, err := s.UpdateContact(ctx, human, id, ContactPatch{Name: ptr("Ada")}); err != nil {
		t.Fatal(err)
	}
	ingest(t, s, clerk, signup.Signup{Source: "clerk", Kind: signup.Updated, ExternalID: "user_1",
		Email: "ada@stripe.com", Name: "Ada K.", OccurredAt: t0.Add(time.Hour)})
	drain(t, s)

	c := contact(t, s, id)
	if c.Name != "Ada" || c.Title != "Countess" || !slices.Equal(c.ManualFields, []Field{FieldName}) {
		t.Fatalf("after ingest and enrichment: name %q title %q manual %v; want the edit kept and the title filled",
			c.Name, c.Title, c.ManualFields)
	}
	if c.Enrichment != JobDone {
		t.Fatalf("enrichment = %q, want done", c.Enrichment)
	}

	reset, err := s.ResetContactField(ctx, human, id, FieldName)
	if err != nil {
		t.Fatal(err)
	}
	if reset.Name != "Ada" || len(reset.ManualFields) != 0 {
		t.Fatalf("right after reset: name %q manual %v; want the value kept until the next observation", reset.Name, reset.ManualFields)
	}
	if state, err := s.EnrichContact(ctx, human, id); err != nil || state != JobPending {
		t.Fatalf("EnrichContact = %q, %v", state, err)
	}
	drain(t, s)
	if c := contact(t, s, id); c.Name != "Ada Lovelace" {
		t.Fatalf("after reset and re-enrich: name %q, want machine data back", c.Name)
	}

	kinds := map[ActivityKind]int{}
	for _, a := range activities(t, s, ActivityQuery{ContactID: id}) {
		kinds[a.Kind]++
	}
	if kinds[ActivityContactUpdated] != 2 || kinds[ActivityEnriched] != 2 || kinds[ActivitySignedUp] != 1 {
		t.Fatalf("timeline kinds = %v", kinds)
	}

	_, err = s.ResetContactField(ctx, human, id, FieldIndustry)
	wantErr(t, err, ErrInvalid)
}

func TestCompanyEditSurvivesEnrichment(t *testing.T) {
	s, _ := newService(t, fakeEnricher{
		company: func(d Domain) (CompanyFacts, error) {
			return CompanyFacts{Found: true, Name: "Stripe", Industry: "Finance", LogoURL: "https://logo"}, nil
		},
	})
	ctx := context.Background()
	ingest(t, s, clerk, created(clerk, "user_1", "ada@stripe.com"))
	co, err := s.GetCompanyByDomain(ctx, "STRIPE.com")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateCompany(ctx, human, co.ID, CompanyPatch{Industry: ptr("Payments")}); err != nil {
		t.Fatal(err)
	}
	drain(t, s)

	got, err := s.GetCompany(ctx, co.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Stripe" || got.Industry != "Payments" || got.LogoURL != "https://logo" || got.ContactCount != 1 || len(got.Contacts) != 1 {
		t.Fatalf("company = %+v", got)
	}
	reset, err := s.ResetCompanyField(ctx, human, co.ID, FieldIndustry)
	if err != nil || len(reset.ManualFields) != 0 {
		t.Fatalf("ResetCompanyField = %+v, %v", reset, err)
	}
	if _, err := s.EnrichCompany(ctx, human, co.ID); err != nil {
		t.Fatal(err)
	}
	drain(t, s)
	if got, _ := s.GetCompany(ctx, co.ID); got.Industry != "Finance" {
		t.Fatalf("industry after reset and re-enrich = %q", got.Industry)
	}

	_, err = s.GetCompanyByDomain(ctx, "example.org")
	wantErr(t, err, ErrNotFound)
	page, err := s.SearchCompanies(ctx, CompanyQuery{Text: "strip"})
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != co.ID {
		t.Fatalf("SearchCompanies = %+v, %v", page, err)
	}
}

func TestUpdateContactWritesOneDiff(t *testing.T) {
	s, _ := newService(t, nil)
	ctx := context.Background()
	id := ingest(t, s, clerk, created(clerk, "user_1", "ada@stripe.com")).ContactID

	c, err := s.UpdateContact(ctx, agent, id, ContactPatch{
		Title: ptr("CTO"), Stage: ptr(Stage("qualified")), CompanyDomain: ptr(mustDomain(t, "analytical.engine")),
		AddTags: []Tag{" VIP ", "beta"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if c.Title != "CTO" || c.Stage != "qualified" || c.Company.Domain.String() != "analytical.engine" ||
		!slices.Equal(c.Tags, []Tag{"beta", "vip"}) {
		t.Fatalf("updated contact = %+v", c)
	}
	if !slices.Contains(c.ManualFields, FieldTitle) || !slices.Contains(c.ManualFields, FieldCompany) {
		t.Fatalf("manual fields = %v, want title and company", c.ManualFields)
	}

	updates := activities(t, s, ActivityQuery{Kind: ActivityContactUpdated})
	if len(updates) != 1 || updates[0].Actor != agent {
		t.Fatalf("contact_updated activities = %+v, want one by the agent", updates)
	}
	var diff map[string]json.RawMessage
	if err := json.Unmarshal(updates[0].Data, &diff); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"title", "stage", "company", "tags"} {
		if _, ok := diff[k]; !ok {
			t.Fatalf("diff %s lacks %q", updates[0].Data, k)
		}
	}

	if _, err := s.UpdateContact(ctx, agent, id, ContactPatch{Title: ptr("CTO"), AddTags: []Tag{"vip"}}); err != nil {
		t.Fatal(err)
	}
	if n := len(activities(t, s, ActivityQuery{Kind: ActivityContactUpdated})); n != 1 {
		t.Fatalf("a patch that changes nothing wrote an activity (%d total)", n)
	}

	_, err = s.UpdateContact(ctx, agent, id, ContactPatch{Stage: ptr(Stage("imaginary"))})
	wantErr(t, err, ErrInvalid)
	_, err = s.UpdateContact(ctx, agent, 999999, ContactPatch{Title: ptr("x")})
	wantErr(t, err, ErrNotFound)

	detached, err := s.UpdateContact(ctx, agent, id, ContactPatch{CompanyDomain: &Domain{}, RemoveTags: []Tag{"beta"}})
	if err != nil || detached.Company != nil || !slices.Equal(detached.Tags, []Tag{"vip"}) {
		t.Fatalf("detach = %+v, %v", detached, err)
	}
	tags, err := s.ListTags(ctx)
	if err != nil || len(tags) != 1 || tags["vip"] != 1 {
		t.Fatalf("ListTags = %v, %v", tags, err)
	}
}

func TestMergeContactsFoldsDropIntoKeep(t *testing.T) {
	s, _ := newService(t, nil)
	ctx := context.Background()
	work := created(clerk, "user_1", "ada@stripe.com")
	work.Name = "Ada Lovelace"
	keep := ingest(t, s, clerk, work).ContactID
	personal := created(auth0, "auth0|1", "ada@gmail.com")
	personal.AvatarURL = "https://img/ada.png"
	personal.OccurredAt = t0.Add(-24 * time.Hour)
	drop := ingest(t, s, auth0, personal).ContactID

	if _, err := s.UpdateContact(ctx, human, drop, ContactPatch{Title: ptr("Countess"), AddTags: []Tag{"vip"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateContact(ctx, human, keep, ContactPatch{AddTags: []Tag{"customer", "vip"}}); err != nil {
		t.Fatal(err)
	}
	note, err := s.AddNote(ctx, human, drop, "met at the analytical engine meetup")
	if err != nil {
		t.Fatal(err)
	}

	merged, err := s.MergeContacts(ctx, human, keep, drop)
	if err != nil {
		t.Fatal(err)
	}
	if merged.Name != "Ada Lovelace" || merged.Title != "Countess" || merged.AvatarURL != "https://img/ada.png" ||
		merged.Company == nil || !merged.FirstSeenAt.Equal(t0.Add(-24*time.Hour)) {
		t.Fatalf("merged = %+v", merged)
	}
	if !slices.Equal(merged.Providers, []string{"auth0", "clerk"}) || !slices.Equal(merged.Tags, []Tag{"customer", "vip"}) ||
		!slices.Contains(merged.ManualFields, FieldTitle) {
		t.Fatalf("merged providers %v tags %v manual %v", merged.Providers, merged.Tags, merged.ManualFields)
	}

	_, err = s.GetContact(ctx, drop)
	wantErr(t, err, ErrNotFound)
	d := contact(t, s, keep)
	if len(d.Notes) != 1 || d.Notes[0].ID != note.ID || d.Notes[0].Author != human {
		t.Fatalf("notes = %+v, want drop's note", d.Notes)
	}
	kinds := map[ActivityKind]int{}
	for _, a := range d.Activity {
		kinds[a.Kind]++
	}
	if kinds[ActivitySignedUp] != 2 || kinds[ActivityNoteAdded] != 1 || kinds[ActivityContactsMerged] != 1 {
		t.Fatalf("keep's timeline kinds = %v, want both sign-ups, the note and the merge", kinds)
	}

	found, err := s.FindContactByEmail(ctx, "ADA@gmail.com")
	if err != nil || found.ID != keep {
		t.Fatalf("FindContactByEmail(dropped email) = %d, %v; want %d", found.ID, err, keep)
	}
	again := ingest(t, s, agent, signup.Signup{Kind: signup.Created, Email: "ada@gmail.com"})
	if again.ContactID != keep || again.Created {
		t.Fatalf("a later sign-up with the dropped email = %+v, want contact %d", again, keep)
	}

	_, err = s.MergeContacts(ctx, human, keep, keep)
	wantErr(t, err, ErrInvalid)
	_, err = s.MergeContacts(ctx, human, keep, drop)
	wantErr(t, err, ErrNotFound)

	if err := s.DeleteNote(ctx, human, note.ID); err != nil {
		t.Fatal(err)
	}
	wantErr(t, s.DeleteNote(ctx, human, note.ID), ErrNotFound)
	if n := len(activities(t, s, ActivityQuery{Kind: ActivityNoteAdded})); n != 1 {
		t.Fatalf("deleting the note removed its activity (%d left)", n)
	}
	_, err = s.AddNote(ctx, human, drop, "to a merged-away contact")
	wantErr(t, err, ErrNotFound)
	_, err = s.AddNote(ctx, human, keep, "  ")
	wantErr(t, err, ErrInvalid)
}

func TestSearchContactsFiltersAndPages(t *testing.T) {
	s, _ := newService(t, nil)
	ctx := context.Background()
	day := func(n int) time.Time { return t0.AddDate(0, 0, n) }
	ids := make([]ContactID, 5)
	for i := range ids {
		a := clerk
		if i%2 == 1 {
			a = auth0
		}
		su := created(a, fmt.Sprint("u", i), fmt.Sprintf("user%d@%s", i, []string{"acme.com", "globex.com"}[i%2]))
		su.Name = fmt.Sprintf("Person_%d", i)
		su.OccurredAt = day(i)
		ids[i] = ingest(t, s, a, su).ContactID
	}
	if _, err := s.UpdateContact(ctx, human, ids[1], ContactPatch{AddTags: []Tag{"vip"}, Stage: ptr(Stage("customer"))}); err != nil {
		t.Fatal(err)
	}
	acme, err := s.GetCompanyByDomain(ctx, "acme.com")
	if err != nil {
		t.Fatal(err)
	}

	search := func(q ContactQuery) []ContactID {
		t.Helper()
		p, err := s.SearchContacts(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		var got []ContactID
		for _, c := range p.Items {
			got = append(got, c.ID)
		}
		return got
	}
	cases := []struct {
		name string
		q    ContactQuery
		want []ContactID
	}{
		{"all, newest first", ContactQuery{}, []ContactID{ids[4], ids[3], ids[2], ids[1], ids[0]}},
		{"email", ContactQuery{Text: "USER3@"}, []ContactID{ids[3]}},
		{"name, underscore is literal", ContactQuery{Text: "person_2"}, []ContactID{ids[2]}},
		{"company domain", ContactQuery{Text: "globex"}, []ContactID{ids[3], ids[1]}},
		{"tag", ContactQuery{Tag: "VIP"}, []ContactID{ids[1]}},
		{"stage", ContactQuery{Stage: "new"}, []ContactID{ids[4], ids[3], ids[2], ids[0]}},
		{"provider", ContactQuery{Provider: "auth0"}, []ContactID{ids[3], ids[1]}},
		{"company", ContactQuery{CompanyID: acme.ID}, []ContactID{ids[4], ids[2], ids[0]}},
		{"date range", ContactQuery{SignedUpAfter: day(1), SignedUpBefore: day(3)}, []ContactID{ids[2], ids[1]}},
		{"combined", ContactQuery{Text: "acme", SignedUpAfter: day(1)}, []ContactID{ids[4], ids[2]}},
		{"no match", ContactQuery{Text: "%"}, nil},
	}
	for _, c := range cases {
		if got := search(c.q); !slices.Equal(got, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}

	var paged []ContactID
	q := ContactQuery{Limit: 2}
	for pages := 0; ; pages++ {
		p, err := s.SearchContacts(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		if pages > 3 || len(p.Items) > 2 {
			t.Fatalf("page %d has %d items", pages, len(p.Items))
		}
		for _, c := range p.Items {
			paged = append(paged, c.ID)
		}
		if p.NextCursor == "" {
			break
		}
		q.Cursor = p.NextCursor
	}
	if want := search(ContactQuery{}); !slices.Equal(paged, want) {
		t.Fatalf("paging by 2 gave %v, want %v", paged, want)
	}
	_, err = s.SearchContacts(ctx, ContactQuery{Cursor: "garbage"})
	wantErr(t, err, ErrInvalid)
}

func TestStagesRenameAndGuard(t *testing.T) {
	s, _ := newService(t, nil)
	ctx := context.Background()
	id := ingest(t, s, clerk, created(clerk, "user_1", "ada@stripe.com")).ContactID

	err := s.SetStages(ctx, human, []Stage{"lead", "qualified", "customer", "churned"}, map[Stage]Stage{"new": "lead"})
	if err != nil {
		t.Fatal(err)
	}
	want := []StageCount{{"lead", 1}, {"qualified", 0}, {"customer", 0}, {"churned", 0}}
	if got, _ := s.ListStages(ctx); !slices.Equal(got, want) {
		t.Fatalf("stages = %v, want %v", got, want)
	}
	if c := contact(t, s, id); c.Stage != "lead" {
		t.Fatalf("renamed stage did not follow: %q", c.Stage)
	}
	next := ingest(t, s, clerk, created(clerk, "user_2", "grace@navy.mil"))
	if c := contact(t, s, next.ContactID); c.Stage != "lead" {
		t.Fatalf("new contact stage = %q, want the first stage", c.Stage)
	}

	wantErr(t, s.SetStages(ctx, human, []Stage{"qualified"}, nil), ErrInvalid)
	wantErr(t, s.SetStages(ctx, human, nil, nil), ErrInvalid)
	wantErr(t, s.SetStages(ctx, human, []Stage{"lead", "lead"}, nil), ErrInvalid)
	if err := s.SetStages(ctx, human, []Stage{"lead", "won"}, nil); err != nil {
		t.Fatalf("dropping unused stages: %v", err)
	}
}

// TestFieldListsMatchSchema keeps ContactFields and CompanyFields equal to
// the manual_fields CHECKs of the migrated schema.
func TestFieldListsMatchSchema(t *testing.T) {
	_, pool := newService(t, nil)
	for table, inGo := range map[string][]Field{"companies": CompanyFields, "contacts": ContactFields} {
		var def string
		if err := pool.QueryRow(context.Background(), `
			SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname = $1`, table+"_manual_fields_check").Scan(&def); err != nil {
			t.Fatal(err)
		}
		var inSchema []string
		for _, m := range regexp.MustCompile(`'([a-z_]+)'`).FindAllStringSubmatch(def, -1) {
			inSchema = append(inSchema, m[1])
		}
		got := fieldNames(inGo)
		slices.Sort(inSchema)
		slices.Sort(got)
		if !slices.Equal(inSchema, got) {
			t.Errorf("%s: schema allows %v, Go lists %v", table, inSchema, got)
		}
	}
}
