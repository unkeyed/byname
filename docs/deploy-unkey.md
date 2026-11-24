# Deploy Byname to Unkey

This guide runs Byname on [Unkey](https://www.unkey.com/docs/compute/get-started/deploy-from-github) compute, connected to a Postgres database you host elsewhere. At the end, Byname runs at an `unkey.app` URL and you are signed in to its onboarding.

The deploy prompt on the [landing page](https://byname.dev/#deploy) has a coding agent do these nine steps for you through Unkey's MCP server, and points the agent at this page for Byname's settings. Each step below says what the agent does and how to do it by hand.

You need:

- An Unkey account with a Compute plan. Unkey needs a plan before it deploys anything.
- A Postgres database. Unkey has no managed database and no persistent disk, and Byname keeps nothing on disk.
- A GitHub account for your fork of `unkeyed/byname`.

Provider keys, the Treg token and search engine connections are not part of the deployment. You enter them in Byname's onboarding after step 9, so neither the agent nor Unkey's environment ever holds them.

## 1. Connect to Unkey's MCP server

The agent checks that Unkey's MCP tools are available, for example `projects.listProjects`. If they are not, add the server. In Claude Code:

```sh
claude mcp add --transport http unkey https://mcp.unkey.com
```

Then run `/mcp` in Claude Code, pick `unkey`, and choose **Authenticate**. Signing in opens Unkey in your browser, where you can also create an account. MCP tools load when a session starts, so restart the session if they do not appear. For other agents, follow [mcp.unkey.com/install](https://mcp.unkey.com/install).

To deploy by hand, sign in to the [Unkey dashboard](https://app.unkey.com) instead.

## 2. Create the database

Create a Postgres database with any managed provider, such as Neon, Supabase, PlanetScale Postgres or Amazon RDS. Pick a region close to the Unkey region you plan to use, for example AWS `us-east-1`.

Copy the connection URL. Most managed providers require TLS, so keep `sslmode=require` if the provider includes it:

```text
postgres://byname:secret@ep-example-123.us-east-1.aws.neon.tech/byname?sslmode=require
```

Use the direct connection, or a session-mode pooler. A transaction-mode pooler, such as PgBouncer on PlanetScale's port 6432 or Supabase's pooler on port 6543, does not work: Byname's background jobs hold session locks, which a transaction-mode pooler drops between statements.

If the provider gives you a read/write role and a separate admin role that can create tables, as PlanetScale Postgres does, copy both URLs. The read/write URL becomes `DATABASE_URL`. The admin URL is used once, in step 6, and never stored on Unkey.

## 3. Fork the repository

Fork `unkeyed/byname` on GitHub, or with the GitHub CLI:

```sh
gh repo fork unkeyed/byname --clone
```

Unkey builds from your fork, and the clone lets you run `byname migrate` in step 6.

## 4. Create the project and app

In the Unkey dashboard, open **Projects** and click **Create project** with the slug `byname`. Then create an app with the slug `default` from a **GitHub repository**, and pick your fork's default branch. If Unkey's GitHub App cannot see the fork, install it on the fork when Unkey asks. The agent uses `projects.createProject`, `apps.createApp` with `git.repository`, and `github.installApp` for the install link.

Set these on the **production** environment, in its build and runtime settings (`environments.updateSettings`):

| Setting | Value |
|---|---|
| Dockerfile | `Dockerfile`. Without a path, Unkey tries to detect the build on its own instead of using Byname's Dockerfile. |
| Root directory | `.` |
| Port | `8080` |
| Health check | `GET /healthz`. It answers 200 while the process runs and does not query the database, so a database outage does not restart the instance. |
| Region | Set it explicitly, for example `us-east-1`, close to your database. The default region may be elsewhere. |
| Replicas | `1` |
| Watch paths | `cmd/**`, `internal/**`, `web/**`, `go.mod`, `go.sum`, `Dockerfile` and `.dockerignore`, so a change to the docs or the site does not redeploy the app. |

Unkey then sends traffic to an instance only after `/healthz` answers 200, and restarts an instance that stops answering. You can also set the health check through Unkey's API:

```sh
curl -X POST https://api.unkey.com/v2/environments.updateSettings \
	-H "Authorization: Bearer $UNKEY_ROOT_KEY" \
	-H "Content-Type: application/json" \
	-d '{"project":"byname","app":"default","environment":"production","healthcheck":{"method":"GET","path":"/healthz"}}'
```

From now on, each push to your fork's default branch deploys to production. Pushes to other branches create preview deployments, which use the **preview** environment and its own variables. Turn off auto-deploy on the preview environment (`environments.updateSettings` with `autoDeploy: false`) unless you give it its own database. A preview that points at the production database applies its migrations to production data.

## 5. Set the environment variables

Unkey stores variables per environment. Set these on the **production** environment, each secret as write-only (**Sensitive** in the dashboard). The agent uses `environments.setEnvironmentVariables`.

| Variable | Value |
|---|---|
| `DATABASE_URL` | The read/write connection URL from step 2. Write-only. |
| `ENCRYPTION_KEY` | 32 random bytes from `openssl rand -base64 32`. Write-only. Keep a copy in your password manager: Unkey cannot show a write-only value again, and Byname cannot read its stored provider keys and the secrets of the places notifications go without it. The agent saves it to `~/.byname-encryption-key` with mode `600` and asks you to move it. |
| `SETUP_TOKEN` | 32 or more random characters, from `openssl rand -hex 32`. Write-only. You open the setup link with it in step 9. |
| `CLIENT_IP_HEADER` | `X-Forwarded-For`. Unkey's gateway sets it to the client's address, so Byname throttles each client on its own instead of everyone behind the gateway at once. |

Do not set `PORT`. Unkey sets it, and Byname listens on it. `PUBLIC_URL` comes in step 8, once the URL is known.

Optional variables, also write-only where secret:

| Variable | When |
|---|---|
| `GSC_GOOGLE_CLIENT_ID`, `GSC_GOOGLE_CLIENT_SECRET` | To connect Google Search Console with Sign in with Google. See [Search engines](search.md#create-the-google-oauth-client). |
| `GOOGLE_*`, `GITHUB_*`, `OIDC_*`, `ALLOWED_EMAIL_DOMAINS` | To let teammates sign in with a provider. See [Sign-in](sign-in.md). |
| `UNKEY_ROOT_KEY`, `UNKEY_API_ID` | To store API keys in Unkey. See [Store API keys in Unkey](#store-api-keys-in-unkey). |

Unkey applies a variable change only to the next deployment. After you change a variable, redeploy. [Configuration](configuration.md) describes each variable in full.

## 6. Apply the migrations

Byname creates its tables when it starts, through `DATABASE_URL`. That needs a role that can create tables.

If step 2 gave you a separate admin URL, apply the migrations from your clone before the first deploy, and keep the admin URL off Unkey:

```sh
MIGRATION_DATABASE_URL='the admin url' go run ./cmd/byname migrate
```

`byname migrate` applies pending migrations and exits. Run it again before you deploy a release that adds a migration. [`MIGRATION_DATABASE_URL`](configuration.md#migration_database_url) explains the grants the read/write role needs.

## 7. Protect it at Unkey's gateway

Set firewall and rate-limit policies on the production environment (`gateway.setPolicies`):

- **Firewall, deny**, case-insensitive: request paths for dotfiles and secrets (`/.env`, `/.git`, `/.aws`, `/.ssh`), backups and keys (`.sql`, `.bak`, `.zip`, `.pem`, `.key`), PHP and WordPress probes (`.php`, `/wp-admin`, `/wp-login`, `/xmlrpc.php`, `/phpmyadmin`, `/cgi-bin`), and appliance scanners (`/actuator`, `/server-status`, `/HNAP1`). Never block `/.well-known/`, `/v1/openapi.yaml`, `/setup/`, `/invite/` or `/auth/gsc/callback`. MCP clients, the setup link, invites and Search Console sign-in need them.
- **Rate limits per remote IP**: `^/oauth/(register|authorize)$` at 30 a minute, and `/auth/` at 120 a minute.

Byname throttles failed sign-ins on its own as well. The gateway policies stop scanners before they reach the container.

## 8. Deploy and set `PUBLIC_URL`

Create a deployment on production (`deployments.createDeployment`), or click **Deploy**. Follow it until it is ready (`deployments.getDeployment` and `deployments.listBuildLogs`).

Find the deployment's stable URL, without a deployment id. It has the form `https://<project>-<workspace>.unkey.app`, or `https://<project>-<app>-<workspace>.unkey.app` when the app slug is not `default`. Set `PUBLIC_URL` to it, then deploy again. Variables and gateway policies apply only to a new deployment, and passkeys, OAuth callbacks and MCP sign-in work only at `PUBLIC_URL`.

Once the second deployment is ready and current, stop the first one (`deployments.stopDeployment`, or **Stop** in the dashboard). Unkey keeps a previous deployment running after a new one takes over, and every running copy holds database connections. Do the same after every later deploy. Byname takes at most a quarter of the connections the database allows, so a copy or two left running never locks the app out, but they still use connections, and their background jobs run with their old settings.

If the deployment does not start, open its logs. Byname logs the name of any missing or invalid variable and exits, for example `DATABASE_URL is required`.

## 9. Check it and create the first account

Check three URLs:

- `PUBLIC_URL/healthz` answers 200.
- `PUBLIC_URL/auth/setup/SETUP_TOKEN` answers 200, which means the setup link is still unused.
- `PUBLIC_URL/.env` answers 403, which means the firewall is on.

Open `PUBLIC_URL/setup/SETUP_TOKEN`. Enter your name and email and register a passkey. You are the first user and an Admin, and the link stops working. The deployment logs print the same link. Add a second passkey under **Settings > Account**, so losing one device does not lock you out.

The next stop is onboarding, which opens on its own: your brand and domain, competitors, questions, the AI apps to ask with a Treg token or your own keys, the search engines to read, social mentions, and the schedule with spend caps. Nothing runs until you finish it.

## Deploy the prebuilt image instead

The image `ghcr.io/unkeyed/byname` is public. Each release is tagged with its version, for example `0.1.0`, and the newest release is also `latest`. To deploy it without a fork, skip step 3, and in step 4 deploy the image with the Unkey CLI:

```sh
npm install -g unkey
unkey login
unkey deploy ghcr.io/unkeyed/byname:latest --project=byname --env=production
```

Without `--env=production`, the CLI deploys to the preview environment. To upgrade later, run the same command with a newer version tag. Step 6 then needs a clone of the same version, or the image itself: `docker run --rm -e MIGRATION_DATABASE_URL=... ghcr.io/unkeyed/byname:<version> migrate`.

## Recover a locked-out account

If someone loses their only passkey, run `byname recover` from your clone with the database URL and the deployment's URL. It prints a one-time link, valid for an hour, that registers a new passkey for that account:

```sh
DATABASE_URL='postgres://...' PUBLIC_URL=https://byname-acme.unkey.app go run ./cmd/byname recover ada@example.com
```

## Store API keys in Unkey

By default, Byname stores the API keys that scripts use in its own database. To manage them in Unkey's API key product instead:

1. In the Unkey dashboard, create an API for Byname's keys and copy its ID. It starts with `api_`.
2. Create a root key with these permissions on that API: `create_key`, `read_key`, `read_api`, `verify_key` and `delete_key`.
3. Set `UNKEY_ROOT_KEY` to the root key and `UNKEY_API_ID` to the API ID, then redeploy.

Keys created before the switch stop working. Create new keys under **Settings > API keys**.

## If the dashboard differs from this guide

This guide follows Unkey's documentation as of November 2026. Two points could not be checked against a live dashboard:

- The exact labels of menus and buttons, such as **Create project** and the **Dockerfile** build setting, may differ. Look for the setting with the same purpose.
- Unkey may ask you to deploy before it lets you set variables. If so, let the first deployment fail, set the variables from step 5, and redeploy.
