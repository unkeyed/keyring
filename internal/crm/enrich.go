package crm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Enricher looks people and companies up in an outside service.
// internal/contextdev implements it. Found=false with a nil error means the
// service had no match; that is a finished job, not a failure.
type Enricher interface {
	Person(ctx context.Context, email Email) (PersonFacts, error)
	Company(ctx context.Context, domain Domain) (CompanyFacts, error)
}

type PersonFacts struct {
	Found       bool
	Name        string
	Title       string
	Location    string
	Bio         string
	AvatarURL   string
	LinkedInURL string
	Raw         json.RawMessage
}

type CompanyFacts struct {
	Found         bool
	Name          string
	Description   string
	LogoURL       string
	EmployeeRange string
	Industry      string
	Country       string
	LinkedInURL   string
	Raw           json.RawMessage
}

// EnrichError is how an Enricher classifies a failed call. Any other error
// (network, timeout) is treated as EnrichRetryable.
type EnrichError struct {
	Kind       EnrichErrorKind
	RetryAfter time.Duration // EnrichThrottled only
	Msg        string
}

type EnrichErrorKind int

const (
	// EnrichRetryable: 5xx, 408 or network. Retry with backoff, spending one
	// failure.
	EnrichRetryable EnrichErrorKind = iota
	// EnrichThrottled: 429. Retry at RetryAfter without spending the failure
	// budget; the request was never served.
	EnrichThrottled
	// EnrichHalted: 401, 402 or 403. The key or the account is the problem,
	// not this job, so the attempt is refunded and every worker pauses.
	// Retrying would burn calls that cannot succeed, and repeated 4xx can get
	// a free context.dev key disabled.
	EnrichHalted
	// EnrichPermanent: any other 4xx. This subject cannot be enriched.
	EnrichPermanent
)

func (e *EnrichError) Error() string { return e.Msg }

// JobState is an enrichment_jobs.state value.
//
//	           claim                 finish(ok | not found)
//	pending ----------> running ----------------------------> done
//	   ^                 |  ^ \      finish(permanent error,
//	   |  finish(retry)  |  |  \     or failures == maxFailures)
//	   +-----------------+  |   +---------------------------> failed
//	   |                    | reclaim when lease_until < now()
//	   |                    +--- (a worker crashed or hung)
//	   |
//	   +--- Enrich() from done | failed | skipped
//
// finish(halted) also goes running -> pending, with failures unchanged and
// run_at after haltPause, and it writes enrichment_halt, which stops every
// replica's claims until it expires.
//
// skipped is written at enqueue for a free-mail contact and never claimed.
// Every running -> * update is fenced by "WHERE state = 'running' AND
// claims = $claims", so a worker whose lease expired and was reclaimed
// cannot overwrite the newer claim's result.
type JobState string

const (
	JobPending JobState = "pending"
	JobRunning JobState = "running"
	JobDone    JobState = "done"
	JobFailed  JobState = "failed"
	JobSkipped JobState = "skipped"
)

const (
	maxFailures  = 5
	leaseFor     = 3 * time.Minute // longer than contextdev's 90s request timeout
	pollInterval = 5 * time.Second
	haltPause    = 15 * time.Minute
)

// claimSQL takes one due job. A running job whose lease expired is due again,
// which is the whole crash-recovery story. SKIP LOCKED keeps replicas from
// waiting on each other. Nothing is due while an unexpired halt exists.
const claimSQL = `
UPDATE enrichment_jobs
   SET state = 'running', lease_until = now() + $1::interval, claims = claims + 1, updated_at = now()
 WHERE id = (
       SELECT id FROM enrichment_jobs
        WHERE ((state = 'pending' AND run_at <= now())
           OR (state = 'running' AND lease_until < now()))
          AND NOT EXISTS (SELECT 1 FROM enrichment_halt WHERE until > now())
        ORDER BY run_at
        FOR UPDATE SKIP LOCKED
        LIMIT 1)
RETURNING id, contact_id, company_id, claims, failures,
          (SELECT email FROM contacts WHERE id = contact_id),
          (SELECT domain FROM companies WHERE id = company_id)`

// job is a claimed row. claims is the fencing token. email or domain is the
// subject to look up, matching whichever of contactID and companyID is set.
type job struct {
	id        int64
	contactID *ContactID
	companyID *CompanyID
	claims    int
	failures  int
	email     Email
	domain    Domain
}

// settlement is the row update that ends a claim.
type settlement struct {
	state    JobState
	runAt    time.Time
	failures int
	lastErr  string
	halt     time.Duration // non-zero only for EnrichHalted: pause every worker this long
}

