import type { APIRoute } from "astro";
import { allDocs } from "../lib/docs";
import { origin } from "../lib/site";

export const GET: APIRoute = async () => {
  const paths = ["/", "/docs/", ...(await allDocs()).map((d) => `/docs/${d.id}`)];
  const body = `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
${paths.map((p) => `  <url><loc>${origin}${p}</loc></url>`).join("\n")}
</urlset>
`;
  return new Response(body, { headers: { "Content-Type": "application/xml; charset=utf-8" } });
};
