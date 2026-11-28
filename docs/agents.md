# Connect agents to Byname

Agents reach Byname in two ways. The MCP servers, at `/mcp` and at `/e/<slug>/mcp` for each entity, give an agent tools to read results and, with the right scopes, change the setup or start a check. The REST API under `/v1` does the same over plain HTTP. Both call the same code as the web UI, so an agent sees exactly what your team sees.

They sign in differently. An MCP client signs in as you, in your browser, and needs no key. The REST API takes an API key. Both carry scopes, so you decide what each agent can do.

In the examples, replace `https://byname.example.com` with your Byname URL, which is its `PUBLIC_URL`.

## Scopes

Every API key and every MCP connection carries one or more of three scopes. Each REST route and MCP tool needs exactly one, and a test fails if one does not name it.

| Scope | Allows |
|---|---|
| `results:read` | Read checks, answers, raw responses, questions, brands, search engine data (Google and Bing), goals, site changes, comparison pages, the repo connection, social mentions and digests, the left-out list and filters, the MCP server grade, notifications and settings. Never a secret. |
| `runs:write` | Queue a check, pause and resume scheduled checks, read past answers for other brands, and grade the MCP server. |
| `config:write` | Edit the brand, competitors, questions and question sets, AI apps and models, the schedule and limits, goals, branded search terms, site changes logged by hand, comparison pages, path rules, when change results fall and what counts as a move, the social search settings, the people left out of social mentions, which brands answers name are tracked or hidden, the settings for finding other brands and their logos, and the MCP server URL. |

