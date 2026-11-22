import type { APIRoute } from "astro";
import { markdownHeaders } from "../lib/docs";
import { deployPrompt } from "../lib/prompt";

// /deploy-prompt.md: the deploy prompt from the landing page, for agents.
export const GET: APIRoute = () => new Response(deployPrompt.trim() + "\n", { headers: markdownHeaders });
