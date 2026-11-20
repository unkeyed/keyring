# Deploy Keyring to Unkey Deploy

This guide puts Keyring on [Unkey Deploy](https://www.unkey.com/docs/compute/get-started/deploy-from-github) (Unkey's docs also call it Unkey Compute), connected to a Postgres database you host elsewhere. At the end, Keyring runs at an `unkey.app` URL and your auth provider sends it every sign-up.

You need:

- An Unkey account with a Compute plan. Unkey needs a plan before it deploys anything.
- A Postgres database. Unkey Deploy has no managed database and no persistent disk.
- For the GitHub path, a GitHub account and your own fork of `unkeyed/keyring`.

## 1. Create the database

Create a Postgres database with any managed provider, such as Neon, Supabase, PlanetScale Postgres, or Amazon RDS. Pick a region close to the Unkey region you plan to use. Unkey runs in `us-east-1` by default.

Copy the connection URL. Most managed providers require TLS, so keep `sslmode=require` if the provider includes it:

```text
postgres://keyring:secret@ep-example-123.us-east-1.aws.neon.tech/keyring?sslmode=require
```

Keyring creates its tables the first time it starts. You do not run any SQL yourself.

## 2. Create the Unkey project

In the [Unkey dashboard](https://app.unkey.com), open **Projects** and click **Create project**. Note the project's slug. The CLI path in step 4 uses it.

## 3. Set the environment variables

Unkey stores variables per environment. Set these on the **production** environment of your project. Mark every secret as **Sensitive**.

| Variable | Value |
|---|---|
| `DATABASE_URL` | The connection URL from step 1. Sensitive. |
| `PUBLIC_URL` | Your deployment's URL from step 6, such as `https://keyring-acme.unkey.app`. Set it after the first deploy and redeploy. |
| `SETUP_TOKEN` | 32 or more random characters, for example from `openssl rand -hex 32`. Sensitive. You open the setup link with it in step 6. |
| `CONTEXT_DEV_API_KEY` | Optional. Your [context.dev](https://context.dev) key, which turns on enrichment. Sensitive. |
| `UNKEY_ROOT_KEY` | Optional. See [Store API keys in Unkey](#store-api-keys-in-unkey). Sensitive. |
| `UNKEY_API_ID` | Optional. Set it together with `UNKEY_ROOT_KEY`. |

Do not set `PORT`. Unkey sets it, and Keyring listens on it.

Unkey applies a variable change only to the next deployment. After you change a variable, redeploy. [Configuration](configuration.md) describes each variable in full.

## 4. Deploy Keyring

Choose one of the two paths. The GitHub path rebuilds on every push. The image path deploys a published release and needs no fork.

### Deploy from GitHub

1. Fork `unkeyed/keyring` on GitHub.
2. In your Unkey project, deploy a new app from a **GitHub repository**. Install the Unkey GitHub App when Unkey asks, and pick your fork.
3. In the app's build settings, set the **Dockerfile** path to `Dockerfile`. Without a path, Unkey tries to detect the build on its own instead of using Keyring's Dockerfile.
4. Deploy.

From now on, each push to your fork's default branch deploys to production. Pushes to other branches create preview deployments.

Preview deployments use the **preview** environment and its own variables. If you set `DATABASE_URL` on preview, use a separate database. A preview that points at the production database applies its migrations to production data.

### Deploy the prebuilt image

The image `ghcr.io/unkeyed/keyring` is public. Each release is tagged with its version, for example `0.1.0`, and the newest release is also `latest`.

1. Install the Unkey CLI and log in:

   ```sh
   npm install -g unkey
   unkey login
   ```

2. Deploy the image to production. Replace `<project-slug>` with the slug from step 2:

   ```sh
   unkey deploy ghcr.io/unkeyed/keyring:latest --project=<project-slug> --env=production
   ```

   Without `--env=production`, the CLI deploys to the preview environment.

To upgrade later, run the same command with a newer version tag.

## 5. Add the health check

In the environment's runtime settings, add a health check with method `GET` and path `/healthz`. Unkey then sends traffic to an instance only after `/healthz` answers 200, and restarts an instance that stops answering.

`/healthz` checks only that the process runs. It does not query the database, so a database outage does not restart every instance.

You can also set the health check through Unkey's API. Replace `keyring` and `default` with your project and app slugs:

```sh
curl -X POST https://api.unkey.com/v2/environments.updateSettings \
	-H "Authorization: Bearer $UNKEY_ROOT_KEY" \
	-H "Content-Type: application/json" \
	-d '{"project":"keyring","app":"default","environment":"production","healthcheck":{"method":"GET","path":"/healthz"}}'
```

Redeploy after you add the health check.

## 6. Create the first account

Find your deployment's URL. Unkey shows it on the deployment page. It has the form `https://<project>-<workspace>.unkey.app`, or `https://<project>-<app>-<workspace>.unkey.app` when the app slug is not `default`. If `PUBLIC_URL` is not that URL yet, set it and redeploy, because passkeys only work at the URL in `PUBLIC_URL`.

Open `<PUBLIC_URL>/setup/<SETUP_TOKEN>`. Enter your name and email and register a passkey. You are the first user, and the link stops working. The deployment logs print the same link. [Sign-in](sign-in.md) explains how to add Google, GitHub, or your own identity provider.

If the page does not load, open the deployment's logs. Keyring logs the name of any missing or invalid variable and exits, for example `DATABASE_URL is required`.

## 7. Connect your auth provider

In Keyring, open **Settings > Providers** and pick your provider. Each provider sends its webhooks to `https://<your-domain>/webhooks/<provider>`:

| Provider | Webhook URL | Guide |
|---|---|---|
| Clerk | `https://<your-domain>/webhooks/clerk` | [Clerk](providers/clerk.md) |
| WorkOS AuthKit | `https://<your-domain>/webhooks/workos` | [WorkOS AuthKit](providers/workos.md) |
| Better Auth | `https://<your-domain>/webhooks/better-auth` | [Better Auth](providers/better-auth.md) |
| Supabase Auth | `https://<your-domain>/webhooks/supabase` | [Supabase Auth](providers/supabase.md) |
| Auth0 | `https://<your-domain>/webhooks/auth0` | [Auth0](providers/auth0.md) |
| Stytch | `https://<your-domain>/webhooks/stytch` | [Stytch](providers/stytch.md) |

Send a test event from the provider. **Settings > Providers** shows the delivery, or the reason it failed, within a few seconds.

## Store API keys in Unkey

By default, Keyring stores the API keys that agents use in its own database. To manage them in Unkey's API key product instead:

1. In the Unkey dashboard, create an API for Keyring's keys and copy its ID. It starts with `api_`.
2. Create a root key with these permissions on that API: `create_key`, `read_key`, `read_api`, `verify_key`, and `delete_key`.
3. Set `UNKEY_ROOT_KEY` to the root key and `UNKEY_API_ID` to the API ID, then redeploy.

Keys created before the switch stop working. Create new keys in **Settings > API keys**.

## If the dashboard differs from this guide

This guide follows Unkey's documentation as of November 2026. Two points could not be checked against a live dashboard:

- The exact labels of menus and buttons, such as **Create project** and the **Dockerfile** build setting, may differ. Look for the setting with the same purpose.
- Unkey may ask you to deploy before it lets you set variables. If so, let the first deployment fail, set the variables from step 3, and redeploy.
