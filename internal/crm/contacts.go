package crm

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// ContactFields are the fields a person or agent can pin on a contact. The
// contacts.manual_fields CHECK lists the same names.
var ContactFields = []Field{FieldName, FieldTitle, FieldLocation, FieldBio, FieldLinkedInURL, FieldCompany}

// fieldAvatarURL names contacts.avatar_url for observe. Only machines write
// it, so it is never in manual_fields.
const fieldAvatarURL Field = "avatar_url"

const contactSelect = `
SELECT c.id, c.email, c.name, c.avatar_url, c.title, c.location, c.bio, c.linkedin_url, c.socials,
       co.id, co.domain, co.name, c.stage,
       ARRAY(SELECT t.tag FROM contact_tags t WHERE t.contact_id = c.id ORDER BY t.tag),
       ARRAY(SELECT t.tag FROM company_tags t WHERE t.company_id = c.company_id ORDER BY t.tag),
       ARRAY(SELECT DISTINCT i.provider FROM identities i WHERE i.contact_id = c.id ORDER BY i.provider),
       COALESCE(j.state, ''), c.manual_fields, c.first_seen_at, c.last_seen_at, c.created_at, c.updated_at
  FROM contacts c
  LEFT JOIN companies co ON co.id = c.company_id
  LEFT JOIN enrichment_jobs j ON j.contact_id = c.id`

func scanContact(row pgx.Row) (Contact, error) {
	var c Contact
	var coID *CompanyID
	var coDomain Domain
	var coName *string
	err := row.Scan(&c.ID, &c.Email, &c.Name, &c.AvatarURL, &c.Title, &c.Location, &c.Bio, &c.LinkedInURL, &c.Socials,
		&coID, &coDomain, &coName, &c.Stage, &c.Tags, &c.CompanyTags, &c.Providers, &c.Enrichment, &c.ManualFields,
		&c.FirstSeenAt, &c.LastSeenAt, &c.CreatedAt, &c.UpdatedAt)
	if coID != nil {
		c.Company = &CompanyRef{ID: *coID, Domain: coDomain, Name: *coName}
	}
	return c, err
}

