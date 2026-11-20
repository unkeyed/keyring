package crm

import (
	"context"
	"errors"
	"github.com/unkeyed/keyring/internal/secretbox"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestSettle(t *testing.T) {
	now := time.Unix(0, 0)
	cases := []struct {
		name         string
		failures     int
		err          error
		want         JobState
		wantFailures int
		wantHalt     bool
	}{
		{"ok", 0, nil, JobDone, 0, false},
		{"network error retries", 0, errors.New("dial tcp: timeout"), JobPending, 1, false},
		{"5xx retries", 3, &EnrichError{Kind: EnrichRetryable}, JobPending, 4, false},
		{"5xx gives up at the budget", maxFailures - 1, &EnrichError{Kind: EnrichRetryable}, JobFailed, maxFailures, false},
		{"throttle never spends budget", maxFailures - 1, &EnrichError{Kind: EnrichThrottled}, JobPending, maxFailures - 1, false},
		{"bad key halts and refunds", maxFailures - 1, &EnrichError{Kind: EnrichHalted, Msg: "401"}, JobPending, maxFailures - 1, true},
		{"other 4xx fails at once", 0, &EnrichError{Kind: EnrichPermanent, Msg: "400"}, JobFailed, 1, false},
	}
	for _, c := range cases {
		got := settle(c.failures, c.err, now)
		if got.state != c.want || got.failures != c.wantFailures || (got.halt > 0) != c.wantHalt {
			t.Errorf("%s: got state %s failures %d halt %v, want %s %d halt=%v",
				c.name, got.state, got.failures, got.halt, c.want, c.wantFailures, c.wantHalt)
		}
	}
}

// TestJobStatesMatchSchema parses the enrichment_jobs CHECK in
// internal/db/migrations/001_init.sql and fails unless it lists exactly the
// JobState constants.
func TestJobStatesMatchSchema(t *testing.T) {
	sql, err := os.ReadFile("../db/migrations/001_init.sql")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`state\s+text NOT NULL CHECK \(state IN \(([^)]*)\)\)`).FindSubmatch(sql)
	if m == nil {
		t.Fatal("no enrichment_jobs.state CHECK found in 001_init.sql")
	}
	var inSchema []string
	for _, v := range strings.Split(string(m[1]), ",") {
		inSchema = append(inSchema, strings.Trim(strings.TrimSpace(v), "'"))
	}
	var inGo []string
	for _, st := range JobStates {
		inGo = append(inGo, string(st))
	}
	slices.Sort(inSchema)
	slices.Sort(inGo)
	if !slices.Equal(inSchema, inGo) {
		t.Fatalf("schema allows %v, Go defines %v", inSchema, inGo)
	}
}

// expireLeases stands in for leaseFor passing on every running job.
func expireLeases(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`UPDATE enrichment_jobs SET lease_until = now() - interval '1 second' WHERE state = 'running'`); err != nil {
		t.Fatal(err)
	}
}

func TestExpiredLeaseIsReclaimed(t *testing.T) {
	s, pool := newService(t, fakeEnricher{
		person: func(Email, string) (PersonFacts, error) { return PersonFacts{Found: true, Title: "CEO"}, nil },
	})
	id := ingest(t, s, clerk, created(clerk, "user_1", "ada@stripe.com")).ContactID

	if _, ok, err := s.claim(context.Background()); !ok || err != nil {
		t.Fatalf("claim = %v, %v", ok, err)
	}
	drain(t, s)
	if st := status(t, s); st.Jobs[JobRunning] != 1 || st.Jobs[JobDone] != 1 {
		t.Fatalf("jobs = %v, want the crashed claim still leased and the other job done", st.Jobs)
	}

	expireLeases(t, pool)
	drain(t, s)
	if st := status(t, s); st.Jobs[JobRunning] != 0 || st.Jobs[JobDone] != 2 {
		t.Fatalf("jobs = %v, want the expired claim reclaimed and done", st.Jobs)
	}
	if c := contact(t, s, id); c.Title != "CEO" {
		t.Fatalf("title = %q after the reclaimed job ran", c.Title)
	}
}

