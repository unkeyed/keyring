# uncrm design: plain state tables, idempotent upserts, one service package

## Problem

uncrm takes sign-ups from six auth providers plus a generic endpoint, merges them into one contact per person, enriches people and companies through context.dev, and serves the result to a React UI, a REST API and an MCP server. It ships as one container on Unkey Deploy, which can restart or run several replicas at any time. Providers retry webhooks and do not order them. The top product value is ease of use, and the codebase is open source, so a first-time contributor has to be able to read the backend in one sitting.

The constraints that shape the design are these. Retries must land once. The same person arriving through two providers must become one contact. Free-mail domains must not create companies. Enrichment must survive crashes and run safely on several replicas, and it must also work with no key at all. API keys come from Postgres or Unkey. REST, MCP and the UI must share one service layer.

The direction is to keep state in plain tables and make every write an idempotent upsert on a natural key. The timeline is a separate `activities` table, written in the same transaction and never replayed.

## Usage (caller's view)

README quickstart:

```sh
docker run -e DATABASE_URL=postgres://... -e ADMIN_EMAIL=me@acme.com -e ADMIN_PASSWORD=... -p 8080:8080 ghcr.io/uncrm/uncrm
# open http://localhost:8080, log in, Settings > Providers > Clerk:
#   copy https://<host>/webhooks/clerk into Clerk, paste Clerk's whsec_ secret back.
# optional: CONTEXT_DEV_API_KEY=... turns on enrichment; UNKEY_ROOT_KEY + UNKEY_API_ID switch keys to Unkey.
```

Any auth system without an adapter:

```sh
curl -X POST https://crm.acme.com/v1/signups -H "Authorization: Bearer uncrm_..." \
  -d '{"email":"ada@stripe.com","name":"Ada Lovelace"}'
# 201 {"contact_id":42,"created":true,"duplicate":false}; the same call again gives 200 with "duplicate":true
```

A contributor adding a seventh provider writes one file, `internal/signup/kinde.go`, plus fixtures. Nothing else lists providers.

```go
func init() {
	register(Provider{
		Name: "kinde", Title: "Kinde",
		Verify: standardWebhooks, // or workosSignature, bearerToken, or a new func in this file
		Parse:  parseKinde,
		Setup:  "In Kinde open Webhooks, add `{{webhook_url}}` ...",
	})
}
func parseKinde(d Delivery) (Signup, error) { /* json.Unmarshal, map fields, ErrIgnored for the rest */ }
```

An MCP tool and a REST handler, side by side, calling the same method:

```go
// internal/mcpserver
c, err := svc.UpdateContact(ctx, actorOf(req), in.ID, in.Changes)

// internal/httpapi
c, err := s.svc.UpdateContact(r.Context(), a, crm.ContactID(id), p)
```

## Shape

Data structures first. See `internal/db/migrations/001_init.sql` and `internal/crm/types.go`.

- `contacts` is keyed by normalized email (`UNIQUE`). `identities` is keyed by `(provider, external_id)` and points at a contact. An identity's `contact_id` is set on insert and never updated. `companies` is keyed by `domain`. `contact_tags` is keyed by `(contact_id, tag)`. `stages` is a user-editable list that contacts reference by foreign key with `ON UPDATE CASCADE`.
- `activities` is the timeline. Its `dedupe_key` is a natural key such as `signed_up:clerk:user_123`, not a delivery id. A Clerk retry, a dashboard replay with a fresh `svix-id`, and a Supabase delivery that has no id at all all dedupe the same way. No delivery log table exists, because nothing needs one (per make-operations-idempotent).
- `enrichment_jobs` has one row per contact or company (`UNIQUE` on each FK, `CHECK num_nonnulls = 1`). The row's `state` is the state machine, and the job is reused for re-enrichment.
- Every table has exactly one writer package, named in the migration header (per separate-before-serializing-shared-state).

Packages, with the dependency direction left to right:

```
cmd/uncrm          config from env, wiring, graceful shutdown
internal/httpapi   REST = the UI's API. route table, decode, call crm, encode
internal/mcpserver MCP tools, same crm calls
internal/auth      API keys (builtin | unkey) and human sessions -> crm.Actor
internal/contextdev  crm.Enricher over net/http
internal/crm       Service: every CRM read and write, all CRM SQL, the enrichment worker
internal/signup    provider registry, verifiers, one file per provider. no I/O
internal/db        embedded migrations, advisory-locked runner
web                embedded React build
```

The load-bearing decisions follow.

