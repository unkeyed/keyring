package crm

import (
	"context"
	"errors"
)

func (s *Service) SearchCompanies(ctx context.Context, q CompanyQuery) (Page[Company], error) {
	return Page[Company]{}, errors.New("not implemented")
}

// GetCompany returns the company and its contacts.
func (s *Service) GetCompany(ctx context.Context, id CompanyID) (CompanyDetail, error) {
	return CompanyDetail{}, errors.New("not implemented")
}

// GetCompanyByDomain lowercases domain and looks it up.
func (s *Service) GetCompanyByDomain(ctx context.Context, domain string) (CompanyDetail, error) {
	return CompanyDetail{}, errors.New("not implemented")
}

// UpdateCompany overwrites non-nil fields and writes a company_updated activity.
func (s *Service) UpdateCompany(ctx context.Context, actor Actor, id CompanyID, p CompanyPatch) (Company, error) {
	return Company{}, errors.New("not implemented")
}
