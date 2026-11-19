package crm

import (
	"context"
	"errors"

	"github.com/uncrm/uncrm/internal/signup"
)

// Ingest records one sign-up fact. The generic POST /v1/signups calls it
// directly; ReceiveWebhook calls it after verifying a provider delivery.
//
// Defaults for callers that know only an email (the generic endpoint):
// Source "" becomes "api", ExternalID "" becomes the normalized email,
// OccurredAt zero becomes now. So posting the same person twice records
// one sign-up. A non-provider actor may not use a registered provider's
// name as Source (ErrInvalid), so an API key cannot forge Clerk identities.
//
// One transaction, every statement idempotent:
//
//  1. Parse s.Email with ParseEmail. Empty or invalid on a non-Deleted
//     sign-up is ErrInvalid (webhooks turn that into ignored).
//  2. Resolve the contact, first match wins:
//     a. identities (s.Source, s.ExternalID)  -- the same provider user
//     b. contacts.email = email               -- another provider, same person
//     c. identities.email = email             -- someone whose provider email changed
//     d. INSERT contacts ... ON CONFLICT (email) DO UPDATE SET email = EXCLUDED.email
//     RETURNING id, (xmax = 0) AS created
//     (d) is also the race resolution: two providers delivering the same
//     new person at once both land on one row.
//     Deleted with no matching identity returns ErrNotFound (ignored upstream).
//  3. Write name and avatar_url on contacts under the manual-values-win rule
//     (package doc), but only when step 4's upsert applied, so a stale
//     webhook cannot overwrite a newer one. first_seen_at = LEAST,
//     last_seen_at = GREATEST.
//  4. Upsert identities ON CONFLICT (provider, external_id) DO UPDATE ...
//     WHERE identities.occurred_at <= EXCLUDED.occurred_at. contact_id is
//     never updated, so ingest never moves an identity between contacts.
//     Only MergeContacts does.
//     Deleted sets deleted_at instead.
//  5. Company: if the email domain is not free-mail and 'company' is not in
//     the contact's manual_fields, upsert companies ON CONFLICT (domain), set
//     contacts.company_id, and INSERT enrichment_jobs (company_id)
//     ON CONFLICT DO NOTHING.
//  6. INSERT enrichment_jobs (contact_id) ON CONFLICT DO NOTHING, with state
//     'skipped' for a free-mail address and 'pending' otherwise.
//  7. Created and Deleted insert an activity with dedupe_key
//     "signed_up:<source>:<external_id>" or "provider_deleted:<source>:<external_id>"
//     ON CONFLICT (dedupe_key) DO NOTHING. Duplicate = no row inserted.
//     Updated writes no activity; it only refreshes state.
//
// A provider retry, a dashboard replay, an Updated arriving before its
// Created, and two providers racing on one email all converge to one
// contact and at most one signed_up row per (source, external_id).
func (s *Service) Ingest(ctx context.Context, actor Actor, su signup.Signup) (IngestResult, error) {
	return IngestResult{}, errors.New("not implemented")
}

// ReceiveWebhook is the whole provider webhook path, so the HTTP handler
// only copies the request into a Delivery.
//
//	provider := signup.Lookup(name)          -> ErrUnknownProvider
//	secret   := provider_connections.secret  -> ErrNotConnected
//	provider.Verify(secret, d)               -> signup.ErrBadSignature
//	su := provider.Parse(d)                  -> signup.ErrIgnored returns nil
//	su.Source = provider.Name
//	s.Ingest(ctx, Actor{ActorProvider, name, provider.Title}, su)
//
// Every outcome updates provider_connections.last_delivery_at or
// last_error/last_error_at, so the Settings page shows "last delivery 2m ago"
// or "bad signature 30s ago" while the user is setting the webhook up.
func (s *Service) ReceiveWebhook(ctx context.Context, name string, d signup.Delivery) error {
	return errors.New("not implemented")
}
