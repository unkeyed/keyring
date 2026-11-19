-- uncrm schema, version 1.
--
-- State tables are the source of truth. Every write is an idempotent upsert
-- keyed by a natural key, so running any write twice converges.
-- `activities` is the timeline. It is written in the same transaction as the
-- state change it describes and is never read back to rebuild state.
--
-- Table ownership (the only package that writes each table):
--   internal/crm   stages, companies, contacts, identities, contact_tags, notes,
--                  activities, enrichment_jobs, enrichment_halt,
--                  provider_connections
--   internal/auth  users, sessions, invites, api_keys

-- Pipeline stages. Editable from Settings. Renames cascade to contacts.
CREATE TABLE stages (
    name     text PRIMARY KEY CHECK (name = btrim(name) AND name <> ''),
    position int  NOT NULL
);
INSERT INTO stages (name, position) VALUES
    ('new', 0), ('qualified', 1), ('customer', 2), ('churned', 3);

-- One row per business email domain. Free-mail domains never get a row.
-- Text columns are '' when unknown, never NULL.
-- manual_fields lists the columns a person or agent set. Machine writers
-- (ingest, enrichment) never overwrite those (see the crm package doc).
CREATE TABLE companies (
    id             bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    domain         text NOT NULL UNIQUE CHECK (domain = lower(domain) AND domain <> ''),
    name           text NOT NULL DEFAULT '',
    description    text NOT NULL DEFAULT '',
    logo_url       text NOT NULL DEFAULT '',
    employee_range text NOT NULL DEFAULT '',
    industry       text NOT NULL DEFAULT '',
    country        text NOT NULL DEFAULT '',
    linkedin_url   text NOT NULL DEFAULT '',
    manual_fields  text[] NOT NULL DEFAULT '{}' CHECK (manual_fields <@
                   ARRAY['name', 'description', 'employee_range', 'industry', 'country', 'linkedin_url']),
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now()
);

