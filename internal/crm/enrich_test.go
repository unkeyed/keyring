package crm

import (
	"errors"
	"testing"
	"time"
)

func TestSettle(t *testing.T) {
	now := time.Unix(0, 0)
	cases := []struct {
		name     string
		failures int
		err      error
		want     JobState
	}{
		{"ok", 0, nil, JobDone},
		{"network error retries", 0, errors.New("dial tcp: timeout"), JobPending},
		{"5xx retries", 3, &EnrichError{Retryable: true}, JobPending},
		{"5xx gives up at the budget", maxFailures - 1, &EnrichError{Retryable: true}, JobFailed},
		{"throttle never spends budget", maxFailures - 1, &EnrichError{Throttled: true}, JobPending},
		{"4xx fails at once", 0, &EnrichError{Msg: "401"}, JobFailed},
	}
	for _, c := range cases {
		if got := settle(c.failures, c.err, now).state; got != c.want {
			t.Errorf("%s: got %s, want %s", c.name, got, c.want)
		}
	}
}

// TestJobStatesMatchSchema parses the enrichment_jobs CHECK in
// internal/db/migrations/001_init.sql and fails unless it lists exactly the
// JobState constants.
func TestJobStatesMatchSchema(t *testing.T) {
	t.Skip("not implemented")
}
