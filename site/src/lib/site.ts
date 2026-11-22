// Facts the whole site shares: where it lives, the repository, and the
// order of the docs.
export const origin = "https://kyring.dev";
export const repo = "https://github.com/unkeyed/keyring";

export const title = "Keyring: everyone you deal with, kept in one place";
export const summary =
  "Keyring is an open source CRM you run yourself. Every sign-up from your auth provider becomes a contact with a job title and a company, ready for your team and your agents.";

// The docs sidebar, in reading order. scripts/check-docs.mjs fails the build
// when a file under docs/ is missing here or a listed page does not exist.
export const sections = [
  { title: "Get started", pages: ["self-hosting", "deploy-unkey", "configuration", "sign-in", "agents"] },
  {
    title: "Auth providers",
    pages: ["providers/clerk", "providers/workos", "providers/better-auth", "providers/supabase", "providers/auth0", "providers/stytch"],
  },
];

// Short sidebar labels where a page's title is long.
export const labels: Record<string, string> = {
  "self-hosting": "Self-host",
  "deploy-unkey": "Deploy on Unkey",
  agents: "Agents",
};
