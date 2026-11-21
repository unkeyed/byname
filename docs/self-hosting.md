# Self-host Byname

Byname is one container plus a Postgres database. This guide runs it with Docker Compose on your own machine, then on any host that runs containers. To deploy on Unkey Deploy, follow [Deploy Byname to Unkey Deploy](deploy-unkey.md).

## Run it with Docker Compose

You need Docker with Compose v2.

1. Clone the repository, create an encryption key, and start the stack:

   ```sh
   git clone https://github.com/unkeyed/byname.git
   cd byname
   echo "ENCRYPTION_KEY=$(openssl rand -base64 32)" >> .env
   docker compose up -d --build --wait
   ```

   `ENCRYPTION_KEY` encrypts the provider keys, Search Console credential, notification channel secrets and two-factor seeds that Byname stores, so a database backup alone does not reveal them. Keep `.env` with your other secrets. Compose builds the image, starts Postgres 17 and Byname, and returns when both pass their health checks.

2. Check that Byname is up:

   ```sh
   curl -i http://localhost:8080/healthz
   ```

   The answer is `HTTP/1.1 200 OK`.

3. Print the setup link:

   ```sh
   docker compose logs app | grep -A2 'Open this link'
   ```

   Open the link, enter your name and email, and register a passkey with your fingerprint, face or device PIN. You are the first user and an Admin. The link works once. [Sign-in](sign-in.md) covers providers, roles, two-factor and passwords.

4. Finish onboarding. Byname opens it after you sign in. It asks for your brand and domain, competitors, questions, the places people ask with a Treg token or your own keys, Search Console, and the schedule and spend caps. Nothing runs until you finish, and a test call for each surface and model pair must pass before you can.

Postgres keeps its data in the `db` volume that `docker-compose.yml` declares. `docker compose down` stops the stack and keeps the data. `docker compose down -v` also deletes the data.

### Change the ports

If port 8080 or 5432 is taken, set `BYNAME_PORT` or `BYNAME_DB_PORT` when you start the stack:

```sh
BYNAME_PORT=18080 BYNAME_DB_PORT=15432 docker compose up -d --build --wait
```

### Set other variables

Compose passes `ENCRYPTION_KEY`, `ENCRYPTION_KEY_PREVIOUS`, `PUBLIC_URL`, `SETUP_TOKEN`, `PASSWORD_LOGIN`, `ALLOWED_EMAIL_DOMAINS`, the `GOOGLE_*`, `GITHUB_*` and `OIDC_*` sign-in variables, and `GSC_GOOGLE_CLIENT_ID` and `GSC_GOOGLE_CLIENT_SECRET` from your shell or from a `.env` file next to `docker-compose.yml`. To offer Sign in with Google for Search Console, put the client in `.env` and restart:

