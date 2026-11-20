-- The rest of what context.dev knows about a company or a person, so the
-- record pages show more than a name and a LinkedIn link. Written only by
-- internal/crm. Existing rows get these on their next enrichment.
--
-- Only tagline and headquarters can be edited, so only they join
-- companies.manual_fields. socials, links, segments, stock and logos are
-- machine observations; logo_url stays the single default pick for API
-- consumers, and logos keeps every candidate in display preference order.
ALTER TABLE companies
    ADD COLUMN tagline      text NOT NULL DEFAULT '',
    ADD COLUMN headquarters text NOT NULL DEFAULT '',
    ADD COLUMN socials      jsonb NOT NULL DEFAULT '{}',
    ADD COLUMN links        jsonb NOT NULL DEFAULT '{}',
    ADD COLUMN segments     text[] NOT NULL DEFAULT '{}',
    ADD COLUMN stock        text NOT NULL DEFAULT '',
    ADD COLUMN logos        jsonb NOT NULL DEFAULT '[]',
    DROP CONSTRAINT companies_manual_fields_check,
    ADD CONSTRAINT companies_manual_fields_check CHECK (manual_fields <@
        ARRAY['name', 'description', 'employee_range', 'industry', 'country', 'linkedin_url', 'tagline', 'headquarters']);

-- socials is type to URL (x, github, website, ...); LinkedIn stays in
-- linkedin_url, which people can edit.
ALTER TABLE contacts ADD COLUMN socials jsonb NOT NULL DEFAULT '{}';