A key or connection never carries a scope its creator's role lacks. An Admin or Editor can grant all three. A Viewer can grant only `results:read`. If the creator's role later loses a scope, so do their keys and connections. [Sign-in](sign-in.md#roles-and-what-they-allow) describes the roles.

Some things no key or MCP connection can do, whatever its scopes. The Treg token, provider keys, search engine connections (Google Search Console and Bing Webmaster Tools), the GitHub repo connection for site changes, the MCP server token, where notifications go, API keys, connected apps and teammates are managed only by a signed-in Admin in the UI. A key that tries gets `403`.

## Connect an MCP client

Byname's MCP server uses the Streamable HTTP transport, without sessions, at `https://byname.example.com/mcp`. Add that URL to your client with no `Authorization` header. **Settings > Apps** shows the snippets with your URL filled in.

### Claude Code

```sh
claude mcp add --transport http byname https://byname.example.com/mcp
```

Then run `/mcp` in Claude Code, pick `byname`, and choose **Authenticate**.

Each section below adds the first entity. [Connect to one entity](#connect-to-one-entity) adds any other.

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

### Connect to one entity

Each entity has its own MCP server at `/e/<slug>/mcp`, with the same tools. `GET /v1/entities` and the `list_entities` tool give each slug. Add one server per entity an agent works on, each under its own name. For an entity with the slug `kyring`:

```sh
claude mcp add --transport http byname-kyring https://byname.example.com/e/kyring/mcp
```

In Cursor's `mcp.json`:

```json
{
	"mcpServers": {
		"byname-kyring": {
			"url": "https://byname.example.com/e/kyring/mcp"
		}
	}
}
```

In Claude Desktop's `claude_desktop_config.json`:

```json
{
	"mcpServers": {
		"byname-kyring": {
			"command": "npx",
			"args": ["-y", "mcp-remote", "https://byname.example.com/e/kyring/mcp"]
		}
	}
}
```

One sign-in covers every entity. The team, its roles and the scopes you approve are the same in each, and a connection's tokens work at every entity's URL. Most clients still keep a sign-in per server you add, so each one asks you to approve it once, and **Settings > Apps** lists each as its own connection.

`/mcp` stays the first entity's, so a client you connected before there were entities keeps working without a change.

### What happens when you connect

1. The client calls the MCP URL without a token. Byname answers `401` with a `WWW-Authenticate` header that points to `/.well-known/oauth-protected-resource/mcp`, or to `/.well-known/oauth-protected-resource/e/<slug>/mcp` for an entity's URL. That document names the URL the client called as the resource.
2. The client reads that document and the authorization server metadata at `/.well-known/oauth-authorization-server`, registers itself at `/oauth/register`, and opens your browser at `/oauth/authorize`. Byname refuses a request without a PKCE `S256` code challenge.
3. You sign in to Byname if you are not signed in already. Byname then shows the app's name marked **Unverified**, where your browser goes after you approve, such as `127.0.0.1:33418` for an app on your computer, and the scopes the app asked for. Only `results:read` is ticked at first. Tick more only when the agent needs them.
4. You approve, and the browser hands a one-time code back to the client. The code lasts 5 minutes. The client trades it at `/oauth/token` for an access token, which lasts an hour, and a refresh token, which lasts 30 days.

From then on the agent acts as you, with the scopes you ticked. A client that asked for no scope Byname knows is offered all three. Approving needs a sign-in within the last 15 minutes, the same as creating an API key.

Any app can register with any name, so the consent page marks every app **Unverified**. Approve only an app you just started connecting, and check where it sends you back.

Each IP address can register 20 apps and start 30 connections every 10 minutes, which covers a team behind one office network. Past that, `/oauth/register` answers `429` with `Retry-After`, and `/oauth/authorize` shows a page that asks you to wait. The wait ends with the 10 minutes, however many more requests arrive. `/mcp` and the OAuth endpoints accept calls from any origin, so a client that runs in a browser can connect too.

Byname stores codes and tokens only as SHA-256 hashes. Access tokens start with `byn_at_` and refresh tokens with `byn_rt_`. A refresh token works once. Presenting it a second time revokes the whole connection, because then someone other than the client holds a copy.

Every tool names the scope it needs in its `_meta`, under `byname.dev/scope`. A tool the connection lacks the scope for fails with a message that says which scope to reconnect with.

### Disconnect an app

**Settings > Apps** lists every connected app, who approved it, its scopes, and when it was last used. **Disconnect** stops its tokens at once, in every entity. Byname also ends a connection in these cases:

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

## Reach each entity

Each brand Byname tracks is an entity with its own setup and results. One API key reaches every entity. Put `/e/<slug>` in front of a path to call it in that entity. A path without it calls the first entity, as every path did before there were entities:

```sh
curl https://byname.example.com/v1/entities -H "Authorization: Bearer $BYNAME_API_KEY"
curl https://byname.example.com/e/kyring/v1/runs -H "Authorization: Bearer $BYNAME_API_KEY"
```

`GET /v1/entities` lists each entity's slug and whether its onboarding is finished. Adding, renaming and deleting an entity take an admin signed in to the UI, not an API key, and MCP has no tool for them. A deleted entity answers `404` at once, under any key or connection, and its data is gone about five minutes later. The team, API keys, apps and provider keys are shared, so their routes answer the same in every entity. MCP works the same way: `/mcp` serves the first entity and `/e/<slug>/mcp` the one with that slug. See [Connect to one entity](#connect-to-one-entity).

## MCP tools

Byname's MCP server has 94 tools. Every REST route an API key can call has one with the same scope, except `DELETE /v1/keys/current`, since MCP clients sign in with OAuth instead of a key, and `GET /v1/brands/{id}/logo`, an image for the browser. A test keeps the two in step. Read tools are marked read-only, and `delete_brand`, `delete_goal`, `delete_change`, `delete_control_page`, `remove_left_out_account`, `remove_social_filter` and `unmark_question_page` are marked destructive, so a client can ask before it runs them.

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
| `list_social_mentions` | `results:read` | `GET /v1/social/mentions` | Posts that mention the brand on X, Reddit, Hacker News, LinkedIn, YouTube, Threads and Bluesky, newest first, each with its author, text, engagement and link, without those by left-out accounts or hidden by a topic or subreddit filter unless include_hidden is set, with how many match and how many are hidden. |
| `get_social_digest` | `results:read` | `GET /v1/social/digest` | One day's or week's social mentions grouped by network with the most engaged first, the authors new in it, and what the searches cost, without left-out accounts or filtered-out posts; the latest period that has ended unless day names one. |
| `get_social_settings` | `results:read` | `GET /v1/social/settings` | Whether social searches are on (they start off), the social search terms, each network with whether it is on, its route (Treg or direct) and its latest call, how often searches run and when the digest goes out. |
| `list_left_out_accounts` | `results:read` | `GET /v1/social/left-out` | The accounts whose mentions are hidden, such as teammates and the company's own accounts, each with its label and how many stored mentions it hides. |
| `update_social_settings` | `config:write` | `PUT /v1/social/settings` | Turn social searches on or off, replace the social search terms, the fetch period and the digest schedule, and turn networks on or off (read them with get_social_settings first). |
| `leave_out_account` | `config:write` | `POST /v1/social/left-out` | Leave an account out of social mentions, or relabel one already left out; its past mentions are hidden too. |
| `remove_left_out_account` | `config:write` | `DELETE /v1/social/left-out/{id}` | Take an account off the left-out list, so its mentions show again. |
| `list_social_filters` | `results:read` | `GET /v1/social/filters` | The topics and subreddits that social mentions are filtered by: each one left out (its posts are hidden) or kept to (only posts matching one are shown), with how many stored mentions it matches. |
| `add_social_filter` | `config:write` | `POST /v1/social/filters` | Leave a topic or subreddit out of social mentions (mode leave_out), or show only the posts that match it (mode only), such as leaving out the topic weather when the brand's name means something else there; it applies to past mentions too, and setting one already set changes its mode. |
| `remove_social_filter` | `config:write` | `DELETE /v1/social/filters/{id}` | Take a topic or subreddit filter off, so the posts it hid show again. |
| `list_other_brands` | `results:read` | `GET /v1/other-brands` | The brands AI answers named that you don't track, most named first: how many answers in range named each, the questions it was named for most, and the change since the check before. They count in no score. |
| `list_hidden_brands` | `results:read` | `GET /v1/other-brands/hidden` | The brands someone marked as not a competitor, with how many answers named each. |
| `get_other_brands_settings` | `results:read` | `GET /v1/other-brands/settings` | Whether each new answer is read for other brands and what reading one costs, and whether logos are looked up, what one new brand costs and the monthly limit on new lookups. |
| `get_brand_backfill` | `results:read` | `GET /v1/other-brands/backfill` | How many past answers were never read for other brands, what reading them would cost, and the latest pass someone started. |
| `list_logos` | `results:read` | `GET /v1/logos` | Every brand with a logo, with its colour and domain. |
| `track_brand` | `config:write` | `POST /v1/other-brands/{id}/track` | Track a brand AI answers named as a competitor. It counts from then on, and every past answer is scored again, locally and for free. |
| `hide_brand` | `config:write` | `POST /v1/other-brands/{id}/hide` | Mark a brand AI answers named as not a competitor, so it leaves the list. |
| `unhide_brand` | `config:write` | `POST /v1/other-brands/{id}/unhide` | Put a brand marked as not a competitor back on the list. |
| `update_other_brands_settings` | `config:write` | `PUT /v1/other-brands/settings` | Turn reading answers for other brands and logo lookups on or off, and set the monthly limit on new lookups. |
| `start_brand_backfill` | `runs:write` | `POST /v1/other-brands/backfill` | Read every past answer never read for other brands, as one job in Check history, within the monthly spend cap. |
| `mcp_server_grade` | `results:read` | `GET /v1/mcp-server` | Your own MCP server's URL, whether a token is stored, its newest grade in full (score, letter, whether it is partial and why, each category and check with its finding and fix, each tool's documentation score) and the scores of the newest 50. See [MCP server grade](mcp-server.md). |
| `set_mcp_server` | `config:write` | `PUT /v1/mcp-server` | Set the MCP server URL. Another origin deletes the stored token; an empty URL removes the server and its token. |
| `grade_mcp_server` | `runs:write` | `POST /v1/mcp-server/grade` | Grade the MCP server now, such as after a deploy, and return the new grade. Never calls a tool; takes up to 30 seconds. |
| `status` | `results:read` | `GET /v1/status` | Byname's version and whether onboarding is done. |
| `list_entities` | `results:read` | `GET /v1/entities` | Every brand this instance tracks, each an entity, and whether its onboarding is finished. A connection reads and changes one entity: the first at `/mcp`, any other at `/e/<slug>/mcp`. |
| `get_costs` | `results:read` | `GET /v1/costs` | What Byname costs, in US dollars: one answer in each AI app, one check, a month of checks and of social searches, and what this month has spent against the monthly cap. See [Treg](engines/treg.md#cost). |
| `list_alerts` | `results:read` | `GET /v1/alerts` | Notifications newest first, the ones the app shows under the bell: finished checks with the AI apps that started or stopped naming you on a question, refused or failed checks, spend caps reached, paused models, search goals reached and search clicks that moved on Google or Bing, change results, an expiring GitHub token and social digests, each with whether the person this connection acts for has read it, and the unread count. |
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
| `save_onboarding_step` | `config:write` | `PUT /v1/onboarding/steps/{step}` | Mark an onboarding step saved, after saving its data with the matching tool, or to skip an optional step (competitors, search, social). |
| `finish_onboarding` | `config:write` | `POST /v1/onboarding/finish` | Finish onboarding once the required steps are saved and every AI app and model turned on has passed test_pair. With no monthly cap set, it sets one of three times the monthly estimate, at least $5.00. |
| `test_pair` | `config:write` | `POST /v1/pairs/{id}/test` | Make one short paid call for an AI app and model through its route. |
| `estimate_cost` | `results:read` | `GET /v1/estimate` | The estimated cost of one check: calls (questions times each AI app and model turned on times how many times each question is asked) and each one's cost per call from its endpoint's past answers or the sampled cost, with those that have neither counted as unknown. |
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
| `list_handled_pages` | `results:read` | `GET /v1/handled` | Every page marked as answering a question: the answers since it was marked that link to it, for any question and from any AI app, and how the question named you on the check before the mark and on the latest since. See [Handled questions](handled.md). |
| `mark_question_handled` | `config:write` | `POST /v1/questions/{id}/pages` | Mark a question as handled by a page you published to answer it. Byname tracks every later answer that links to the page. |
| `unmark_question_page` | `config:write` | `DELETE /v1/question-pages/{id}` | Remove a marked page and its tracking. |
| `content_brief` | `results:read` | `GET /v1/questions/{id}/brief` | A prompt for an agent working on your site, to write the content that gets you named and linked to on one question: how each AI app answered on a check, the competitors named instead, the web searches the apps ran and the pages they linked to, with the markdown to follow in prompt. Works for any question, not only missed ones. Filter by AI app (surface) and model. |
| `list_jobs` | `results:read` | `GET /v1/jobs` | The jobs log newest first: each AI check and model list refresh with its status, error and spend. |
| `search_status` | `results:read` | `GET /v1/search/providers` | Every search engine Byname reads, Google Search Console and Bing Webmaster Tools: whether each is connected, the site synced, the last sync and its error, the latest final day every window ends on, the partial days the next sync overwrites, how many days one row of searches covers (7 for Bing's weekly top lists) and how its numbers differ. |
| `search_summary` | `results:read` | `GET /v1/search/{provider}/summary` | One search engine's headline: clicks, appearances (impressions), click rate (CTR), average position, and clicks from searches with your name, without it (non-brand) and unlisted, over the latest 28 days ending on the latest final day, with the 28 days before for the trend. Bing gives no average position for the whole site, and its windows are whole weeks. |
| `search_daily` | `results:read` | `GET /v1/search/{provider}/daily` | One search engine's site clicks, appearances, click rate and average position per day, partial days marked final false. |
| `search_brand_split` | `results:read` | `GET /v1/search/{provider}/brand` | Clicks from searches with your name against searches without it over trailing 28-day windows ending on final days only, one a day for Google and one a week for Bing. |
| `top_pages` | `results:read` | `GET /v1/search/{provider}/pages` | One search engine's top pages by clicks over a window ending on the latest final day. For Bing, sums of its weekly top lists. |
| `top_queries` | `results:read` | `GET /v1/search/{provider}/queries` | One search engine's top search queries by clicks over a window ending on the latest final day, each marked branded or not; non_brand leaves branded ones out. For Bing, sums of its weekly top lists. |
| `list_goals` | `results:read` | `GET /v1/goals` | Every search engine's goals by due date, each with its provider and its progress over its trailing 28- or 90-day window ending on that provider's latest final day. |
| `upsert_goal` | `config:write` | `POST /v1/goals` | Add a search goal for one provider, or set the target of the goal with the same provider, metric, window and due date. |
| `update_goal` | `config:write` | `PUT /v1/goals/{id}` | Replace a search goal by id. |
| `delete_goal` | `config:write` | `DELETE /v1/goals/{id}` | Delete a search goal. |
| `update_branded_terms` | `config:write` | `PUT /v1/settings/branded-terms` | Replace the branded search terms, which decide brand against non-brand on every search engine. |

The server's instructions tell an agent where to start: `list_runs`, then `compare_runs` for the AI apps that started or stopped naming you on a question, `gaps` for missed questions, `content_brief` for a prompt that tells an agent working on your site what to write to win one, and `question_history` for one question over time. They also say that each AI app and model is its own line with its own accuracy label, never averaged with another, and that naming you and linking to your site are counted apart.

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

The answer counts the questions on each AI app and model that started naming you (`gained`), stopped (`lost`), moved your position (`moved`) or stayed the same, with the net change, and lists each question and AI app that changed. The app shows the same comparison on **AI answers > What changed**.

Find the missed questions, where some AI app did not name you:

```sh
curl "https://byname.example.com/v1/gaps" \
	-H "Authorization: Bearer byn_..."
```

Get a prompt that tells the agent working in your site's repository what to write so AI apps name you on one question. `prompt` is the markdown to paste in, and the rest of the answer is the same facts as data. The prompt ends by asking the agent to mark the question as handled by the page once it is live, with `mark_question_handled`, so Byname tracks the answers that link to it. See [Handled questions](handled.md). In the app, **Prompt for your agent** on each question under **AI answers > Missed questions**, and at the top of the question drawer, opens the same prompt with a copy button:

```sh
curl "https://byname.example.com/v1/questions/12/brief" \
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