func (s *Service) contact(ctx context.Context, id ContactID) (Contact, error) {
	c, err := scanContact(s.db.QueryRow(ctx, contactSelect+` WHERE c.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return c, notFound(RecordContact, "no contact %d", id)
	}
	return c, err
}

func (s *Service) contacts(ctx context.Context, sql string, args ...any) ([]Contact, error) {
	rows, err := s.db.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Contact, error) { return scanContact(r) })
}

// contactRow is a contact's writable columns, read under a row lock so a
// write computed from it cannot lose a concurrent one.
type contactRow struct {
	id                                                 ContactID
	email                                              Email
	name, avatarURL, title, location, bio, linkedInURL string
	socials                                            Socials
	companyID                                          *CompanyID
	stage                                              Stage
	manual                                             []Field
	firstSeen, lastSeen                                time.Time
}

func lockContact(ctx context.Context, tx pgx.Tx, id ContactID) (contactRow, error) {
	c := contactRow{id: id}
	err := tx.QueryRow(ctx, `
		SELECT email, name, avatar_url, title, location, bio, linkedin_url, socials, company_id, stage,
		       manual_fields, first_seen_at, last_seen_at
		  FROM contacts WHERE id = $1 FOR NO KEY UPDATE`, id).
		Scan(&c.email, &c.name, &c.avatarURL, &c.title, &c.location, &c.bio, &c.linkedInURL, &c.socials, &c.companyID, &c.stage,
			&c.manual, &c.firstSeen, &c.lastSeen)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, notFound(RecordContact, "no contact %d", id)
	}
	return c, err
}

func writeContact(ctx context.Context, tx pgx.Tx, c contactRow) error {
	_, err := tx.Exec(ctx, `
		UPDATE contacts
		   SET name = $2, avatar_url = $3, title = $4, location = $5, bio = $6, linkedin_url = $7,
		       company_id = $8, stage = $9, manual_fields = $10, first_seen_at = $11, last_seen_at = $12,
		       socials = $13, updated_at = now()
		 WHERE id = $1`,
		c.id, c.name, c.avatarURL, c.title, c.location, c.bio, c.linkedInURL,
		c.companyID, c.stage, fieldNames(c.manual), c.firstSeen, c.lastSeen, c.socials)
	if pgCode(err) == foreignKeyViolation {
		return fmt.Errorf("%w: unknown stage %q", ErrInvalid, c.stage)
	}
	return err
}

func fieldNames(fs []Field) []string {
	out := make([]string, len(fs))
	for i, f := range fs {
		out[i] = string(f)
	}
	return out
}

func addField(fs []Field, f Field) []Field {
	if slices.Contains(fs, f) {
		return fs
	}
	return append(fs, f)
}

// SearchContacts filters and pages contacts, newest first_seen_at first.
// Text is ILIKE over contacts.email, contacts.name, companies.name and
// companies.domain. No trigram index in v1: sequential scans are fine below
// ~100k contacts, and CREATE EXTENSION fails on some managed Postgres plans.
func (s *Service) SearchContacts(ctx context.Context, q ContactQuery) (Page[Contact], error) {
	var w where
	if text := strings.TrimSpace(q.Text); text != "" {
		p := w.arg(contains(text))
		w.add("(c.email ILIKE %[1]s OR c.name ILIKE %[1]s OR co.name ILIKE %[1]s OR co.domain ILIKE %[1]s)", p)
	}
	if q.Email != "" {
		e, err := ParseEmail(q.Email)
		if err != nil {
			return Page[Contact]{}, err
		}
		w.add("(c.email = %[1]s OR EXISTS (SELECT 1 FROM identities i WHERE i.contact_id = c.id AND i.email = %[1]s))", w.arg(e))
	}
	if tag, err := ParseTag(string(q.Tag)); err == nil {
		w.add(`(EXISTS (SELECT 1 FROM contact_tags t WHERE t.contact_id = c.id AND t.tag = %[1]s)
			OR EXISTS (SELECT 1 FROM company_tags t WHERE t.company_id = c.company_id AND t.tag = %[1]s))`, w.arg(tag))
	}
	if q.Stage != "" {
		if err := s.checkStage(ctx, q.Stage); err != nil {
			return Page[Contact]{}, err
		}
		w.add("c.stage = %s", w.arg(q.Stage))
	}
	if q.Provider != "" {
		w.add("EXISTS (SELECT 1 FROM identities i WHERE i.contact_id = c.id AND i.provider = %s)", w.arg(q.Provider))
	}
	if q.CompanyID != 0 {
		w.add("c.company_id = %s", w.arg(q.CompanyID))
	}
	if !q.SignedUpAfter.IsZero() {
		w.add("c.first_seen_at >= %s", w.arg(q.SignedUpAfter))
	}
	if !q.SignedUpBefore.IsZero() {
		w.add("c.first_seen_at < %s", w.arg(q.SignedUpBefore))
	}
	if q.Cursor != "" {
		t, id, err := decodeCursor(q.Cursor)
		if err != nil {
			return Page[Contact]{}, err
		}
		w.add("(c.first_seen_at, c.id) < (%s, %s)", w.arg(t), w.arg(id))
	}
	limit := pageLimit(q.Limit)
	items, err := s.contacts(ctx, contactSelect+w.String()+
		" ORDER BY c.first_seen_at DESC, c.id DESC LIMIT "+strconv.Itoa(limit+1), w.args...)
	if err != nil {
		return Page[Contact]{}, err
	}
	return pageOf(items, limit, func(c Contact) string { return encodeCursor(c.FirstSeenAt, int64(c.ID)) }), nil
}

// GetContact returns the contact with identities, notes and the newest 50
// activities. ErrNotFound when the id does not exist.
func (s *Service) GetContact(ctx context.Context, id ContactID) (ContactDetail, error) {
	c, err := s.contact(ctx, id)
	if err != nil {
		return ContactDetail{}, err
	}
	d := ContactDetail{Contact: c}

	rows, err := s.db.Query(ctx, `
		SELECT provider, external_id, email, email_verified, name, avatar_url, occurred_at, deleted_at
		  FROM identities WHERE contact_id = $1 ORDER BY created_at, provider`, id)
	if err != nil {
		return d, err
	}
	d.Identities, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (Identity, error) {
		var i Identity
		err := r.Scan(&i.Provider, &i.ExternalID, &i.Email, &i.EmailVerified, &i.Name, &i.AvatarURL, &i.OccurredAt, &i.DeletedAt)
		return i, err
	})
	if err != nil {
		return d, err
	}

	rows, err = s.db.Query(ctx, `
		SELECT id, contact_id, body, author_kind, author_id, author_label, created_at
		  FROM notes WHERE contact_id = $1 ORDER BY created_at DESC, id DESC`, id)
	if err != nil {
		return d, err
	}
	d.Notes, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (Note, error) {
		var n Note
		err := r.Scan(&n.ID, &n.ContactID, &n.Body, &n.Author.Kind, &n.Author.ID, &n.Author.Label, &n.CreatedAt)
		return n, err
	})
	if err != nil {
		return d, err
	}

	activity, err := s.ListActivity(ctx, ActivityQuery{ContactID: id, Limit: 50})
	d.Activity = activity.Items
	return d, err
}

// FindContactByEmail parses email and returns the matching contact, checking
// contacts.email and then identities.email. Agents usually know an email,
// not an id.
func (s *Service) FindContactByEmail(ctx context.Context, email string) (ContactDetail, error) {
	e, err := ParseEmail(email)
	if err != nil {
		return ContactDetail{}, err
	}
	var id ContactID
	err = s.db.QueryRow(ctx, `
		SELECT id FROM (
		  SELECT id, 0 AS rank FROM contacts WHERE email = $1
		  UNION ALL
		  SELECT contact_id, 1 FROM identities WHERE email = $1
		) m ORDER BY rank LIMIT 1`, e).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return ContactDetail{}, notFound(RecordContact, "no contact with email %s", e)
	}
	if err != nil {
		return ContactDetail{}, err
	}
	return s.GetContact(ctx, id)
}

// CreateContact adds a person by hand, such as someone met at an event who
// never signed up. The company comes from the email domain as it does for a
// sign-up (none for free mail), and the contact and a new company are queued
// for enrichment. Given fields are manual. An email that already resolves to
// a contact, directly or through an identity, is an *ExistsError naming it.
// An unknown Stage or a LinkedInURL that is not a web address is ErrInvalid.
// Writes a contact_created activity.
func (s *Service) CreateContact(ctx context.Context, actor Actor, in NewContact) (Contact, error) {
	email, err := ParseEmail(in.Email)
	if err != nil {
		return Contact{}, err
	}
	tags, err := parseTags(in.Tags)
	if err != nil {
		return Contact{}, err
	}
	if in.LinkedInURL, err = webURL(FieldLinkedInURL, in.LinkedInURL); err != nil {
		return Contact{}, err
	}
	if in.Stage != "" {
		if err := s.checkStage(ctx, in.Stage); err != nil {
			return Contact{}, err
		}
	}
	var id ContactID
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
			INSERT INTO contacts (email, stage, first_seen_at, last_seen_at)
			SELECT $1, COALESCE(NULLIF($2, ''), (SELECT name FROM stages ORDER BY position, name LIMIT 1)), $3, $3
			 WHERE NOT EXISTS (SELECT 1 FROM identities WHERE email = $1)
			ON CONFLICT (email) DO NOTHING RETURNING id`, email, in.Stage, s.now()).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			err = tx.QueryRow(ctx, `
				SELECT id FROM contacts WHERE email = $1
				UNION ALL SELECT contact_id FROM identities WHERE email = $1 LIMIT 1`, email).Scan(&id)
			if err != nil {
				return err
			}
			return &ExistsError{Record: RecordContact, ID: int64(id), Key: email.String()}
		}
		if pgCode(err) == foreignKeyViolation {
			return fmt.Errorf("%w: unknown stage %q", ErrInvalid, in.Stage)
		}
		if err != nil {
			return err
		}
		c, err := lockContact(ctx, tx, id)
		if err != nil {
			return err
		}
		for _, e := range []struct {
			f   Field
			col *string
			v   string
		}{
			{FieldName, &c.name, in.Name},
			{FieldTitle, &c.title, in.Title},
			{FieldLocation, &c.location, in.Location},
			{FieldLinkedInURL, &c.linkedInURL, in.LinkedInURL},
		} {
			if v := strings.TrimSpace(e.v); v != "" {
				*e.col = v
				c.manual = append(c.manual, e.f)
			}
		}
		state := JobSkipped
		if d := email.Domain(); !d.IsFreeMail() {
			state = JobPending
			coID, err := upsertCompany(ctx, tx, d)
			if err != nil {
				return err
			}
			c.companyID = &coID
		}
		if err := writeContact(ctx, tx, c); err != nil {
			return err
		}
		if _, err := changeTags(ctx, tx, contactTags, int64(id), tags, nil); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO enrichment_jobs (contact_id, state) VALUES ($1, $2)`, id, state); err != nil {
			return err
		}
		_, err = recordActivity(ctx, tx, activityRow{contact: id, kind: ActivityContactCreated, actor: actor, data: struct{}{}, at: s.now()})
		return err
	})
	if err != nil {
		return Contact{}, err
	}
	return s.contact(ctx, id)
}

// UpdateContact applies a human or agent edit. Non-nil fields overwrite and
// are added to manual_fields, so later sign-ups and enrichment leave them
// alone. Stage and tags are never machine-written, so they need no entry.
// An unknown Stage or a LinkedInURL that is not a web address is ErrInvalid.
// Writes one contact_updated activity whose data is the field diff; a patch
// that changes nothing writes nothing.
func (s *Service) UpdateContact(ctx context.Context, actor Actor, id ContactID, p ContactPatch) (Contact, error) {
	add, err := parseTags(p.AddTags)
	if err != nil {
		return Contact{}, err
	}
	remove, err := parseTags(p.RemoveTags)
	if err != nil {
		return Contact{}, err
	}
	if p.CompanyDomain != nil && p.CompanyDomain.IsFreeMail() {
		return Contact{}, fmt.Errorf("%w: %s is a free-mail domain, not a company", ErrInvalid, p.CompanyDomain)
	}
	if p.LinkedInURL != nil {
		u, err := webURL(FieldLinkedInURL, *p.LinkedInURL)
		if err != nil {
			return Contact{}, err
		}
		p.LinkedInURL = &u
	}
	if p.Stage != nil {
		if err := s.checkStage(ctx, *p.Stage); err != nil {
			return Contact{}, err
		}
	}
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		c, err := lockContact(ctx, tx, id)
		if err != nil {
			return err
		}
		before := len(c.manual)
		diff := map[string]any{}
		for _, e := range []struct {
			f   Field
			col *string
			v   *string
		}{
			{FieldName, &c.name, p.Name},
			{FieldTitle, &c.title, p.Title},
			{FieldLocation, &c.location, p.Location},
			{FieldBio, &c.bio, p.Bio},
			{FieldLinkedInURL, &c.linkedInURL, p.LinkedInURL},
		} {
			if e.v == nil {
				continue
			}
			c.manual = addField(c.manual, e.f)
			if v := strings.TrimSpace(*e.v); v != *e.col {
				diff[string(e.f)] = change{*e.col, v}
				*e.col = v
			}
		}
		if p.Stage != nil && *p.Stage != c.stage {
			diff["stage"] = change{string(c.stage), string(*p.Stage)}
			c.stage = *p.Stage
		}
		if p.CompanyDomain != nil {
			c.manual = addField(c.manual, FieldCompany)
			var to *CompanyID
			if !p.CompanyDomain.IsZero() {
				coID, err := upsertCompany(ctx, tx, *p.CompanyDomain)
				if err != nil {
					return err
				}
				to = &coID
			}
			if !equalID(c.companyID, to) {
				from, err := companyDomain(ctx, tx, c.companyID)
				if err != nil {
					return err
				}
				diff["company"] = change{from.String(), p.CompanyDomain.String()}
				c.companyID = to
			}
		}
		if len(diff) > 0 || len(c.manual) != before {
			if err := writeContact(ctx, tx, c); err != nil {
				return err
			}
		}
		if tags, err := changeTags(ctx, tx, contactTags, int64(id), add, remove); err != nil {
			return err
		} else if tags != nil {
			diff["tags"] = tags
		}
		if len(diff) == 0 {
			return nil
		}
		_, err = recordActivity(ctx, tx, activityRow{contact: id, kind: ActivityContactUpdated, actor: actor, data: diff, at: s.now()})
		return err
	})
	if err != nil {
		return Contact{}, err
	}
	return s.contact(ctx, id)
}

func parseTags(in []Tag) ([]string, error) {
	out := make([]string, 0, len(in))
	for _, t := range in {
		tag, err := ParseTag(string(t))
		if err != nil {
			return nil, err
		}
		out = append(out, string(tag))
	}
	return out, nil
}

type tagChange struct {
	Added   []string `json:"added,omitempty"`
	Removed []string `json:"removed,omitempty"`
}

// tagTable is a tag table and the column naming the tagged row.
type tagTable struct{ table, key string }

var (
	contactTags = tagTable{"contact_tags", "contact_id"}
	companyTags = tagTable{"company_tags", "company_id"}
)

// changeTags applies tag set changes to row id of t and returns the ones
// that took effect, or nil when none did.
func changeTags(ctx context.Context, tx pgx.Tx, t tagTable, id int64, add, remove []string) (*tagChange, error) {
	var tc tagChange
	if len(add) > 0 {
		rows, err := tx.Query(ctx, `
			INSERT INTO `+t.table+` (`+t.key+`, tag) SELECT $1, unnest($2::text[])
			ON CONFLICT DO NOTHING RETURNING tag`, id, add)
		if err != nil {
			return nil, err
		}
		if tc.Added, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil {
			return nil, err
		}
	}
	if len(remove) > 0 {
		rows, err := tx.Query(ctx, `DELETE FROM `+t.table+` WHERE `+t.key+` = $1 AND tag = ANY($2) RETURNING tag`, id, remove)
		if err != nil {
			return nil, err
		}
		if tc.Removed, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil {
			return nil, err
		}
	}
	if len(tc.Added) == 0 && len(tc.Removed) == 0 {
		return nil, nil
	}
	return &tc, nil
}

func equalID(a, b *CompanyID) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

func companyDomain(ctx context.Context, tx pgx.Tx, id *CompanyID) (Domain, error) {
	var d Domain
	if id == nil {
		return d, nil
	}
	err := tx.QueryRow(ctx, `SELECT domain FROM companies WHERE id = $1`, *id).Scan(&d)
	return d, err
}

// ResetContactField removes f from the contact's manual_fields and writes a
// contact_updated activity. The column keeps its value until the next
// machine observation (a provider update or a re-enrich) replaces it.
// A field the contact does not accept is ErrInvalid; one that is not manual
// is a no-op.
func (s *Service) ResetContactField(ctx context.Context, actor Actor, id ContactID, f Field) (Contact, error) {
	if !slices.Contains(ContactFields, f) {
		return Contact{}, fmt.Errorf("%w: contacts have no field %q; the fields are %s", ErrInvalid, f, joinNames(ContactFields))
	}
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		c, err := lockContact(ctx, tx, id)
		if err != nil || !slices.Contains(c.manual, f) {
			return err
		}
		c.manual = slices.DeleteFunc(c.manual, func(m Field) bool { return m == f })
		if err := writeContact(ctx, tx, c); err != nil {
			return err
		}
		_, err = recordActivity(ctx, tx, activityRow{contact: id, kind: ActivityContactUpdated, actor: actor,
			data: map[string]Field{"reset": f}, at: s.now()})
		return err
	})
	if err != nil {
		return Contact{}, err
	}
	return s.contact(ctx, id)
}

// MergeContacts folds drop into keep, for one person that identity
// resolution saw as two (a work and a personal address). A person or agent
// asks for it; nothing merges automatically. In one transaction it moves
// drop's identities, notes, tags and activities to keep, copies each of
// drop's non-empty columns that keep has empty (and drop's manual_fields
// with them), keeps the earlier first_seen_at and later last_seen_at,
// deletes drop, and writes a contacts_merged activity on keep. Drop's email
// stays on its identities, so a later sign-up with it resolves to keep
// (see resolveContact). keep == drop is ErrInvalid.
func (s *Service) MergeContacts(ctx context.Context, actor Actor, keep, drop ContactID) (Contact, error) {
	if keep == drop {
		return Contact{}, fmt.Errorf("%w: cannot merge a contact into itself", ErrInvalid)
	}
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		// FOR UPDATE, not NO KEY UPDATE: drop is deleted below. Locking in
		// id order keeps two crossed merges from deadlocking.
		if _, err := tx.Exec(ctx, `SELECT 1 FROM contacts WHERE id IN ($1, $2) ORDER BY id FOR UPDATE`, keep, drop); err != nil {
			return err
		}
		k, err := lockContact(ctx, tx, keep)
		if err != nil {
			return err
		}
		d, err := lockContact(ctx, tx, drop)
		if err != nil {
			return err
		}
		for _, e := range []struct {
			f        Field
			dst, src *string
		}{
			{FieldName, &k.name, &d.name},
			{fieldAvatarURL, &k.avatarURL, &d.avatarURL},
			{FieldTitle, &k.title, &d.title},
			{FieldLocation, &k.location, &d.location},
			{FieldBio, &k.bio, &d.bio},
			{FieldLinkedInURL, &k.linkedInURL, &d.linkedInURL},
		} {
			if *e.dst == "" && *e.src != "" {
				*e.dst = *e.src
				if slices.Contains(d.manual, e.f) {
					k.manual = addField(k.manual, e.f)
				}
			}
		}
		if len(k.socials) == 0 {
			k.socials = d.socials
		}
		if k.companyID == nil && d.companyID != nil {
			k.companyID = d.companyID
			if slices.Contains(d.manual, FieldCompany) {
				k.manual = addField(k.manual, FieldCompany)
			}
		}
		k.firstSeen = minTime(k.firstSeen, d.firstSeen)
		k.lastSeen = maxTime(k.lastSeen, d.lastSeen)

		for _, sql := range []string{
			`UPDATE identities SET contact_id = $1 WHERE contact_id = $2`,
			`UPDATE notes SET contact_id = $1 WHERE contact_id = $2`,
			`UPDATE activities SET contact_id = $1 WHERE contact_id = $2`,
			`INSERT INTO contact_tags (contact_id, tag, created_at)
			 SELECT $1, tag, created_at FROM contact_tags WHERE contact_id = $2 ON CONFLICT DO NOTHING`,
		} {
			if _, err := tx.Exec(ctx, sql, keep, drop); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `DELETE FROM contacts WHERE id = $1`, drop); err != nil {
			return err
		}
		if err := writeContact(ctx, tx, k); err != nil {
			return err
		}
		_, err = recordActivity(ctx, tx, activityRow{contact: keep, kind: ActivityContactsMerged, actor: actor,
			data: map[string]any{"drop_id": drop, "drop_email": d.email}, at: s.now()})
		return err
	})
	if err != nil {
		return Contact{}, err
	}
	return s.contact(ctx, keep)
}

func minTime(a, b time.Time) time.Time {
	if b.Before(a) {
		return b
	}
	return a
}

func maxTime(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

// AddNote appends a note and a note_added activity.
func (s *Service) AddNote(ctx context.Context, actor Actor, id ContactID, body string) (Note, error) {
	n := Note{ContactID: id, Body: strings.TrimSpace(body), Author: actor}
	if n.Body == "" {
		return Note{}, fmt.Errorf("%w: empty note", ErrInvalid)
	}
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
			INSERT INTO notes (contact_id, body, author_kind, author_id, author_label)
			VALUES ($1, $2, $3, $4, $5) RETURNING id, created_at`,
			id, n.Body, actor.Kind, actor.ID, actor.Label).Scan(&n.ID, &n.CreatedAt)
		if pgCode(err) == foreignKeyViolation {
			return notFound(RecordContact, "no contact %d", id)
		}
		if err != nil {
			return err
		}
		_, err = recordActivity(ctx, tx, activityRow{contact: id, kind: ActivityNoteAdded, actor: actor,
			data: map[string]NoteID{"note_id": n.ID}, at: n.CreatedAt})
		return err
	})
	if err != nil {
		return Note{}, err
	}
	return n, nil
}

