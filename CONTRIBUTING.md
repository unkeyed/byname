# Contributing to Byname

This page covers running Byname from source, the checks a change has to pass, the house rules, and the most common contribution: a new surface or provider.

## How the code fits together

- One Go binary, `cmd/byname`, serves the React UI, the REST API under `/v1`, the MCP server at `/mcp` and `/healthz`, and runs the scheduler. `byname migrate` and `byname recover` are subcommands of the same binary.
- Postgres holds all state. Byname applies its migrations from `internal/db/migrations` when it starts.
- Each brand an instance tracks is a workspace. The first workspace's tables are in `public`. Every later workspace has its own schema, `ws_<id>`, with a copy of every table except the shared ones: people and how they sign in, API keys, OAuth and the provider keys, all listed in `db.SharedTables`. `db.In` binds a pool to one workspace.
- The service packages do every read and write: `internal/config` (settings, brands, questions, surfaces, credentials, onboarding), `internal/checks` (queueing and running checks), `internal/results` (everything the result pages read), `internal/discovery` (brands answers name that you don't track), `internal/logos` (brand logos), `internal/search` (Google Search Console and Bing Webmaster Tools), `internal/changes` (site changes, recrawl watch and verdicts), `internal/social` (social mentions and digests), `internal/notify` (alerts and channels), `internal/sources` (cited pages), `internal/instance` (whether the instance is set up, and what it costs) and `internal/auth` (people, keys and OAuth).
- `internal/httpapi/server.go` holds the one route table. `internal/mcpserver` registers each MCP tool. Both call the same service methods, so REST, MCP and the UI see the same thing.
- `internal/engines` asks one question on one surface. `registry.go` is the one list of surfaces and their pinned endpoints, and `routes.go` the one list of routes and adapters.
- `internal/scheduler` runs every minute: due checks, the daily search engine sync with the site changes follow-up after it, the hourly GitHub poll, the daily model list refresh, social searches, reading answers for other brands, logo lookups, raw response pruning and cited-source fetching.

## Run Byname from source

You need Go 1.26, Node.js 24, and Docker.

```sh
make dev
```

`make dev` starts Postgres in Docker on port 5432 and runs the Go server on <http://localhost:8080>. It keeps an `ENCRYPTION_KEY` in `.byname-dev-key`, which git ignores, so secrets you store while developing stay readable across restarts. On an empty database it prints a setup link. Open it and register a passkey to create your account.

The Go binary embeds the web UI from `web/dist`. To work on the UI, run Vite in a second terminal:

```sh
cd web
npm ci
npm run dev
```

Open <http://localhost:5173>. Vite reloads the UI on every change and forwards `/v1`, `/auth`, `/mcp` and `/healthz` to the Go server on port 8080. Passkeys need `PUBLIC_URL` to match the address in your browser, so set `PUBLIC_URL=http://localhost:5173` for the server when you sign in through Vite.

To build the binary with the UI inside, run `make build`. The result is `bin/byname`.

To try checks without paying for calls, point Byname at a fake Treg with `TREG_BASE_URL`. [Configuration](docs/configuration.md#development-and-test-overrides) lists every override for tests and local development. Never set them in production.

## Check your change

CI runs these on every pull request:

```sh
make lint                 # gofmt, go vet (also on the e2e package), the web unit tests and the repo's grep rules
make test                 # unit tests and Postgres tests
cd web && npm run build   # TypeScript check and production build
```

`make test` needs Postgres. `make dev` starts it, or run `docker compose up -d --wait db`. Each test gets its own freshly migrated database, named `byname_test_<random>`, which it drops afterwards. If Postgres is unreachable, the tests fail instead of skipping. To use another server, set `TEST_DATABASE_URL` to a role that can create databases:

```sh
TEST_DATABASE_URL='postgres://byname:byname@localhost:55432/byname?sslmode=disable' go test -count=1 ./...
```

`make test GO_TEST_SKIP=<regexp>` skips tests by name.

The end-to-end tests drive a running Byname over HTTP. The stack needs an `ENCRYPTION_KEY` in `.env`:

```sh
grep -q ENCRYPTION_KEY .env 2>/dev/null || echo "ENCRYPTION_KEY=$(openssl rand -base64 32)" >> .env
docker compose up -d --build --wait
make e2e E2E_SETUP_TOKEN=<token from the setup link>
```

The suite creates its account with a software passkey and saves the passkey, so later runs need no token. `e2e/doc.go` lists the other variables. The e2e tests carry the `e2e` build tag, so `go test ./...` skips them.

The site in `site/` is an Astro app that serves the landing page and renders `docs/*.md`. Its build fails on a doc missing from the sidebar, a broken link between docs, or a missing heading:

```sh
cd site
npm ci
npm run build   # or npm run dev to preview at localhost:4321
```

## Follow the house rules

- Keep SQL in the service packages named in [How the code fits together](#how-the-code-fits-together), and in `internal/db`.
- Make each HTTP handler and each MCP tool call one service method. A handler decodes, calls, and encodes. Any logic beyond that belongs in the service.
- Make every write an upsert on a natural key, so a retry, a replay or a second replica ends in the same state.
- Never edit a migration that has been released. Add a new numbered file instead, on one of two tracks:
  - A change to a table every workspace has, such as `brand` or `surface`, goes in `internal/db/migrations/workspace`. Byname applies it to `public` and to every `ws_<id>` schema, and records it per workspace in `workspace_migration`. Write it without schema names.
  - A change to a shared table goes in `internal/db/migrations`, the global track, applied once and recorded in `schema_migrations`.

  `TestWorkspaceBaselineMatchesPublic` fails when a global migration changes a workspace's table. `internal/db/migrations/workspace/000_baseline.sql` is generated by `make workspace-baseline` and is never edited by hand.
- Key an advisory lock with `db.WorkspaceLock` unless the work is shared by every workspace, as sign-in is.
- When you add or change a route in `internal/httpapi/server.go`, update `internal/httpapi/openapi.yaml`. `TestRoutesMatchOpenAPI` fails when the two disagree. Every route needs an access class (`TestEveryRouteHasAnAccessClass`), and every route an API key can call needs an MCP tool with the same scope (`TestEveryAPIKeyRouteHasAnMCPTool`).
- Encrypt every secret with `internal/secretbox`, never return one from any API, and let only a signed-in Admin set it.
- Assert behavior against real Postgres. Fake external services with recorded or documented response shapes.
- Keep each commit to one purpose, and say why in its message.

## Add a surface or a provider

A surface is a row in a table, so adding one touches a few known places and nothing else. Onboarding, **Settings > AI apps**, the REST API and MCP all read the registry.

1. Add the surface key to `internal/engines/engines.go`, and a row to `registry` in `internal/engines/registry.go` with its title, pinned endpoint and default fidelity. A new provider family gets its own file, such as `internal/engines/serpapi.go`, with the request builder and the parser for its native response.
2. Add a workspace migration in `internal/db/migrations/workspace` that allows the new key in the `surface` table's `CHECK` constraint. Released migrations stay untouched, so this is a new file.
3. Add at least one real response body as `internal/engines/testdata/<family>/<endpoint id>.json`, or a hand-made one as `<endpoint id>.<case>.synthetic.json`, and the expected parse as `<name>.want.json` next to it. `TestEveryEndpointHasFixtures` fails until you do, and `go test ./internal/engines -update` writes the `.want.json` files from the parser for you to review.
4. Document it: add the surface to the table in `docs/engines/surfaces.md`, or a page under `docs/engines/` for a new route family, and add that page to the sidebar in `site/src/lib/site.ts`.

A direct model provider or gateway is a row in `adapters` in `internal/engines/routes.go` plus its own file, with fixtures under `testdata/<provider>/`.

## Add a search engine

A search engine is one adapter file and one entry in a registry. Connecting, the daily sync, storage, goals, notifications, the REST API and MCP read the registry, so they need no change.

1. Write `internal/search/<engine>.go`, with `google.go` and `bing.go` as examples. Its `providerSpec` gives the title, the ways to connect, the engine's calendar, how many days one row of searches covers (`StepDays`) and `Notes` on how its numbers differ. Its adapter has three methods: `verify` checks a pasted key, `sites` lists the sites the key can read, and `pull` parses the engine's API into reports. The adapter never touches SQL.
2. Add a `Provider` value and the spec to `registry` in `internal/search/search.go`. The value is stored in every row, so it never changes once released.
3. Add a fake of the engine's API to `internal/search/searchtest`, built from its documented response shapes, and test the sync against it. Add a `*_BASE_URL` override in `cmd/byname/main.go` that points the adapter at the fake, with a warning at start.
4. Document the engine in `docs/search.md`, and the override in `docs/configuration.md`.

## Release

Maintainers release by pushing a version tag:

```sh
git tag v0.1.0
git push origin v0.1.0
```

`.github/workflows/release.yml` builds the image for `linux/amd64` and `linux/arm64` and pushes it to `ghcr.io/unkeyed/byname` with the version (`0.1.0`), the minor version (`0.1`), and `latest`. A pre-release tag such as `v0.2.0-rc.1` gets only its own version.

Unkey and `docker pull` need the image without credentials. After the first release, check that the package's visibility is **Public** in the package settings on GitHub.
