package crm

import (
	"errors"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
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
	inGo := []string{string(JobPending), string(JobRunning), string(JobDone), string(JobFailed), string(JobSkipped)}
	slices.Sort(inSchema)
	slices.Sort(inGo)
	if !slices.Equal(inSchema, inGo) {
		t.Fatalf("schema allows %v, Go defines %v", inSchema, inGo)
	}
}