// DeleteNote removes a note and writes a note_deleted activity naming who
// did. The note_added activity stays, with its body gone.
func (s *Service) DeleteNote(ctx context.Context, actor Actor, id NoteID) error {
	return pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var contactID ContactID
		err := tx.QueryRow(ctx, `DELETE FROM notes WHERE id = $1 RETURNING contact_id`, id).Scan(&contactID)
		if errors.Is(err, pgx.ErrNoRows) {
			return notFound(RecordNote, "no note %d", id)
		}
		if err != nil {
			return err
		}
		_, err = recordActivity(ctx, tx, activityRow{contact: contactID, kind: ActivityNoteDeleted, actor: actor,
			data: map[string]NoteID{"note_id": id}, at: s.now()})
		return err
	})
}

// DeleteContact erases a contact. Its identities, tags, notes, timeline and
// enrichment job go with it (ON DELETE CASCADE), and nothing records the
// deletion, so nothing about the person stays behind. A later sign-up from
// the same provider user creates a new contact.
func (s *Service) DeleteContact(ctx context.Context, id ContactID) error {
	tag, err := s.db.Exec(ctx, `DELETE FROM contacts WHERE id = $1`, id)
	if err == nil && tag.RowsAffected() == 0 {
		return notFound(RecordContact, "no contact %d", id)
	}
	return err
}

