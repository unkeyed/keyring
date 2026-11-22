// The prompt a coding agent follows to deploy Keyring on Unkey through
// Unkey's MCP server. The landing page shows it with a copy button, and
// /deploy-prompt.md serves it as plain Markdown for agents.
export const deployPrompt = `Deploy Keyring, an open source CRM (https://github.com/unkeyed/keyring), to Unkey for me, using Unkey's MCP server. The repository's docs/deploy-unkey.md is the reference for Keyring's settings.

0. Connect to Unkey
   Check that Unkey's MCP tools are available (for example projects.listProjects). If they are not:
   - Claude Code: run  claude mcp add --transport http unkey https://mcp.unkey.com
     then tell me to run /mcp, choose unkey, and Authenticate. MCP tools load when a session starts, so if they still do not appear, tell me to restart the session and paste this prompt again.
   - Any other agent: tell me to follow https://mcp.unkey.com/install and stop until I say it is done.
   Signing in opens Unkey in my browser, where I can also create an account. Do every Unkey step below through the MCP tools.

1. Ask me
   - For a Postgres connection URL (Neon, Supabase, PlanetScale Postgres, RDS or similar), ideally in AWS us-east-1. If the provider gives a read/write role and a separate admin role, ask for both URLs.
   - Whether I want enrichment, and if so for a context.dev API key.

2. Fork Keyring
   Run  gh repo fork unkeyed/keyring --clone  and note the fork as owner/keyring.

3. Create the project and app on Unkey
   - Create a project with slug keyring (projects.createProject).
   - Create an app with slug default, sourced from my fork's default branch (apps.createApp with git.repository). If Unkey's GitHub App cannot see the fork, get the install link with github.installApp, give it to me, and retry once I have granted access.
   - On the production environment (environments.updateSettings): Dockerfile "Dockerfile", root directory ".", port 8080, health check GET /healthz, and regions set to us-east-1 with 1 replica. The default region may be elsewhere, so set it explicitly.

4. Set production variables (environments.setEnvironmentVariables), every secret as writeonly
   - DATABASE_URL: the read/write URL.
   - ENCRYPTION_KEY: generate with  openssl rand -base64 32. Save it to ~/.keyring-encryption-key with chmod 600 and tell me to copy it into my password manager. Unkey cannot show a writeonly value again, and Keyring cannot read its stored secrets without it.
   - SETUP_TOKEN: generate with  openssl rand -hex 32.
   - CLIENT_IP_HEADER: X-Forwarded-For. Unkey's gateway sets it to the real client address, so Keyring throttles each client separately.
   - CONTEXT_DEV_API_KEY: only if I gave one.
   Do not set PORT. Never print a database password, and never write a secret into the repository.

5. Migrations
   If I gave a separate admin URL, apply the migrations from the clone before the first deploy and keep the admin URL off Unkey:
     MIGRATION_DATABASE_URL='the admin url' go run ./cmd/keyring migrate
   Otherwise DATABASE_URL must be able to create tables; Keyring migrates when it starts.

6. Protect it at Unkey's gateway (gateway.setPolicies on production)
   - Firewall, deny: request paths matching dotfiles and secrets (/.env, /.git, /.aws, /.ssh), backups and keys (.sql, .bak, .zip, .pem, .key), PHP and WordPress probes (.php, /wp-admin, /wp-login, /xmlrpc.php, /phpmyadmin, /cgi-bin) and appliance scanners (/actuator, /server-status, /HNAP1), case-insensitive. Never block /.well-known/, /v1/openapi.yaml, /setup/ or /invite/.
   - Rate limits per remote IP: ^/oauth/(register|authorize)$ at 30 a minute, /auth/ at 120 a minute, /webhooks/ at 600 a minute.

7. Deploy
   - Create a deployment on production (deployments.createDeployment) and follow it with deployments.getDeployment and deployments.listBuildLogs until it is ready.
   - Set PUBLIC_URL to the stable address without a deployment id, https://keyring-<workspace>.unkey.app, then deploy again. Variables and gateway policies only apply to a new deployment, and passkeys only work at PUBLIC_URL.

8. Check and hand over
   - PUBLIC_URL/healthz and PUBLIC_URL/auth/setup/SETUP_TOKEN both answer 200, and PUBLIC_URL/.env answers 403.
   - Give me PUBLIC_URL/setup/SETUP_TOKEN to create the first account with a passkey. Tell me to add a second passkey under Settings > Account and to connect my auth provider under Settings > Providers.
   - Finish with a short summary: the URL, the Unkey project, the variables you set, where the encryption key is saved, and anything you could not do.`;
