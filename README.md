# uncrm

uncrm is an open source CRM you host yourself. It records every sign-up from your auth provider (Clerk, WorkOS, Better Auth, Supabase, Auth0, Stytch, or any other through one API call), enriches people and companies with context.dev, and gives your team a web UI and your agents a REST API and an MCP server. It runs as one container next to a Postgres database.

uncrm is under construction. Most of the API is not implemented yet.

## Quickstart

```sh
docker compose up -d --build --wait
open http://localhost:8080   # log in as admin@example.com / uncrm-dev-password
```

A full guide, including deploying to Unkey Deploy, is coming.

## Development

```sh
make dev     # Postgres in Docker, the server on the host at :8080
make test    # unit and Postgres tests (needs `docker compose up -d db`)
make lint
make e2e     # end-to-end checks against BASE_URL (default http://localhost:8080)
```

The design is in [docs/design.md](docs/design.md). uncrm is MIT licensed.
