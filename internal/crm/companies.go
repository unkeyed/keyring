package crm

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

// CompanyFields are the fields a person or agent can pin on a company. The
// companies.manual_fields CHECK lists the same names.
var CompanyFields = []Field{FieldName, FieldDescription, FieldEmployeeRange, FieldIndustry, FieldCountry, FieldLinkedInURL,
	FieldTagline, FieldHeadquarters}

// fieldLogoURL names companies.logo_url for observe. Only machines write it.
const fieldLogoURL Field = "logo_url"

const companySelect = `
SELECT co.id, co.domain, co.name, co.description, co.logo_url, co.employee_range, co.industry, co.country,
       co.linkedin_url, co.tagline, co.headquarters, co.socials, co.links, co.segments, co.stock, co.logos,
       (SELECT count(*) FROM contacts c WHERE c.company_id = co.id),
       COALESCE(j.state, ''), co.manual_fields, co.created_at, co.updated_at
  FROM companies co
  LEFT JOIN enrichment_jobs j ON j.company_id = co.id`

func scanCompany(row pgx.Row) (Company, error) {
	var c Company
	err := row.Scan(&c.ID, &c.Domain, &c.Name, &c.Description, &c.LogoURL, &c.EmployeeRange, &c.Industry, &c.Country,
		&c.LinkedInURL, &c.Tagline, &c.Headquarters, &c.Socials, &c.Links, &c.Segments, &c.Stock, &c.Logos,
		&c.ContactCount, &c.Enrichment, &c.ManualFields, &c.CreatedAt, &c.UpdatedAt)
	return c, err
}

