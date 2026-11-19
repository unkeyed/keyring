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
// (network, timeout) is treated as Retryable.
type EnrichError struct {
	// Throttled: 429 or 402 quota. Retry at RetryAfter without spending
	// the failure budget; the request was never served.
	Throttled bool
	// Retryable: 5xx or 408. Retry with backoff, spending one failure.
	Retryable  bool
	RetryAfter time.Duration
	Msg        string
}

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
)

// claimSQL takes one due job. A running job whose lease expired is due again,
// which is the whole crash-recovery story. SKIP LOCKED keeps replicas from
// waiting on each other.
const claimSQL = `
UPDATE enrichment_jobs
   SET state = 'running', lease_until = now() + $1::interval, claims = claims + 1, updated_at = now()
 WHERE id = (
       SELECT id FROM enrichment_jobs
        WHERE (state = 'pending' AND run_at <= now())
           OR (state = 'running' AND lease_until < now())
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
}

// settle is the only place that decides where a running job goes next. Pure,
// so the state machine is tested without a database.
func settle(failures int, err error, now time.Time) settlement {
	if err == nil {
		return settlement{state: JobDone, runAt: now, failures: failures}
	}
	var ee *EnrichError
	if !errors.As(err, &ee) {
		ee = &EnrichError{Retryable: true, Msg: err.Error()}
	}
	switch {
	case ee.Throttled:
		wait := ee.RetryAfter
		if wait <= 0 {
			wait = time.Minute
		}
		return settlement{state: JobPending, runAt: now.Add(wait), failures: failures, lastErr: ee.Msg}
	case ee.Retryable && failures+1 < maxFailures:
		backoff := time.Minute << (2 * failures) // 1m, 4m, 16m, 64m
		return settlement{state: JobPending, runAt: now.Add(backoff), failures: failures + 1, lastErr: ee.Msg}
	default:
		return settlement{state: JobFailed, runAt: now, failures: failures + 1, lastErr: ee.Msg}
	}
}

// RunEnrichment claims and runs jobs until ctx is done. Every replica runs
// one loop, so in-flight calls to the enricher equal the replica count.
// With no enricher it blocks on ctx and touches nothing.
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
// transaction. The fenced UPDATE runs first; zero rows means the lease was
// lost, so the facts are discarded. Facts fill blank columns only, and an
// enriched activity lists the fields actually filled. The raw response goes
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
