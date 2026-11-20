-- uncrm is the OAuth authorization server for its own MCP server. MCP
-- clients register themselves (RFC 7591), a signed-in person approves one
-- on the consent page, and the client gets an access token for /mcp that
-- acts as that person. Codes and tokens are stored only as SHA-256 hashes.
-- Written only by internal/auth (OAuthServer).

-- Public clients from dynamic registration. Nothing about them is verified:
-- the consent page says so and shows where the code goes.
CREATE TABLE oauth_clients (
    id            text PRIMARY KEY,
    name          text NOT NULL,
    redirect_uris text[] NOT NULL CHECK (cardinality(redirect_uris) BETWEEN 1 AND 10),
    created_at    timestamptz NOT NULL DEFAULT now(),
    last_used_at  timestamptz
);

-- Authorization requests waiting on the consent page. The handle in the
-- consent URL is stored hashed; a request is answered once.
CREATE TABLE oauth_requests (
    handle_hash    bytea PRIMARY KEY,
    client_id      text NOT NULL REFERENCES oauth_clients (id) ON DELETE CASCADE,
    redirect_uri   text NOT NULL,
    code_challenge text NOT NULL,
    state          text NOT NULL,
    resource       text NOT NULL,
    expires_at     timestamptz NOT NULL
);
CREATE INDEX oauth_requests_expires ON oauth_requests (expires_at);

-- One approval: a client acting as a person. Removing the person deletes
-- their grants and, through them, every token.
CREATE TABLE oauth_grants (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    client_id    text NOT NULL REFERENCES oauth_clients (id) ON DELETE CASCADE,
    user_id      bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    resource     text NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz,
    revoked_at   timestamptz
);
CREATE INDEX oauth_grants_user ON oauth_grants (user_id);

-- Single-use authorization codes. grant_id is set when the code is
-- exchanged, so a second exchange can revoke what the first one issued.
CREATE TABLE oauth_codes (
    code_hash      bytea PRIMARY KEY,
    client_id      text NOT NULL REFERENCES oauth_clients (id) ON DELETE CASCADE,
    user_id        bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    redirect_uri   text NOT NULL,
    code_challenge text NOT NULL,
    resource       text NOT NULL,
    expires_at     timestamptz NOT NULL,
    grant_id       bigint REFERENCES oauth_grants (id) ON DELETE CASCADE
);
CREATE INDEX oauth_codes_expires ON oauth_codes (expires_at);

-- Access and refresh tokens. A refresh token is used once: used_at marks
-- it spent, and presenting it again revokes the whole grant.
CREATE TABLE oauth_tokens (
    token_hash bytea PRIMARY KEY,
    grant_id   bigint NOT NULL REFERENCES oauth_grants (id) ON DELETE CASCADE,
    kind       text NOT NULL CHECK (kind IN ('access', 'refresh')),
    expires_at timestamptz NOT NULL,
    used_at    timestamptz
);
CREATE INDEX oauth_tokens_grant ON oauth_tokens (grant_id);
CREATE INDEX oauth_tokens_expires ON oauth_tokens (expires_at);
