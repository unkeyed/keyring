# Configuration

Keyring reads its configuration from environment variables when it starts. `cmd/keyring/main.go` reads them all in `loadConfig`, and this page lists every one. A change takes effect on the next start.

| Variable | Required | Default | What it does |
|---|---|---|---|
| `DATABASE_URL` | Yes | none | The Postgres connection URL, for example `postgres://user:pass@host:5432/keyring`. |
| `PORT` | No | `8080` | The port the HTTP server listens on. |
| `PUBLIC_URL` | No | `http://localhost:$PORT` | The URL people open Keyring at, such as `https://crm.example.com`. Passkeys, provider callbacks and MCP sign-in depend on it. |
| `SETUP_TOKEN` | No | random | The token of the setup link, at least 32 characters. See [The setup link](sign-in.md#the-setup-link). |
| `PASSWORD_LOGIN` | No | `false` | `true` lets people set a password, which always needs a second step. |
| `ALLOWED_EMAIL_DOMAINS` | No | none | Comma-separated domains, such as `acme.com`, whose addresses can join without an invite through Google Workspace or the OIDC issuer. GitHub accounts always need an invite. |
| `CLIENT_IP_HEADER` | No | none | The header your proxy writes the client IP to, such as `X-Forwarded-For`. Used to throttle failed sign-ins per IP. |
| `GOOGLE_CLIENT_ID` | No | none | A Google OAuth client ID. Set it with `GOOGLE_CLIENT_SECRET` to offer Google sign-in. |
| `GOOGLE_CLIENT_SECRET` | No | none | The Google OAuth client secret. |
| `GITHUB_CLIENT_ID` | No | none | A GitHub OAuth app client ID. Set it with `GITHUB_CLIENT_SECRET` to offer GitHub sign-in. |
| `GITHUB_CLIENT_SECRET` | No | none | The GitHub OAuth app client secret. |
| `OIDC_ISSUER` | No | none | An OIDC issuer URL. Set it with `OIDC_CLIENT_ID` and `OIDC_CLIENT_SECRET` to offer OIDC sign-in. |
| `OIDC_CLIENT_ID` | No | none | The OIDC client ID. |
| `OIDC_CLIENT_SECRET` | No | none | The OIDC client secret. |
| `OIDC_NAME` | No | `SSO` | The OIDC provider's name on the sign-in button, such as `Okta`. |
| `ADMIN_EMAIL` | No | none | Removed. Keyring refuses to start while it is set. |
| `ADMIN_PASSWORD` | No | none | Removed. Keyring refuses to start while it is set. |
| `CONTEXT_DEV_API_KEY` | No | none | The context.dev API key. Without it, enrichment is off. |
| `UNKEY_ROOT_KEY` | No | none | An Unkey root key. Set it together with `UNKEY_API_ID` to store API keys in Unkey. |
| `UNKEY_API_ID` | No | none | The Unkey API that holds Keyring's keys, for example `api_123`. |

Keyring exits with an error that names the variable when one of these rules fails:

- `DATABASE_URL` is empty.
- Only one of `UNKEY_ROOT_KEY` and `UNKEY_API_ID` is set.
- `PUBLIC_URL` is not an origin, or uses `http` with a host other than `localhost` or `127.0.0.1`.
- `PASSWORD_LOGIN` is not `true` or `false`.
- A provider has some of its variables set but not all.
- `SETUP_TOKEN` is shorter than 32 characters.
- `ADMIN_EMAIL` or `ADMIN_PASSWORD` is set.

## `DATABASE_URL`

Any Postgres that accepts a connection URL works: a local container, Neon, Supabase, RDS, or another managed service. Keyring needs no extensions.

On start, Keyring applies its migrations from `internal/db/migrations`. Each migration runs in its own transaction under a Postgres advisory lock, so several replicas can start at the same time.

## `PORT`

The Docker image sets `PORT=8080`. Unkey Deploy sets `PORT` for you and sends traffic and health checks to it.

## `PUBLIC_URL`

The origin people type into their browser, with no path. Its host is the WebAuthn relying party ID, so a passkey registered at one `PUBLIC_URL` does not work at another. Provider callbacks go to `PUBLIC_URL/auth/oauth/<provider>/callback`. When it starts with `https`, cookies are marked `Secure` and named with the `__Host-` prefix, and every response carries `Strict-Transport-Security`.

MCP clients sign in with OAuth, and Keyring derives every OAuth URL from `PUBLIC_URL`. The authorization server's issuer is `PUBLIC_URL`, and the MCP server's resource identifier is `PUBLIC_URL/mcp`. A client checks that the resource Keyring names matches the URL it connects to, so connect agents to `PUBLIC_URL/mcp` exactly. Access tokens are bound to that resource, so changing `PUBLIC_URL` signs every connected app out, and each one asks you to approve it again.

When it is unset, Keyring uses `http://localhost:$PORT`, which suits `make dev`. Browsers allow passkeys on `http` only for `localhost`, so any other host needs `https`. `docker-compose.yml` sets it to `http://localhost:$KEYRING_PORT`. If you run the Vite dev server on port 5173, set `PUBLIC_URL=http://localhost:5173`.

## Sign-in variables

`SETUP_TOKEN`, `PASSWORD_LOGIN`, `ALLOWED_EMAIL_DOMAINS`, `CLIENT_IP_HEADER`, and the `GOOGLE_*`, `GITHUB_*`, and `OIDC_*` variables decide how people sign in. [Sign-in](sign-in.md) explains each one.

`ADMIN_EMAIL` and `ADMIN_PASSWORD` seeded a password account in earlier versions. They are gone, and Keyring exits while either is set, so a password you meant to rely on cannot sit unused in the environment. Unset both and open the setup link Keyring prints at start.

## `CONTEXT_DEV_API_KEY`

With a key, Keyring looks up each new contact and company on [context.dev](https://context.dev) in the background. Sign-ups with a free-mail address, such as Gmail, are not looked up.

For a company, Keyring stores the name, description, tagline, logos, size, industry, sub-industries, headquarters, country, stock ticker, LinkedIn and other social profiles, and links to its careers, pricing, blog, and contact pages. It keeps every logo context.dev has and picks one per theme: an icon before a wordmark, and art drawn for the current light or dark theme before art for the other one, which gets a contrasting tile. For a person, Keyring stores the name, title, location, bio, photo, LinkedIn, and other profiles such as X, GitHub, or a personal site. When a contact already has a LinkedIn URL, Keyring sends it with the email to improve the match. It does not store the email address or phone number context.dev lists for a company, because these are often a social handle or one employee's own address.

Companies enriched before an upgrade get the new details the next time they are enriched. **Re-enrich** on the company page does it now.

Without a key, enrichment jobs wait in the `pending` state. Set the key and restart Keyring, and the worker runs every waiting job.

If context.dev rejects the key or the account (HTTP 401, 402, or 403), every replica pauses enrichment for 15 minutes. **Settings > Enrichment** shows the reason. A restart clears the pause.

## `UNKEY_ROOT_KEY` and `UNKEY_API_ID`

Keyring has two stores for the API keys that agents and scripts use. It picks one when it starts:

- With neither variable set, keys live in Keyring's Postgres database. This is the built-in store.
- With both set, keys live in your [Unkey](https://www.unkey.com) API. Keyring creates, lists, verifies, and deletes them through Unkey's v2 API.

The root key needs these permissions on the API that `UNKEY_API_ID` names: `create_key`, `read_key`, `read_api`, `verify_key`, and `delete_key`.

In Unkey mode, each replica caches a valid key for 30 seconds. A revoked key can keep working for up to 30 seconds on a replica that did not revoke it.

Keys from one store do not work in the other. If you switch stores, create new keys and give them to your agents.

The `Keyring listening` log line at startup shows the active store in its `keys` field, as `builtin` or `unkey`.