1. **One deep package, `crm`.** `Service` has about 25 methods. Each one is a transaction with its SQL inline (pgx, no ORM, no sqlc, no repository layer). Transports never see SQL, pgx, or the manual-values-win rule. Tracing any request takes three files at most: the handler, the `crm` method, and the migration (per minimize-reader-load). The interface is wide, but each method hides a whole transaction and its invariants, so callers learn no internals.
2. **Two write rules, stated once in the `crm` package doc.** First, every write is an upsert on a natural key. Second, manual values win, and otherwise the newest observation wins. Humans and agents overwrite a column and add its name to the row's `manual_fields`. Machine writers (ingest, enrichment) overwrite a column only when it is not in `manual_fields` and the new value is non-empty. A machine write can never clobber a person's edit, so no lock or version column is needed (per separate-before-serializing-shared-state). Two machine writers racing on the same column end with whichever committed last, which is acceptable because both are observations of the same person.
3. **Identity resolution is one ordered lookup inside `Ingest`.** It checks the identity first, then `contacts.email`, then `identities.email`, and finally `INSERT ... ON CONFLICT (email)`. The final upsert also settles the race when two providers deliver the same new person at once. `identities.occurred_at` guards against out-of-order updates. The ordered steps are written out in `crm/ingest.go`.
4. **Adapters are data plus two pure functions.** `signup.Provider{Name, Title, IssueSecret, Setup, Verify, Parse}` registers itself from `init`. The webhook route, the Settings page (`crm.ListProviders` reads `signup.All`) and dispatch all read the registry. Verification is a closed set of three schemes in `verify.go`, so most providers name one instead of writing crypto (per model-the-domain). Adapters pass the email through raw, and `crm.Ingest` normalizes it, so no adapter can skip normalization (per boundary-discipline). `ReceiveWebhook` overwrites `Source` with the registered name, and `Ingest` rejects provider names from API callers, so neither side can forge an identity.
5. **The webhook path lives in the service.** `crm.ReceiveWebhook(name, Delivery)` does the lookup, loads the secret, then verifies, parses and ingests. It also writes `last_delivery_at` or `last_error` to `provider_connections`. The Settings page then shows "bad signature 30s ago" while the user is wiring the webhook up, which is the single biggest ease-of-use lever in setup.
6. **The enrichment state machine is a pure `settle` function plus fenced SQL.** The claim uses `FOR UPDATE SKIP LOCKED` and also picks up `running` rows whose lease has expired, so crash recovery is just the next claim. Each claim increments `claims`. Every finishing update has `WHERE claims = $claims`, so a worker that lost its lease cannot overwrite a newer one. Throttles (429, 402) retry without spending the failure budget. With no `CONTEXT_DEV_API_KEY`, jobs stay `pending` and run after the key is set. Free-mail contacts get `skipped` when they are enqueued. `TestSettle` covers the transitions, and `TestJobStatesMatchSchema` keeps the Go constants and the SQL `CHECK` in sync, so the two lists cannot drift silently.
7. **Auth sits beside crm, not inside it.** `auth.KeyStore` has two implementations, chosen once in `NewKeyStore`. Human login uses email and password (stdlib PBKDF2) with a DB-backed session cookie. The first admin is seeded from `ADMIN_EMAIL`/`ADMIN_PASSWORD` with `ON CONFLICT DO NOTHING`, which is idempotent across replicas and needs no shell on Unkey. Teammates join by invite link. Both auth paths produce a `crm.Actor`, which handlers pass explicitly to every write. The actor is never read from a context value.
8. **One route table for REST and the UI.** Each route declares `public`, `anyone` (key or cookie) or `humans` (cookie only). Key management, provider secrets and invites are `humans`, so one leaked agent key cannot mint more keys. CSRF protection comes from stdlib `http.CrossOriginProtection`. `TestRoutesMatchOpenAPI` keeps the hand-written `openapi.yaml` honest.

Invariants and where they are enforced:

| Invariant | Enforced by |
|---|---|
| one contact per normalized email | `contacts.email UNIQUE` + `crm.Email` has an unexported field, so only parsing builds one |
| one signed_up per provider user | `activities.dedupe_key UNIQUE` |
| identity never moves contacts | `contact_id` absent from the upsert's `DO UPDATE` |
| stale webhook cannot regress identity | `WHERE identities.occurred_at <= EXCLUDED.occurred_at` |
| running job has a lease | `CHECK ((state='running') = (lease_until IS NOT NULL))` |
| lost lease cannot write | `WHERE claims = $claims` |
| no company for free mail | `Domain.IsFreeMail` checked in `Ingest` step 5 |
| providers listed once | `signup.register` + `init`, duplicate panics at boot |

What the design deliberately leaves out: event sourcing, a delivery log, a jobs framework, an ORM, code generation, contact merge, org events, and phone-only users.

## Synthesis decision

Two designs were sketched and scored against one rubric: an event log as the source of truth (candidate 1) and these plain state tables (candidate 2). This design is the base. It won on short request traces and readability for a contributor, which matter most for an open source project whose top value is ease of use. Candidate 1's global write lock, refold on deploy, and erasure exception would cost every contributor for an audit trail the product does not need yet.

Five ideas were grafted from candidate 1:

