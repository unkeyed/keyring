package crm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/uncrm/uncrm/internal/signup"
)

func TestProviderRetryAndReplayLandOnce(t *testing.T) {
	s, _ := newService(t, nil)
	su := created(clerk, "user_1", "Ada@Stripe.com")
	su.Name = "Ada Lovelace"
	su.Raw = json.RawMessage(`{"svix_id":"msg_1"}`)

	first := ingest(t, s, clerk, su)
	if !first.Created || first.Duplicate {
		t.Fatalf("first delivery = %+v, want created", first)
	}
	retry := ingest(t, s, clerk, su)
	su.Raw = json.RawMessage(`{"svix_id":"msg_2"}`) // a dashboard replay mints a new delivery id
	replay := ingest(t, s, clerk, su)
	for _, r := range []IngestResult{retry, replay} {
		if r.ContactID != first.ContactID || r.Created || !r.Duplicate {
			t.Fatalf("redelivery = %+v, want duplicate of contact %d", r, first.ContactID)
		}
	}

	if st := status(t, s); st.Contacts != 1 || st.Companies != 1 {
		t.Fatalf("status = %+v, want one contact at one company", st)
	}
	signups := activities(t, s, ActivityQuery{Kind: ActivitySignedUp})
	if len(signups) != 1 || signups[0].Contact.ID != first.ContactID || !signups[0].OccurredAt.Equal(t0) {
		t.Fatalf("signed_up activities = %+v, want one for contact %d at %v", signups, first.ContactID, t0)
	}
	c := contact(t, s, first.ContactID)
	if c.Email.String() != "ada@stripe.com" || c.Name != "Ada Lovelace" || c.Company == nil || c.Company.Domain.String() != "stripe.com" {
		t.Fatalf("contact = %+v", c.Contact)
	}
	if c.Stage != "new" || c.Enrichment != JobPending || !slices.Equal(c.Providers, []string{"clerk"}) {
		t.Fatalf("contact stage %q enrichment %q providers %v", c.Stage, c.Enrichment, c.Providers)
	}
}

func TestGenericSignupDefaultsAndGuards(t *testing.T) {
	s, _ := newService(t, nil)
	ctx := context.Background()
	su := signup.Signup{Kind: signup.Created, Email: " Grace@Navy.mil "}

	first := ingest(t, s, agent, su)
	again := ingest(t, s, agent, su)
	if !first.Created || first.Duplicate || again.ContactID != first.ContactID || again.Created || !again.Duplicate {
		t.Fatalf("first %+v, again %+v: want created then duplicate", first, again)
	}
	c := contact(t, s, first.ContactID)
	if len(c.Identities) != 1 || c.Identities[0].Provider != "api" || c.Identities[0].ExternalID != "grace@navy.mil" {
		t.Fatalf("identities = %+v, want one api identity keyed by the email", c.Identities)
	}

	_, err := s.Ingest(ctx, agent, signup.Signup{Source: "clerk", Kind: signup.Created, ExternalID: "user_9", Email: "x@acme.com"})
	wantErr(t, err, ErrInvalid)
	_, err = s.Ingest(ctx, agent, signup.Signup{Kind: signup.Created, Email: "not an email"})
	wantErr(t, err, ErrInvalid)
	_, err = s.Ingest(ctx, agent, signup.Signup{Kind: signup.Created})
	wantErr(t, err, ErrInvalid)
}

