# Contributing to Keyring

This page covers running Keyring from source, the checks a change has to pass, and the most common contribution: a new auth provider.

## Run Keyring from source

You need Go 1.26, Node.js 24, and Docker.

```sh
make dev
```

`make dev` starts Postgres in Docker on port 5432 and runs the Go server on <http://localhost:8080>. On an empty database it prints a setup link. Open it and register a passkey to create your account.

The Go binary embeds the web UI from `web/dist`. To work on the UI, run Vite in a second terminal:

```sh
cd web
npm ci
npm run dev
```

Open <http://localhost:5173>. Vite reloads the UI on every change and forwards API calls to the Go server on port 8080.

To build the binary with the UI inside, run `make build`. The result is `bin/keyring`.

## Check your change

CI runs these on every pull request:

```sh
make lint   # gofmt, go vet
make test   # unit tests and Postgres tests
cd web && npm run build   # TypeScript check and production build
```

`make test` needs Postgres. `make dev` starts it, or run `docker compose up -d --wait db`. Each test gets its own freshly migrated database, which it drops afterwards. If Postgres is unreachable, the tests fail instead of skipping. To use another server, set `TEST_DATABASE_URL` to a role that can create databases.

The end-to-end tests drive a running Keyring over HTTP. The stack needs an `ENCRYPTION_KEY` in `.env` (`make dev` keeps its own in `.keyring-dev-key`):

```sh
grep -q ENCRYPTION_KEY .env 2>/dev/null || echo "ENCRYPTION_KEY=$(openssl rand -base64 32)" >> .env
docker compose up -d --build --wait
make e2e
```

On a fresh stack, pass the token from the setup link that Keyring prints: `make e2e E2E_SETUP_TOKEN=<token>`. The suite creates its account with a software passkey and saves the passkey, so later runs need no token. `e2e/doc.go` lists the other variables.

## Follow the house rules

- Keep SQL in `internal/crm`, `internal/auth`, and `internal/db`. HTTP handlers and MCP tools call one `crm.Service` method each.
- Make every write an upsert on a natural key, so a retry ends in the same state.
- Never edit a migration that has been released. Add a new file in `internal/db/migrations`.
- When you add or change a route in `internal/httpapi/server.go`, update `internal/httpapi/openapi.yaml`. `TestRoutesMatchOpenAPI` fails when the two disagree.
- Keep each commit to one purpose, and say why in its message.

## Add an auth provider

A provider is one Go file. The webhook route, the Settings page, and the dispatch all read the provider registry, so nothing else needs a change.

1. Add `internal/signup/<name>.go`. Its `init` calls `register` with a `Provider` that names a verification scheme from `verify.go` and a parse function. Copy the shape of `clerk.go` or `workos.go`.
2. Add at least one real delivery body as `internal/signup/testdata/<name>/<case>.json`, and the expected parse result as `<case>.want.json`. `TestEveryProviderHasFixtures` fails until you do.
3. Add `docs/providers/<name>.md` with the setup steps and the fields Keyring reads, and add a row to the provider table in `README.md`.

## Release

Maintainers release by pushing a version tag:

```sh
git tag v0.1.0
git push origin v0.1.0
```

`.github/workflows/release.yml` builds the image for `linux/amd64` and `linux/arm64` and pushes it to `ghcr.io/unkeyed/keyring` with the version (`0.1.0`), the minor version (`0.1`), and `latest`. A pre-release tag such as `v0.2.0-rc.1` gets only its own version.

Unkey Deploy and `docker pull` need the image without credentials. After the first release, check that the package's visibility is **Public** in the package settings on GitHub.