// MaxBulk is the most contacts one BulkUpdateContacts call changes.
const MaxBulk = 200

// BulkUpdateContacts applies one stage or tag change to each contact in its
// own transaction, as UpdateContact would. A contact that is missing or
// rejects the change gets a failed result and the rest go ahead. An invalid
// request (no ids, more than MaxBulk, nothing to change, an unknown stage or
// a bad tag) is ErrInvalid before anything is written, and any other error
// stops the run and is returned.
func (s *Service) BulkUpdateContacts(ctx context.Context, actor Actor, b BulkUpdate) ([]BulkResult, error) {
	switch {
	case len(b.IDs) == 0:
		return nil, fmt.Errorf("%w: ids is empty", ErrInvalid)
	case len(b.IDs) > MaxBulk:
		return nil, fmt.Errorf("%w: %d ids, at most %d per call", ErrInvalid, len(b.IDs), MaxBulk)
	case b.Stage == nil && len(b.AddTags) == 0 && len(b.RemoveTags) == 0:
		return nil, fmt.Errorf("%w: nothing to change; give stage, add_tags or remove_tags", ErrInvalid)
	}
	if _, err := parseTags(slices.Concat(b.AddTags, b.RemoveTags)); err != nil {
		return nil, err
	}
	if b.Stage != nil {
		if err := s.checkStage(ctx, *b.Stage); err != nil {
			return nil, err
		}
	}
	p := ContactPatch{Stage: b.Stage, AddTags: b.AddTags, RemoveTags: b.RemoveTags}
	results := make([]BulkResult, len(b.IDs))
	for i, id := range b.IDs {
		results[i] = BulkResult{ID: id, OK: true}
		_, err := s.UpdateContact(ctx, actor, id, p)
		switch {
		case errors.Is(err, ErrNotFound), errors.Is(err, ErrInvalid):
			results[i].OK, results[i].Error = false, err.Error()
		case err != nil:
			return nil, err
		}
	}
	return results, nil
}

