// Package e2e checks uncrm's definition of done against a running instance,
// over HTTP only, the way a user and their agents reach it:
//
//   - GET /healthz answers 200.
//   - A Clerk user.created webhook, signed with the Svix scheme, creates one
//     contact, and a retry or replay of it leaves one. A bad signature is
//     rejected.
//   - POST /v1/signups with an API key creates a contact and is idempotent.
//   - GET /v1/contacts lists and searches contacts with that key.
//   - GET /v1/openapi.yaml serves the spec without credentials.
//   - The MCP server at /mcp lists its tools, and search_contacts finds the
//     contact.
//
// Run it against the compose stack:
//
//	docker compose up -d --build --wait
//	make e2e
//
// The tests need an API key and a Clerk connection whose secret they know.
// They get both the way a person does, with no test-only backdoor: log in as
// the admin seeded from ADMIN_EMAIL and ADMIN_PASSWORD, create a key with
// POST /v1/keys, and connect Clerk with PUT /v1/providers/clerk and a fresh
// random secret. Every check uses a new email address, so runs against the
// same database never collide.
//
// Environment: BASE_URL (default http://localhost:8080), E2E_ADMIN_EMAIL and
// E2E_ADMIN_PASSWORD (default: the compose admin). The tests carry the e2e
// build tag, so `go test ./...` skips this package.
package e2e
