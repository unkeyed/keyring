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

// companyFields are the fields a person or agent can pin on a company. The
// companies.manual_fields CHECK lists the same names.
var companyFields = []Field{FieldName, FieldDescription, FieldEmployeeRange, FieldIndustry, FieldCountry, FieldLinkedInURL}

// fieldLogoURL names companies.logo_url for observe. Only machines write it.
const fieldLogoURL Field = "logo_url"

const companySelect = `
SELECT co.id, co.domain, co.name, co.description, co.logo_url, co.employee_range, co.industry, co.country,
       co.linkedin_url, (SELECT count(*) FROM contacts c WHERE c.company_id = co.id),
       COALESCE(j.state, ''), co.manual_fields, co.created_at, co.updated_at
  FROM companies co
  LEFT JOIN enrichment_jobs j ON j.company_id = co.id`

func scanCompany(row pgx.Row) (Company, error) {
	var c Company
	err := row.Scan(&c.ID, &c.Domain, &c.Name, &c.Description, &c.LogoURL, &c.EmployeeRange, &c.Industry, &c.Country,
		&c.LinkedInURL, &c.ContactCount, &c.Enrichment, &c.ManualFields, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Company{}, ErrNotFound
	}
	return c, err
}

func (s *Service) company(ctx context.Context, id CompanyID) (Company, error) {
	return scanCompany(s.db.QueryRow(ctx, companySelect+` WHERE co.id = $1`, id))
}

// companyRow is a company's writable columns, read under a row lock.
type companyRow struct {
	id                                                                        CompanyID
	domain                                                                    Domain
	name, description, logoURL, employeeRange, industry, country, linkedInURL string
	manual                                                                    []Field
}

func lockCompany(ctx context.Context, tx pgx.Tx, id CompanyID) (companyRow, error) {
	c := companyRow{id: id}
	err := tx.QueryRow(ctx, `
		SELECT domain, name, description, logo_url, employee_range, industry, country, linkedin_url, manual_fields
		  FROM companies WHERE id = $1 FOR NO KEY UPDATE`, id).
		Scan(&c.domain, &c.name, &c.description, &c.logoURL, &c.employeeRange, &c.industry, &c.country, &c.linkedInURL, &c.manual)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, fmt.Errorf("%w: company %d", ErrNotFound, id)
	}
	return c, err
}

func writeCompany(ctx context.Context, tx pgx.Tx, c companyRow) error {
	_, err := tx.Exec(ctx, `
		UPDATE companies
		   SET name = $2, description = $3, logo_url = $4, employee_range = $5, industry = $6, country = $7,
		       linkedin_url = $8, manual_fields = $9, updated_at = now()
		 WHERE id = $1`,
		c.id, c.name, c.description, c.logoURL, c.employeeRange, c.industry, c.country, c.linkedInURL, fieldNames(c.manual))
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

func (s *Service) SearchCompanies(ctx context.Context, q CompanyQuery) (Page[Company], error) {
	var w where
	if text := strings.TrimSpace(q.Text); text != "" {
		w.add("(co.name ILIKE %[1]s OR co.domain ILIKE %[1]s)", w.arg(contains(text)))
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

func (s *Service) GetCompany(ctx context.Context, id CompanyID) (CompanyDetail, error) {
	c, err := s.company(ctx, id)
	if err != nil {
		return CompanyDetail{}, err
	}
	contacts, err := s.contacts(ctx, contactSelect+` WHERE c.company_id = $1 ORDER BY c.first_seen_at DESC, c.id DESC`, id)
	return CompanyDetail{Company: c, Contacts: contacts}, err
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
		return CompanyDetail{}, fmt.Errorf("%w: no company %s", ErrNotFound, d)
	}
	if err != nil {
		return CompanyDetail{}, err
	}
	return s.GetCompany(ctx, id)
}

// UpdateCompany overwrites non-nil fields, adds them to manual_fields and
// writes a company_updated activity.
func (s *Service) UpdateCompany(ctx context.Context, actor Actor, id CompanyID, p CompanyPatch) (Company, error) {
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
	if !slices.Contains(companyFields, f) {
		return Company{}, fmt.Errorf("%w: companies have no field %q", ErrInvalid, f)
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
