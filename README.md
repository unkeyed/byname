<p align="center">
  <img src="site/og.png" alt="Byname: find out when AI assistants name you, and who they name instead. The overview of an example brand, with how often each AI app named it over eight checks." width="760" />
</p>

<h1 align="center">Byname</h1>

<p align="center">
  Find out when AI assistants name you. And who they name instead.<br />
  <a href="https://byname.dev">Website</a> · <a href="docs/">Docs</a> · <a href="https://byname.dev/#deploy">Deploy on Unkey</a>
</p>

Byname is an open source app you run yourself that shows how your brand shows up in AI answers and in Google search. It asks your questions where your buyers ask them, such as the ChatGPT app, Google AI Mode or Claude Code, on a schedule you set, keeps every answer with its cost, and counts who was named and whose site was linked. Your Search Console numbers sit on the same timeline, and your agents read it all over MCP and REST.

## Why Byname

- **What a person sees, or labeled as not.** You pick the AI apps people use, not the models behind them. Each one is pinned to the endpoint closest to the real app, and every answer is labeled **Real app answer**, **Same model, not the full tool**, or **Stand-in (not the real app)**. Labels are charted apart and never averaged.
- **Every answer kept.** The raw response, the cost of every call, every brand mention with its position, and every citation. A call that reported no cost is flagged, never stored as $0.
- **From a missed question to a fix.** Each question an AI app missed you on shows the brands named instead, the pages its answers link to, and a brief your coding agent can write a page from. Mark the question as handled by that page, and Byname tracks every later answer that links to it and whether the question starts naming you.
- **Who else gets named, and who talks about you.** Byname finds the brands answers offer instead of you, so you can track them as competitors, and searches X, Reddit, Hacker News, LinkedIn, YouTube, Threads and Bluesky for posts that name you.
- **Your keys, your limits.** One Treg token runs every AI app, or bring your own provider keys and gateways, app by app. A spend cap per check and an optional monthly cap bound the bill.
- **Built for agents.** Claude Code, Cursor and other MCP clients sign in as you in the browser, with the scopes you pick. Scripts use scoped API keys that can expire.
- **MIT licensed. Yours to run, change and keep.** One container and a Postgres database, on Unkey or any host. The scheduler runs inside it, so there is no cron to set up.

## Get started

### Deploy on Unkey with one prompt

The fastest way to a running Byname. Add Unkey's MCP server to your coding agent:

```sh
claude mcp add --transport http unkey https://mcp.unkey.com
```

Then copy the deploy prompt from the landing page, [byname.dev](https://byname.dev/#deploy), into the agent. The same prompt is in [`site/src/lib/prompt.ts`](site/src/lib/prompt.ts), and served as Markdown at [byname.dev/deploy-prompt.md](https://byname.dev/deploy-prompt.md). It asks for your Postgres URL, deploys Byname to Unkey, and gives you the link to create your account. To do it by hand, follow [Deploy on Unkey](docs/deploy-unkey.md).

### Run it locally with Docker

You need Docker with Compose v2.

```sh
git clone https://github.com/unkeyed/byname.git
cd byname
echo "ENCRYPTION_KEY=$(openssl rand -base64 32)" >> .env
docker compose up -d --build --wait
docker compose logs app | grep -A2 'Open this link'
```

Open the link Byname prints and register a passkey. Onboarding then asks for your brand, competitors, questions, the AI apps to ask, Search Console, social mentions and the schedule. Keep `.env`: the `ENCRYPTION_KEY` in it protects the provider keys and secrets Byname stores. To run it on your own server, see [Self-host Byname](docs/self-hosting.md).

## Choose your AI apps

Open **Settings > AI apps**, tick the AI apps your buyers use, and paste one Treg token under **Settings > Treg**. Developer tools such as Claude Code take one or more of your team's models. Every AI app and model makes one short test call before the first check, which confirms its accuracy label.

[AI apps and accuracy labels](docs/engines/surfaces.md) · [Treg](docs/engines/treg.md) · [Scraping providers](docs/engines/scraping-providers.md) · [Model providers](docs/engines/model-providers.md) · [Gateways](docs/engines/gateways.md)

Not using Treg? Any AI app can call DataForSEO, cloro or SerpApi with your key and stay a Real app answer, or call OpenAI, Anthropic, Gemini, Perplexity or an OpenAI-compatible gateway directly, which makes an app a Stand-in.

## Connect Search Console

In onboarding's **Search Console** step, or later under **Settings > Search Console**, click **Sign in with Google**, or paste a service account's JSON key. Byname asks Google for read-only access, syncs once a day, and backfills about 16 months on the first sync. Sign in with Google needs your own Google OAuth client in `GSC_GOOGLE_CLIENT_ID` and `GSC_GOOGLE_CLIENT_SECRET`. [Search Console](docs/search-console.md) shows how to create one, and covers branded search terms and goals.

## Connect your agents

```sh
claude mcp add --transport http byname https://<your-byname>/mcp
```

There is no key to copy. The first time, your browser opens, you pick the scopes, and the agent acts as you. Ask it which questions you lost since the last check, which competitors the answers named instead, or to queue a check. [Agents](docs/agents.md) covers Cursor, Claude Desktop, the REST API and the scope table.

## Documentation

| Guide | What it covers |
|---|---|
| [Self-host](docs/self-hosting.md) | Run Byname with Docker Compose or on any container host, upgrades, backups and recovery |
| [Deploy on Unkey](docs/deploy-unkey.md) | Every step of an Unkey deployment |
| [Configuration](docs/configuration.md) | Every environment variable |
| [Sign-in](docs/sign-in.md) | Passkeys, Google, GitHub, OIDC, roles, two-factor and recovery |
| [AI apps](docs/engines/surfaces.md) | Every AI app Byname asks, the accuracy labels, models, routes and known gaps |
| [Search Console](docs/search-console.md) | The Google OAuth client, service accounts, the sync, branded terms and goals |
| [Agents](docs/agents.md) | MCP over OAuth, the REST API, API keys and scopes |
| [Notifications](docs/notifications.md) | The bell, where notifications go (email, Slack and webhooks), each kind, and webhook signatures |
| [Site changes](docs/changes.md) | Site changes from GitHub, when Google sees them, and results against comparison pages |
| [How answers are scored](docs/scores.md) | Named you, questions that name your brand, and aliases that are everyday words |
| [Social](docs/social.md) | Posts that name you on seven social networks, the people you leave out, and the digest |

## Contributing

[CONTRIBUTING.md](CONTRIBUTING.md) explains how to run Byname from source, run the tests, and add an AI app.

## License

[MIT](LICENSE). Provided by [Unkey](https://www.unkey.com).
