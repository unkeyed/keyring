package crm

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/uncrm/uncrm/internal/signup"
)

// Ingest records one sign-up fact in one transaction. The generic
// POST /v1/signups calls it directly; ReceiveWebhook calls it after
// verifying a provider delivery.
//
// A caller that knows only an email gets defaults: Source "api", ExternalID
// the normalized email, OccurredAt now. So posting the same person twice
// records one sign-up. A non-provider actor may not use a registered
// provider's name as Source (ErrInvalid), so an API key cannot forge Clerk
// identities. An AvatarURL that is not an http(s) URL is ErrInvalid from an
// API caller and dropped from a provider webhook, which must not fail on it.
//
// The provider's raw payload is not stored: nothing reads it, and it is
// third-party text that every timeline read would hand to agents.
//
// A provider retry, a dashboard replay, an Updated arriving before its
// Created, and two providers racing on one email all converge to one
// contact and at most one signed_up activity per (source, external_id),
// because that pair is the activity's dedupe_key. Ingest never moves an
// identity between contacts; only MergeContacts does. A Deleted moves the
// identity's occurred_at forward, so an update older than the delete is
// stale.
func (s *Service) Ingest(ctx context.Context, actor Actor, su signup.Signup) (IngestResult, error) {
	if su.Source == "" {
		su.Source = "api"
	}
	if _, ok := signup.Lookup(su.Source); ok && (actor.Kind != ActorProvider || actor.ID != su.Source) {
		return IngestResult{}, fmt.Errorf("%w: source %q is a sign-up provider; connect it under Settings > Providers instead", ErrInvalid, su.Source)
	}
	if su.Kind != signup.Created && su.Kind != signup.Updated && su.Kind != signup.Deleted {
		return IngestResult{}, fmt.Errorf("%w: sign-up kind %q", ErrInvalid, su.Kind)
	}
	email, err := ParseEmail(su.Email)
	if err != nil && su.Kind != signup.Deleted {
		return IngestResult{}, err
	}
	if su.ExternalID == "" {
		if email.IsZero() {
			return IngestResult{}, fmt.Errorf("%w: external_id or email is required", ErrInvalid)
		}
		su.ExternalID = email.String()
	}
	if su.OccurredAt.IsZero() {
		su.OccurredAt = s.now()
	}
	if su.AvatarURL, err = webURL(fieldAvatarURL, su.AvatarURL); err != nil {
		if actor.Kind != ActorProvider {
			return IngestResult{}, err
		}
		su.AvatarURL = ""
	}

	var res IngestResult
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var err error
		if su.Kind == signup.Deleted {
			res, err = ingestDeleted(ctx, tx, actor, su)
		} else {
			res, err = ingestSignup(ctx, tx, actor, su, email)
		}
		return err
	})
	return res, err
}

func ingestSignup(ctx context.Context, tx pgx.Tx, actor Actor, su signup.Signup, email Email) (IngestResult, error) {
	id, created, err := resolveContact(ctx, tx, su.Source, su.ExternalID, email, su.OccurredAt)
	if err != nil {
		return IngestResult{}, err
	}
	res := IngestResult{ContactID: id, Created: created}

	var applied bool
	err = tx.QueryRow(ctx, `
		INSERT INTO identities (provider, external_id, contact_id, email, email_verified, name, avatar_url, occurred_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (provider, external_id) DO UPDATE
		   SET email = EXCLUDED.email, email_verified = EXCLUDED.email_verified, name = EXCLUDED.name,
		       avatar_url = EXCLUDED.avatar_url, occurred_at = EXCLUDED.occurred_at
		 WHERE identities.occurred_at <= EXCLUDED.occurred_at
		RETURNING true`,
		su.Source, su.ExternalID, id, email, su.EmailVerified, strings.TrimSpace(su.Name), su.AvatarURL, su.OccurredAt).
		Scan(&applied)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return res, err
	}

	c, err := lockContact(ctx, tx, id)
	if err != nil {
		return res, err
	}
	c.firstSeen = minTime(c.firstSeen, su.OccurredAt)
	c.lastSeen = maxTime(c.lastSeen, su.OccurredAt)
	// The identity upsert skips a fact older than the stored one, so a stale
	// webhook leaves the contact's fields and company alone too.
	if applied {
		observe(c.manual, FieldName, &c.name, strings.TrimSpace(su.Name))
		observe(c.manual, fieldAvatarURL, &c.avatarURL, su.AvatarURL)
		if d := email.Domain(); !d.IsFreeMail() && !slices.Contains(c.manual, FieldCompany) {
			coID, err := upsertCompany(ctx, tx, d)
			if err != nil {
				return res, err
			}
			c.companyID = &coID
		}
	}
	if err := writeContact(ctx, tx, c); err != nil {
		return res, err
	}

	if created {
		state := JobPending
		if email.Domain().IsFreeMail() {
			state = JobSkipped
		}
		if _, err := tx.Exec(ctx, `INSERT INTO enrichment_jobs (contact_id, state) VALUES ($1, $2)`, id, state); err != nil {
			return res, err
		}
	}

	if su.Kind == signup.Created {
		inserted, err := recordActivity(ctx, tx, activityRow{
			contact: id, kind: ActivitySignedUp, actor: actor,
			dedupe: "signed_up:" + su.Source + ":" + su.ExternalID,
			data:   map[string]string{"provider": su.Source, "external_id": su.ExternalID},
			at:     su.OccurredAt,
		})
		res.Duplicate = !inserted
		return res, err
	}
	return res, nil
}