-- One row per person, keyed by normalized email (crm.ParseEmail).
-- Manual values win: a column named in manual_fields was set by a person or
-- agent, and machine writers leave it alone. Otherwise the newest non-empty
-- machine observation wins. 'company' stands for company_id.
CREATE TABLE contacts (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    email         text NOT NULL UNIQUE CHECK (email = lower(btrim(email)) AND email LIKE '_%@_%'),
    name          text NOT NULL DEFAULT '',
    avatar_url    text NOT NULL DEFAULT '',
    title         text NOT NULL DEFAULT '',
    location      text NOT NULL DEFAULT '',
    bio           text NOT NULL DEFAULT '',
    linkedin_url  text NOT NULL DEFAULT '',
    company_id    bigint REFERENCES companies (id) ON DELETE SET NULL,
    stage         text NOT NULL DEFAULT 'new' REFERENCES stages (name) ON UPDATE CASCADE,
    manual_fields text[] NOT NULL DEFAULT '{}' CHECK (manual_fields <@
                  ARRAY['name', 'title', 'location', 'bio', 'linkedin_url', 'company']),
    first_seen_at timestamptz NOT NULL,
    last_seen_at  timestamptz NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX contacts_first_seen ON contacts (first_seen_at DESC, id DESC);
CREATE INDEX contacts_company ON contacts (company_id);
CREATE INDEX contacts_stage ON contacts (stage);

-- One row per (auth provider, provider user id). The provider's latest view
-- of the user. contact_id is set on first insert; only crm.MergeContacts
-- changes it.
-- occurred_at guards against out-of-order webhooks: an upsert only applies
-- when its fact is at least as new as the stored one.
CREATE TABLE identities (
    provider       text NOT NULL,
    external_id    text NOT NULL,
    contact_id     bigint NOT NULL REFERENCES contacts (id) ON DELETE CASCADE,
    email          text NOT NULL,
    email_verified boolean NOT NULL DEFAULT false,
    name           text NOT NULL DEFAULT '',
    avatar_url     text NOT NULL DEFAULT '',
    raw            jsonb NOT NULL,
    occurred_at    timestamptz NOT NULL,
    deleted_at     timestamptz,
    created_at     timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (provider, external_id)
);
CREATE INDEX identities_contact ON identities (contact_id);
CREATE INDEX identities_email ON identities (email);

CREATE TABLE contact_tags (
    contact_id bigint NOT NULL REFERENCES contacts (id) ON DELETE CASCADE,
    tag        text NOT NULL CHECK (tag = lower(btrim(tag)) AND tag <> ''),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (contact_id, tag)
);
CREATE INDEX contact_tags_tag ON contact_tags (tag);

CREATE TABLE notes (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    contact_id   bigint NOT NULL REFERENCES contacts (id) ON DELETE CASCADE,
    body         text NOT NULL CHECK (body <> ''),
    author_kind  text NOT NULL,
    author_id    text NOT NULL,
    author_label text NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX notes_contact ON notes (contact_id, created_at DESC);

-- Timeline. Append-only, written beside the state change, never replayed.
-- dedupe_key makes provider retries land once: it is a natural key such as
-- 'signed_up:clerk:user_123', not a delivery id, so a dashboard replay with a
-- fresh delivery id still dedupes. NULL for activities that need no dedupe.
-- kind values are crm.ActivityKind constants; Go owns that list.
CREATE TABLE activities (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    contact_id  bigint REFERENCES contacts (id) ON DELETE CASCADE,
    company_id  bigint REFERENCES companies (id) ON DELETE CASCADE,
    kind        text NOT NULL,
    actor_kind  text NOT NULL,
    actor_id    text NOT NULL,
    actor_label text NOT NULL,
    dedupe_key  text UNIQUE,
    data        jsonb NOT NULL DEFAULT '{}',
    occurred_at timestamptz NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    CHECK (contact_id IS NOT NULL OR company_id IS NOT NULL)
);
CREATE INDEX activities_contact ON activities (contact_id, occurred_at DESC);
CREATE INDEX activities_kind ON activities (kind, occurred_at DESC);

-- One enrichment job per contact or company, reused for re-enrichment.
-- State machine (crm/enrich.go):
--   pending -> running            claim (FOR UPDATE SKIP LOCKED), claims += 1
--   running -> running            reclaim after lease_until passes (crashed worker)
--   running -> done | pending | failed   finish, fenced by claims
--   done | failed | skipped -> pending   manual re-enrich
--   running -> pending (halted)  context.dev 401/402/403: failures unchanged,
--                                and enrichment_halt pauses every claim
-- 'skipped' is set at enqueue for subjects that cannot be enriched
-- (a contact with a free-mail address).
-- The state list is duplicated in crm.JobState; TestJobStatesMatchSchema
-- parses this file and fails when they disagree.
CREATE TABLE enrichment_jobs (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    contact_id  bigint UNIQUE REFERENCES contacts (id) ON DELETE CASCADE,
    company_id  bigint UNIQUE REFERENCES companies (id) ON DELETE CASCADE,
    state       text NOT NULL CHECK (state IN ('pending', 'running', 'done', 'failed', 'skipped')),
    run_at      timestamptz NOT NULL DEFAULT now(),
    lease_until timestamptz,
    claims      int NOT NULL DEFAULT 0,
    failures    int NOT NULL DEFAULT 0,
    last_error  text NOT NULL DEFAULT '',
    response    jsonb,
    updated_at  timestamptz NOT NULL DEFAULT now(),
    CHECK (num_nonnulls(contact_id, company_id) = 1),
    CHECK ((state = 'running') = (lease_until IS NOT NULL))
);
CREATE INDEX enrichment_jobs_due ON enrichment_jobs (run_at) WHERE state IN ('pending', 'running');

-- At most one row: why enrichment is paused for every replica, and until when.
-- Written when context.dev rejects the key or the account (401, 402, 403).
-- Settings shows it while until > now().
CREATE TABLE enrichment_halt (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    reason    text NOT NULL,
    until     timestamptz NOT NULL,
    at        timestamptz NOT NULL DEFAULT now()
);

-- A connected sign-up provider. secret is the webhook signing secret or bearer
-- token; it must be readable to verify signatures, so it is stored as given.
-- last_* columns feed the Settings page so a misconfigured webhook is visible.
CREATE TABLE provider_connections (
    provider         text PRIMARY KEY,
    secret           text NOT NULL CHECK (secret <> ''),
    connected_at     timestamptz NOT NULL DEFAULT now(),
    last_delivery_at timestamptz,
    last_error       text NOT NULL DEFAULT '',
    last_error_at    timestamptz
);

-- Humans who log into the UI.
CREATE TABLE users (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    email         text NOT NULL UNIQUE CHECK (email = lower(btrim(email))),
    name          text NOT NULL DEFAULT '',
    password_hash text NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now()
);

-- Session cookies. Only the SHA-256 of the cookie value is stored.
CREATE TABLE sessions (
    token_hash bytea PRIMARY KEY,
    user_id    bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX sessions_expires ON sessions (expires_at);

-- One-time teammate invite links. Only the token hash is stored.
CREATE TABLE invites (
    token_hash  bytea PRIMARY KEY,
    email       text NOT NULL CHECK (email = lower(btrim(email))),
    created_by  bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    expires_at  timestamptz NOT NULL,
    accepted_at timestamptz
);

-- Built-in API keys (used when Unkey is not configured).
-- Keys are 32 random bytes, so a plain SHA-256 is enough; no slow hash needed.
CREATE TABLE api_keys (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name         text NOT NULL,
    prefix       text NOT NULL,
    token_hash   bytea NOT NULL UNIQUE,
    created_by   bigint REFERENCES users (id) ON DELETE SET NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz,
    revoked_at   timestamptz
);