```sh
echo 'GSC_GOOGLE_CLIENT_ID=1234-abc.apps.googleusercontent.com' >> .env
echo 'GSC_GOOGLE_CLIENT_SECRET=your-client-secret' >> .env
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

[Configuration](configuration.md) lists every variable. Provider keys, the Treg token and notification channels are not variables. You enter them in the app, which stores them encrypted.

Compose publishes Byname and Postgres on `127.0.0.1` only. To reach Byname from other machines, put an HTTPS proxy in front of it and set `PUBLIC_URL` to the proxy's URL.

## Run it on any container host

Use the prebuilt image `ghcr.io/unkeyed/byname` on any host that runs containers, such as Fly.io, Railway, Render, Kubernetes, or a VM with Docker. Each release is tagged with its version, for example `ghcr.io/unkeyed/byname:0.1.0`, and the newest release is also `latest`. The image is built for `linux/amd64` and `linux/arm64`.

1. Create a Postgres database that the host can reach, and copy its connection URL.
2. Start the container with `DATABASE_URL`, the HTTPS URL people will open Byname at as `PUBLIC_URL`, and an `ENCRYPTION_KEY` from `openssl rand -base64 32` that you store with your other secrets:

   ```sh
   docker run -d --name byname -p 8080:8080 \
   	-e DATABASE_URL='postgres://user:pass@db.example.com:5432/byname' \
   	-e PUBLIC_URL=https://byname.example.com \
   	-e ENCRYPTION_KEY='your-32-byte-base64-key' \
   	ghcr.io/unkeyed/byname:latest
   ```

3. Point the host's health check at `GET /healthz`. It returns 200 while the process runs and does not query the database.
4. Put HTTPS in front of Byname at `PUBLIC_URL`. If the proxy writes the client address to a header, set `CLIENT_IP_HEADER` to it, so failed sign-ins and other per-address limits apply per client instead of to everyone behind the proxy at once.
5. Open the setup link from the container log (`docker logs byname`) and register a passkey. [The setup link](sign-in.md#the-setup-link) explains it.

The container contract is short:

- Byname listens on `$PORT`, which defaults to `8080`.
- One process serves the web UI, the REST API under `/v1`, the MCP server at `/mcp` and `/healthz`, and runs the scheduler.
- On `SIGTERM`, Byname stops taking new requests and waits up to 20 seconds for open requests to finish. A check in progress stops queueing calls and resumes on the next start.
- Byname keeps no state on disk. You can run several replicas against one database. Migrations, the setup link, scheduled checks and the Search Console sync each take a Postgres advisory lock, so a second replica never starts the same check or sync twice.
- Byname writes JSON logs to stdout.

## How the schedule works

The scheduler runs inside the container, so you need no cron service. Every minute it does each of these that is due:

- Starts the AI check when its time comes. You set "run AI checks every N days" and the time of day under **Settings > Schedule**, with **Pause** and **Run now**. A check missed while no process was running starts on the next start. A check missed while paused does not.
- Syncs Search Console once a day, and retries an hour after a failed sync.
- Refreshes each surface's model list once a day.
- Deletes raw provider responses past the retention set under **Settings > General**, once a day.
- Fetches pages that answers newly cited, to see whether they name you.

**Run now** queues a check in the background and never runs it inside the request, so a long check survives a request timeout.

## Upgrade

Pull or build the new image and restart the container. Byname applies new migrations when it starts and has no command to undo them, so back up the database before you upgrade.

With Docker Compose:

```sh
git pull
docker compose up -d --build --wait
```

If `DATABASE_URL` cannot create tables, run `byname migrate` with `MIGRATION_DATABASE_URL` before you start the new version. [`MIGRATION_DATABASE_URL`](configuration.md#migration_database_url) explains it.

## Back up

All state is in Postgres: settings, questions, every answer with its raw response, Search Console data, people and keys. Back it up as you would any Postgres database, for example with `pg_dump`. With Docker Compose:

```sh
docker compose exec -T db pg_dump -U byname byname > byname-backup.sql
```

A backup holds the stored secrets only in encrypted form. Keep `ENCRYPTION_KEY` somewhere other than next to the backup, and keep it: without it, a restored database cannot read its provider keys or channel secrets.

Raw responses are the largest part of the database. To keep it small, set **Raw response retention** under **Settings > General** to a number of days. Answers, mentions and citations stay. Only the raw provider body goes.

## Rotate the encryption key

Set `ENCRYPTION_KEY_PREVIOUS` to the current key and `ENCRYPTION_KEY` to a new one, restart, then remove `ENCRYPTION_KEY_PREVIOUS` and restart again. [`ENCRYPTION_KEY`](configuration.md#encryption_key) has the details and what to do if you lose the key.

## Recover a locked-out account

If someone loses every way to sign in, such as their only passkey, run `byname recover` with their email. It needs the same `DATABASE_URL` and `PUBLIC_URL` as the server:

```sh
DATABASE_URL='postgres://...' PUBLIC_URL=https://byname.example.com byname recover ada@example.com
```

With Docker, run `docker run --rm -e DATABASE_URL=... -e PUBLIC_URL=... ghcr.io/unkeyed/byname recover ada@example.com`. From a clone, run `go run ./cmd/byname recover ada@example.com`. The command prints a link that works once, for an hour. Opening it registers a new passkey for that account and signs it out everywhere else. Only someone with the database credentials can run it, and they can change every account anyway.

To avoid needing it, give every account two ways to sign in, such as a passkey on a laptop and one on a phone. **Settings > Account** warns while an account has only one.