func TestOutOfOrderUpdateDoesNotRegressIdentity(t *testing.T) {
	s, _ := newService(t, nil)
	update := signup.Signup{Source: "clerk", Kind: signup.Updated, ExternalID: "user_1", Email: "ada@stripe.com",
		Name: "Ada Lovelace", AvatarURL: "https://img/new.png", OccurredAt: t0.Add(time.Hour)}
	create := signup.Signup{Source: "clerk", Kind: signup.Created, ExternalID: "user_1", Email: "ada@stripe.com",
		Name: "Ada", AvatarURL: "https://img/old.png", OccurredAt: t0}

	r := ingest(t, s, clerk, update)
	late := ingest(t, s, clerk, create)
	if late.ContactID != r.ContactID || late.Duplicate {
		t.Fatalf("late create = %+v, want first signed_up for contact %d", late, r.ContactID)
	}

	c := contact(t, s, r.ContactID)
	if c.Name != "Ada Lovelace" || c.AvatarURL != "https://img/new.png" {
		t.Fatalf("contact name %q avatar %q, want the newer update's", c.Name, c.AvatarURL)
	}
	if id := c.Identities[0]; id.Name != "Ada Lovelace" || !id.OccurredAt.Equal(t0.Add(time.Hour)) {
		t.Fatalf("identity = %+v, want the newer update's", id)
	}
	if !c.FirstSeenAt.Equal(t0) || !c.LastSeenAt.Equal(t0.Add(time.Hour)) {
		t.Fatalf("seen %v..%v, want %v..%v", c.FirstSeenAt, c.LastSeenAt, t0, t0.Add(time.Hour))
	}
	if n := len(activities(t, s, ActivityQuery{Kind: ActivitySignedUp})); n != 1 {
		t.Fatalf("%d signed_up activities, want 1", n)
	}
}

func TestSameEmailFromTwoProvidersAtOnceIsOneContact(t *testing.T) {
	s, _ := newService(t, nil)
	const people = 20
	for i := range people {
		email := fmt.Sprintf("person%d@acme.com", i)
		deliveries := []struct {
			actor Actor
			su    signup.Signup
		}{
			{clerk, created(clerk, fmt.Sprint("user_", i), email)},
			{auth0, created(auth0, fmt.Sprint("auth0|", i), email)},
			{clerk, created(clerk, fmt.Sprint("user_", i), email)}, // a retry racing the original
			{auth0, created(auth0, fmt.Sprint("auth0|", i), email)},
		}
		results := make([]IngestResult, len(deliveries))
		errs := make([]error, len(deliveries))
		var wg sync.WaitGroup
		for j, d := range deliveries {
			wg.Go(func() { results[j], errs[j] = s.Ingest(context.Background(), d.actor, d.su) })
		}
		wg.Wait()

		creations := 0
		for j, r := range results {
			if errs[j] != nil {
				t.Fatalf("%s: concurrent Ingest: %v", email, errs[j])
			}
			if r.ContactID != results[0].ContactID {
				t.Fatalf("%s: results %+v name different contacts", email, results)
			}
			if r.Created {
				creations++
			}
		}
		if creations != 1 {
			t.Fatalf("%s: %d results claim to have created the contact, want 1", email, creations)
		}
		if p := contact(t, s, results[0].ContactID).Providers; !slices.Equal(p, []string{"auth0", "clerk"}) {
			t.Fatalf("%s: providers %v", email, p)
		}
	}
	if st := status(t, s); st.Contacts != people || st.Companies != 1 {
		t.Fatalf("status = %+v, want %d contacts at one company", st, people)
	}
	if n := len(activities(t, s, ActivityQuery{Kind: ActivitySignedUp, Limit: 200})); n != 2*people {
		t.Fatalf("%d signed_up activities, want one per provider user (%d)", n, 2*people)
	}
}

func TestFreeMailMakesNoCompanyAndSkipsEnrichment(t *testing.T) {
	s, _ := newService(t, fakeEnricher{})
	r := ingest(t, s, clerk, created(clerk, "user_1", "ada@gmail.com"))

	c := contact(t, s, r.ContactID)
	if c.Company != nil || c.Enrichment != JobSkipped {
		t.Fatalf("free-mail contact company %+v enrichment %q, want none and skipped", c.Company, c.Enrichment)
	}
	st := status(t, s)
	if st.Companies != 0 || st.Jobs[JobSkipped] != 1 || st.Jobs[JobPending] != 0 {
		t.Fatalf("status = %+v, want no company and one skipped job", st)
	}
	if state, err := s.EnrichContact(context.Background(), human, r.ContactID); err != nil || state != JobSkipped {
		t.Fatalf("EnrichContact = %q, %v; want it to stay skipped", state, err)
	}
	if ran, err := s.enrichOnce(context.Background()); ran || err != nil {
		t.Fatalf("worker ran %v err %v on a free-mail contact, want nothing due", ran, err)
	}

	_, err := s.UpdateContact(context.Background(), human, r.ContactID, ContactPatch{CompanyDomain: ptr(mustDomain(t, "gmail.com"))})
	wantErr(t, err, ErrInvalid)
}

