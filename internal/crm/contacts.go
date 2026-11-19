package crm

import (
	"context"
	"errors"
)

// SearchContacts filters and pages contacts, newest first_seen_at first.
// Text is ILIKE over contacts.email, contacts.name, companies.name and
// companies.domain. No trigram index in v1: sequential scans are fine below
// ~100k contacts, and CREATE EXTENSION fails on some managed Postgres plans.
func (s *Service) SearchContacts(ctx context.Context, q ContactQuery) (Page[Contact], error) {
	return Page[Contact]{}, errors.New("not implemented")
}

// GetContact returns the contact with identities, notes and the newest 50
// activities. ErrNotFound when the id does not exist.
func (s *Service) GetContact(ctx context.Context, id ContactID) (ContactDetail, error) {
	return ContactDetail{}, errors.New("not implemented")
}

// FindContactByEmail parses email and returns the matching contact, checking
// contacts.email and then identities.email. Agents usually know an email,
// not an id.
func (s *Service) FindContactByEmail(ctx context.Context, email string) (ContactDetail, error) {
	return ContactDetail{}, errors.New("not implemented")
}

// UpdateContact applies a human or agent edit. Non-nil fields overwrite and
// are added to manual_fields, so later sign-ups and enrichment leave them
// alone. Stage and tags are never machine-written, so they need no entry.
// An unknown Stage is ErrInvalid (foreign key violation 23503 mapped here).
// Writes one contact_updated activity whose data is the field diff; a patch
// that changes nothing writes nothing.
func (s *Service) UpdateContact(ctx context.Context, actor Actor, id ContactID, p ContactPatch) (Contact, error) {
	return Contact{}, errors.New("not implemented")
}

// ResetContactField removes f from the contact's manual_fields and writes a
// contact_updated activity. The column keeps its value until the next
// machine observation (a provider update or a re-enrich) replaces it.
// A field the contact does not accept is ErrInvalid; one that is not manual
// is a no-op.
func (s *Service) ResetContactField(ctx context.Context, actor Actor, id ContactID, f Field) (Contact, error) {
	return Contact{}, errors.New("not implemented")
}

// AddNote appends a note and a note_added activity.
func (s *Service) AddNote(ctx context.Context, actor Actor, id ContactID, body string) (Note, error) {
	return Note{}, errors.New("not implemented")
}

// DeleteNote removes a note. The note_added activity stays, with its body gone.
func (s *Service) DeleteNote(ctx context.Context, actor Actor, id NoteID) error {
	return errors.New("not implemented")
}

// ListTags returns every tag in use with its contact count.
func (s *Service) ListTags(ctx context.Context) (map[Tag]int, error) {
	return nil, errors.New("not implemented")
}

// ListStages returns stages in position order.
func (s *Service) ListStages(ctx context.Context) ([]Stage, error) {
	return nil, errors.New("not implemented")
}

// SetStages replaces the ordered stage list. renames maps old name to new
// name so contacts follow (ON UPDATE CASCADE). Removing a stage that
// contacts still use is ErrInvalid.
func (s *Service) SetStages(ctx context.Context, actor Actor, stages []Stage, renames map[Stage]Stage) error {
	return errors.New("not implemented")
}
