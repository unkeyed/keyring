import type { APIRoute } from "astro";
import { textHeaders } from "../lib/docs";
import { origin } from "../lib/site";

// Every crawler, search and AI alike, may read the whole site.
export const GET: APIRoute = () =>
  new Response(`User-agent: *\nAllow: /\n\nSitemap: ${origin}/sitemap.xml\n`, { headers: textHeaders });
