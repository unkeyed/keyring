// Package crm is uncrm. Every read and write of CRM data goes through
// Service, whoever the caller is: REST handlers, MCP tools, the UI (which
// calls the same REST routes), webhooks and the enrichment worker.
//
// Service owns its tables outright (see the header of 001_init.sql) and
// talks to Postgres with plain SQL through pgx. There is no repository
// layer: a method is the transaction, and the SQL sits in the method.
//
// Two write rules hold everywhere:
//
//  1. Every write is an upsert on a natural key, so running it twice
//     converges. Retries need no delivery log.
//
//  2. Manual values win; otherwise the newest observation wins. Humans and
//     agents (UpdateContact, UpdateCompany) overwrite a column and add its
//     name to the row's manual_fields in the same statement. Machine
//     writers (Ingest, enrichment) overwrite a column only when it is not in
//     manual_fields and the new value is non-empty, so a provider or
//     context.dev can correct stale data but never a person's edit. Every
//     machine write goes through observe, on a row held by lockContact or
//     lockCompany, so the check and the write see the same manual_fields.
//
//     ResetContactField and ResetCompanyField remove a name from
//     manual_fields, so the next machine observation shows again.
//
// Every write that a person would want on the timeline inserts one
// activities row in the same transaction. Nothing reads activities to
// decide state.
//
// Transactions lock rows in one order: contacts, then companies, then
// enrichment_jobs. That order is why the enrichment worker locks the
// contact before it fences its job row.
package crm

import (
	"errors"
	"reflect"
	"slices"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Errors every transport maps to a status code or tool error. Wrap them with
// fmt.Errorf("%w: detail", ErrInvalid) to add a message.
var (
	ErrNotFound        = errors.New("not found")
	ErrInvalid         = errors.New("invalid input")
	ErrConflict        = errors.New("conflict") // the thing to create already exists
	ErrUnknownProvider = errors.New("unknown provider")
	ErrNotConnected    = errors.New("provider not connected")
)

type Service struct {
	db       *pgxpool.Pool
	enricher Enricher // nil when CONTEXT_DEV_API_KEY is unset
	now      func() time.Time
}

// New returns the service. enricher may be nil: jobs then stay pending and
// run once a key is configured and the container restarts.
func New(db *pgxpool.Pool, enricher Enricher) *Service {
	return &Service{db: db, enricher: enricher, now: time.Now}
}

// observe is write rule 2 for one column: it sets *col to the machine value
// v unless f is manual or v is empty, and reports whether *col changed.
// T is a string, or a list or map for machine-only facts; an empty one is no
// observation.
func observe[T any](manual []Field, f Field, col *T, v T) bool {
	if reflect.ValueOf(v).Len() == 0 || reflect.DeepEqual(*col, v) || slices.Contains(manual, f) {
		return false
	}
	*col = v
	return true
}