// settle is the only place that decides where a running job goes next. Pure,
// so the state machine is tested without a database.
func settle(failures int, err error, now time.Time) settlement {
	if err == nil {
		return settlement{state: JobDone, runAt: now, failures: failures}
	}
	var ee *EnrichError
	if !errors.As(err, &ee) {
		ee = &EnrichError{Kind: EnrichRetryable, Msg: err.Error()}
	}
	switch {
	case ee.Kind == EnrichThrottled:
		wait := ee.RetryAfter
		if wait <= 0 {
			wait = time.Minute
		}
		return settlement{state: JobPending, runAt: now.Add(wait), failures: failures, lastErr: ee.Msg}
	case ee.Kind == EnrichHalted:
		return settlement{state: JobPending, runAt: now.Add(haltPause), failures: failures, lastErr: ee.Msg, halt: haltPause}
	case ee.Kind == EnrichRetryable && failures+1 < maxFailures:
		backoff := time.Minute << (2 * failures) // 1m, 4m, 16m, 64m
		return settlement{state: JobPending, runAt: now.Add(backoff), failures: failures + 1, lastErr: ee.Msg}
	default:
		return settlement{state: JobFailed, runAt: now, failures: failures + 1, lastErr: ee.Msg}
	}
}

// RunEnrichment claims and runs jobs until ctx is done. Every replica runs
// one loop, so in-flight calls to the enricher equal the replica count.
// With no enricher it blocks on ctx and touches nothing. With one, it first
// deletes enrichment_halt, because a restart is how a new key arrives.
//
//	for {
//	    j, ok := claim()                 // claimSQL
//	    if !ok { sleep(pollInterval); continue }
//	    facts, err := enricher.Person|Company(...)   // outside any transaction
//	    finish(j, facts, settle(j.failures, err, now))
//	}
func (s *Service) RunEnrichment(ctx context.Context) error {
	if s.enricher == nil {
		<-ctx.Done()
		return ctx.Err()
	}
	if _, err := s.db.Exec(ctx, `DELETE FROM enrichment_halt`); err != nil {
		return err
	}
	for {
		ran, err := s.enrichOnce(ctx)
		if err != nil && ctx.Err() == nil {
			slog.Error("enrichment: job not finished; its lease will return it", "err", err)
		}
		if ran && err == nil {
			continue
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}

// enrichOnce claims one due job, calls the enricher outside any
// transaction and finishes the claim. It reports false when nothing was due.
// On shutdown it leaves the claim running, and the lease returns it.
func (s *Service) enrichOnce(ctx context.Context) (bool, error) {
	j, ok, err := s.claim(ctx)
	if err != nil || !ok {
		return false, err
	}
	var person *PersonFacts
	var company *CompanyFacts
	var callErr error
	if j.contactID != nil {
		f, err := s.enricher.Person(ctx, j.email)
		person, callErr = &f, err
	} else {
		f, err := s.enricher.Company(ctx, j.domain)
		company, callErr = &f, err
	}
	if ctx.Err() != nil {
		return true, ctx.Err()
	}
	return true, s.finish(ctx, j, person, company, settle(j.failures, callErr, s.now()))
}

func (s *Service) claim(ctx context.Context) (job, bool, error) {
	var j job
	err := s.db.QueryRow(ctx, claimSQL, leaseFor).
		Scan(&j.id, &j.contactID, &j.companyID, &j.claims, &j.failures, &j.email, &j.domain)
	if errors.Is(err, pgx.ErrNoRows) {
		return j, false, nil
	}
	return j, err == nil, err
}

// enrichmentActor writes enrichment's facts and timeline entries.
var enrichmentActor = Actor{Kind: ActorSystem, ID: "enrichment", Label: "context.dev"}

// finish writes the settlement and, on success, the facts, in one
// transaction. A settlement with halt upserts enrichment_halt with the
// error and now() + halt. The UPDATE is fenced by claims; zero rows means
// the lease was lost, so the facts are discarded. When facts will be
// written, the subject row is locked before the job row (lock order, see
// the package doc). Facts are written under the manual-values-win rule,
// and an enriched activity lists the fields that actually changed. The raw
// response goes to enrichment_jobs.response.
func (s *Service) finish(ctx context.Context, j job, person *PersonFacts, company *CompanyFacts, st settlement) error {
	return pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var raw json.RawMessage
		var c contactRow
		var co companyRow
		var err error
		found := false
		switch {
		case st.state != JobDone:
		case person != nil:
			if raw, found = person.Raw, person.Found; found {
				c, err = lockContact(ctx, tx, *j.contactID)
			}
		case company != nil:
			if raw, found = company.Raw, company.Found; found {
				co, err = lockCompany(ctx, tx, *j.companyID)
			}
		}
		if errors.Is(err, ErrNotFound) {
			return nil // the subject was deleted, and its job with it
		}
		if err != nil {
			return err
		}

		tag, err := tx.Exec(ctx, `
			UPDATE enrichment_jobs
			   SET state = $3, run_at = $4, lease_until = NULL, failures = $5, last_error = $6,
			       response = COALESCE($7::jsonb, response), updated_at = now()
			 WHERE id = $1 AND state = 'running' AND claims = $2`,
			j.id, j.claims, st.state, st.runAt, st.failures, st.lastErr, raw)
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		if st.halt > 0 {
			if _, err := tx.Exec(ctx, `
				INSERT INTO enrichment_halt (reason, until) VALUES ($1, now() + $2::interval)
				ON CONFLICT (singleton) DO UPDATE SET reason = EXCLUDED.reason, until = EXCLUDED.until, at = now()`,
				st.lastErr, st.halt); err != nil {
				return err
			}
		}
		if !found {
			return nil
		}

		var changed []Field
		if person != nil {
			changed = observeAll(c.manual, []observation{
				{FieldName, &c.name, person.Name},
				{fieldAvatarURL, &c.avatarURL, person.AvatarURL},
				{FieldTitle, &c.title, person.Title},
				{FieldLocation, &c.location, person.Location},
				{FieldBio, &c.bio, person.Bio},
				{FieldLinkedInURL, &c.linkedInURL, person.LinkedInURL},
			})
			if len(changed) > 0 {
				err = writeContact(ctx, tx, c)
			}
		} else {
			changed = observeAll(co.manual, []observation{
				{FieldName, &co.name, company.Name},
				{FieldDescription, &co.description, company.Description},
				{fieldLogoURL, &co.logoURL, company.LogoURL},
				{FieldEmployeeRange, &co.employeeRange, company.EmployeeRange},
				{FieldIndustry, &co.industry, company.Industry},
				{FieldCountry, &co.country, company.Country},
				{FieldLinkedInURL, &co.linkedInURL, company.LinkedInURL},
			})
			if len(changed) > 0 {
				err = writeCompany(ctx, tx, co)
			}
		}
		if err != nil || len(changed) == 0 {
			return err
		}
		a := activityRow{kind: ActivityEnriched, actor: enrichmentActor, data: map[string][]Field{"changed": changed}, at: s.now()}
		if person != nil {
			a.contact = c.id
		} else {
			a.company = co.id
		}
		_, err = recordActivity(ctx, tx, a)
		return err
	})
}

