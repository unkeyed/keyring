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

3. Open <http://localhost:8080> and log in as `admin@example.com` with the password `uncrm-dev-password`. `docker-compose.yml` sets these two values.

Postgres keeps its data in the `db` volume that `docker-compose.yml` declares. `docker compose down` stops the stack and keeps the data. `docker compose down -v` also deletes the data.

### Change the ports

If port 8080 or 5432 is taken, set `UNCRM_PORT` or `UNCRM_DB_PORT` when you start the stack:

```sh
UNCRM_PORT=18080 UNCRM_DB_PORT=15432 docker compose up -d --build --wait
```

### Set other variables

Compose passes `CONTEXT_DEV_API_KEY` from your shell or from a `.env` file next to `docker-compose.yml`. To turn on enrichment, put the key in `.env` and restart:

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

### Replace the dev password

The dev admin has a password that is in this repository. Before other people can reach your uncrm, log in as `admin@example.com` and set a new password in **Settings > Team**.

Setting a different `ADMIN_EMAIL` later does not remove `admin@example.com`. It adds a second user.

## Run it on any container host

Use the prebuilt image `ghcr.io/uncrm/uncrm` on any host that runs containers, such as Fly.io, Railway, Render, Kubernetes, or a VM with Docker. Each release is tagged with its version, for example `ghcr.io/uncrm/uncrm:0.1.0`, and the newest release is also `latest`. The image is built for `linux/amd64` and `linux/arm64`.

1. Create a Postgres database that the host can reach, and copy its connection URL.
2. Start the container with at least `DATABASE_URL`, `ADMIN_EMAIL`, and `ADMIN_PASSWORD`:

   ```sh
   docker run -d --name uncrm -p 8080:8080 \
   	-e DATABASE_URL='postgres://user:pass@db.example.com:5432/uncrm' \
   	-e ADMIN_EMAIL=you@example.com \
   	-e ADMIN_PASSWORD='a-password-of-12-or-more-characters' \
   	ghcr.io/uncrm/uncrm:latest
   ```

3. Point the host's health check at `GET /healthz`. It returns 200 while the process runs and does not query the database.
4. Put HTTPS in front of uncrm. Auth providers send webhooks only to public HTTPS URLs. If your proxy terminates TLS, have it send `X-Forwarded-Proto: https`, so uncrm marks the session cookie `Secure`.

The container contract is short:

- uncrm listens on `$PORT`, which defaults to `8080`.
- On `SIGTERM`, uncrm stops taking new requests and waits up to 20 seconds for open requests to finish.
- uncrm keeps no state on disk. You can run several replicas against one database. Migrations, the first admin, and enrichment jobs are safe to run on every replica at once.
- uncrm writes JSON logs to stdout.

To test webhooks from a provider against uncrm on your laptop, expose port 8080 through a tunnel, for example `cloudflared tunnel --url http://localhost:8080`, and use the tunnel's URL as your host.

## Upgrade

Pull or build the new image and restart the container. uncrm applies new migrations when it starts and has no command to undo them, so back up the database before you upgrade.

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
