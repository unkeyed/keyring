package crm

import (
	"context"
	"encoding/json"
	"errors"
	"time"
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
RETURNING id, contact_id, company_id, claims, failures`

// job is a claimed row. claims is the fencing token.
type job struct {
	id        int64
	contactID *ContactID
	companyID *CompanyID
	claims    int
	failures  int
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
	return errors.New("not implemented")
}

// finish writes the settlement and, on success, the facts, in one
// transaction. A settlement with halt upserts enrichment_halt with the
// error and now() + halt. The fenced UPDATE runs first; zero rows means the lease was
// lost, so the facts are discarded. Facts are written under the
// manual-values-win rule (package doc), and an enriched activity lists the
// fields that actually changed. The raw response goes
// to enrichment_jobs.response.
func (s *Service) finish(ctx context.Context, j job, person *PersonFacts, company *CompanyFacts, st settlement) error {
	return errors.New("not implemented")
}

// EnrichContact re-queues the contact's job (and its company's) from done,
// failed or skipped: state = 'pending', run_at = now(), failures = 0.
// A pending or running job is left alone. Returns the resulting state.
func (s *Service) EnrichContact(ctx context.Context, actor Actor, id ContactID) (JobState, error) {
	return "", errors.New("not implemented")
}

func (s *Service) EnrichCompany(ctx context.Context, actor Actor, id CompanyID) (JobState, error) {
	return "", errors.New("not implemented")
}
