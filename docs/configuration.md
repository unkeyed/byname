# Configuration

Byname reads its configuration from environment variables when it starts. `cmd/byname/main.go` reads them all in `loadConfig`, and this page lists every one. A change takes effect on the next start.

Environment variables hold only secrets and infrastructure. Everything about your brand, questions, AI apps, schedule, provider keys, Search Console, site changes and notifications lives in Postgres. You set those in onboarding, in **Settings**, or through the REST API and MCP, so there are no schedule or provider-key variables.

| Variable | Required | Default | What it does |
|---|---|---|---|
| `DATABASE_URL` | Yes | none | The Postgres connection URL, for example `postgres://user:pass@host:5432/byname`. |
| `ENCRYPTION_KEY` | Yes | none | 32 random bytes as base64 or hex, from `openssl rand -base64 32`. Encrypts provider keys, the Search Console credential, the GitHub token for site changes, the secrets of the places notifications go and TOTP seeds in the database. |
| `ENCRYPTION_KEY_PREVIOUS` | No | none | The key you are rotating away from. Set it for one restart after changing `ENCRYPTION_KEY`. |
| `MIGRATION_DATABASE_URL` | No | `DATABASE_URL` | A connection URL for a role that can create tables. Byname applies migrations through it, so `DATABASE_URL` can be a role that only reads and writes data. |
| `PORT` | No | `8080` | The port the HTTP server listens on. |
| `PUBLIC_URL` | No | `http://localhost:$PORT` | The URL people open Byname at, such as `https://byname.example.com`. Passkeys, sign-in callbacks, the Search Console callback, links in notifications and MCP sign-in depend on it. |
| `SETUP_TOKEN` | No | random | The token of the setup link, at least 32 characters. See [The setup link](sign-in.md#the-setup-link). |
| `PASSWORD_LOGIN` | No | `false` | `true` lets people set a password, which always needs a second step. |
| `ALLOWED_EMAIL_DOMAINS` | No | none | Comma-separated domains, such as `example.com`, whose addresses can join without an invite through Google Workspace or the OIDC issuer, in the role `ALLOWED_EMAIL_DOMAINS_ROLE` names. GitHub accounts always need an invite. |
| `ALLOWED_EMAIL_DOMAINS_ROLE` | No | `viewer` | The role of someone who joins through `ALLOWED_EMAIL_DOMAINS`: `viewer` or `editor`. `admin` is refused, because an admin manages credentials and the team; invite admins by name. |
| `CLIENT_IP_HEADER` | No | none | The header your proxy writes the client IP to, such as `X-Forwarded-For`. Byname throttles failed sign-ins and OAuth requests per client IP. Set it to `X-Forwarded-For` on Unkey. |
| `GOOGLE_CLIENT_ID` | No | none | A Google OAuth client ID. Set it with `GOOGLE_CLIENT_SECRET` to offer Google sign-in. |
| `GOOGLE_CLIENT_SECRET` | No | none | The Google OAuth client secret for sign-in. |
| `GITHUB_CLIENT_ID` | No | none | A GitHub OAuth app client ID. Set it with `GITHUB_CLIENT_SECRET` to offer GitHub sign-in. |
| `GITHUB_CLIENT_SECRET` | No | none | The GitHub OAuth app client secret. |
| `OIDC_ISSUER` | No | none | An OIDC issuer URL. Set it with `OIDC_CLIENT_ID` and `OIDC_CLIENT_SECRET` to offer OIDC sign-in. |
| `OIDC_CLIENT_ID` | No | none | The OIDC client ID. |
| `OIDC_CLIENT_SECRET` | No | none | The OIDC client secret. |
| `OIDC_NAME` | No | `SSO` | The OIDC provider's name on the sign-in button, such as `Okta`. |
| `GSC_GOOGLE_CLIENT_ID` | No | none | A Google OAuth client ID for the Search Console connection. Set it with `GSC_GOOGLE_CLIENT_SECRET` to offer **Sign in with Google** in Search Console settings. Without it, Search Console connects only through a service account. |
| `GSC_GOOGLE_CLIENT_SECRET` | No | none | The Google OAuth client secret for the Search Console connection. |
| `UNKEY_ROOT_KEY` | No | none | An Unkey root key. Set it together with `UNKEY_API_ID` to store API keys in Unkey. |
| `UNKEY_API_ID` | No | none | The Unkey API that holds Byname's keys, for example `api_123`. |

Byname exits with an error that names the variable when one of these rules fails:

- `DATABASE_URL` is empty or not a Postgres connection string.
- `MIGRATION_DATABASE_URL` is set and not a Postgres connection string.
- `ENCRYPTION_KEY` is empty or not 32 bytes, or `ENCRYPTION_KEY_PREVIOUS` is set and not 32 bytes.
- `PORT` is not a port number from 1 to 65535.
- Only one of `UNKEY_ROOT_KEY` and `UNKEY_API_ID` is set.
- Only one of `GSC_GOOGLE_CLIENT_ID` and `GSC_GOOGLE_CLIENT_SECRET` is set.
- `PUBLIC_URL` is not an origin, or uses `http` with a host other than `localhost` or `127.0.0.1`.
- `PASSWORD_LOGIN` is not `true` or `false`.
- `ALLOWED_EMAIL_DOMAINS` lists an email address instead of a domain.
- `ALLOWED_EMAIL_DOMAINS_ROLE` is not `viewer` or `editor`.
- A sign-in provider has some of its variables set but not all.
- `SETUP_TOKEN` is shorter than 32 characters.
- One of the development overrides below is set and is not an `http` or `https` URL, or `SOURCES_ALLOW_PRIVATE_IPS` is not `true` or `false`.

## `DATABASE_URL`

Any Postgres that accepts a connection URL works: a local container, Neon, Supabase, PlanetScale Postgres, RDS, or another managed service. Byname needs no extensions and keeps nothing on disk.

On start, Byname applies its migrations from `internal/db/migrations`. Each migration runs in its own transaction under a Postgres advisory lock and is recorded in `schema_migrations`, so several replicas can start at the same time.

## `MIGRATION_DATABASE_URL`

Some providers, such as PlanetScale Postgres, give you a role that can only read and write data, and a separate admin role that can create tables. Set `DATABASE_URL` to the read/write role and `MIGRATION_DATABASE_URL` to the admin role. Byname opens the admin connection at start, applies the migrations, closes it, and then serves every request through `DATABASE_URL`.

The read/write role needs `SELECT`, `INSERT`, `UPDATE` and `DELETE` on the tables the admin role creates. Granting it the `pg_read_all_data` and `pg_write_all_data` roles covers that, including tables added by later migrations.

When it is unset, Byname migrates through `DATABASE_URL`. A role that cannot create tables still starts when every migration is already applied. When a migration is pending, Byname exits with an error that names `MIGRATION_DATABASE_URL` and `byname migrate`.

To keep the admin URL out of the deployment, leave `MIGRATION_DATABASE_URL` unset there and apply migrations yourself before you deploy a release that adds one:

```sh
MIGRATION_DATABASE_URL='postgres://admin:pass@host:5432/byname' byname migrate
```

`byname migrate` applies any pending migrations and exits. It uses `MIGRATION_DATABASE_URL`, or `DATABASE_URL` when that is unset. With the Docker image, run `docker run --rm -e MIGRATION_DATABASE_URL=... ghcr.io/unkeyed/byname migrate`. From a clone, run `go run ./cmd/byname migrate`.

## `ENCRYPTION_KEY`

Byname hashes the tokens it only compares, such as session cookies, API keys, OAuth tokens and recovery codes. It must read some secrets back, so it encrypts those with AES-256-GCM under `ENCRYPTION_KEY`:

- the Treg token under **Settings > Treg** and every key under **Settings > Provider keys**
- the Search Console credential: the Google refresh token or the service account key
- the GitHub token for site changes
- the secret of each place notifications go: an SMTP password, an email API key, a Slack webhook URL or a webhook signing secret
- each person's TOTP seed

A database backup, a replica or a read-only role then reveals none of them. Each value is bound to its row, so copying one into another row does not decrypt it there.

Generate the key once and store it with your other secrets:

```sh
openssl rand -base64 32
```

If you lose the key, Byname cannot read those secrets and exits at start, naming the first one it could not read. To start again without them, delete the encrypted rows, then re-enter the Treg token under **Settings > Treg** and the keys under **Settings > Provider keys**, reconnect Search Console and the GitHub repository under **Settings > Site changes**, add where notifications go again under **Settings > Notifications**, and have each person set up their authenticator app again:

```sql
DELETE FROM credential;
DELETE FROM gsc_connection;
UPDATE repo_connection SET ciphertext = NULL, token_expires_at = NULL;
DELETE FROM notification_channel;
DELETE FROM totp;
```

To rotate the key:

1. Set `ENCRYPTION_KEY_PREVIOUS` to the current key and `ENCRYPTION_KEY` to a new one.
2. Restart. Byname re-encrypts every secret with the new key and logs how many it changed.
3. Remove `ENCRYPTION_KEY_PREVIOUS` and restart again.

## `PORT`

The Docker image sets `PORT=8080`. Unkey sets `PORT` for you and sends traffic and health checks to it.

## `PUBLIC_URL`

The origin people type into their browser, with no path. Its host is the WebAuthn relying party ID, so a passkey registered at one `PUBLIC_URL` does not work at another. Sign-in providers call back to `PUBLIC_URL/auth/oauth/<provider>/callback`, and Google calls back to `PUBLIC_URL/auth/gsc/callback` after a Search Console connection. When it starts with `https`, cookies are marked `Secure` and named with the `__Host-` prefix, and every response carries `Strict-Transport-Security`.

MCP clients sign in with OAuth, and Byname derives every OAuth URL from `PUBLIC_URL`. The authorization server's issuer is `PUBLIC_URL`, and the MCP server's resource identifier is `PUBLIC_URL/mcp`. A client checks that the resource Byname names matches the URL it connects to, so connect agents to `PUBLIC_URL/mcp` exactly. Access tokens are bound to that resource, so changing `PUBLIC_URL` signs every connected app out.

When it is unset, Byname uses `http://localhost:$PORT`, which suits `make dev`. Browsers allow passkeys on `http` only for `localhost`, so any other host needs `https`. `docker-compose.yml` sets it to `http://localhost:$BYNAME_PORT`. If you run the Vite dev server on port 5173, set `PUBLIC_URL=http://localhost:5173`.

## Sign-in variables

`SETUP_TOKEN`, `PASSWORD_LOGIN`, `ALLOWED_EMAIL_DOMAINS`, `ALLOWED_EMAIL_DOMAINS_ROLE`, `CLIENT_IP_HEADER`, and the `GOOGLE_*`, `GITHUB_*` and `OIDC_*` variables decide how people sign in. [Sign-in](sign-in.md) explains each one.

## `GSC_GOOGLE_CLIENT_ID` and `GSC_GOOGLE_CLIENT_SECRET`

The Google OAuth client that Search Console's **Sign in with Google** uses. It is separate from `GOOGLE_CLIENT_ID`, which signs people in to Byname, because it asks Google for a different scope: `https://www.googleapis.com/auth/webmasters.readonly`, read-only Search Console. You can use the same Google Cloud project for both. [Search Console](search-console.md) shows how to create the client.

## `UNKEY_ROOT_KEY` and `UNKEY_API_ID`

Byname has two stores for the API keys that agents and scripts use. It picks one when it starts:

- With neither variable set, keys live in Byname's Postgres database. This is the built-in store.
- With both set, keys live in your [Unkey](https://www.unkey.com) API. Byname creates, lists, verifies and deletes them through Unkey's v2 API, and keeps each key's scopes and creator in the key's metadata.

The root key needs these permissions on the API that `UNKEY_API_ID` names: `create_key`, `read_key`, `read_api`, `verify_key` and `delete_key`.

In Unkey mode, each replica caches a valid key for 30 seconds. A revoked key can keep working for up to 30 seconds on a replica that did not revoke it.

Keys from one store do not work in the other. If you switch stores, create new keys and give them to your agents. The `Byname listening` log line at startup shows the active store in its `keys` field, as `builtin` or `unkey`.

## Development and test overrides

Never set these in production. Each one sends Byname's outbound calls, or lets it reach addresses, that a production instance must not. Byname logs a warning at start while any of them is set.

| Variable | What it does |
|---|---|
| `TREG_BASE_URL` | Sends every Treg call to this URL instead of `https://treg.to`, such as a fake Treg on localhost. |
| `OPENAI_BASE_URL` | Sends calls on the OpenAI route to this URL instead of `https://api.openai.com`. |
| `ANTHROPIC_BASE_URL` | Sends calls on the Anthropic route to this URL instead of `https://api.anthropic.com`. |
| `GEMINI_BASE_URL` | Sends calls on the Google Gemini route to this URL instead of `https://generativelanguage.googleapis.com`. |
| `PERPLEXITY_BASE_URL` | Sends calls on the Perplexity route to this URL instead of `https://api.perplexity.ai`. |
| `DATAFORSEO_BASE_URL` | Sends calls with your own DataForSEO key to this URL instead of `https://api.dataforseo.com`. |
| `CLORO_BASE_URL` | Sends calls with your own cloro key to this URL instead of `https://api.cloro.dev`. |
| `SERPAPI_BASE_URL` | Sends calls with your own SerpApi key to this URL instead of `https://serpapi.com`. |
| `GSC_GOOGLE_BASE_URL` | Sends every Google call the Search Console connection makes, sign-in included, to this URL. |
| `GITHUB_API_BASE_URL` | Sends every GitHub call that site changes make to this URL instead of `https://api.github.com`, such as a fake GitHub on localhost. |
| `BLUESKY_BASE_URL` | Sends social search on Bluesky to this URL instead of `https://api.bsky.app`, such as a fake on localhost. |
| `RESEND_BASE_URL` | Sends notifications that go by email through Resend to this URL instead of `https://api.resend.com`. |
| `SOURCES_ALLOW_PRIVATE_IPS` | `true` lets the fetch of pages that answers link to, and of brand logos, reach private and loopback addresses, such as a fake site on localhost. Off by default, so a link in an answer or a logo address cannot make Byname fetch from your internal network. |

The tests read their own variables, which the server never reads. `TEST_DATABASE_URL` points the Go tests at a Postgres role that can create databases. `make e2e` reads `BASE_URL`, `E2E_SETUP_TOKEN`, `E2E_PUBLIC_URL`, `E2E_PASSKEY_FILE` and `E2E_ADMIN_EMAIL`. [CONTRIBUTING.md](../CONTRIBUTING.md) explains both.
