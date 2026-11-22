import type { APIRoute } from "astro";
import { allDocs, markdown, markdownHeaders } from "../../lib/docs";

// /docs/<page>.md: the doc's Markdown source with absolute links, for agents
// and anyone who prefers plain text.
export const GET: APIRoute = async ({ params, rewrite }) => {
  const doc = (await allDocs()).find((d) => d.id === params.slug);
  if (!doc) return rewrite("/404");
  return new Response(markdown(doc), { headers: markdownHeaders });
};