// ListTags returns every tag on a contact or a company, with how many
// contacts carry it themselves or through their company: the number a
// contact search by that tag finds. A tag only on companies without
// contacts counts 0.
func (s *Service) ListTags(ctx context.Context) (map[Tag]int, error) {
	rows, err := s.db.Query(ctx, `
		SELECT tag, count(DISTINCT contact_id) FROM (
		  SELECT tag, contact_id FROM contact_tags
		  UNION ALL
		  SELECT t.tag, c.id FROM company_tags t LEFT JOIN contacts c ON c.company_id = t.company_id
		) x GROUP BY tag`)
	if err != nil {
		return nil, err
	}
	tags := map[Tag]int{}
	var tag Tag
	var n int
	_, err = pgx.ForEachRow(rows, []any{&tag, &n}, func() error {
		tags[tag] = n
		return nil
	})
	return tags, err
}

// ListStages returns stages in position order with their contact counts.
func (s *Service) ListStages(ctx context.Context) ([]StageCount, error) {
	rows, err := s.db.Query(ctx, `
		SELECT s.name, count(c.id) FROM stages s LEFT JOIN contacts c ON c.stage = s.name
		 GROUP BY s.name, s.position ORDER BY s.position, s.name`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (StageCount, error) {
		var sc StageCount
		err := r.Scan(&sc.Name, &sc.ContactCount)
		return sc, err
	})
}

