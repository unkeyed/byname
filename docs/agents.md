# Connect agents to Byname

Agents reach Byname in two ways. The MCP server at `/mcp` gives an agent tools to read results and, with the right scopes, change the setup or start a check. The REST API under `/v1` does the same over plain HTTP. Both call the same code as the web UI, so an agent sees exactly what your team sees.

They sign in differently. An MCP client signs in as you, in your browser, and needs no key. The REST API takes an API key. Both carry scopes, so you decide what each agent can do.

In the examples, replace `https://byname.example.com` with your Byname URL, which is its `PUBLIC_URL`.

## Scopes

Every API key and every MCP connection carries one or more of three scopes. Each REST route and MCP tool needs exactly one, and a test fails if one does not name it.

| Scope | Allows |
|---|---|
| `results:read` | Read checks, answers, raw responses, questions, brands, Search Console data, goals, site changes, comparison pages, the repo connection, social mentions and digests, the left-out list, notifications and settings. Never a secret. |
| `runs:write` | Queue a check, and pause and resume scheduled checks. |
| `config:write` | Edit the brand, competitors, questions and question sets, AI apps and models, the schedule and limits, goals, branded search terms, site changes logged by hand, comparison pages, path rules, when change results fall and what counts as a move, the social search settings, and the people left out of social mentions. |