func TestProviderDeleteMarksIdentityOnce(t *testing.T) {
	s, _ := newService(t, nil)
	ctx := context.Background()
	r := ingest(t, s, clerk, created(clerk, "user_1", "ada@stripe.com"))
	del := signup.Signup{Source: "clerk", Kind: signup.Deleted, ExternalID: "user_1", OccurredAt: t0.Add(time.Hour)}

	first := ingest(t, s, clerk, del)
	again := ingest(t, s, clerk, del)
	if first.ContactID != r.ContactID || first.Duplicate || !again.Duplicate {
		t.Fatalf("deletes = %+v then %+v", first, again)
	}
	c := contact(t, s, r.ContactID)
	if c.Identities[0].DeletedAt == nil || !c.Identities[0].DeletedAt.Equal(t0.Add(time.Hour)) {
		t.Fatalf("identity = %+v, want deleted at %v", c.Identities[0], t0.Add(time.Hour))
	}
	stale := ingest(t, s, clerk, signup.Signup{Source: "clerk", Kind: signup.Updated, ExternalID: "user_1",
		Email: "ada@stripe.com", Name: "Stale", OccurredAt: t0.Add(time.Minute)})
	if stale.ContactID != r.ContactID || contact(t, s, r.ContactID).Name == "Stale" {
		t.Fatal("an update older than the delete was applied")
	}
	if n := len(activities(t, s, ActivityQuery{Kind: ActivityProviderDeleted})); n != 1 {
		t.Fatalf("%d provider_deleted activities, want 1", n)
	}

	_, err := s.Ingest(ctx, clerk, signup.Signup{Source: "clerk", Kind: signup.Deleted, ExternalID: "user_unknown"})
	wantErr(t, err, ErrNotFound)
}

func TestReceiveWebhookRecordsEachOutcome(t *testing.T) {
	s, _ := newService(t, nil)
	ctx := context.Background()
	d := signup.Delivery{
		Header:     http.Header{"Svix-Id": {"msg_1"}, "Svix-Timestamp": {"1"}, "Svix-Signature": {"v1,bm9wZQ=="}},
		Body:       []byte(`{"type":"user.created"}`),
		ReceivedAt: time.Now(),
	}

	wantErr(t, s.ReceiveWebhook(ctx, "kinde", d), ErrUnknownProvider)
	wantErr(t, s.ReceiveWebhook(ctx, "clerk", d), ErrNotConnected)

	_, err := s.ConnectProvider(ctx, human, "clerk", "")
	wantErr(t, err, ErrInvalid)
	if _, err := s.ConnectProvider(ctx, human, "clerk", "whsec_c2VjcmV0"); err != nil {
		t.Fatal(err)
	}
	wantErr(t, s.ReceiveWebhook(ctx, "clerk", d), signup.ErrBadSignature)

	providers, err := s.ListProviders(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(providers) != len(signup.All()) {
		t.Fatalf("%d providers listed, %d registered", len(providers), len(signup.All()))
	}
	for _, p := range providers {
		connected := p.Name == "clerk"
		if p.Connected != connected || p.WebhookPath != "/webhooks/"+p.Name {
			t.Fatalf("provider %+v", p)
		}
		if connected && (!strings.Contains(p.LastError, "bad signature") || p.LastErrorAt == nil || p.LastDeliveryAt != nil) {
			t.Fatalf("clerk after a bad signature = %+v, want the error shown", p)
		}
	}

	issued, err := s.ConnectProvider(ctx, human, "auth0", "")
	if err != nil {
		t.Fatal(err)
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(issued, "whsec_"))
	if !strings.HasPrefix(issued, "whsec_") || err != nil || len(key) != 32 {
		t.Fatalf("issued secret %q, want whsec_ and 32 base64 bytes", issued)
	}

	if err := s.DisconnectProvider(ctx, human, "clerk"); err != nil {
		t.Fatal(err)
	}
	wantErr(t, s.ReceiveWebhook(ctx, "clerk", d), ErrNotConnected)
}

func mustDomain(t *testing.T, s string) Domain {
	t.Helper()
	d, err := ParseDomain(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}
