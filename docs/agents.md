# Connect agents to Keyring

Agents reach Keyring in two ways. The MCP server at `/mcp` gives an agent tools to search, read, and update contacts. The REST API under `/v1` does the same over plain HTTP. Both call the same code as the web UI, so an agent sees exactly what your team sees.

They sign in differently. An MCP client signs in as you, in your browser, and needs no key. The REST API takes an API key.

In the examples, replace `https://crm.example.com` with your Keyring URL, which is its `PUBLIC_URL`.

## Connect an MCP client

Keyring's MCP server uses the Streamable HTTP transport, without sessions, at `https://crm.example.com/mcp`. Add that URL to your client with no `Authorization` header. **Settings > Agents & MCP** shows the snippets with your URL filled in.

### Claude Code

```sh
claude mcp add --transport http keyring https://crm.example.com/mcp
```

Then run `/mcp` in Claude Code, pick `keyring`, and choose **Authenticate**.

### Cursor

Add Keyring to `.cursor/mcp.json` in your project, or to `~/.cursor/mcp.json` for every project:

```json
{
	"mcpServers": {
		"keyring": {
			"url": "https://crm.example.com/mcp"
		}
	}
}
```

Cursor then asks you to log in to Keyring from its MCP settings.

### Claude Desktop

Claude Desktop connects through [`mcp-remote`](https://www.npmjs.com/package/mcp-remote), which needs Node.js and signs in the same way.

1. In Claude Desktop, open **Settings > Developer** and click **Edit Config**.
2. Add Keyring to `claude_desktop_config.json`:

   ```json
   {
   	"mcpServers": {
   		"keyring": {
   			"command": "npx",
   			"args": ["-y", "mcp-remote", "https://crm.example.com/mcp"]
   		}
   	}
   }
   ```

3. Restart Claude Desktop.

### What happens when you connect

1. The client calls `/mcp` without a token. Keyring answers `401` with a `WWW-Authenticate` header that points to `/.well-known/oauth-protected-resource/mcp`.
2. The client reads that document and the authorization server metadata at `/.well-known/oauth-authorization-server`, registers itself, and opens your browser at Keyring.
3. You sign in to Keyring if you are not signed in already. Keyring then shows the app's name and where your browser goes after you approve, such as `127.0.0.1:33418` for an app on your computer.
4. You approve, and the browser hands a one-time code back to the client. The client trades it for an access token, which lasts an hour, and a refresh token, which lasts 30 days from its last use.

From then on the agent acts as you. Its changes show on the timeline as `Claude Code (via Alex Rivera)`, with the name the app registered and your name. Like an API key, it can read and change contacts, companies, notes, and tags, and cannot manage keys, providers, teammates, or stages.

Any app can register with any name, so the consent page marks every app **Unverified**. Approve only an app you just started connecting, and check where it sends you back. Approving needs a sign-in within the last 15 minutes, the same as creating an API key.

### Disconnect an app

**Settings > Agents & MCP** lists every connected app, who approved it, and when it was last used. **Disconnect** stops its tokens at once. Keyring also ends a connection when:

- the client signs out, through the revocation endpoint;
- a refresh token is used twice, because then someone other than the client holds a copy;
- the person who approved it is removed from the team.

### API keys and `/mcp`

`/mcp` does not take API keys. A client configured with an `Authorization: Bearer keyring_...` header gets `401`. Remove the header, and the client signs in through the browser instead.

## Create an API key

The REST API takes API keys.

1. In Keyring, open **Settings > API keys**, or **REST API** under **Settings > Agents & MCP**.
2. Name the key after what uses it, for example `Signup backend`, and create it.
3. Copy the key. Keyring shows it only once.

An API key can read and change contacts, companies, notes, and tags, and can post sign-ups. Only a person logged in to the web UI can manage API keys, providers, teammates, and stages, so a leaked key cannot create more keys.

## MCP tools

Keyring's MCP server has 24 tools. Every REST operation an API key can call has one, except `POST /v1/signups`, which is for auth systems. A test keeps the two in step.

| Tool | REST | What it does |
|---|---|---|
| `search_contacts` | `GET /v1/contacts` | Searches contacts by text, exact email, tag, stage, auth provider, company, or sign-up time. Returns a page of short summaries and a cursor for the next. |
| `get_contact` | `GET /v1/contacts/{id}` | Gets one contact by id or any email it used, with identities, notes, and the newest 50 timeline entries. |
| `create_contact` | `POST /v1/contacts` | Adds a person by email, such as someone met at an event. Finds the company from the email domain and queues enrichment. |
| `update_contact` | `PATCH /v1/contacts/{id}` | Changes a contact's fields, stage, company, or tags. |
| `bulk_update_contacts` | `POST /v1/contacts/bulk` | Sets one stage and adds or removes tags on up to 200 contacts. Answers one result per id. |
| `set_stage` | `PATCH /v1/contacts/{id}` | Moves a contact to a pipeline stage. |
| `reset_contact_field` | `DELETE /v1/contacts/{id}/manual_fields/{field}` | Gives a field back to the auth provider and enrichment, so their next value shows. |
| `merge_contacts` | `POST /v1/contacts/{id}/merge` | Merges two contacts that are the same person. Moves the identities, notes, tags, and history of `drop` to `keep`, then deletes `drop`. |
| `delete_contact` | `DELETE /v1/contacts/{id}` | Erases a contact with its identities, tags, notes, and timeline. Cannot be undone. |
| `add_note` | `POST /v1/contacts/{id}/notes` | Adds a Markdown note to a contact's timeline. |
| `delete_note` | `DELETE /v1/notes/{id}` | Deletes a note. The timeline records who deleted it. |
| `list_activity` | `GET /v1/activity` | Pages the timeline, filtered by contact, company, kind, and time. |
| `list_recent_signups` | `GET /v1/activity?kind=signed_up` | Pages the newest sign-ups from every provider. |
| `get_company` | `GET /v1/companies/{id}` | Gets a company by id or domain, with its newest 50 contacts and timeline. |
| `search_companies` | `GET /v1/companies` | Searches companies by name or exact domain. Returns a page of short summaries and a cursor for the next. |
| `create_company` | `POST /v1/companies` | Adds a company by domain and queues enrichment. Free-mail domains are refused. |
| `update_company` | `PATCH /v1/companies/{id}` | Changes a company's fields. |
| `reset_company_field` | `DELETE /v1/companies/{id}/manual_fields/{field}` | Gives a company field back to enrichment. |
| `delete_company` | `DELETE /v1/companies/{id}` | Deletes a company and its timeline. Its contacts stay, unlinked. Cannot be undone. |
| `enrich_contact` | `POST /v1/contacts/{id}/enrich` | Queues a new context.dev lookup for a contact and its company. |
| `enrich_company` | `POST /v1/companies/{id}/enrich` | Queues a new context.dev lookup for a company. |
| `list_stages` | `GET /v1/stages/counts` | Lists the pipeline stages in order with how many contacts are in each. New contacts start in the first one. |
| `list_tags` | `GET /v1/tags` | Lists every tag in use and how many contacts have it. |
| `get_status` | `GET /v1/status` | Counts contacts and companies, says whether enrichment is on or paused and why, and names the API key store. |

A value that a person or an agent sets wins over machine data. After an agent sets a contact's `title`, a later webhook or enrichment does not change it. `reset_contact_field` removes that hold.

Read tools are marked read-only, and `delete_contact`, `delete_company`, `delete_note`, and `merge_contacts` are marked destructive, so a client can ask before it runs them. Closed sets such as fields and activity kinds are enums in each tool's schema. Time filters take an RFC 3339 time or a date such as `2026-01-02`.

Names, bios, descriptions, taglines, and notes come from sign-up providers, context.dev, and other people. The server tells agents to treat that text as data, not as instructions. Keyring does not store or return the raw webhook payload of a sign-up.

When a tool fails, its error says what to do next. A duplicate names the existing record, such as `contact 12 already has ada@stripe.com; use get_contact {"id":12} or update_contact`. An unknown stage, field, or activity kind lists the valid ones. A missing record names the tool that finds one. An internal failure shows only a `request_id`, and the server log has the cause under the same id.

## Use the REST API

Send the key in the `Authorization` header. In the examples, replace `keyring_...` with your key. Request and response bodies are JSON.

Record a sign-up from any backend:

```sh
curl -X POST https://crm.example.com/v1/signups \
	-H "Authorization: Bearer keyring_..." \
	-H "Content-Type: application/json" \
	-d '{"email": "ada@example.com", "name": "Ada Lovelace"}'
```

The first call answers `201` with `{"contact_id": 1, "created": true, "duplicate": false}`. The same call again answers `200` with `"duplicate": true` and creates nothing, so retries are safe.

Add a person who never signed up. Only `email` is required, and `name`, `title`, `location`, `linkedin_url`, `stage`, and `tags` are optional:

```sh
curl -X POST https://crm.example.com/v1/contacts \
	-H "Authorization: Bearer keyring_..." \
	-H "Content-Type: application/json" \
	-d '{"email": "grace@example.com", "name": "Grace Hopper", "stage": "qualified"}'
```

The answer is `201` with the contact. If the email is already in Keyring, the answer is `409` with `{"error": "...", "id": 12}`, where `id` is the existing contact. `POST /v1/companies` with `{"domain": "example.com"}` adds a company the same way.

Search contacts. The `q` parameter matches email, name, and company:

```sh
curl "https://crm.example.com/v1/contacts?q=example.com" \
	-H "Authorization: Bearer keyring_..."
```

The answer is `{"items": [...], "next_cursor": "..."}`. To get the next page, pass `next_cursor` as `cursor`.

Move a contact to a stage and tag it:

```sh
curl -X PATCH https://crm.example.com/v1/contacts/1 \
	-H "Authorization: Bearer keyring_..." \
	-H "Content-Type: application/json" \
	-d '{"stage": "qualified", "add_tags": ["beta"]}'
```

Add a note:

```sh
curl -X POST https://crm.example.com/v1/contacts/1/notes \
	-H "Authorization: Bearer keyring_..." \
	-H "Content-Type: application/json" \
	-d '{"body": "Asked about SSO on the onboarding call."}'
```

Find a contact by exact email, including any email the person signed up with, with `GET /v1/contacts?email=ada@example.com`. Find a company by domain with `GET /v1/companies?domain=example.com`. Both answer a page like a search.

Tag many contacts at once:

```sh
curl -X POST https://crm.example.com/v1/contacts/bulk \
	-H "Authorization: Bearer keyring_..." \
	-H "Content-Type: application/json" \
	-d '{"ids": [1, 2, 3], "stage": "qualified", "add_tags": ["webinar"]}'
```

The answer is `200` with `{"results": [{"id": 1, "ok": true}, {"id": 2, "ok": false, "error": "no contact 2"}, ...]}`. A missing contact fails alone. Up to 200 ids fit in one call.

`DELETE /v1/contacts/{id}` erases a contact with its identities, notes, and timeline, and answers `204`. `DELETE /v1/companies/{id}` deletes a company and leaves its contacts unlinked. Neither can be undone.

`GET /v1/activity` pages the timeline like a search. It takes `kind`, `contact_id`, `company_id`, `since`, `before`, `cursor`, and `limit`. `GET /v1/stages/counts` lists each stage with its number of contacts.

The enrich routes answer `202` when a lookup is queued, and `200` with `{"state": "skipped"}` when there is nothing to look up, such as a free-mail contact.

An error answers with a status code and `{"error": "<message>"}`:

| Status | Meaning |
|---|---|
| `400` | The request body or a parameter is invalid. An unknown stage, field, or activity kind lists the valid ones. |
| `401` | The key is missing, unknown, or revoked. |
| `403` | The route is for people logged in to the web UI, not for API keys. |
| `404` | The contact, company, note, or route does not exist. |
| `405` | The path exists but not with this method. The `Allow` header lists the methods it takes. |
| `409` | The contact or company to create already exists. The body's `id` names it. |
| `429` | Too many failed attempts. Wait the number of seconds in the `Retry-After` header. |
| `503` | The database or key store is down. Retry shortly. |

A `5xx` answer also has a `request_id`. The server log records the cause under the same id.

### API reference

Keyring serves its OpenAPI spec at `https://crm.example.com/v1/openapi.yaml`. The spec is built into the binary, so it describes the version you run. No API key is needed to read it.
