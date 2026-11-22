import type { APIRoute } from "astro";

// Unkey's health check. It answers while the server runs.
export const GET: APIRoute = () => new Response("ok\n", { headers: { "Content-Type": "text/plain", "Cache-Control": "no-store" } });
