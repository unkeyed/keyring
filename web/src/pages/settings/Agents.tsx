import { useState } from "react";
import { Link } from "react-router";
import type { CreatedKey } from "../../api";
import { CodeBlock } from "../../ui";
import { signupCurl } from "../Onboarding";
import { CreateKeyForm, NewKey } from "./ApiKeys";
import { SettingsHeader, SettingsSubhead } from "./Settings";

export function snippets(origin: string, key: string) {
  return {
    claude: `claude mcp add --transport http uncrm ${origin}/mcp --header "Authorization: Bearer ${key}"`,
    cursor: JSON.stringify({ mcpServers: { uncrm: { url: `${origin}/mcp`, headers: { Authorization: `Bearer ${key}` } } } }, null, 2),
    curl: `curl "${origin}/v1/contacts?q=acme.com" \\\n  -H "Authorization: Bearer ${key}"`,
  };
}

export function Agents() {
  const [created, setCreated] = useState<CreatedKey | null>(null);
  const s = snippets(location.origin, created?.key ?? "<your-api-key>");

  return (
    <>
      <SettingsHeader title="Agents & MCP">
        Give an agent the same CRM your team sees. The MCP server at <code className="font-mono text-fg">/mcp</code> lets it search, read and update contacts;
        the REST API does the same over HTTP.
      </SettingsHeader>

      <section className="space-y-3">
        <SettingsSubhead className="mb-0">1. Create a key for the agent</SettingsSubhead>
        {created ? (
          <NewKey k={created} />
        ) : (
          <>
            <CreateKeyForm defaultName="Claude Code" onCreated={setCreated} />
            <p className="text-xs text-faint">
              The snippets below fill in with the new key. You can also manage keys under{" "}
              <Link to="/settings/keys" className="text-fg underline underline-offset-2 hover:opacity-70">
                API keys
              </Link>
              .
            </p>
          </>
        )}
      </section>

      <section className="mt-8 space-y-4">
        <SettingsSubhead className="mb-0">2. Connect it</SettingsSubhead>
        <CodeBlock label="Claude Code" code={s.claude} />
        <CodeBlock label="Cursor (.cursor/mcp.json)" code={s.cursor} />
        <CodeBlock label="REST (curl)" code={s.curl} />
        <CodeBlock label="Send a sign-up from any backend" code={signupCurl(location.origin).replace("$UNCRM_API_KEY", created?.key ?? "$UNCRM_API_KEY")} />
        <p className="text-xs text-muted">
          Every route is described in{" "}
          <a href="/v1/openapi.yaml" className="text-fg underline underline-offset-2 hover:opacity-70">
            openapi.yaml
          </a>
          .
        </p>
      </section>
    </>
  );
}
