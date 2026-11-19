# Configuration

uncrm reads its configuration from environment variables when it starts. `cmd/uncrm/main.go` reads them all in `loadConfig`, and this page lists every one. A change takes effect on the next start.

| Variable | Required | Default | What it does |
|---|---|---|---|
| `DATABASE_URL` | Yes | none | The Postgres connection URL, for example `postgres://user:pass@host:5432/uncrm`. |
| `PORT` | No | `8080` | The port the HTTP server listens on. |
| `ADMIN_EMAIL` | No | none | The email of the first user. Set it together with `ADMIN_PASSWORD`. |
| `ADMIN_PASSWORD` | No | none | The password of the first user. At least 12 characters. |
| `CONTEXT_DEV_API_KEY` | No | none | The context.dev API key. Without it, enrichment is off. |
| `UNKEY_ROOT_KEY` | No | none | An Unkey root key. Set it together with `UNKEY_API_ID` to store API keys in Unkey. |
| `UNKEY_API_ID` | No | none | The Unkey API that holds uncrm's keys, for example `api_123`. |

uncrm exits with an error that names the variable when one of these rules fails:

- `DATABASE_URL` is empty.
- Only one of `UNKEY_ROOT_KEY` and `UNKEY_API_ID` is set.
- Only one of `ADMIN_EMAIL` and `ADMIN_PASSWORD` is set.
- `ADMIN_PASSWORD` is shorter than 12 characters. This check runs on every start, also after the admin exists.

## `DATABASE_URL`

Any Postgres that accepts a connection URL works: a local container, Neon, Supabase, RDS, or another managed service. uncrm needs no extensions.

On start, uncrm applies its migrations from `internal/db/migrations`. Each migration runs in its own transaction under a Postgres advisory lock, so several replicas can start at the same time.

## `PORT`

The Docker image sets `PORT=8080`. Unkey Deploy sets `PORT` for you and sends traffic and health checks to it.

## `ADMIN_EMAIL` and `ADMIN_PASSWORD`

On every start, uncrm inserts a user with this email and password if no user with that email exists. If the user exists, uncrm changes nothing. Changing `ADMIN_PASSWORD` later does not change the password of an existing user. Change it in **Settings > Team** instead.

Leave both unset to skip the seed. With no users in the database, nobody can log in, and the login page explains which variables to set.

Invite other people from **Settings > Team**. Each invite link works once and expires after 7 days.

## `CONTEXT_DEV_API_KEY`

With a key, uncrm looks up each new contact and company on [context.dev](https://context.dev) in the background. Sign-ups with a free-mail address, such as Gmail, are not looked up.

Without a key, enrichment jobs wait in the `pending` state. Set the key and restart uncrm, and the worker runs every waiting job.

If context.dev rejects the key or the account (HTTP 401, 402, or 403), every replica pauses enrichment for 15 minutes. **Settings > Enrichment** shows the reason. A restart clears the pause.

## `UNKEY_ROOT_KEY` and `UNKEY_API_ID`

uncrm has two stores for the API keys that agents and scripts use. It picks one when it starts:

- With neither variable set, keys live in uncrm's Postgres database. This is the built-in store.
- With both set, keys live in your [Unkey](https://www.unkey.com) API. uncrm creates, lists, verifies, and deletes them through Unkey's v2 API.

The root key needs these permissions on the API that `UNKEY_API_ID` names: `create_key`, `read_key`, `read_api`, `verify_key`, and `delete_key`.

In Unkey mode, each replica caches a valid key for 30 seconds. A revoked key can keep working for up to 30 seconds on a replica that did not revoke it.

Keys from one store do not work in the other. If you switch stores, create new keys and give them to your agents.

The `uncrm listening` log line at startup shows the active store in its `keys` field, as `builtin` or `unkey`.
