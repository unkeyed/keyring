import { useState } from "react";
import { Link } from "react-router";
import type { CreatedKey } from "../../api";
import { CodeBlock } from "../../ui";
import { signupCurl } from "../Onboarding";
import { CreateKeyForm, NewKey } from "./ApiKeys";
import { SettingsHeader } from "./Settings";

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
        <h3 className="text-[13px] font-semibold">1. Create a key for the agent</h3>
        {created ? (
          <NewKey k={created} />
        ) : (
          <>
            <CreateKeyForm defaultName="Claude Code" onCreated={setCreated} />
            <p className="text-xs text-faint">
              The snippets below fill in with the new key. You can also manage keys under{" "}
              <Link to="/settings/keys" className="text-accent hover:underline">
                API keys
              </Link>
              .
            </p>
          </>
        )}
      </section>

      <section className="mt-8 space-y-4">
        <h3 className="text-[13px] font-semibold">2. Connect it</h3>
        <CodeBlock label="Claude Code" code={s.claude} />
        <CodeBlock label="Cursor (.cursor/mcp.json)" code={s.cursor} />
        <CodeBlock label="REST (curl)" code={s.curl} />
        <CodeBlock label="Send a sign-up from any backend" code={signupCurl(location.origin).replace("$UNCRM_API_KEY", created?.key ?? "$UNCRM_API_KEY")} />
        <p className="text-xs text-muted">
          Every route is described in{" "}
          <a href="/v1/openapi.yaml"className="text-accent hover:underline">
            openapi.yaml
          </a>
          .
        </p>
      </section>
    </>
  );
}