func TestStaleWorkerCannotOverwriteNewerClaim(t *testing.T) {
	ctx := context.Background()
	seed, pool := newService(t, nil)
	id := ingest(t, seed, clerk, created(clerk, "user_1", "ada@stripe.com")).ContactID

	entered, release := make(chan struct{}), make(chan struct{})
	stale := New(pool, fakeEnricher{
		person: func(Email, string) (PersonFacts, error) {
			close(entered)
			<-release
			return PersonFacts{Found: true, Title: "stale"}, nil
		},
		company: func(Domain) (CompanyFacts, error) {
			close(entered)
			<-release
			return CompanyFacts{Found: true, Name: "Stale Inc"}, nil
		},
	}, secretbox.ForTest())
	fresh := New(pool, fakeEnricher{
		person:  func(Email, string) (PersonFacts, error) { return PersonFacts{Found: true, Title: "fresh"}, nil },
		company: func(Domain) (CompanyFacts, error) { return CompanyFacts{Found: true, Name: "Fresh Inc"}, nil },
	}, secretbox.ForTest())

	finished := make(chan error)
	go func() {
		_, err := stale.enrichOnce(ctx)
		finished <- err
	}()
	<-entered
	expireLeases(t, pool)
	drain(t, fresh)
	close(release)
	if err := <-finished; err != nil {
		t.Fatalf("stale worker: %v", err)
	}

	co, err := seed.GetCompanyByDomain(ctx, "stripe.com")
	if err != nil {
		t.Fatal(err)
	}
	if c := contact(t, seed, id); c.Title != "fresh" || co.Name != "Fresh Inc" {
		t.Fatalf("title %q company %q: the worker that lost its lease overwrote the newer claim", c.Title, co.Name)
	}
	if st := status(t, seed); st.Jobs[JobDone] != 2 {
		t.Fatalf("jobs = %v", st.Jobs)
	}
}

func TestHaltPausesEveryReplicaUntilRestart(t *testing.T) {
	ctx := context.Background()
	seed, pool := newService(t, nil)
	ingest(t, seed, clerk, created(clerk, "user_1", "ada@stripe.com"))
	ingest(t, seed, clerk, created(clerk, "user_2", "grace@navy.mil"))

	badKey := &EnrichError{Kind: EnrichHalted, Msg: "context.dev 401 UNAUTHORIZED"}
	rejected := fakeEnricher{
		person:  func(Email, string) (PersonFacts, error) { return PersonFacts{}, badKey },
		company: func(Domain) (CompanyFacts, error) { return CompanyFacts{}, badKey },
	}
	a, b := New(pool, rejected, secretbox.ForTest()), New(pool, rejected, secretbox.ForTest())
	if ran, err := a.enrichOnce(ctx); !ran || err != nil {
		t.Fatalf("replica a: ran %v err %v", ran, err)
	}
	st := status(t, seed)
	if st.EnrichmentHalt == nil || !strings.Contains(st.EnrichmentHalt.Reason, "401") || st.Jobs[JobPending] != 4 {
		t.Fatalf("after a 401: halt %+v jobs %v; want a halt and every job still pending", st.EnrichmentHalt, st.Jobs)
	}
	if ran, err := b.enrichOnce(ctx); ran || err != nil {
		t.Fatalf("replica b ran %v err %v while halted, want no claim", ran, err)
	}

	fixed := New(pool, fakeEnricher{}, secretbox.ForTest())
	runCtx, stop := context.WithCancel(ctx)
	stopped := make(chan error, 1)
	go func() { stopped <- fixed.RunEnrichment(runCtx) }()
	for deadline := time.Now().Add(10 * time.Second); status(t, seed).Jobs[JobDone] < 3; {
		if time.Now().After(deadline) {
			t.Fatalf("restarted worker did not resume: jobs %v", status(t, seed).Jobs)
		}
		time.Sleep(20 * time.Millisecond)
	}
	stop()
	wantErr(t, <-stopped, context.Canceled)
	if st := status(t, seed); st.EnrichmentHalt != nil || st.Jobs[JobPending] != 1 {
		t.Fatalf("after restart: halt %+v jobs %v; want no halt and only the halted job waiting out its pause",
			st.EnrichmentHalt, st.Jobs)
	}
}

func TestNoKeyLeavesJobsPending(t *testing.T) {
	s, _ := newService(t, nil)
	ingest(t, s, clerk, created(clerk, "user_1", "ada@stripe.com"))

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	wantErr(t, s.RunEnrichment(ctx), context.DeadlineExceeded)

	st := status(t, s)
	if st.EnrichmentEnabled || st.Jobs[JobPending] != 2 || st.Jobs[JobRunning] != 0 {
		t.Fatalf("status = %+v, want enrichment off and both jobs pending", st)
	}
}

func TestEnrichmentErrorsRetryOrFail(t *testing.T) {
	s, _ := newService(t, fakeEnricher{
		person: func(Email, string) (PersonFacts, error) {
			return PersonFacts{}, &EnrichError{Kind: EnrichPermanent, Msg: "context.dev 400"}
		},
		company: func(Domain) (CompanyFacts, error) { return CompanyFacts{}, errors.New("dial tcp: connection refused") },
	})
	id := ingest(t, s, clerk, created(clerk, "user_1", "ada@stripe.com")).ContactID
	drain(t, s)

	c := contact(t, s, id)
	co, err := s.GetCompany(context.Background(), c.Company.ID)
	if err != nil {
		t.Fatal(err)
	}
	if c.Enrichment != JobFailed || co.Enrichment != JobPending {
		t.Fatalf("person after a 400 = %q, company after a network error = %q; want failed and pending (retry later)",
			c.Enrichment, co.Enrichment)
	}
}
