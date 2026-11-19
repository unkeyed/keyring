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
//  2. Machine writers (Ingest, enrichment) only fill blank columns.
//     Humans and agents (UpdateContact, UpdateCompany) overwrite.
//     Fill-blank commutes, so concurrent machine writers cannot clobber
//     each other or a human edit. In SQL every machine write is:
//
//     SET col = CASE WHEN length(col) = 0 THEN $new ELSE col END
//
// Every write that a person would want on the timeline inserts one
// activities row in the same transaction. Nothing reads activities to
// decide state.
package crm

import (
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Errors every transport maps to a status code or tool error. Wrap them with
// fmt.Errorf("%w: detail", ErrInvalid) to add a message.
var (
	ErrNotFound        = errors.New("not found")
	ErrInvalid         = errors.New("invalid input")
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
