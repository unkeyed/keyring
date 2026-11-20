package crm

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"

	"github.com/jackc/pgx/v5"
)

// ListActivity pages the timeline, newest first. An unknown Kind is
// ErrInvalid naming the kinds. list_recent_signups is ListActivity with Kind
// = ActivitySignedUp.
func (s *Service) ListActivity(ctx context.Context, q ActivityQuery) (Page[Activity], error) {
	var w where
	if q.Kind != "" {
		if !slices.Contains(ActivityKinds, q.Kind) {
			return Page[Activity]{}, fmt.Errorf("%w: unknown activity kind %q; the kinds are %s", ErrInvalid, q.Kind, joinNames(ActivityKinds))
		}
		w.add("a.kind = %s", w.arg(q.Kind))
	}
	if q.ContactID != 0 {
		w.add("a.contact_id = %s", w.arg(q.ContactID))
	}
	if q.CompanyID != 0 {
		w.add("(a.company_id = %[1]s OR a.contact_id IN (SELECT id FROM contacts WHERE company_id = %[1]s))", w.arg(q.CompanyID))
	}
	if !q.Since.IsZero() {
		w.add("a.occurred_at >= %s", w.arg(q.Since))
	}
	if !q.Before.IsZero() {
		w.add("a.occurred_at < %s", w.arg(q.Before))
	}
	if q.Cursor != "" {
		t, id, err := decodeCursor(q.Cursor)
		if err != nil {
			return Page[Activity]{}, err
		}
		w.add("(a.occurred_at, a.id) < (%s, %s)", w.arg(t), w.arg(id))
	}
	limit := pageLimit(q.Limit)
	rows, err := s.db.Query(ctx, `
		SELECT a.id, a.kind, c.id, c.email, c.name, co.id, co.domain, co.name,
		       a.actor_kind, a.actor_id, a.actor_label, a.data, a.occurred_at
		  FROM activities a
		  LEFT JOIN contacts c ON c.id = a.contact_id
		  LEFT JOIN companies co ON co.id = a.company_id`+w.String()+`
		 ORDER BY a.occurred_at DESC, a.id DESC LIMIT `+strconv.Itoa(limit+1), w.args...)
	if err != nil {
		return Page[Activity]{}, err
	}
	items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Activity, error) {
		var a Activity
		var contactID *ContactID
		var email Email
		var contactName *string
		var companyID *CompanyID
		var domain Domain
		var companyName *string
		err := r.Scan(&a.ID, &a.Kind, &contactID, &email, &contactName, &companyID, &domain, &companyName,
			&a.Actor.Kind, &a.Actor.ID, &a.Actor.Label, &a.Data, &a.OccurredAt)
		if contactID != nil {
			a.Contact = &ContactRef{ID: *contactID, Email: email, Name: *contactName}
		}
		if companyID != nil {
			a.Company = &CompanyRef{ID: *companyID, Domain: domain, Name: *companyName}
		}
		return a, err
	})
	if err != nil {
		return Page[Activity]{}, err
	}
	return pageOf(items, limit, func(a Activity) string { return encodeCursor(a.OccurredAt, a.ID) }), nil
}

// Status summarizes the instance for Settings and agents.
func (s *Service) Status(ctx context.Context) (Status, error) {
	st := Status{
		EnrichmentEnabled: s.enricher != nil,
		Jobs:              map[JobState]int{JobPending: 0, JobRunning: 0, JobDone: 0, JobFailed: 0, JobSkipped: 0},
	}
	err := s.db.QueryRow(ctx, `SELECT (SELECT count(*) FROM contacts), (SELECT count(*) FROM companies)`).
		Scan(&st.Contacts, &st.Companies)
	if err != nil {
		return st, err
	}
	rows, err := s.db.Query(ctx, `SELECT state, count(*) FROM enrichment_jobs GROUP BY state`)
	if err != nil {
		return st, err
	}
	var state JobState
	var n int
	if _, err := pgx.ForEachRow(rows, []any{&state, &n}, func() error {
		st.Jobs[state] = n
		return nil
	}); err != nil {
		return st, err
	}
	var h EnrichmentHalt
	err = s.db.QueryRow(ctx, `SELECT reason, until FROM enrichment_halt WHERE until > now()`).Scan(&h.Reason, &h.Until)
	switch {
	case err == nil:
		st.EnrichmentHalt = &h
	case !errors.Is(err, pgx.ErrNoRows):
		return st, err
	}
	return st, nil
}