func (s *Service) company(ctx context.Context, id CompanyID) (Company, error) {
	c, err := scanCompany(s.db.QueryRow(ctx, companySelect+` WHERE co.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return c, notFound(RecordCompany, "no company %d", id)
	}
	return c, err
}

// companyRow is a company's writable columns, read under a row lock.
type companyRow struct {
	id                                                                        CompanyID
	domain                                                                    Domain
	name, description, logoURL, employeeRange, industry, country, linkedInURL string
	tagline, headquarters, stock                                              string
	socials                                                                   Socials
	links                                                                     Links
	segments                                                                  []string
	logos                                                                     []Logo
	manual                                                                    []Field
}

func lockCompany(ctx context.Context, tx pgx.Tx, id CompanyID) (companyRow, error) {
	c := companyRow{id: id}
	err := tx.QueryRow(ctx, `
		SELECT domain, name, description, logo_url, employee_range, industry, country, linkedin_url,
		       tagline, headquarters, stock, socials, links, segments, logos, manual_fields
		  FROM companies WHERE id = $1 FOR NO KEY UPDATE`, id).
		Scan(&c.domain, &c.name, &c.description, &c.logoURL, &c.employeeRange, &c.industry, &c.country, &c.linkedInURL,
			&c.tagline, &c.headquarters, &c.stock, &c.socials, &c.links, &c.segments, &c.logos, &c.manual)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, notFound(RecordCompany, "no company %d", id)
	}
	return c, err
}

func writeCompany(ctx context.Context, tx pgx.Tx, c companyRow) error {
	_, err := tx.Exec(ctx, `
		UPDATE companies
		   SET name = $2, description = $3, logo_url = $4, employee_range = $5, industry = $6, country = $7,
		       linkedin_url = $8, tagline = $9, headquarters = $10, stock = $11, socials = $12, links = $13,
		       segments = $14, logos = $15, manual_fields = $16, updated_at = now()
		 WHERE id = $1`,
		c.id, c.name, c.description, c.logoURL, c.employeeRange, c.industry, c.country, c.linkedInURL,
		c.tagline, c.headquarters, c.stock, c.socials, c.links, c.segments, c.logos, fieldNames(c.manual))
	return err
}

// upsertCompany returns the company for d, creating it and its pending
// enrichment job when it is new. Callers have already ruled out free mail.
func upsertCompany(ctx context.Context, tx pgx.Tx, d Domain) (CompanyID, error) {
	var id CompanyID
	err := tx.QueryRow(ctx, `INSERT INTO companies (domain) VALUES ($1) ON CONFLICT (domain) DO NOTHING RETURNING id`, d).Scan(&id)
	if err == nil {
		_, err = tx.Exec(ctx, `INSERT INTO enrichment_jobs (company_id, state) VALUES ($1, 'pending')`, id)
		return id, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, err
	}
	// A separate statement, so it sees a row a concurrent insert just committed.
	err = tx.QueryRow(ctx, `SELECT id FROM companies WHERE domain = $1`, d).Scan(&id)
	return id, err
}

// CreateCompany adds a company by hand and queues its enrichment. A
// free-mail domain is ErrInvalid; a domain already in Keyring is an
// *ExistsError naming that company. A given Name is manual. Writes a
// company_created activity.
func (s *Service) CreateCompany(ctx context.Context, actor Actor, in NewCompany) (Company, error) {
	d, err := ParseDomain(in.Domain)
	if err != nil {
		return Company{}, err
	}
	if d.IsFreeMail() {
		return Company{}, fmt.Errorf("%w: %s is a free-mail domain, not a company", ErrInvalid, d)
	}
	name := strings.TrimSpace(in.Name)
	manual := []string{}
	if name != "" {
		manual = append(manual, string(FieldName))
	}
	var id CompanyID
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
			INSERT INTO companies (domain, name, manual_fields) VALUES ($1, $2, $3)
			ON CONFLICT (domain) DO NOTHING RETURNING id`, d, name, manual).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			if err := tx.QueryRow(ctx, `SELECT id FROM companies WHERE domain = $1`, d).Scan(&id); err != nil {
				return err
			}
			return &ExistsError{Record: RecordCompany, ID: int64(id), Key: d.String()}
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO enrichment_jobs (company_id, state) VALUES ($1, 'pending')`, id); err != nil {
			return err
		}
		_, err = recordActivity(ctx, tx, activityRow{company: id, kind: ActivityCompanyCreated, actor: actor, data: struct{}{}, at: s.now()})
		return err
	})
	if err != nil {
		return Company{}, err
	}
	return s.company(ctx, id)
}

func (s *Service) SearchCompanies(ctx context.Context, q CompanyQuery) (Page[Company], error) {
	var w where
	if text := strings.TrimSpace(q.Text); text != "" {
		w.add("(co.name ILIKE %[1]s OR co.domain ILIKE %[1]s)", w.arg(contains(text)))
	}
	if q.Domain != "" {
		d, err := ParseDomain(q.Domain)
		if err != nil {
			return Page[Company]{}, err
		}
		w.add("co.domain = %s", w.arg(d))
	}
	if q.Cursor != "" {
		t, id, err := decodeCursor(q.Cursor)
		if err != nil {
			return Page[Company]{}, err
		}
		w.add("(co.created_at, co.id) < (%s, %s)", w.arg(t), w.arg(id))
	}
	limit := pageLimit(q.Limit)
	rows, err := s.db.Query(ctx, companySelect+w.String()+
		" ORDER BY co.created_at DESC, co.id DESC LIMIT "+strconv.Itoa(limit+1), w.args...)
	if err != nil {
		return Page[Company]{}, err
	}
	items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Company, error) { return scanCompany(r) })
	if err != nil {
		return Page[Company]{}, err
	}
	return pageOf(items, limit, func(c Company) string { return encodeCursor(c.CreatedAt, int64(c.ID)) }), nil
}

// GetCompany returns the company with every contact, newest sign-up first,
// and the newest 50 activities of the company and its contacts.
func (s *Service) GetCompany(ctx context.Context, id CompanyID) (CompanyDetail, error) {
	c, err := s.company(ctx, id)
	if err != nil {
		return CompanyDetail{}, err
	}
	d := CompanyDetail{Company: c}
	if d.Contacts, err = s.contacts(ctx, contactSelect+` WHERE c.company_id = $1 ORDER BY c.first_seen_at DESC, c.id DESC`, id); err != nil {
		return d, err
	}
	activity, err := s.ListActivity(ctx, ActivityQuery{CompanyID: id, Limit: 50})
	d.Activity = activity.Items
	return d, err
}

// GetCompanyByDomain lowercases domain and looks it up.
func (s *Service) GetCompanyByDomain(ctx context.Context, domain string) (CompanyDetail, error) {
	d, err := ParseDomain(domain)
	if err != nil {
		return CompanyDetail{}, err
	}
	var id CompanyID
	err = s.db.QueryRow(ctx, `SELECT id FROM companies WHERE domain = $1`, d).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return CompanyDetail{}, notFound(RecordCompany, "no company %s", d)
	}
	if err != nil {
		return CompanyDetail{}, err
	}
	return s.GetCompany(ctx, id)
}

// UpdateCompany overwrites non-nil fields, adds them to manual_fields and
// writes a company_updated activity. A LinkedInURL that is not a web address
// is ErrInvalid.
func (s *Service) UpdateCompany(ctx context.Context, actor Actor, id CompanyID, p CompanyPatch) (Company, error) {
	if p.LinkedInURL != nil {
		u, err := webURL(FieldLinkedInURL, *p.LinkedInURL)
		if err != nil {
			return Company{}, err
		}
		p.LinkedInURL = &u
	}
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		c, err := lockCompany(ctx, tx, id)
		if err != nil {
			return err
		}
		before := len(c.manual)
		diff := map[string]change{}
		for _, e := range []struct {
			f   Field
			col *string
			v   *string
		}{
			{FieldName, &c.name, p.Name},
			{FieldDescription, &c.description, p.Description},
			{FieldEmployeeRange, &c.employeeRange, p.EmployeeRange},
			{FieldIndustry, &c.industry, p.Industry},
			{FieldCountry, &c.country, p.Country},
			{FieldLinkedInURL, &c.linkedInURL, p.LinkedInURL},
			{FieldTagline, &c.tagline, p.Tagline},
			{FieldHeadquarters, &c.headquarters, p.Headquarters},
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
		if len(diff) == 0 && len(c.manual) == before {
			return nil
		}
		if err := writeCompany(ctx, tx, c); err != nil || len(diff) == 0 {
			return err
		}
		_, err = recordActivity(ctx, tx, activityRow{company: id, kind: ActivityCompanyUpdated, actor: actor, data: diff, at: s.now()})
		return err
	})
	if err != nil {
		return Company{}, err
	}
	return s.company(ctx, id)
}

// ResetCompanyField is ResetContactField for companies.
func (s *Service) ResetCompanyField(ctx context.Context, actor Actor, id CompanyID, f Field) (Company, error) {
	if !slices.Contains(CompanyFields, f) {
		return Company{}, fmt.Errorf("%w: companies have no field %q; the fields are %s", ErrInvalid, f, joinNames(CompanyFields))
	}
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		c, err := lockCompany(ctx, tx, id)
		if err != nil || !slices.Contains(c.manual, f) {
			return err
		}
		c.manual = slices.DeleteFunc(c.manual, func(m Field) bool { return m == f })
		if err := writeCompany(ctx, tx, c); err != nil {
			return err
		}
		_, err = recordActivity(ctx, tx, activityRow{company: id, kind: ActivityCompanyUpdated, actor: actor,
			data: map[string]Field{"reset": f}, at: s.now()})
		return err
	})
	if err != nil {
		return Company{}, err
	}
	return s.company(ctx, id)
}

// DeleteCompany removes a company with its timeline and enrichment job. Its
// contacts stay, unlinked, each with a contact_updated activity. Their
// company field is pinned, so the next sign-up does not re-create the
// company from the email domain; resetting a contact's company field undoes
// that for one contact.
func (s *Service) DeleteCompany(ctx context.Context, actor Actor, id CompanyID) error {
	return pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id FROM contacts WHERE company_id = $1 ORDER BY id FOR NO KEY UPDATE`, id)
		if err != nil {
			return err
		}
		contacts, err := pgx.CollectRows(rows, pgx.RowTo[ContactID])
		if err != nil {
			return err
		}
		var d Domain
		err = tx.QueryRow(ctx, `SELECT domain FROM companies WHERE id = $1 FOR UPDATE`, id).Scan(&d)
		if errors.Is(err, pgx.ErrNoRows) {
			return notFound(RecordCompany, "no company %d", id)
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE contacts
			   SET company_id = NULL, updated_at = now(),
			       manual_fields = CASE WHEN 'company' = ANY(manual_fields) THEN manual_fields
			                            ELSE array_append(manual_fields, 'company') END
			 WHERE id = ANY($1)`, contacts); err != nil {
			return err
		}
		for _, c := range contacts {
			if _, err := recordActivity(ctx, tx, activityRow{contact: c, kind: ActivityContactUpdated, actor: actor,
				data: map[string]change{"company": {d.String(), ""}}, at: s.now()}); err != nil {
				return err
			}
		}
		_, err = tx.Exec(ctx, `DELETE FROM companies WHERE id = $1`, id)
		return err
	})
}
