// The prompt a coding agent follows to deploy Byname on Unkey through
// Unkey's MCP server. The landing page shows it with a copy button, and
// /deploy-prompt.md serves it as plain Markdown for agents.
export const deployPrompt = `Deploy Byname (https://github.com/unkeyed/byname) to Unkey for me, using Unkey's MCP server. Byname tracks how a brand shows up in AI answers and in Google search. The repository's docs/deploy-unkey.md is the reference for Byname's settings.

1. Connect to Unkey
   Check that Unkey's MCP tools are available (for example projects.listProjects). If they are not:
   - Claude Code: run  claude mcp add --transport http unkey https://mcp.unkey.com
     then tell me to run /mcp, choose unkey, and Authenticate. MCP tools load when a session starts, so if they still do not appear, tell me to restart the session and paste this prompt again.
   - Any other agent: tell me to follow https://mcp.unkey.com/install and stop until I say it is done.
   Signing in opens Unkey in my browser, where I can also create an account. Do every Unkey step below through the MCP tools.

2. Ask me
   For a Postgres connection URL (Neon, Supabase, PlanetScale Postgres, RDS or similar), ideally in AWS us-east-1. It must be the direct connection, or a session-mode pooler, not a transaction-mode pooler such as PgBouncer on port 6432 or Supabase on port 6543: Byname holds session locks for its background jobs. If the provider gives a read/write role and a separate admin role that can create tables, ask for both URLs.
   Do not ask me for a Treg token or provider keys. I enter those in Byname's onboarding after the deploy.

3. Fork Byname
   Run  gh repo fork unkeyed/byname --clone  and note the fork as owner/byname.

4. Create the project and app on Unkey
   - Create a project with slug byname (projects.createProject).
   - Create an app with slug default, sourced from my fork's default branch (apps.createApp with git.repository). If Unkey's GitHub App cannot see the fork, get the install link with github.installApp, give it to me, and retry once I have granted access.
   - On the production environment (environments.updateSettings): Dockerfile "Dockerfile", root directory ".", port 8080, health check GET /healthz, and regions set to us-east-1, or the region closest to my database, with 1 replica. The default region may be elsewhere, so set it explicitly. Set watchPaths to cmd/**, internal/**, web/**, go.mod, go.sum, Dockerfile and .dockerignore, so a change to the docs or the site does not redeploy the app.
   - On the preview environment, set autoDeploy to false. A preview has no variables of its own, and must never point at the production database.

5. Set production variables (environments.setEnvironmentVariables), every secret as writeonly
   - DATABASE_URL: the read/write URL.
   - ENCRYPTION_KEY: generate with  openssl rand -base64 32. Save it to ~/.byname-encryption-key with chmod 600 and tell me to copy it into my password manager. Unkey cannot show a writeonly value again, and Byname cannot read its stored provider keys and channel secrets without it.
   - SETUP_TOKEN: generate with  openssl rand -hex 32.
   - CLIENT_IP_HEADER: X-Forwarded-For. Unkey's gateway sets it to the real client address, so Byname throttles each client separately.
   Do not set PORT. Never print a database password, and never write a secret into the repository.

6. Migrations
   If I gave a separate admin URL, apply the migrations from the clone before the first deploy and keep the admin URL off Unkey:
     MIGRATION_DATABASE_URL='the admin url' go run ./cmd/byname migrate
   Otherwise DATABASE_URL must be able to create tables. Byname migrates when it starts.

7. Protect it at Unkey's gateway (gateway.setPolicies on production)
   - Firewall, deny: request paths matching dotfiles and secrets (/.env, /.git, /.aws, /.ssh), backups and keys (.sql, .bak, .zip, .pem, .key), PHP and WordPress probes (.php, /wp-admin, /wp-login, /xmlrpc.php, /phpmyadmin, /cgi-bin) and appliance scanners (/actuator, /server-status, /HNAP1), case-insensitive. Never block /.well-known/, /v1/openapi.yaml, /setup/, /invite/ or /auth/gsc/callback.
   - Rate limits per remote IP: ^/oauth/(register|authorize)$ at 30 a minute, and /auth/ at 120 a minute.

8. Deploy
   - Create a deployment on production (deployments.createDeployment) and follow it with deployments.getDeployment and deployments.listBuildLogs until it is ready.
   - Set PUBLIC_URL to the stable address without a deployment id, https://byname-<workspace>.unkey.app, then deploy again. Variables and gateway policies only apply to a new deployment, and passkeys only work at PUBLIC_URL.
   - Once the second deployment is ready and current, stop the first one (deployments.stopDeployment). Unkey keeps a previous deployment running, and every running copy holds database connections.

9. Check and hand over
   - PUBLIC_URL/healthz and PUBLIC_URL/auth/setup/SETUP_TOKEN both answer 200, and PUBLIC_URL/.env answers 403.
   - Give me PUBLIC_URL/setup/SETUP_TOKEN to create the first account with a passkey. Tell me to add a second passkey under Settings > Account.
   - Tell me the next stop is onboarding: my brand and domain, competitors, questions, the AI apps to ask with a Treg token or my own keys, Search Console, social mentions, and the schedule with spend caps. Nothing runs until I finish it.
   - Tell me that each later deploy leaves the previous deployment running, and to stop it once the new one is current.
   - Finish with a short summary: the URL, the Unkey project, the variables you set, where the encryption key is saved, and anything you could not do.`;
