-- Sign-in by passkey, social or OIDC identity, or password plus a second
-- factor. Written only by internal/auth.
--
-- A password never yields a session on its own, so the sessions that
-- password-only logins created before this migration end here.
DELETE FROM sessions;

ALTER TABLE users ALTER COLUMN password_hash DROP NOT NULL;
-- The WebAuthn user handle. Random, so a passkey never carries the user id.
ALTER TABLE users ADD COLUMN webauthn_handle bytea NOT NULL UNIQUE DEFAULT uuid_send(gen_random_uuid());

ALTER TABLE sessions
    ADD COLUMN id               bigint GENERATED ALWAYS AS IDENTITY UNIQUE,
    ADD COLUMN method           text NOT NULL CHECK (method <> ''),
    ADD COLUMN user_agent       text NOT NULL DEFAULT '',
    ADD COLUMN last_seen_at     timestamptz NOT NULL DEFAULT now(),
    -- When the person last proved a factor in this session. Removing a
    -- sign-in method needs this to be recent.
    ADD COLUMN authenticated_at timestamptz NOT NULL DEFAULT now();
CREATE INDEX sessions_user ON sessions (user_id);

-- A login at an external provider (google, github, oidc) linked to a user.
-- Distinct from identities, which are the CRM's sign-ups.
CREATE TABLE user_identities (
    provider     text NOT NULL,
    subject      text NOT NULL,
    user_id      bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    email        text NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz,
    PRIMARY KEY (provider, subject),
    UNIQUE (user_id, provider)
);

-- credential is go-webauthn's Credential record as JSON: public key, sign
-- count, transports, flags and attestation, kept whole so no field the
-- library verifies on a later login is lost.
CREATE TABLE passkeys (
    id           bytea PRIMARY KEY,
    user_id      bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name         text NOT NULL,
    credential   jsonb NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz
);
CREATE INDEX passkeys_user ON passkeys (user_id);

-- A confirmed authenticator app. last_step is the newest 30-second time step
-- a code was accepted for; a code for that step or an older one is a replay.
CREATE TABLE totp (
    user_id    bigint PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    secret     text NOT NULL,
    last_step  bigint NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- Single-use recovery codes. Using one deletes its row.
CREATE TABLE recovery_codes (
    user_id   bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    code_hash bytea NOT NULL,
    PRIMARY KEY (user_id, code_hash)
);

-- Short-lived state between two requests of one sign-in step: an OAuth
-- redirect, a WebAuthn ceremony, a login waiting for its second factor, an
-- authenticator app waiting for its first code. The browser holds a random
-- handle; only its hash is stored. Shared by every replica.
CREATE TABLE auth_challenges (
    handle_hash bytea PRIMARY KEY,
    kind        text NOT NULL,
    data        jsonb NOT NULL,
    expires_at  timestamptz NOT NULL
);
CREATE INDEX auth_challenges_expires ON auth_challenges (expires_at);

-- Setup links. Exist only while nobody can sign in; the first account's
-- creation deletes them all. Each boot in that state adds one, so every
-- logged link keeps working until setup is done.
CREATE TABLE setup_tokens (
    token_hash bytea PRIMARY KEY,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- Failed sign-in attempts per key: "email:<address>", "ip:<address or /64>"
-- or "user:<id>" for second-factor codes.
DROP TABLE login_failures;
CREATE TABLE login_failures (
    key            text PRIMARY KEY,
    failures       int NOT NULL CHECK (failures > 0),
    last_failed_at timestamptz NOT NULL
);
CREATE INDEX login_failures_last_failed ON login_failures (last_failed_at);