// checkStage is ErrInvalid naming the valid stages when st is not one, so a
// caller can fix a typo without another round trip.
func (s *Service) checkStage(ctx context.Context, st Stage) error {
	rows, err := s.db.Query(ctx, `SELECT name FROM stages ORDER BY position, name`)
	if err != nil {
		return err
	}
	stages, err := pgx.CollectRows(rows, pgx.RowTo[Stage])
	if err != nil || slices.Contains(stages, st) {
		return err
	}
	return fmt.Errorf("%w: unknown stage %q; the stages are %s", ErrInvalid, st, joinNames(stages))
}

// SetStages replaces the ordered stage list. renames maps old name to new
// name so contacts follow (ON UPDATE CASCADE). Removing a stage that
// contacts still use is ErrInvalid. New contacts take the first stage.
func (s *Service) SetStages(ctx context.Context, actor Actor, stages []Stage, renames map[Stage]Stage) error {
	if len(stages) == 0 {
		return fmt.Errorf("%w: keep at least one stage; new contacts start in the first", ErrInvalid)
	}
	names := make([]string, len(stages))
	for i, st := range stages {
		names[i] = string(st)
		if slices.Contains(names[:i], names[i]) {
			return fmt.Errorf("%w: stage %q listed twice", ErrInvalid, st)
		}
	}
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		for from, to := range renames {
			if _, err := tx.Exec(ctx, `UPDATE stages SET name = $2 WHERE name = $1`, from, to); err != nil {
				return err
			}
		}
		for i, name := range names {
			if _, err := tx.Exec(ctx, `
				INSERT INTO stages (name, position) VALUES ($1, $2)
				ON CONFLICT (name) DO UPDATE SET position = EXCLUDED.position`, name, i); err != nil {
				return err
			}
		}
		_, err := tx.Exec(ctx, `DELETE FROM stages WHERE NOT (name = ANY($1))`, names)
		return err
	})
	switch pgCode(err) {
	case foreignKeyViolation:
		return fmt.Errorf("%w: a removed stage still has contacts; move them first", ErrInvalid)
	case uniqueViolation, checkViolation:
		return fmt.Errorf("%w: stage names must be unique, trimmed and non-empty", ErrInvalid)
	}
	return err
}
