import type { APIRoute } from "astro";
import { allDocs, description, title, textHeaders } from "../lib/docs";
import { origin, repo, sections } from "../lib/site";

// /llms.txt (https://llmstxt.org): what Keyring is and where each doc's
// Markdown lives, for language models and agents.
export const GET: APIRoute = async () => {
  const docs = await allDocs();
  const byId = new Map(docs.map((d) => [d.id, d]));
  const lines = [
    "# Keyring",
    "",
    "> Keyring is an open source CRM you run yourself. Every person who signs up through your auth provider (Clerk, WorkOS AuthKit, Better Auth, Supabase Auth, Auth0, Stytch, or any backend over HTTP) becomes a contact, enriched with their job title and company from context.dev. Your team works in a web UI, and agents use an MCP server (OAuth sign-in) and a REST API (scoped API keys). It runs as one container next to Postgres, is MIT licensed, and is provided by Unkey.",
    "",
    `To deploy Keyring on Unkey with a coding agent, add Unkey's MCP server (\`claude mcp add --transport http unkey https://mcp.unkey.com\`) and follow the prompt at ${origin}/deploy-prompt.md.`,
    "",
  ];
  for (const s of sections) {
    lines.push(`## ${s.title === "Get started" ? "Docs" : s.title}`, "");
    for (const id of s.pages) {
      const d = byId.get(id)!;
      lines.push(`- [${title(d)}](${origin}/docs/${id}.md): ${description(d)}`);
    }
    lines.push("");
  }
  lines.push(
    "## Optional",
    "",
    `- [Deploy Keyring on Unkey](${origin}/deploy-prompt.md): The prompt a coding agent follows to deploy Keyring through Unkey's MCP server.`,
    `- [Full documentation](${origin}/llms-full.txt): Every doc above in one Markdown file.`,
    `- [Source code](${repo}): The Go server, React UI, and these docs.`,
    `- [OpenAPI spec](${repo}/blob/main/internal/httpapi/openapi.yaml): Every REST route. A running Keyring also serves it at /v1/openapi.yaml.`,
    "",
  );
  return new Response(lines.join("\n"), { headers: textHeaders });
};