1. **Manual values win; otherwise the newest observation wins.** This replaces the fill-blank rule. `contacts.manual_fields` and `companies.manual_fields` list the columns a person or agent has set. Machine writers (ingest, enrichment) overwrite a column only when it is not in `manual_fields` and the new value is non-empty. Human or agent edits add the column to `manual_fields`, and `ResetContactField` / `ResetCompanyField` remove it. Machine data can now correct a filled field, and re-enrichment does something.
2. **`crm.MergeContacts(ctx, actor, keep, drop)`**, triggered by a person or an agent through REST, MCP, or the UI, never automatically. It moves identities, notes, tags and activities to `keep`, and fills fields on `keep` from `drop`.
3. **A fixture guard.** A test iterates `signup.All()` and fails when a provider has no `testdata/<name>/*.json` golden delivery.
4. **A Halted enrichment outcome.** On a context.dev 401, 402 or 403 the attempt is refunded and every worker pauses. The reason and its expiry are stored in `enrichment_halt`, so Settings can show them.
5. **Parse-only `crm.Email` and `crm.Domain`.** Their field is unexported, so `ParseEmail`, `ParseDomain`, `UnmarshalText` and `Scan` are the only ways to build one.

Rejected from candidate 1: the event log as source of truth, which costs a global write lock, a refold on deploy, an erasure exception and a forever payload format. Also rejected: API-key-only UI login with the setup link printed in the logs, which is clunkier on Unkey Deploy than seeding from `ADMIN_EMAIL` and `ADMIN_PASSWORD`.

## Tradeoffs accepted

- We accept that a manual edit pins a field until someone resets it, in exchange for machine data that can correct everything else. If a person typed "Ada", a later Clerk "Ada Lovelace" does not replace it until `ResetContactField` runs.
- We accept `init()` self-registration, which is mild magic, in exchange for one-file providers. This is the same pattern as `database/sql` drivers and `image` decoders, so most Go readers already know it.
- We accept domain types doubling as the JSON wire shape (`crm.Contact`, `crm.ContactPatch`) in exchange for one shape across REST, MCP and the UI. Renaming a field is an API change, and the OpenAPI test makes that visible.
- We accept a wide `Service` (about 25 methods) in exchange for zero layers between a transport and its SQL.
- We accept no trigram index in exchange for a migration that runs on every managed Postgres. Search does sequential scans, which is fine below roughly 100k contacts (a guess).
- We accept ignoring phone-only and anonymous users in exchange for `contacts.email NOT NULL`, which makes identity resolution one rule.
- We accept that a crash mid-call can double-spend one context.dev lookup in exchange for lease-based recovery with no coordinator.
- We accept stdlib `net/http` for context.dev and Unkey instead of their SDKs. We need two or four calls, the job machine owns retries, and Unkey's SDK v3 would force Go 1.26.10 or later.
- We accept `provider_connections.secret` stored in the clear, since it has to be readable to verify HMACs. Anyone who can read the database already owns the CRM.

## Alternatives considered

- **Event log as source of truth, with state as projections.** This makes replay and audit free, but every reader has to understand projection rebuilds and ordering, and the provider order problem becomes a fold-order problem. It hides less from callers than upserts do, and it exposes a second representation to keep in sync. It lost on reader load.
- **Separate per-source columns merged at read time** (`provider_name`, `enriched_name`, `name`, displayed as `COALESCE(name, enriched_name, provider_name)`). This is more correct, since machine data stays fresh. But every read query and every filter carries the merge, and search has to match merged values. It is a clean fallback if a single column per field proves too lossy.
- **A delivery log table (`webhook_deliveries(provider, delivery_id)`) as the idempotency gate.** It lost because Supabase sends no id, dashboard replays mint new ids, and natural keys already make every write convergent. The log would be a second mechanism for the same guarantee.
- **Generic typed REST handlers that derive the OpenAPI spec through reflection.** This removes the OpenAPI sync test, but it adds a binder with struct tags for path and query parameters, which is a framework a newcomer has to learn. A plain handler plus one test was judged more boring.
- **An adapter interface (`type Adapter interface{ Verify; Parse }`)** instead of a struct of funcs. It is equivalent, but the struct puts name, setup text, scheme and parser in one literal per file, which reads better.

## Open questions and risks

- Resolution is exact-email only. A person who signs up with a work address and later with a personal one stays two contacts until someone runs `MergeContacts`. Is a "possible duplicates" hint (same name, different email) worth adding to the UI?
- Unkey v2 `keys.verifyKey` has no `apiId` filter. Is checking the returned `keyspaceId` against the configured API's keyspace correct, and how do we learn that keyspace id from `UNKEY_API_ID` (`apis.getApi`)? Without the check, any key in the workspace verifies.
- Does Clerk send `user.updated` on every sign-in? If so, `last_seen_at` becomes "last sign-in", which may be a useful signal. Is it?
- Login has no rate limit beyond PBKDF2 cost. Is a per-email failure counter in v1 scope?
- `minScore = 70` for context.dev person matches is a guess. It needs real data.
- Go cannot stop `httpapi` from importing pgx and writing SQL, since anything under `internal/` is importable inside the module. Should we add a test that runs `go list -deps` and fails when packages other than `crm`, `auth` and `db` import pgx?

## Next implementation step

Implement `crm.Ingest` against a real Postgres (testcontainers or `DATABASE_URL` in CI) with table tests for retry, replay, out-of-order update, two-provider race and free-mail, then `ParseEmail`, `IsFreeMail` and `parseClerk` fixtures.