A key or connection never carries a scope its creator's role lacks. An Admin or Editor can grant all three. A Viewer can grant only `results:read`. If the creator's role later loses a scope, so do their keys and connections. [Sign-in](sign-in.md#roles-and-what-they-allow) describes the roles.

Some things no key or MCP connection can do, whatever its scopes. The Treg token, provider keys, Search Console connections, the GitHub repo connection for site changes, where notifications go, API keys, connected apps and teammates are managed only by a signed-in Admin in the UI. A key that tries gets `403`.

## Connect an MCP client

Byname's MCP server uses the Streamable HTTP transport, without sessions, at `https://byname.example.com/mcp`. Add that URL to your client with no `Authorization` header. **Settings > Apps** shows the snippets with your URL filled in.

### Claude Code

```sh
claude mcp add --transport http byname https://byname.example.com/mcp
```

Then run `/mcp` in Claude Code, pick `byname`, and choose **Authenticate**.

### Cursor

Add Byname to `.cursor/mcp.json` in your project, or to `~/.cursor/mcp.json` for every project:

```json
{
	"mcpServers": {
		"byname": {
			"url": "https://byname.example.com/mcp"
		}
	}
}
```

Cursor then asks you to log in to Byname from its MCP settings.

### Claude Desktop

Claude Desktop connects through [`mcp-remote`](https://www.npmjs.com/package/mcp-remote), which needs Node.js and signs in the same way.

1. In Claude Desktop, open **Settings > Developer** and click **Edit Config**.
2. Add Byname to `claude_desktop_config.json`:

   ```json
   {
   	"mcpServers": {
   		"byname": {
   			"command": "npx",
   			"args": ["-y", "mcp-remote", "https://byname.example.com/mcp"]
   		}
   	}
   }
   ```

3. Restart Claude Desktop.

### What happens when you connect

1. The client calls `/mcp` without a token. Byname answers `401` with a `WWW-Authenticate` header that points to `/.well-known/oauth-protected-resource/mcp`.
2. The client reads that document and the authorization server metadata at `/.well-known/oauth-authorization-server`, registers itself at `/oauth/register`, and opens your browser at `/oauth/authorize`. Byname refuses a request without a PKCE `S256` code challenge.
3. You sign in to Byname if you are not signed in already. Byname then shows the app's name marked **Unverified**, where your browser goes after you approve, such as `127.0.0.1:33418` for an app on your computer, and the scopes the app asked for. Only `results:read` is ticked at first. Tick more only when the agent needs them.
4. You approve, and the browser hands a one-time code back to the client. The code lasts 5 minutes. The client trades it at `/oauth/token` for an access token, which lasts an hour, and a refresh token, which lasts 30 days.

From then on the agent acts as you, with the scopes you ticked. A client that asked for no scope Byname knows is offered all three. Approving needs a sign-in within the last 15 minutes, the same as creating an API key.

Any app can register with any name, so the consent page marks every app **Unverified**. Approve only an app you just started connecting, and check where it sends you back.

Byname stores codes and tokens only as SHA-256 hashes. Access tokens start with `byn_at_` and refresh tokens with `byn_rt_`. A refresh token works once. Presenting it a second time revokes the whole connection, because then someone other than the client holds a copy.

Every tool names the scope it needs in its `_meta`, under `byname.dev/scope`. A tool the connection lacks the scope for fails with a message that says which scope to reconnect with.

### Disconnect an app

**Settings > Apps** lists every connected app, who approved it, its scopes, and when it was last used. **Disconnect** stops its tokens at once. Byname also ends a connection in these cases:

- The client signs out through `/oauth/revoke`.
- A refresh token is used twice.
- The person who approved it is removed from the team.

### API keys and `/mcp`

`/mcp` does not take API keys. A client configured with an `Authorization: Bearer byn_...` header gets `401`. Remove the header, and the client signs in through the browser instead.

## Create an API key

The REST API takes API keys.

1. In Byname, open **Settings > API keys**. Only an Admin can.
2. Name the key after what uses it, for example `Weekly report`.
3. Choose its scopes. A reporting script needs only `results:read`.
4. Choose when it expires: in 30 days, 90 days, a year, or never. An expired key gets `401` and stays listed until you revoke it. Give a key an expiry unless something you cannot easily update relies on it.
5. Create the key and copy it. Byname shows it only once.

A key is `byn_` plus 52 characters. Byname stores only its SHA-256 hash, and lists show `byn_` plus the next four characters so you can tell keys apart. A key's last use is recorded at most once a minute.

Only a signed-in Admin can create, list or revoke keys, so a leaked key cannot create more keys. A key can revoke itself, whatever its scopes, which suits an agent that finds its key in a log or a commit:

```sh
curl -X DELETE https://byname.example.com/v1/keys/current -H "Authorization: Bearer $BYNAME_API_KEY"
```

Keys live in Postgres by default, or in Unkey when `UNKEY_ROOT_KEY` and `UNKEY_API_ID` are set. See [Configuration](configuration.md#unkey_root_key-and-unkey_api_id).

## MCP tools

Byname's MCP server has 73 tools. Every REST route an API key can call has one with the same scope, except `DELETE /v1/keys/current`, since MCP clients sign in with OAuth instead of a key. A test keeps the two in step. Read tools are marked read-only, and `delete_brand`, `delete_goal`, `delete_change`, `delete_control_page` and `remove_left_out_account` are marked destructive, so a client can ask before it runs them.

| Tool | Scope | REST | What it does |
|---|---|---|---|
| `list_changes` | `results:read` | `GET /v1/changes` | Every site change, the latest merge first: the page, the pull request or manual log, Google's first crawl after the merge, when the first and final results fall (`interim_on`, `verdict_on`), the page's before-period numbers, and the first and final difference-in-differences results against the comparison pages. |
| `get_change` | `results:read` | `GET /v1/changes/{id}` | One site change with its timeline (merge, URL inspections, first crawl, the days of the first and final results) and the full results. |
| `change_markers` | `results:read` | `GET /v1/changes/markers` | One marker per merged pull request or logged change between the from and to dates, on Search Console (Pacific Time) days, for marking a daily chart. |
| `change_settings` | `results:read` | `GET /v1/changes/settings` | When a change's first and final results fall, in days after the first crawl, and the thresholds a move must pass against the comparison pages. |
| `list_control_pages` | `results:read` | `GET /v1/control-pages` | The comparison pages, which the API calls control pages, that change results compare against, each with its hold date; a page past it is inactive and drops out of the comparison set. |
| `repo_status` | `results:read` | `GET /v1/repo` | The GitHub repo site changes come from: whether a token is stored, its expiry, the path rules and the last hourly poll. |
| `log_change` | `config:write` | `POST /v1/changes` | Log a change to one page made outside GitHub, so recrawl watch and change results follow it. |
| `delete_change` | `config:write` | `DELETE /v1/changes/{id}` | Delete a change logged by hand. |
| `update_change_settings` | `config:write` | `PUT /v1/changes/settings` | Replace the days of the first and final results and the thresholds. |
| `upsert_control_page` | `config:write` | `POST /v1/control-pages` | Add a comparison page, or change the hold date and note of the one with the same URL. |
| `delete_control_page` | `config:write` | `DELETE /v1/control-pages/{id}` | Remove a comparison page. |
| `update_path_rules` | `config:write` | `PUT /v1/repo/rules` | Replace the rules that map a pull request's changed files to page URLs. |
| `list_social_mentions` | `results:read` | `GET /v1/social/mentions` | Posts that mention the brand on X, Reddit, Hacker News, LinkedIn, YouTube, Threads and Bluesky, newest first, each with its author, text, engagement and link, without those by left-out accounts unless include_hidden is set, with how many match and how many are hidden. |
| `get_social_digest` | `results:read` | `GET /v1/social/digest` | One day's or week's social mentions grouped by network with the most engaged first, the authors new in it, and what the searches cost, without left-out accounts; the latest period that has ended unless day names one. |
| `get_social_settings` | `results:read` | `GET /v1/social/settings` | Whether social searches are on (they start off), the social search terms, each network with whether it is on, its route (Treg or direct) and its latest call, how often searches run and when the digest goes out. |
| `list_left_out_accounts` | `results:read` | `GET /v1/social/left-out` | The accounts whose mentions are hidden, such as teammates and the company's own accounts, each with its label and how many stored mentions it hides. |
| `update_social_settings` | `config:write` | `PUT /v1/social/settings` | Turn social searches on or off, replace the social search terms, the fetch period and the digest schedule, and turn networks on or off (read them with get_social_settings first). |
| `leave_out_account` | `config:write` | `POST /v1/social/left-out` | Leave an account out of social mentions, or relabel one already left out; its past mentions are hidden too. |
| `remove_left_out_account` | `config:write` | `DELETE /v1/social/left-out/{id}` | Take an account off the left-out list, so its mentions show again. |
| `status` | `results:read` | `GET /v1/status` | Byname's version and whether onboarding is done. |
| `get_costs` | `results:read` | `GET /v1/costs` | What Byname costs, in US dollars: one answer in each AI app, one check, a month of checks and of social searches, and what this month has spent against the monthly cap. See [Treg](engines/treg.md#cost). |
| `list_alerts` | `results:read` | `GET /v1/alerts` | Notifications newest first, the ones the app shows under the bell: finished checks with the answers that started or stopped naming you, refused or failed checks, spend caps reached, paused models, Google search goals reached, Google clicks that moved, change results, an expiring GitHub token and social digests, each with whether the person this connection acts for has read it, and the unread count. |
| `mark_alerts_read` | `results:read` | `POST /v1/alerts/read` | Mark notifications read for the person this connection acts for, by id or all at once. |
| `prompts` | `results:read` | `GET /v1/prompts` | Every question of the latest check in range: the AI apps and models that named you there, your best position, the competitors named instead, and per check whether some AI app named you, only linked to your site, did not, or every call failed. |
| `cited_sources` | `results:read` | `GET /v1/sources` | The domains and pages answers linked to most over the checks in range, whether each page's HTML names you and which brands it names. |
| `engine_view` | `results:read` | `GET /v1/metrics/engine` | One chart line (an AI app, or one of its models, on one endpoint and accuracy label, as mention_rates reports it): how often it named you, the questions it named you on and your average position on its latest check, how often it named you per check, its brand ranking, and the questions it named you on or not, with who it named instead. |
| `get_settings` | `results:read` | `GET /v1/settings` | The instance's settings: your brand, market, language, schedule (with the next and last check), spend caps and check limits. |
| `update_settings` | `config:write` | `PUT /v1/settings` | Replace the settings, every field at once (read them with get_settings first). |
| `list_brands` | `results:read` | `GET /v1/brands` | The brand table: your own brand (kind self), competitors and technologies. |
| `upsert_brand` | `config:write` | `POST /v1/brands` | Add a competitor or technology, or replace the one with the same name ignoring case. |
| `update_brand` | `config:write` | `PUT /v1/brands/{id}` | Replace a competitor or technology by id, which can rename it. |
| `delete_brand` | `config:write` | `DELETE /v1/brands/{id}` | Delete a competitor or technology and its recorded mentions. |
| `get_questions` | `results:read` | `GET /v1/questions` | The current question set: its version and questions, and how it carries on the previous version. |
| `update_questions` | `config:write` | `PUT /v1/questions` | Replace the questions. |
| `list_question_sets` | `results:read` | `GET /v1/question-sets` | Every question set version, newest first. |
| `get_question_set` | `results:read` | `GET /v1/question-sets/{id}` | One question set version by id with its questions. |
| `list_surfaces` | `results:read` | `GET /v1/surfaces` | Every AI app Byname can ask: its current route (Treg, a scraping provider's own key, a direct provider key, or a gateway) and endpoint, the routes it can take with the accuracy label each gives, whether it is on, and its models with their last test. |
| `update_surface` | `config:write` | `PUT /v1/surfaces/{key}` | Turn an AI app on or off, move it to another route, and, when models is given, set its models (each checked against the route's live list). |
| `list_surface_models` | `results:read` | `GET /v1/surfaces/{key}/models` | An AI app's live model list on its current route, or on another route given in route, read for free. |
| `get_onboarding` | `results:read` | `GET /v1/onboarding` | How far onboarding has come: each step and whether it is saved, when it was finished, what still blocks finishing, and the scheduled check finishing now would set. |
| `save_onboarding_step` | `config:write` | `PUT /v1/onboarding/steps/{step}` | Mark an onboarding step saved, after saving its data with the matching tool, or to skip an optional step (competitors, search_console, social). |
| `finish_onboarding` | `config:write` | `POST /v1/onboarding/finish` | Finish onboarding once the required steps are saved and every AI app and model turned on has passed test_pair. With no monthly cap set, it sets one of three times the monthly estimate, at least $5.00. |
| `test_pair` | `config:write` | `POST /v1/pairs/{id}/test` | Make one short paid call for an AI app and model through its route. |
| `estimate_cost` | `results:read` | `GET /v1/estimate` | The estimated cost of one check: calls (questions times each AI app and model turned on) and each one's cost per call from its endpoint's past answers or the sampled cost, with those that have neither counted as unknown. |
| `set_question_scoring` | `config:write` | `PUT /v1/questions/{id}/scoring` | Count a question's answers in scores, leave them out, or go back to the default, which leaves out a question whose text names your brand. See [How answers are scored](scores.md). |
| `check_naming` | `results:read` | `POST /v1/naming-check` | Check text before saving it: which brands each question names, and whether each alias is a common English word. Stores nothing. |
| `run_now` | `runs:write` | `POST /v1/runs` | Queue an AI check now without moving the schedule. |
| `pause_checks` | `runs:write` | `POST /v1/checks/pause` | Pause scheduled checks, keeping the schedule. |
| `resume_checks` | `runs:write` | `POST /v1/checks/resume` | Resume scheduled checks. |
| `list_runs` | `results:read` | `GET /v1/runs` | Checks newest first, with status, spend, calls with no reported cost, how many scored answers named you, and success rate per endpoint. |
| `get_run` | `results:read` | `GET /v1/runs/{id}` | One check by id. |
| `list_run_answers` | `results:read` | `GET /v1/runs/{id}/answers` | A check's answers: each question, AI app and model with whether it named you or linked to your site, your position, the brands named and the cost. |
| `get_answer` | `results:read` | `GET /v1/answers/{id}` | One answer in full: its text, links, the AI app's own searches and the raw provider response. |
| `compare_runs` | `results:read` | `GET /v1/compare` | Every question, AI app and model that started naming you (gained), stopped (lost), moved your position or stayed the same from one check to a later one, with counts and the net change. |
| `question_history` | `results:read` | `GET /v1/questions/{id}/history` | One question's answers across checks, one line per AI app and model, with a one-line change summary per check. |
| `run_questions` | `results:read` | `GET /v1/runs/{id}/questions` | One check as the questions table: a row per question and a column per AI app and model, each cell named you, linked to your site and position with its change since the previous check, and per row how many named you and your best position. |
| `mention_rates` | `results:read` | `GET /v1/metrics/rates` | How often answers named you (`mention_rate`) and linked to your site (`citation_rate`), per check for each line: one per AI app and model, or per AI app with by=surface. |
| `share_of_mentions` | `results:read` | `GET /v1/metrics/share` | Each brand's share of mentions against your competitors, per check for each line. |
| `results_summary` | `results:read` | `GET /v1/metrics/summary` | The headline per check: how many answers named you or linked to your site out of those answered, and your share of mentions. |
| `competitors` | `results:read` | `GET /v1/metrics/competitors` | Share of mentions over the checks in range: each brand's mentions, share and rank, and how often each line's answers named each brand. |
| `gaps` | `results:read` | `GET /v1/gaps` | Missed questions, where at least one AI app did not name you on a check, ranked by the share of AI apps that left you out and by search demand where Byname has a figure, with the competitors named instead and each AI app's answer with the web searches it ran, where the endpoint exposes them. |
| `list_jobs` | `results:read` | `GET /v1/jobs` | The jobs log newest first: each AI check and model list refresh with its status, error and spend. |
| `search_console_status` | `results:read` | `GET /v1/gsc` | Whether Search Console is connected, the property synced, the last sync and its error, the latest day Google has finalized and the partial days the next sync overwrites. |
| `search_summary` | `results:read` | `GET /v1/search/summary` | The Search Console headline: clicks, appearances (impressions), click rate (CTR), average position, and clicks from searches with your name, without it (non-brand) and anonymized over the latest 28 days ending on the latest final day, with the 28 days before for the trend. |
| `search_daily` | `results:read` | `GET /v1/search/daily` | The site's Search Console clicks, appearances, click rate and average position per day, partial days marked final false. |
| `search_brand_split` | `results:read` | `GET /v1/search/brand` | Clicks from searches with your name against searches without it over trailing 28-day windows, one per day, ending on final days only. |
| `top_pages` | `results:read` | `GET /v1/search/pages` | The top pages by Search Console clicks over a window ending on the latest final day. |
| `top_queries` | `results:read` | `GET /v1/search/queries` | The top search queries by Search Console clicks over a window ending on the latest final day, each marked branded or not; non_brand leaves branded ones out. |
| `list_goals` | `results:read` | `GET /v1/goals` | The search goals by due date, each with its progress over its trailing 28- or 90-day window ending on the latest final day. |
| `upsert_goal` | `config:write` | `POST /v1/goals` | Add a search goal, or set the target of the goal with the same metric, window and due date. |
| `update_goal` | `config:write` | `PUT /v1/goals/{id}` | Replace a search goal by id. |
| `delete_goal` | `config:write` | `DELETE /v1/goals/{id}` | Delete a search goal. |
| `update_branded_terms` | `config:write` | `PUT /v1/settings/branded-terms` | Replace the branded search terms. |

The server's instructions tell an agent where to start: `list_runs`, then `compare_runs` for the answers that started or stopped naming you, `gaps` for missed questions, and `question_history` for one question over time. They also say that each AI app and model is its own line with its own accuracy label, never averaged with another, and that naming you and linking to your site are counted apart.

When a tool fails, its error says what to do next. An invalid value names the field and what it accepts, and a missing record says how to find or create one. An internal failure shows only a `request_id`, and the server log has the cause under the same id.

## Use the REST API

Send the key in the `Authorization` header. In the examples, replace `byn_...` with your key. Request and response bodies are JSON.

List the newest checks:

```sh
curl "https://byname.example.com/v1/runs?limit=5" \
	-H "Authorization: Bearer byn_..."
```

Compare two checks, question by question and AI app by AI app:

```sh
curl "https://byname.example.com/v1/compare?before=41&after=42" \
	-H "Authorization: Bearer byn_..."
```

The answer counts the answers that started naming you (`gained`), stopped (`lost`), moved your position (`moved`) or stayed the same, with the net change, and lists each question and AI app that changed. The app shows the same comparison on **AI answers > What changed**.

Find the missed questions, where some AI app did not name you:

```sh
curl "https://byname.example.com/v1/gaps" \
	-H "Authorization: Bearer byn_..."
```

Queue a check now. The key needs `runs:write`:

```sh
curl -X POST https://byname.example.com/v1/runs \
	-H "Authorization: Bearer byn_..."
```

The answer is `201` with the queued check. When a check is already queued or running, the answer is `200` with that check, and nothing new is queued. The check runs in the background, so follow it with `GET /v1/runs/{id}`. A key with only `results:read` gets `403`.

Results routes take `surface` and `model` to filter by AI app and model, and `since` (a date such as `2026-11-01`, or an RFC 3339 time) to set where the range starts. `GET /v1/openapi.yaml` lists each route's parameters.

An error answers with a status code and `{"error": "<message>"}`:

| Status | Meaning |
|---|---|
| `400` | The request body or a parameter is invalid. The message says which and why. |
| `401` | The key is missing, unknown, revoked or expired. Without a valid key, an unknown `/v1` route also answers `401`. |
| `403` | The key lacks the route's scope, or the route is for a signed-in person or an Admin, not for API keys. |
| `404` | The check, answer, question, brand or route does not exist. |
| `405` | The path exists but not with this method. The `Allow` header lists the methods it takes. |
| `409` | The thing to create already exists. |
| `429` | Too many failed attempts. Wait the number of seconds in the `Retry-After` header. |
| `502` | Treg or a provider did not answer a call Byname made for you, such as reading a model list. Retrying later may work. |
| `503` | The database or key store is down. Retry shortly. |

A `5xx` answer other than `502` also has a `request_id`. The server log records the cause under the same id.

### API reference

Byname serves its OpenAPI spec at `https://byname.example.com/v1/openapi.yaml`. The spec is built into the binary, so it describes the version you run. No API key is needed to read it.