// observation is one machine value for one column.
type observation struct {
	f   Field
	col *string
	v   string
}

// observeAll applies each observation under write rule 2 and returns the
// fields that changed.
func observeAll(manual []Field, obs []observation) []Field {
	var changed []Field
	for _, o := range obs {
		if observe(manual, o.f, o.col, o.v) {
			changed = append(changed, o.f)
		}
	}
	return changed
}

// EnrichContact re-queues the contact's job (and its company's) from done,
// failed or skipped: state = 'pending', run_at = now(), failures = 0.
// A pending or running job is left alone. Returns the resulting state.
// A free-mail contact stays skipped: context.dev answers it with a 4xx,
// and repeated 4xx can get a free key disabled.
func (s *Service) EnrichContact(ctx context.Context, actor Actor, id ContactID) (JobState, error) {
	var email Email
	var companyID *CompanyID
	err := s.db.QueryRow(ctx, `SELECT email, company_id FROM contacts WHERE id = $1`, id).Scan(&email, &companyID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("%w: contact %d", ErrNotFound, id)
	}
	if err != nil {
		return "", err
	}
	if companyID != nil {
		if _, err := s.requeue(ctx, "company_id", int64(*companyID), JobPending); err != nil {
			return "", err
		}
	}
	state := JobPending
	if email.Domain().IsFreeMail() {
		state = JobSkipped
	}
	return s.requeue(ctx, "contact_id", int64(id), state)
}

func (s *Service) EnrichCompany(ctx context.Context, actor Actor, id CompanyID) (JobState, error) {
	return s.requeue(ctx, "company_id", int64(id), JobPending)
}

// requeue moves the job for subject (contact_id or company_id) id to state,
// creating it if missing, unless it is pending or running.
func (s *Service) requeue(ctx context.Context, subject string, id int64, state JobState) (JobState, error) {
	var got JobState
	err := s.db.QueryRow(ctx, fmt.Sprintf(`
		INSERT INTO enrichment_jobs (%[1]s, state) VALUES ($1, $2)
		ON CONFLICT (%[1]s) DO UPDATE
		   SET state = EXCLUDED.state, run_at = now(), failures = 0, last_error = '', updated_at = now()
		 WHERE enrichment_jobs.state IN ('done', 'failed', 'skipped')
		RETURNING state`, subject), id, state).Scan(&got)
	if errors.Is(err, pgx.ErrNoRows) {
		err = s.db.QueryRow(ctx, fmt.Sprintf(`SELECT state FROM enrichment_jobs WHERE %s = $1`, subject), id).Scan(&got)
	}
	if pgCode(err) == foreignKeyViolation {
		return "", fmt.Errorf("%w: %s %d", ErrNotFound, strings.TrimSuffix(subject, "_id"), id)
	}
	return got, err
}