// resolveContact finds the sign-up's contact, first match wins: the same
// provider user, then a contact with this email from another provider, then
// the contact of the newest identity with this email, which catches a person
// whose provider email changed. Otherwise it inserts one, and ON CONFLICT
// (email) settles two providers racing on one new person. The returned contact row is locked for the rest of the
// transaction.
func resolveContact(ctx context.Context, tx pgx.Tx, source, externalID string, email Email, at time.Time) (ContactID, bool, error) {
	lookups := []struct {
		sql  string
		args []any
	}{
		{`SELECT c.id FROM identities i JOIN contacts c ON c.id = i.contact_id
		   WHERE i.provider = $1 AND i.external_id = $2 FOR NO KEY UPDATE OF c`, []any{source, externalID}},
		{`SELECT id FROM contacts WHERE email = $1 FOR NO KEY UPDATE`, []any{email}},
		{`SELECT c.id FROM identities i JOIN contacts c ON c.id = i.contact_id
		   WHERE i.email = $1 ORDER BY i.occurred_at DESC LIMIT 1 FOR NO KEY UPDATE OF c`, []any{email}},
	}
	var id ContactID
	for _, l := range lookups {
		err := tx.QueryRow(ctx, l.sql, l.args...).Scan(&id)
		if !errors.Is(err, pgx.ErrNoRows) {
			return id, false, err
		}
	}
	var created bool
	err := tx.QueryRow(ctx, `
		INSERT INTO contacts (email, stage, first_seen_at, last_seen_at)
		VALUES ($1, (SELECT name FROM stages ORDER BY position, name LIMIT 1), $2, $2)
		ON CONFLICT (email) DO UPDATE SET email = EXCLUDED.email
		RETURNING id, xmax = 0`, email, at).Scan(&id, &created)
	return id, created, err
}

// ingestDeleted marks the provider identity deleted. The contact stays: it
// may have other identities, notes and history.
func ingestDeleted(ctx context.Context, tx pgx.Tx, actor Actor, su signup.Signup) (IngestResult, error) {
	var id ContactID
	err := tx.QueryRow(ctx, `
		SELECT c.id FROM identities i JOIN contacts c ON c.id = i.contact_id
		 WHERE i.provider = $1 AND i.external_id = $2 FOR NO KEY UPDATE OF c`, su.Source, su.ExternalID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return IngestResult{}, fmt.Errorf("%w: no %s user %q", ErrNotFound, su.Source, su.ExternalID)
	}
	if err != nil {
		return IngestResult{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE identities SET deleted_at = COALESCE(deleted_at, $3), occurred_at = GREATEST(occurred_at, $3)
		 WHERE provider = $1 AND external_id = $2`, su.Source, su.ExternalID, su.OccurredAt); err != nil {
		return IngestResult{}, err
	}
	inserted, err := recordActivity(ctx, tx, activityRow{
		contact: id, kind: ActivityProviderDeleted, actor: actor,
		dedupe: "provider_deleted:" + su.Source + ":" + su.ExternalID,
		data:   map[string]string{"provider": su.Source, "external_id": su.ExternalID},
		at:     su.OccurredAt,
	})
	return IngestResult{ContactID: id, Duplicate: !inserted}, err
}

// ReceiveWebhook is the whole provider webhook path, so the HTTP handler
// only copies the request into a Delivery.
//
// Every outcome updates provider_connections.last_delivery_at or
// last_error/last_error_at, so the Settings page shows "last delivery 2m ago"
// or "bad signature 30s ago" while the user is setting the webhook up.
func (s *Service) ReceiveWebhook(ctx context.Context, name string, d signup.Delivery) error {
	p, ok := signup.Lookup(name)
	if !ok {
		return fmt.Errorf("%w: %q", ErrUnknownProvider, name)
	}
	var secret string
	err := s.db.QueryRow(ctx, `SELECT secret FROM provider_connections WHERE provider = $1`, name).Scan(&secret)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: connect %s under Settings > Providers", ErrNotConnected, p.Title)
	}
	if err != nil {
		return err
	}
	err = s.deliver(ctx, p, secret, d)
	if err == nil {
		_, err = s.db.Exec(ctx, `
			UPDATE provider_connections SET last_delivery_at = now(), last_error = '', last_error_at = NULL
			 WHERE provider = $1`, name)
		return err
	}
	_, bookErr := s.db.Exec(ctx, `
		UPDATE provider_connections SET last_error = $2, last_error_at = now() WHERE provider = $1`, name, err.Error())
	return errors.Join(err, bookErr)
}

// deliver verifies, parses and ingests one delivery. A delivery that
// verifies but carries nothing to store (ErrIgnored, no usable email, a
// delete for a user never seen) is a success, so the provider stops retrying.
func (s *Service) deliver(ctx context.Context, p signup.Provider, secret string, d signup.Delivery) error {
	if err := p.Verify(secret, d); err != nil {
		if errors.Is(err, signup.ErrBadSignature) {
			return err
		}
		return fmt.Errorf("%w: %v", signup.ErrBadSignature, err)
	}
	su, err := p.Parse(d)
	if errors.Is(err, signup.ErrIgnored) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("%w: could not parse the %s delivery: %v", ErrInvalid, p.Title, err)
	}
	su.Source = p.Name
	_, err = s.Ingest(ctx, Actor{Kind: ActorProvider, ID: p.Name, Label: p.Title}, su)
	if errors.Is(err, ErrInvalid) || errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}
