# uncrm

uncrm is an open source CRM you host yourself. It records every sign-up from your auth provider, enriches each person and company with [context.dev](https://context.dev), and gives your team a web UI and your agents a REST API and an MCP server. It runs as one container next to a Postgres database.

## Quickstart

You need Docker with Compose v2.

1. Start uncrm and Postgres:

   ```sh
   git clone https://github.com/uncrm/uncrm.git
   cd uncrm
   docker compose up -d --build --wait
   ```

   Open <http://localhost:8080> and log in as `admin@example.com` with the password `uncrm-dev-password`. `docker-compose.yml` seeds this admin through `ADMIN_EMAIL` and `ADMIN_PASSWORD`.

2. Open **Settings > Providers**, pick your auth provider, and follow the steps on the page. uncrm shows the webhook URL to paste into your provider, and the provider's guide is linked under [Connect your auth](#connect-your-auth). A provider cannot reach `localhost`, so on your laptop put a tunnel in front of port 8080, for example `cloudflared tunnel --url http://localhost:8080`, and use the tunnel's URL.

3. Sign up a test user in your app. The contact appears on the **Contacts** page, and **Settings > Providers** shows the delivery or the reason it failed.

To run uncrm on a server, read [Self-host uncrm](docs/self-hosting.md) or [Deploy uncrm to Unkey Deploy](docs/deploy-unkey.md).

## Features

- Sign-ups from Clerk, WorkOS AuthKit, Better Auth, Supabase Auth, Auth0, and Stytch, plus one API call for any other auth system.
- One contact per email address. A person who signs up through two providers is one contact, and a webhook retry or replay is recorded once.
- A company for each work email domain. Gmail and other free-mail domains create no company.
- Job titles, locations, company logos, sizes, and industries from context.dev.
- A timeline per contact with sign-ups, notes, edits, and enrichment.
- Stages, tags, Markdown notes, and a merge for duplicate contacts.
- Values that a person or an agent sets stay set. Webhooks and enrichment never overwrite them.
- Team logins with one-time invite links.
- A REST API and an MCP server that use the same API keys.
- One container that you can run as several replicas against one database.

## Deploy to Unkey

uncrm runs on [Unkey Deploy](https://www.unkey.com/docs/compute/get-started/deploy-from-github) from your fork of this repository or from the prebuilt image `ghcr.io/uncrm/uncrm`. You bring the Postgres database. Follow [Deploy uncrm to Unkey Deploy](docs/deploy-unkey.md).

## Connect your auth

Each provider sends its webhooks to `https://<your-domain>/webhooks/<provider>`. **Settings > Providers** shows the exact URL and the last delivery for each one.

| Provider | Webhook path | Guide |
|---|---|---|
| Clerk | `/webhooks/clerk` | [docs/providers/clerk.md](docs/providers/clerk.md) |
| WorkOS AuthKit | `/webhooks/workos` | [docs/providers/workos.md](docs/providers/workos.md) |
| Better Auth | `/webhooks/better-auth` | [docs/providers/better-auth.md](docs/providers/better-auth.md) |
| Supabase Auth | `/webhooks/supabase` | [docs/providers/supabase.md](docs/providers/supabase.md) |
| Auth0 | `/webhooks/auth0` | [docs/providers/auth0.md](docs/providers/auth0.md) |
| Stytch | `/webhooks/stytch` | [docs/providers/stytch.md](docs/providers/stytch.md) |

For any other auth system, post each sign-up from your backend with an [API key](#api-keys):

```sh
curl -X POST https://<your-domain>/v1/signups \
	-H "Authorization: Bearer $UNCRM_API_KEY" \
	-H "Content-Type: application/json" \
	-d '{"email": "ada@example.com", "name": "Ada Lovelace"}'
```

The first call answers `201` with the new contact's id. The same call again answers `200` with `"duplicate": true`, so retries are safe.

## Enrichment

Set `CONTEXT_DEV_API_KEY` and uncrm looks up each new person by email and each new company by domain on [context.dev](https://context.dev). The free context.dev plan has 1,000 credits a month, which is about 50 person lookups or about 100 company lookups.

Without a key, uncrm records sign-ups as usual and queues the lookups. Set the key and restart uncrm, and it works through the queue. If context.dev rejects the key or the plan runs out, uncrm pauses enrichment for 15 minutes and shows the reason in **Settings > Enrichment**.

## Agents

Create a key in **Settings > Agents & MCP**, then add uncrm to Claude Code:

```sh
claude mcp add --transport http uncrm https://<host>/mcp --header "Authorization: Bearer <key>"
```

The agent can then search, read, update, tag, note, and merge contacts. [Connect agents to uncrm](docs/agents.md) covers Cursor, Claude Desktop, the full tool list, and the REST API. Every route is in the OpenAPI spec at `https://<host>/v1/openapi.yaml`.

## API keys

Agents and scripts authenticate with API keys. People create and revoke them in **Settings > API keys**. uncrm stores keys in one of two places:

- **Built-in.** Keys live in uncrm's Postgres database as SHA-256 hashes. This is the default and needs no setup.
- **Unkey.** Set `UNKEY_ROOT_KEY` and `UNKEY_API_ID`, and uncrm creates, verifies, and revokes keys in your [Unkey](https://www.unkey.com) API. Use this if you already manage keys in Unkey.

[Configuration](docs/configuration.md) lists the root key permissions and every other variable.

## Architecture

- One Go binary serves the React UI, the REST API, the MCP server, and the webhooks, and runs the enrichment worker.
- Postgres holds all state. uncrm applies its migrations when it starts.
- `internal/signup` has one file per auth provider that verifies and parses its webhooks.
- `internal/crm` does every read and write, and REST, MCP, and the UI all call it.
- Every write is an upsert on a natural key, so retries, replays, and replicas end in the same state. [docs/design.md](docs/design.md) explains why.

## License

uncrm is [MIT licensed](LICENSE).

## Contributing

[CONTRIBUTING.md](CONTRIBUTING.md) explains how to run uncrm from source, run the tests, and add an auth provider.
