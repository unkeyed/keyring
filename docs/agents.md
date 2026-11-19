# Connect agents to uncrm

Agents reach uncrm in two ways. The MCP server at `/mcp` gives an agent tools to search, read, and update contacts. The REST API under `/v1` does the same over plain HTTP. Both use the same API keys and call the same code as the web UI, so an agent sees exactly what your team sees.

In the examples, replace `https://crm.example.com` with your uncrm URL and `uncrm_...` with your API key.

## Create an API key

1. In uncrm, open **Settings > Agents & MCP**.
2. Name the key after the agent that uses it, for example `Claude Code`, and create it.
3. Copy the key. uncrm shows it only once.

After you create the key, the page shows the setup snippets below with your URL and key filled in. **Settings > API keys** lists and revokes keys.

An API key can read and change contacts, companies, notes, and tags, and can post sign-ups. Only a person logged in to the web UI can manage API keys, providers, teammates, and stages, so a leaked agent key cannot create more keys.

## Connect an MCP client

uncrm's MCP server uses the Streamable HTTP transport, without sessions, at `https://crm.example.com/mcp`. Each request carries the key in an `Authorization: Bearer` header.

### Claude Code

```sh
claude mcp add --transport http uncrm https://crm.example.com/mcp --header "Authorization: Bearer uncrm_..."
```

Run `/mcp` in Claude Code to check that `uncrm` is connected.

### Cursor

Add uncrm to `.cursor/mcp.json` in your project, or to `~/.cursor/mcp.json` for every project:

```json
{
	"mcpServers": {
		"uncrm": {
			"url": "https://crm.example.com/mcp",
			"headers": {
				"Authorization": "Bearer uncrm_..."
			}
		}
	}
}
```

### Claude Desktop

Claude Desktop cannot send a custom header to a remote MCP server, so it connects through [`mcp-remote`](https://www.npmjs.com/package/mcp-remote), which needs Node.js.

1. In Claude Desktop, open **Settings > Developer** and click **Edit Config**.
2. Add uncrm to `claude_desktop_config.json`:

   ```json
   {
   	"mcpServers": {
   		"uncrm": {
   			"command": "npx",
   			"args": ["-y", "mcp-remote", "https://crm.example.com/mcp", "--header", "Authorization:${UNCRM_AUTH}"],
   			"env": {
   				"UNCRM_AUTH": "Bearer uncrm_..."
   			}
   		}
   	}
   }
   ```

3. Restart Claude Desktop.

The header value comes from `env` because some clients split arguments at spaces, and `Bearer uncrm_...` has one.

## MCP tools

| Tool | What it does |
|---|---|
| `search_contacts` | Searches contacts by text, tag, stage, or auth provider. Returns one page and a cursor for the next. |
| `get_contact` | Gets one contact by id or email, with identities, notes, and recent activity. |
| `update_contact` | Changes a contact's fields, stage, company, or tags. |
| `reset_contact_field` | Gives a field back to the auth provider and enrichment, so their next value shows. |
| `merge_contacts` | Merges two contacts that are the same person. Moves the identities, notes, tags, and history of `drop` to `keep`, then deletes `drop`. |
| `add_note` | Adds a Markdown note to a contact's timeline. |
| `list_recent_signups` | Lists the newest sign-ups from every provider. |
| `get_company` | Gets a company and its contacts, by id or domain. |
| `enrich_contact` | Queues a new context.dev lookup for a contact and its company. |

A value that a person or an agent sets wins over machine data. After an agent sets a contact's `title`, a later webhook or enrichment does not change it. `reset_contact_field` removes that hold.

## Use the REST API

Send the key in the `Authorization` header. Request and response bodies are JSON.

Record a sign-up from any backend:

```sh
curl -X POST https://crm.example.com/v1/signups \
	-H "Authorization: Bearer uncrm_..." \
	-H "Content-Type: application/json" \
	-d '{"email": "ada@example.com", "name": "Ada Lovelace"}'
```

The first call answers `201` with `{"contact_id": 1, "created": true, "duplicate": false}`. The same call again answers `200` with `"duplicate": true` and creates nothing, so retries are safe.

Search contacts. The `q` parameter matches email, name, and company:

```sh
curl "https://crm.example.com/v1/contacts?q=example.com" \
	-H "Authorization: Bearer uncrm_..."
```

The answer is `{"items": [...], "next_cursor": "..."}`. To get the next page, pass `next_cursor` as `cursor`.

Move a contact to a stage and tag it:

```sh
curl -X PATCH https://crm.example.com/v1/contacts/1 \
	-H "Authorization: Bearer uncrm_..." \
	-H "Content-Type: application/json" \
	-d '{"stage": "qualified", "add_tags": ["beta"]}'
```

Add a note:

```sh
curl -X POST https://crm.example.com/v1/contacts/1/notes \
	-H "Authorization: Bearer uncrm_..." \
	-H "Content-Type: application/json" \
	-d '{"body": "Asked about SSO on the onboarding call."}'
```

An error answers with a status code and `{"error": "<message>"}`:

| Status | Meaning |
|---|---|
| `400` | The request body or a parameter is invalid. |
| `401` | The key is missing, unknown, or revoked. |
| `403` | The route is for people logged in to the web UI, not for API keys. |
| `404` | The contact, company, or route does not exist. |

### API reference

uncrm serves its OpenAPI spec at `https://crm.example.com/v1/openapi.yaml`. The spec is built into the binary, so it describes the version you run. No API key is needed to read it.
