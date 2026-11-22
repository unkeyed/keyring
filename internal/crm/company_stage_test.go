package crm

import (
	"context"
	"testing"
)

// Setting a company's stage moves everyone at it, and people who sign up
// there later start in it: one change marks a whole customer.
func TestCompanyStageMovesItsContacts(t *testing.T) {
	s, _ := newService(t, nil)
	ctx := context.Background()
	ada := ingest(t, s, clerk, created(clerk, "u1", "ada@acme.com")).ContactID
	bob := ingest(t, s, clerk, created(clerk, "u2", "bob@acme.com")).ContactID
	eve := ingest(t, s, clerk, created(clerk, "u3", "eve@globex.com")).ContactID
	if _, err := s.UpdateContact(ctx, human, bob, ContactPatch{Stage: ptr(Stage("qualified"))}); err != nil {
		t.Fatal(err)
	}
	acme, err := s.GetCompanyByDomain(ctx, "acme.com")
	if err != nil {
		t.Fatal(err)
	}
	if acme.Stage != nil {
		t.Fatalf("new company stage = %q, want none", *acme.Stage)
	}

	co, err := s.UpdateCompany(ctx, agent, acme.ID, CompanyPatch{Stage: ptr(Stage("customer"))})
	if err != nil {
		t.Fatal(err)
	}
	if co.Stage == nil || *co.Stage != "customer" || len(co.ManualFields) != 0 {
		t.Fatalf("company = stage %v, manual %v", co.Stage, co.ManualFields)
	}
	for _, id := range []ContactID{ada, bob} {
		if c := contact(t, s, id); c.Stage != "customer" {
			t.Fatalf("contact %d stage = %q, want customer", id, c.Stage)
		}
	}
	if c := contact(t, s, eve); c.Stage != "new" {
		t.Fatalf("eve at another company moved to %q", c.Stage)
	}
	a := activities(t, s, ActivityQuery{ContactID: bob, Kind: ActivityContactUpdated})
	if string(a[0].Data) != `{"stage": {"to": "customer", "from": "qualified"}}` || a[0].Actor != agent {
		t.Fatalf("bob's newest activity = %s by %v", a[0].Data, a[0].Actor)
	}
	a = activities(t, s, ActivityQuery{CompanyID: acme.ID, Kind: ActivityCompanyUpdated})
	if len(a) != 1 || a[0].Company == nil || string(a[0].Data) != `{"stage": {"to": "customer", "from": ""}}` {
		t.Fatalf("company activity = %+v", a)
	}

	// Later people at the company, by sign-up or by hand, start as customers;
	// an explicit stage on create still wins.
	cat := ingest(t, s, clerk, created(clerk, "u4", "cat@acme.com")).ContactID
	dan, err := s.CreateContact(ctx, human, NewContact{Email: "dan@acme.com"})
	if err != nil {
		t.Fatal(err)
	}
	fay, err := s.CreateContact(ctx, human, NewContact{Email: "fay@acme.com", Stage: "qualified"})
	if err != nil {
		t.Fatal(err)
	}
	if c := contact(t, s, cat); c.Stage != "customer" || dan.Stage != "customer" || fay.Stage != "qualified" {
		t.Fatalf("new contacts: cat %q, dan %q, fay %q", c.Stage, dan.Stage, fay.Stage)
	}

	// One person can still move alone, and setting the company again brings
	// them back. Setting it again with nobody to move writes nothing.
	if _, err := s.UpdateContact(ctx, human, ada, ContactPatch{Stage: ptr(Stage("churned"))}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateCompany(ctx, human, acme.ID, CompanyPatch{Stage: ptr(Stage("customer"))}); err != nil {
		t.Fatal(err)
	}
	if c := contact(t, s, ada); c.Stage != "customer" {
		t.Fatalf("ada after re-setting the company = %q", c.Stage)
	}
	before := len(activities(t, s, ActivityQuery{CompanyID: acme.ID}))
	if _, err := s.UpdateCompany(ctx, human, acme.ID, CompanyPatch{Stage: ptr(Stage("customer"))}); err != nil {
		t.Fatal(err)
	}
	if after := len(activities(t, s, ActivityQuery{CompanyID: acme.ID})); after != before {
		t.Fatalf("a no-op stage set wrote %d activities", after-before)
	}

	p, err := s.SearchCompanies(ctx, CompanyQuery{Stage: "customer"})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Items) != 1 || p.Items[0].ID != acme.ID {
		t.Fatalf("companies in customer = %+v", p.Items)
	}
	stages, err := s.ListStages(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stages[2] != (StageCount{Name: "customer", ContactCount: 5, CompanyCount: 1}) {
		t.Fatalf("stages = %+v", stages)
	}

	// Removing a stage a company is in is refused; renaming one carries the
	// company along.
	err = s.SetStages(ctx, human, []Stage{"new", "qualified", "churned"}, nil)
	wantErr(t, err, ErrInvalid)
	if err := s.SetStages(ctx, human, []Stage{"new", "qualified", "paying", "churned"}, map[Stage]Stage{"customer": "paying"}); err != nil {
		t.Fatal(err)
	}
	if co, err := s.GetCompany(ctx, acme.ID); err != nil || co.Stage == nil || *co.Stage != "paying" {
		t.Fatalf("after rename, company stage = %v, %v", co.Stage, err)
	}

	// Clearing the company's stage leaves its people where they are.
	co, err = s.UpdateCompany(ctx, human, acme.ID, CompanyPatch{Stage: ptr(Stage(""))})
	if err != nil || co.Stage != nil {
		t.Fatalf("clear = %v, %v", co.Stage, err)
	}
	if c := contact(t, s, ada); c.Stage != "paying" {
		t.Fatalf("clearing moved ada to %q", c.Stage)
	}
	_, err = s.UpdateCompany(ctx, human, acme.ID, CompanyPatch{Stage: ptr(Stage("vip"))})
	wantErr(t, err, ErrInvalid)
}
