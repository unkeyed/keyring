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
//   - A password alone never sets a session, and a used setup link is dead.
//
// Run it against a fresh compose stack, passing the token of the setup link
// uncrm logs at start:
//
//	docker compose up -d --build --wait
//	make e2e E2E_SETUP_TOKEN=<token from the setup link>
//
// The tests need an API key and a Clerk connection whose secret they know.
// They get both the way a person does, with no test-only backdoor: create
// the first account through the setup link with a software passkey
// (internal/authtest), create a key with POST /v1/keys, and connect Clerk
// with PUT /v1/providers/clerk and a fresh random secret. The passkey is
// saved to E2E_PASSKEY_FILE (default: a file in the temp directory named
// after BASE_URL), so later runs sign in with it and need no token. Every
// check uses a new email address, so runs against the same database never
// collide.
//
// Environment: BASE_URL (default http://localhost:8080), E2E_PUBLIC_URL (the
// instance's PUBLIC_URL, default BASE_URL), E2E_SETUP_TOKEN,
// E2E_PASSKEY_FILE and E2E_ADMIN_EMAIL. The tests carry the e2e build tag,
// so `go test ./...` skips this package.
package e2e
