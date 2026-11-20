-- API keys can be read-only and can expire. A key with read_only only
-- calls GET routes; one past expires_at stops verifying.
ALTER TABLE api_keys
    ADD COLUMN read_only  boolean NOT NULL DEFAULT false,
    ADD COLUMN expires_at timestamptz;

-- A setup token with user_id is a recovery link from `keyring recover`:
-- it registers a passkey for that existing user even after setup closed,
-- and stops working at expires_at.
ALTER TABLE setup_tokens
    ADD COLUMN user_id    bigint REFERENCES users (id) ON DELETE CASCADE,
    ADD COLUMN expires_at timestamptz,
    ADD CONSTRAINT setup_tokens_recovery_expires CHECK (user_id IS NULL OR expires_at IS NOT NULL);

-- Calls to the enricher per UTC day, so ENRICHMENT_DAILY_LIMIT caps what a
-- leaked key or a flood of sign-ups can spend.
CREATE TABLE enrichment_usage (
    day   date PRIMARY KEY,
    calls integer NOT NULL DEFAULT 0
);
