-- Provider payloads are no longer stored. Nothing read identities.raw, and
-- the copy in each signed_up activity was third-party text that every
-- timeline read handed to agents.
ALTER TABLE identities DROP COLUMN raw;
UPDATE activities SET data = data - 'raw' WHERE kind = 'signed_up' AND data ? 'raw';

-- A company's timeline (GET /v1/companies/{id}, list_activity company_id)
-- reads its own entries by company_id.
CREATE INDEX activities_company ON activities (company_id, occurred_at DESC);
