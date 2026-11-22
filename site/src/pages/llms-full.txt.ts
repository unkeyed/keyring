import type { APIRoute } from "astro";
import { allDocs, markdown, textHeaders } from "../lib/docs";
import { origin } from "../lib/site";

// /llms-full.txt: every doc's Markdown in one file, in sidebar order.
export const GET: APIRoute = async () => {
  const docs = await allDocs();
  const parts = docs.map((d) => `<!-- ${origin}/docs/${d.id} -->\n\n${markdown(d).trim()}\n`);
  return new Response(parts.join("\n---\n\n"), { headers: textHeaders });
};
