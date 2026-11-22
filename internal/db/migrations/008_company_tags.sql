-- Tags on a company, such as 'customer'. They apply to every contact at the
-- company: a contact search by tag matches contacts tagged themselves or
-- through their company, so one tag covers everyone at a customer.
CREATE TABLE company_tags (
    company_id bigint NOT NULL REFERENCES companies (id) ON DELETE CASCADE,
    tag        text NOT NULL CHECK (tag = lower(btrim(tag)) AND tag <> ''),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (company_id, tag)
);
CREATE INDEX company_tags_tag ON company_tags (tag);
