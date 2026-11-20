# Self-host uncrm

uncrm is one container plus a Postgres database. This guide runs it with Docker Compose on your own machine, then on any host that runs containers. To deploy on Unkey Deploy, follow [Deploy uncrm to Unkey Deploy](deploy-unkey.md).

## Run it with Docker Compose

You need Docker with Compose v2.

1. Clone the repository and start the stack:

   ```sh
   git clone https://github.com/uncrm/uncrm.git
   cd uncrm
   docker compose up -d --build --wait
   ```

   Compose builds the image, starts Postgres 17 and uncrm, and returns when both pass their health checks.

2. Check that uncrm is up:

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

If port 8080 or 5432 is taken, set `UNCRM_PORT` or `UNCRM_DB_PORT` when you start the stack:

```sh
UNCRM_PORT=18080 UNCRM_DB_PORT=15432 docker compose up -d --build --wait
```

### Set other variables

Compose passes `CONTEXT_DEV_API_KEY`, `PUBLIC_URL`, `SETUP_TOKEN`, `PASSWORD_LOGIN`, `ALLOWED_EMAIL_DOMAINS`, and the `GOOGLE_*`, `GITHUB_*`, and `OIDC_*` variables from your shell or from a `.env` file next to `docker-compose.yml`. To turn on enrichment, put the key in `.env` and restart:

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

Compose publishes uncrm and Postgres on `127.0.0.1` only. To reach uncrm from other machines, put an HTTPS proxy in front of it and set `PUBLIC_URL` to the proxy's URL.

## Run it on any container host

Use the prebuilt image `ghcr.io/uncrm/uncrm` on any host that runs containers, such as Fly.io, Railway, Render, Kubernetes, or a VM with Docker. Each release is tagged with its version, for example `ghcr.io/uncrm/uncrm:0.1.0`, and the newest release is also `latest`. The image is built for `linux/amd64` and `linux/arm64`.

1. Create a Postgres database that the host can reach, and copy its connection URL.
2. Start the container with `DATABASE_URL` and the HTTPS URL people will open uncrm at as `PUBLIC_URL`:

   ```sh
   docker run -d --name uncrm -p 8080:8080 \
   	-e DATABASE_URL='postgres://user:pass@db.example.com:5432/uncrm' \
   	-e PUBLIC_URL=https://crm.example.com \
   	ghcr.io/uncrm/uncrm:latest
   ```

3. Point the host's health check at `GET /healthz`. It returns 200 while the process runs and does not query the database.
4. Put HTTPS in front of uncrm at `PUBLIC_URL`. If the proxy writes the client address to a header, set `CLIENT_IP_HEADER` to it, so failed sign-ins are throttled per client instead of per proxy.
5. Open the setup link from the container log (`docker logs uncrm`) and register a passkey. [The setup link](sign-in.md#the-setup-link) explains it.

The container contract is short:

- uncrm listens on `$PORT`, which defaults to `8080`.
- On `SIGTERM`, uncrm stops taking new requests and waits up to 20 seconds for open requests to finish.
- uncrm keeps no state on disk. You can run several replicas against one database. Migrations, setup links, sign-in steps, and enrichment jobs are safe to run on every replica at once.
- uncrm writes JSON logs to stdout.

To test webhooks from a provider against uncrm on your laptop, expose port 8080 through a tunnel, for example `cloudflared tunnel --url http://localhost:8080`, and use the tunnel's URL as your host.

## Upgrade

Pull or build the new image and restart the container. uncrm applies new migrations when it starts and has no command to undo them, so back up the database before you upgrade.

An upgrade from a version that seeded `ADMIN_EMAIL` and `ADMIN_PASSWORD` signs everyone out, because a password alone no longer signs anyone in. Unset both variables, restart, and open the setup link from the log with the admin's email. That account gets a passkey and keeps its data. Then teammates can sign in with a provider, or you can remove and re-invite them.

With Docker Compose:

```sh
git pull
docker compose up -d --build --wait
```

## Back up

All state is in Postgres. Back it up as you would any Postgres database, for example with `pg_dump`. With Docker Compose:

```sh
docker compose exec -T db pg_dump -U uncrm uncrm > uncrm-backup.sql
```
