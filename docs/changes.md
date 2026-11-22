# Site changes

**Google search > Site changes** answers one question: did a change you shipped to your site help the page in Google once Google saw it? Byname records each change to a page, waits until Google sees the change, which is Google's first crawl of the page after it, and then compares the page's Search Console numbers before and after with comparison pages: a few steady pages you promise not to edit.

A change comes from one of two places. Byname reads pull requests merged into your site's GitHub repository, or someone logs the change by hand. Either way, seeing Google's crawl and working out the results need Search Console. Without it, Byname still records changes but cannot see Google crawl them or work out their results. [Search Console](search-console.md) explains the connection and the daily sync.

`internal/changes` holds all of it, and this page follows that package.

## Connect your site's repository

Byname reads merged pull requests from one GitHub repository, the one your site is built from.

1. On GitHub, create a fine-grained personal access token for that one repository, with **Contents** and **Pull requests** set to **Read-only**. Give it an expiry date.
2. In Byname, an Admin opens **Settings > Site changes** and enters the repository as `owner/name`, or pastes its GitHub URL, with the token. Connecting needs a sign-in within the last 15 minutes.
3. Byname reads the repository with the token before it saves anything. It records the default branch, encrypts the token with `ENCRYPTION_KEY`, and never returns it.
4. Add path rules, so merged files map to page URLs. See [Map files to pages](#map-files-to-pages).

Connecting the same repository again replaces the token and keeps its path rules and the changes found. Connecting another repository starts over with no rules. **Test** calls GitHub with the stored token. **Disconnect** deletes the token and stops the poll. The repository, its rules and the changes found stay.

## The hourly poll

The scheduler polls GitHub once an hour, inside the container, as the `site_changes` job. Connecting a repository, or saving changed path rules, makes the next poll run within a minute. There is no webhook, so there is no webhook secret to manage and no public endpoint to protect.

Each poll lists the pull requests merged into the default branch, newest update first. It stops at the first pull request last updated before the previous poll's high-water mark. The first poll after connecting, and the first poll after the path rules change, read the merges of the last 30 days instead, so recent changes show up at once. One poll reads at most 10 pages of 100 pull requests, and at most 30 pages of 100 files per pull request, GitHub's own limit.

For each merged pull request, Byname maps every changed file through the path rules and stores one change per page. A page whose files include an added file is a new page. Otherwise, a page with a removed file is removed, and any other page is an edit. A pull request read twice updates its changes in place, so no change is stored twice.

GitHub sends the token's expiry in the `GitHub-Authentication-Token-Expiration` header on every call made with a fine-grained token. Byname records it on each poll and each **Test**, and **Settings > Site changes** shows it. A token without an expiry shows **Never**. Within 14 days of the expiry, Settings and **Google search > Site changes** warn, and Byname raises a **Token expiring** alert, which only Admins see. See [Alerts](notifications.md).

When a poll fails, Settings and **Google search > Site changes** show why. A `401` from GitHub means the token expired or was revoked. A `404` means the repository does not exist or the token cannot read it.

## Map files to pages

A path rule maps the files a pull request changed to a page URL. It has a glob, matched against the file's path in the repository, and a URL template:

- `*` matches within one folder.
- `**` matches any number of folders, including none.
- `{1}`, `{2}` and so on in the URL are what the first, second and later wildcards matched.

| Glob | URL | A file | Its page |
|---|---|---|---|
| `content/blog/*.mdx` | `https://www.example.com/blog/{1}` | `content/blog/launch.mdx` | `https://www.example.com/blog/launch` |
| `app/**/page.tsx` | `https://www.example.com/{1}` | `app/pricing/page.tsx` | `https://www.example.com/pricing` |
| `app/**/page.tsx` | `https://www.example.com/{1}` | `app/page.tsx` | `https://www.example.com/` |

The first rule a file matches gives its page. A file that no rule matches is ignored, so a pull request that touches no mapped file adds no change. A list holds at most 50 rules. A glob has no leading slash. A URL must be an absolute `http` or `https` URL, and can only use placeholders for wildcards its glob has.

Under the rules, **Try a repo path** takes a path such as `content/blog/launch.mdx` and shows which rule matches it and the page URL it gives. The preview uses the rules as typed, saved or not.

An Admin edits the rules under **Settings > Site changes**. A key or MCP connection with `config:write` can replace them with `PUT /v1/repo/rules` or the `update_path_rules` tool. Saving changed rules makes the next poll read the last 30 days of merges again with them. Changes already found stay.

## Log a change by hand

For a site without GitHub, or a change made outside it, an Editor or Admin clicks **Log a change** on **Google search > Site changes**. A change logged by hand needs:

- the page's full URL, as Search Console reports it
- what changed: **New page**, **Edit** or **Removed**
- the day it went live, counted from midnight in the instance's time zone

Notes are optional. Through `POST /v1/changes` or the `log_change` tool, `merged_at` takes a day such as `2026-11-18` or a time such as `2026-11-18T15:04:05Z`. Byname refuses a time more than an hour in the future.

Only a change logged by hand can be deleted. A change from GitHub follows the repository and its path rules, so deleting one answers `409`.

## Recrawl watch

A result is only fair once Google has seen the change, so the clock starts at Google's first crawl after it, not at the merge. After each Search Console sync that completes, Byname runs the `recrawl_watch` job and then the `verdicts` job.

Recrawl watch asks Google's URL Inspection API about each page with a change that Google has not crawled since, the oldest change first. It records Google's inspection result, the coverage state, the last crawl time, and the canonical URLs. When the last crawl is after the change, that crawl becomes the change's first crawl, shown as **Google saw it**. The days of the first and final results then follow from the crawl's day in the instance's time zone.

- Byname spends at most 200 inspections per Pacific Time day, the day Google's quota follows. Google allows 2,000 per property. Pages past the cap wait for the next day's sync.
- URL Inspection reports only Google's latest crawl of a page, not each crawl. Byname inspects a waiting page after each daily sync, so the first crawl it records is within a day of the real one. If Google crawled twice between two inspections, Byname records the later crawl.
- URL Inspection reads what Google knows. It cannot ask Google to crawl or index a page.

The change's timeline on **Google search > Site changes** lists each inspection.

## How a change gets its result

Results are experimental. The method is new and has few real results behind it yet, so the app labels every result **Experimental**. Check a surprising result against the page's numbers on **Google search > Traffic** before acting on it.

Each change gets two results, counted in days from the day Google saw it:

1. The first result, after 17 days by default. It is provisional, and the page marks it so.
2. The final result, after 31 days by default. It settles whether the change helped and raises a **Change result** alert.

The `verdicts` job works out each result on or after its day, as days in the instance's time zone, and stores each one once. It also stores each change's before numbers, the page's totals over the final result's length ending the day before the merge, once Search Console has finalized those days.

`assess` in `internal/changes/verdict.go` defines the method. Each result is a difference-in-differences check. The change in the page's numbers, minus the change in the comparison pages' numbers over the same days, removes what moved every page, such as seasonality or a swing in demand.

### Windows

Search Console reports Pacific Time days, so Byname places merges and crawls on Pacific Time days. The length is 17 days for the first result and 31 for the final result, by default.

- The after window runs from the day after the first crawl for the result's length. It stops early at the latest day Search Console has finalized. Call its length `n` days.
- The before window is the `n` days that end the day before the merge.
- The days from the merge to the first crawl are in neither window. The page had changed, but Google had not seen it.

### Totals

Only days that Search Console marks final count. A day the page has no row for had no impressions. Byname totals the changed page and the comparison set over each window. The comparison set is every comparison page whose hold date is on or after the result's day, without the changed page itself.

- Clicks and appearances (impressions) are sums.
- Average position is the mean of the daily average positions, weighted by appearances. 1 is the top.
- Click rate (CTR) is clicks over appearances.

### Not enough data

A result's outcome is `insufficient_data`, not enough data, with the reason, when:

- the after window has fewer than 7 final days
- the stored Search Console history does not reach back to the start of the before window
- there are no active comparison pages
- the page, or the comparison set, had no appearances in the before window or in the after window

### Deltas

Each delta is the page's change minus the comparison set's change:

| Delta | Formula | Reads as |
|---|---|---|
| Average position | (page after - page before) - (comparison after - comparison before), in places | Negative means the page moved up |
| Click rate (CTR) | The same over click rate, in percentage points | Positive means the page gained clicks per appearance |
| Appearances (impressions) | (page after / page before) / (comparison after / comparison before), a ratio | Above 1 means the page gained on the comparison pages |

### Signals and outcome

Each delta that passes its threshold is a signal, for the better or for the worse:

| Metric | Improved at | Worsened at | Default threshold |
|---|---|---|---|
| Average position | -T places or less | +T places or more | 2.5 places |
| Click rate (CTR) | +T points or more | -T points or less | 0.8 percentage points |
| Appearances (impressions) | A ratio of T or more | A ratio of 1/T or less | 2.7 |

The outcome is `improved`, shown as helped, when at least one signal improved and none worsened, and `worsened`, shown as hurt, when at least one worsened and none improved. It is `no_change` when no signal fired, or when signals point both ways.

## Change the result days and thresholds

The days and thresholds above are defaults. An Admin changes them under **Settings > Site changes**. A key or MCP connection with `config:write` can change them with `PUT /v1/changes/settings` or the `update_change_settings` tool.

| Setting | Field | Default | Allowed |
|---|---|---|---|
| First result | `interim_days` | 17 | 7 to 90 days |
| Final result | `verdict_days` | 31 | 14 to 180 days, and more than `interim_days` |
| Average position | `position` | 2.5 | More than 0, at most 50 places |
| Click rate (CTR) | `ctr_points` | 0.8 | More than 0, at most 50 points |
| Appearances (impressions) | `impressions_ratio` | 2.7 | More than 1, at most 100 |

A change Google has already seen keeps the result days it was given. Every result worked out after a save uses the new thresholds.

## Comparison pages

Comparison pages are the baseline every result compares against. Each is a page you promise not to change until its hold date, the last day it counts. Pick pages with steady traffic that are like the pages you change. The API calls them control pages.

An Editor or Admin adds them on **Google search > Site changes**, under **Comparison pages**. A key or MCP connection with `config:write` can use `POST /v1/control-pages` or the `upsert_control_page` tool. Adding a URL that is already a comparison page changes its hold date and note.

A comparison page counts in a result while its hold date is on or after the result's day, in the instance's time zone. After that, the page shows as **Ended** and drops out of the comparison set. A changed page never counts as its own comparison. Byname does not check whether a comparison page changed during its hold. A comparison page you change early skews every result that uses it, and with no active comparison pages every result is `insufficient_data`.

## What Site changes shows

**Google search > Site changes** lists every change, the latest merge first, with its page, its pull request or manual log, the merge day, **Google saw it**, the **First result** and **Final result**, and its status. Opening a change shows its before numbers, both results against the comparison pages, and its timeline of the merge, each inspection, the day Google saw it, and the result days. An alert about a change links to `/google/site-changes?chg=<id>`, which opens that change. **Google search > Traffic** marks each merged pull request and each change logged by hand on its daily chart.

Google data arrives about three days late, and one page's appearances swing with demand as much as with rank. Read a result as evidence about one page, not as proof.

## Routes and MCP tools

Each REST route has an MCP tool with the same scope, except the three that manage the repository connection. Only a signed-in Admin can call those, so no key or MCP connection ever sets or reads the token.

| Route | MCP tool | Needs | Does |
|---|---|---|---|
| `GET /v1/changes` | `list_changes` | `results:read` | Lists every change with its results |
| `GET /v1/changes/{id}` | `get_change` | `results:read` | Returns one change with its timeline and inspections |
| `POST /v1/changes` | `log_change` | `config:write` | Logs a change by hand |
| `DELETE /v1/changes/{id}` | `delete_change` | `config:write` | Deletes a change logged by hand |
| `GET /v1/changes/markers` | `change_markers` | `results:read` | Lists one chart marker per merge between `from` and `to` |
| `GET /v1/changes/settings` | `change_settings` | `results:read` | Returns the result days and thresholds |
| `PUT /v1/changes/settings` | `update_change_settings` | `config:write` | Replaces the result days and thresholds |
| `GET /v1/control-pages` | `list_control_pages` | `results:read` | Lists the comparison pages with their hold dates |
| `POST /v1/control-pages` | `upsert_control_page` | `config:write` | Adds a comparison page, or updates the one with the same URL |
| `DELETE /v1/control-pages/{id}` | `delete_control_page` | `config:write` | Removes a comparison page |
| `GET /v1/repo` | `repo_status` | `results:read` | Returns the repository, the token's expiry, the path rules and the last poll, never the token |
| `PUT /v1/repo/rules` | `update_path_rules` | `config:write` | Replaces the path rules |
| `PUT /v1/repo` | None | A signed-in Admin, signed in within 15 minutes | Connects a repository with a token |
| `POST /v1/repo/test` | None | A signed-in Admin | Calls GitHub with the stored token |
| `DELETE /v1/repo` | None | A signed-in Admin | Deletes the token and stops the poll |

`GET /v1/openapi.yaml` lists each route's fields. [Connect agents](agents.md) covers keys and MCP sign-in.
