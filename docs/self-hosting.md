# Self-host Keyring

Keyring is one container plus a Postgres database. This guide runs it with Docker Compose on your own machine, then on any host that runs containers. To deploy on Unkey Deploy, follow [Deploy Keyring to Unkey Deploy](deploy-unkey.md).

## Run it with Docker Compose

You need Docker with Compose v2.

1. Clone the repository, create an encryption key, and start the stack:

   ```sh
   git clone https://github.com/unkeyed/keyring.git
   cd keyring
   echo "ENCRYPTION_KEY=$(openssl rand -base64 32)" >> .env
   docker compose up -d --build --wait
   ```

   `ENCRYPTION_KEY` encrypts the webhook secrets and two-factor seeds Keyring stores, so a database backup alone does not reveal them. Keep `.env` with your other secrets. Compose builds the image, starts Postgres 17 and Keyring, and returns when both pass their health checks.

2. Check that Keyring is up:

   ```sh
   curl -i http://localhost:8080/healthz
   ```

   The answer is `HTTP/1.1 200 OK`.

3. Print the setup link:

   ```sh
   docker compose logs app | grep -A2 'Open this link'
   ```

   Open the link, enter your name and email, and register a passkey with your fingerprint, face, or device PIN. You are the first user. The link works once. [Sign-in](sign-in.md) covers providers, two-factor, and passwords.

Postgres keeps its data in the `db` volume that `docker-compose.yml` declares. `docker compose down` stops the stack and keeps the data. `docker compose down -v` also deletes the data.

### Change the ports

If port 8080 or 5432 is taken, set `KEYRING_PORT` or `KEYRING_DB_PORT` when you start the stack:

```sh
KEYRING_PORT=18080 KEYRING_DB_PORT=15432 docker compose up -d --build --wait
```

### Set other variables

Compose passes `ENCRYPTION_KEY`, `ENCRYPTION_KEY_PREVIOUS`, `ENRICHMENT_DAILY_LIMIT`, `CONTEXT_DEV_API_KEY`, `PUBLIC_URL`, `SETUP_TOKEN`, `PASSWORD_LOGIN`, `ALLOWED_EMAIL_DOMAINS`, and the `GOOGLE_*`, `GITHUB_*`, and `OIDC_*` variables from your shell or from a `.env` file next to `docker-compose.yml`. To turn on enrichment, put the key in `.env` and restart:

```sh
echo 'CONTEXT_DEV_API_KEY=your-context-dev-key' >> .env
docker compose up -d --wait
```

For any other variable, create `docker-compose.override.yml`. Compose merges it into `docker-compose.yml` on every `docker compose` command:

```yaml
services:
  app:
    environment:
      UNKEY_ROOT_KEY: your-unkey-root-key
      UNKEY_API_ID: api_123
```

[Configuration](configuration.md) lists every variable.

Compose publishes Keyring and Postgres on `127.0.0.1` only. To reach Keyring from other machines, put an HTTPS proxy in front of it and set `PUBLIC_URL` to the proxy's URL.

## Run it on any container host

Use the prebuilt image `ghcr.io/unkeyed/keyring` on any host that runs containers, such as Fly.io, Railway, Render, Kubernetes, or a VM with Docker. Each release is tagged with its version, for example `ghcr.io/unkeyed/keyring:0.1.0`, and the newest release is also `latest`. The image is built for `linux/amd64` and `linux/arm64`.

1. Create a Postgres database that the host can reach, and copy its connection URL.
2. Start the container with `DATABASE_URL`, the HTTPS URL people will open Keyring at as `PUBLIC_URL`, and an `ENCRYPTION_KEY` from `openssl rand -base64 32` that you store with your other secrets:

   ```sh
   docker run -d --name keyring -p 8080:8080 \
   	-e DATABASE_URL='postgres://user:pass@db.example.com:5432/keyring' \
   	-e PUBLIC_URL=https://crm.example.com \
   	-e ENCRYPTION_KEY='your-32-byte-base64-key' \
   	ghcr.io/unkeyed/keyring:latest
   ```

3. Point the host's health check at `GET /healthz`. It returns 200 while the process runs and does not query the database.
4. Put HTTPS in front of Keyring at `PUBLIC_URL`. If the proxy writes the client address to a header, set `CLIENT_IP_HEADER` to it, so failed sign-ins and other per-address limits apply per client instead of to everyone behind the proxy at once.
5. Open the setup link from the container log (`docker logs keyring`) and register a passkey. [The setup link](sign-in.md#the-setup-link) explains it.

The container contract is short:

- Keyring listens on `$PORT`, which defaults to `8080`.
- On `SIGTERM`, Keyring stops taking new requests and waits up to 20 seconds for open requests to finish.
- Keyring keeps no state on disk. You can run several replicas against one database. Migrations, setup links, sign-in steps, and enrichment jobs are safe to run on every replica at once.
- Keyring writes JSON logs to stdout.

To test webhooks from a provider against Keyring on your laptop, expose port 8080 through a tunnel, for example `cloudflared tunnel --url http://localhost:8080`, and use the tunnel's URL as your host.

## Upgrade

Pull or build the new image and restart the container. Keyring applies new migrations when it starts and has no command to undo them, so back up the database before you upgrade.

An upgrade from a version that seeded `ADMIN_EMAIL` and `ADMIN_PASSWORD` signs everyone out, because a password alone no longer signs anyone in. Unset both variables, restart, and open the setup link from the log with the admin's email. That account gets a passkey and keeps its data. Then teammates can sign in with a provider, or you can remove and re-invite them.

With Docker Compose:

```sh
git pull
docker compose up -d --build --wait
```

## Back up

All state is in Postgres. Back it up as you would any Postgres database, for example with `pg_dump`. With Docker Compose:

```sh
docker compose exec -T db pg_dump -U keyring keyring > keyring-backup.sql
```

## Recover a locked-out account

If someone loses every way to sign in, such as their only passkey, run `keyring recover` with their email. It needs the same `DATABASE_URL` and `PUBLIC_URL` as the server:

```sh
DATABASE_URL='postgres://...' PUBLIC_URL=https://crm.example.com keyring recover ada@example.com
```

With Docker, run `docker run --rm -e DATABASE_URL=... -e PUBLIC_URL=... ghcr.io/unkeyed/keyring recover ada@example.com`. The command prints a link that works once, for an hour. Opening it registers a new passkey for that account and signs it out everywhere else. Only someone with the database credentials can run it, and they can change every account anyway.

To avoid needing it, give every account two ways to sign in, such as a passkey on a laptop and one on a phone. **Settings > Account** warns while an account has only one.
