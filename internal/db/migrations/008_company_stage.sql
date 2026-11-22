-- A company's pipeline stage, such as 'customer'. NULL until someone sets it.
-- Setting it moves every contact at the company to the same stage, and new
-- contacts at the company start in it (crm.UpdateCompany, crm.Ingest), so
-- marking a customer once covers everyone who signs up from it.
ALTER TABLE companies ADD COLUMN stage text REFERENCES stages (name) ON UPDATE CASCADE;
CREATE INDEX companies_stage ON companies (stage);
