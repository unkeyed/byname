# Search engines

Byname reads how often your site appears in Google and Bing results and how many people click, from each engine's own webmaster tools, Google Search Console and Bing Webmaster Tools. It puts each engine's numbers on **Search > Traffic**, on the same timeline as your AI answers: daily clicks, appearances (impressions), click rate (CTR, clicks divided by appearances) and average position (1 is the top), clicks from searches with your name against searches without it, top pages and searches, and progress toward goals. Byname calls each engine's API directly and only reads. No third party sits in between, and Treg is not involved.

Connect one engine or both. Once two or more engines are connected or have data, **Search > Traffic** shows a **Search engine** switch with **Google** and **Bing**. Byname never adds Google and Bing together, because their days and what they count differ. [How Google and Bing differ](#how-google-and-bing-differ) lists each difference. **This week** shows a **Google clicks** card and a **Bing clicks** card, one for each engine with data. Until an engine is connected, it shows Admins **Connect a search engine** under **Finish setting up**.

Site changes, URL Inspection and Sign in with Google work with Google only. See [Site changes](changes.md).

`internal/search` holds the connections, the sync and the numbers. Each engine is one adapter file in it, `google.go` or `bing.go`, and everything else is shared.

## Connect an engine

You connect engines in onboarding's fifth step, **Search engines**, or later under **Settings > Search engines**, which has one card per engine. Both offer the same forms and site picker. Only an Admin can connect an engine, and connecting needs a sign-in within the last 15 minutes. In onboarding, someone who is not an Admin sees a note to skip the step. The step is optional. It offers **Skip** until any engine has a site picked, and **Continue** after.

| Engine | Method | You need | Best when |
|---|---|---|---|
| Google | Sign in with Google | `GSC_GOOGLE_CLIENT_ID` and `GSC_GOOGLE_CLIENT_SECRET` set on the instance | A person who can see the property signs in once |
| Google | Service account | A Google Cloud service account added as a user on the property | You would rather not depend on one person's Google account |
| Bing | API key | The API key of a Bing Webmaster Tools account that has verified the site | It is the only way Byname connects Bing |

Google's two methods feed the same tables, and you can switch later. Without a Google OAuth client, the Google card shows only the service account form, with the three steps to follow.

Byname encrypts each credential, the Google refresh token, the service account key or the Bing API key, with `ENCRYPTION_KEY` and never returns it.

A Search Console connection made before Bing joined stays connected after an upgrade, as Google's, with its numbers and goals. See [Upgrade](self-hosting.md#upgrade).

## Create the Google OAuth client

Each Byname instance uses its own Google OAuth client, which you create once in Google Cloud. It is separate from the client in `GOOGLE_CLIENT_ID` that signs people in, because it asks for a different scope. One Google Cloud project can hold both.

1. In the [Google Cloud console](https://console.cloud.google.com), pick or create a project.
2. Under **APIs & Services > Library**, enable the **Google Search Console API**.
3. Under **APIs & Services > OAuth consent screen**, set up the consent screen. Choose **Internal** if everyone who connects Search Console is in your Google Workspace. Otherwise choose **External** and add the people who will connect as test users. Add the scope `https://www.googleapis.com/auth/webmasters.readonly`.
4. Under **APIs & Services > Credentials**, click **Create credentials > OAuth client ID**, with the type **Web application**.
5. Add the authorized redirect URI `PUBLIC_URL/auth/gsc/callback`, for example `https://byname.example.com/auth/gsc/callback`.
6. Copy the client ID and secret into the instance's environment and restart:

   ```sh
   GSC_GOOGLE_CLIENT_ID=1234-abc.apps.googleusercontent.com
   GSC_GOOGLE_CLIENT_SECRET=your-client-secret
   ```

Set both or neither. Byname refuses to start with only one. Without them, the Google card under **Settings > Search engines** offers only the service account.

Google documents that an External app whose publishing status is **Testing** gets refresh tokens that expire after 7 days. When that happens, the Google sync fails and **Settings > Search engines** shows the error. Sign in again, publish the app, use an Internal app, or switch to a service account.

## Connect Google with Sign in with Google

1. On the Google card, click **Sign in with Google**. Google shows the consent page. Byname asks for read-only access to Search Console with offline access, so Google returns a refresh token.
2. Sign in with an account that can see the property, and allow access. Google sends you back to the page where you started, onboarding's **Search engines** step or **Settings > Search engines**.
3. Pick the property. Byname lists every property the account can see, and selects the one that matches your primary domain.

Only the Admin who started the sign-in can finish it. If Google sends no refresh token, Byname says so. Remove Byname's access at [myaccount.google.com/permissions](https://myaccount.google.com/permissions) and sign in again.

## Connect Google with a service account

1. In the [Google Cloud console](https://console.cloud.google.com), pick or create a project, and under **APIs & Services > Library**, enable the **Google Search Console API**. Without it, every sync fails with "API has not been used in project".
2. Under **IAM & Admin > Service accounts**, create a service account. It needs no roles in the project.
3. On the service account, under **Keys**, add a key of type **JSON** and download it.
4. In Search Console, open the property's **Settings > Users and permissions** and add the service account's `client_email` as a user with **Restricted** permission. Read access is all Byname needs.
5. In Byname, on the Google card, paste the whole JSON key under **Or use a service account**, or **Connect with a service account** when the instance has no Google OAuth client, and pick the property. Then delete the downloaded file, or keep it only in a password manager. Byname stores its own encrypted copy.

If Google says **Service account key creation is disabled**, your Google Cloud organization enforces the `iam.disableServiceAccountKeyCreation` policy. Use [Sign in with Google](#connect-google-with-sign-in-with-google) instead, with an **Internal** consent screen so the sign-in does not expire, or ask an organization policy admin to allow keys for this one project.

Byname accepts a key with `type` `service_account`, a `client_email` and an RSA `private_key`.

## Google property kinds

| Kind | Looks like | Covers |
|---|---|---|
| Domain | `sc-domain:example.com` | Every protocol and subdomain of the domain |
| URL prefix | `https://www.example.com/` | Only URLs that start with the prefix |

Pick a domain property when you have one. It counts traffic to every subdomain, the same way a link to any of your domains counts as linking to your site.

## Connect Bing with an API key

One Bing Webmaster Tools API key reads every site its account has verified. Bing also offers OAuth, but Byname connects with the API key only.

1. In [Bing Webmaster Tools](https://www.bing.com/webmasters), open **Settings**, then **API access**.
2. Generate an API key, or copy the one you have.
3. In Byname, on the Bing card in onboarding's **Search engines** step or under **Settings > Search engines**, paste the key under **Paste your API key**. Byname asks Bing for the account's sites before it saves the key, so a key Bing refuses is never stored.
4. Pick the site. Byname lists only the sites the account has verified, and selects the one that matches your primary domain.

Byname never shows the key again. Bing takes the key in the query string of every call, so Byname keeps the key out of its logs and of every error message.

## The daily sync

Byname syncs each connected engine once a day, inside the container, as the `search_sync` job. The jobs log names the engine of each sync.

- The first sync starts within a minute of picking a site. Picking another site or reconnecting syncs at once.
- A failed sync retries an hour later. A failure on one engine never holds up the other, and each engine retries on its own.
- Two replicas never sync at once. The sync takes a Postgres advisory lock that every engine shares.
- After a sync that succeeds, Byname checks that engine's goals and its clicks over the last 28 days, and raises **Goal reached** and **Search clicks moved** notifications for that engine. [Notifications](notifications.md#events) says when each fires.
- After a Google sync that succeeds, Byname also asks URL Inspection about pages with site changes and works out the change results that are due. See [Site changes](changes.md#recrawl-watch).

**Settings > Search engines** shows each engine's site, its last sync and the sync's error. **Disconnect** deletes that engine's stored credential and stops its sync. The numbers already synced stay on **Search > Traffic**.

### Google

- With **Backfill history** on, the default, the first sync pulls every day Google keeps, about 16 months. With it off, it starts with the last 28 days. Later syncs start at the first day not stored as final.
- Each sync pulls web search data per day three ways: site totals, by search and by page. It pages through 25,000 rows a request and commits 30 days at a time, so a long backfill that stops resumes where it left off.
- Google marks recent days as not yet final. Byname stores them as partial, using Google's `firstIncompleteDate`, and overwrites them on the next sync. A final day is never replaced by a partial one. Search Console data lags about three days.

### Bing

- Each sync makes three calls: the weekly top searches (`GetQueryStats`), the weekly top pages (`GetPageStats`), then the daily site totals (`GetRankAndTrafficStats`).
- No Bing method takes dates, so every sync reads all that Bing keeps, about six months through the API, and upserts it. A week that ages out of Bing stays stored in Byname, so your Bing history grows past six months. Bing has no **Backfill history** setting.
- Bing's newest day and newest week are still counting. Byname stores them as partial, and the next sync replaces them. Older days and weeks are final.

## How Google and Bing differ

| | Google | Bing |
|---|---|---|
| Days | Pacific Time days, as Google counts them | UTC days, as Bing counts them |
| Site totals | Per day, web search only | Per day, every kind of Bing result: web, chat (Copilot), news, images and videos |
| Searches and pages | Per day, every search and page Google lists | Per week, Bing's top searches and top pages of that week |
| Unlisted clicks | Clicks on rare searches Google leaves out to keep searchers anonymous | Clicks on searches outside Bing's weekly top lists |
| Average position | For the whole site, each search and each page | For each search and each page, Bing's average impression position. None for the whole site |
| Windows | Any number of days, ending on the latest final day | Whole weeks, ending on the last day of the latest final week |
| History | Backfill reaches about 16 months | About six months in the API, and Byname keeps every week it has copied |

Because their days and what they count differ, Byname shows one engine at a time and never sums the two. Each engine has its own goals and its own notifications.

Bing gives no average position for the whole site, so **Search > Traffic** shows no average position tile for Bing. In the API, Bing's site `position` is `0`.

### Bing's weeks

Bing reports searches and pages a week at a time, and dates each week on a Friday. Each week lists only that week's top searches and top pages. Bing does not document which seven days a week holds. Byname stores each weekly row as the seven days that end on its date.

A Bing window always holds whole weeks, so no week is ever split. Byname rounds the days asked for to the nearest whole number of weeks, at least one:

- 28 days stays 28 days, 4 weeks.
- A 90-day goal counts 91 days, 13 weeks.
- Top lists over fewer than 7 days show one week.

Every Bing window ends on the last day of Bing's latest final week. The summary, the top lists and each goal's progress give the window's real `from` and `to`.

## When a sync fails

**Settings > Search engines** shows why the last sync failed, in words that say what to do.

Bing answers every call it refuses with an error code, throttling included. Byname turns the code into a message, for example:

| The message says | Do this |
|---|---|
| Bing refused the API key | Generate a new key under **Settings**, then **API access**, in Bing Webmaster Tools, and paste it under **Settings > Search engines**. |
| Bing is throttling | Nothing. Byname tries again in an hour. |
| Bing does not know this site under this API key | Pick one of the key's sites under **Settings > Search engines**. |
| The account this API key belongs to may not read this site | Verify the site in Bing Webmaster Tools. |

Google's errors come through with Google's own message, which says what to fix, such as a property the account cannot read. If Google refuses Byname's credential, an Admin reconnects Google under **Settings > Search engines**.

## Branded search terms

A search counts as one with your name when it contains one of your branded terms, ignoring case. Every other listed search is a search without your name, which the API calls non-brand. The same terms apply to every engine. By default the terms are your brand's name and aliases, lowercased, and they follow the name and aliases until someone changes them.

You can change them in three places: **Searches for your name** in onboarding's first step, **Brand and domain**, the same field under **Settings > General**, and **Branded terms** on the brand card of **Search > Traffic**. Agents use `PUT /v1/settings/branded-terms` or the `update_branded_terms` tool. Resetting the terms in any of the three places, or saving an empty list, goes back to the default.

Some clicks in an engine's site totals belong to no search the engine lists. Byname counts those as unlisted clicks, `unlisted_clicks`, never as clicks from searches without your name. For Google, they are the rare searches Google leaves out to keep searchers anonymous. For Bing, they are the searches outside its weekly top lists. So `non_brand_clicks`, the clicks from searches without your name, is a floor, not an estimate.

## Goals

A goal is a target for one metric on one engine over a trailing window, reached by a due date:

| Field | Values |
|---|---|
| Engine | `google` or `bing`, the API's `provider` |
| Metric | `clicks`, `impressions` (appearances) or `non_brand_clicks` (clicks from searches without your name) |
| Window | 28 or 90 days. A Bing window is whole weeks, so a 90-day Bing goal counts 91 days. |
| Target | A whole number |
| Due date | A date, such as `2030-06-30` |

**Search > Traffic** shows each goal's progress over its window, ending where that engine's windows end. Goals live in the database, so a new target needs no deploy. Editors and Admins add them on **Search > Traffic**, or with `POST /v1/goals` and the `upsert_goal` MCP tool, which require `provider`. Adding a goal with the same engine, metric, window and due date as an existing one changes that goal's target.

**Goal reached** and **Search clicks moved** fire for each engine on its own. Their links open **Search > Traffic** with that engine selected.

## Routes and MCP tools

`{provider}` is `google` or `bing`. Every read names one engine, and no route or tool adds two engines together. Each REST route that a key can call has an MCP tool with the same scope. The routes that connect an engine have none. Only a signed-in Admin can call those, so no key or MCP connection ever sets or reads a credential.

| Route | MCP tool | Needs | Does |
|---|---|---|---|
| `GET /v1/search/providers` | `search_status` | `results:read` | Lists every engine with its connection, the site synced, the last sync and its error, the latest final day, the partial days, `step_days` (7 for Bing) and notes on how its numbers differ. Never the credential. |
| `GET /v1/search/{provider}/summary` | `search_summary` | `results:read` | Returns the latest 28 days and the 28 days before, with clicks from searches with your name, without it and unlisted |
| `GET /v1/search/{provider}/daily` | `search_daily` | `results:read` | Returns the site's numbers per day, partial days marked `final` false |
| `GET /v1/search/{provider}/brand` | `search_brand_split` | `results:read` | Returns brand against non-brand clicks over trailing 28-day windows, one a day for Google and one a week for Bing |
| `GET /v1/search/{provider}/pages` | `top_pages` | `results:read` | Lists the top pages by clicks |
| `GET /v1/search/{provider}/queries` | `top_queries` | `results:read` | Lists the top searches by clicks, each marked branded or not |
| `GET /v1/goals` | `list_goals` | `results:read` | Lists every engine's goals by due date with their progress |
| `POST /v1/goals` | `upsert_goal` | `config:write` | Adds a goal, or sets the target of the goal with the same provider, metric, window and due date |
| `PUT /v1/goals/{id}` | `update_goal` | `config:write` | Replaces a goal |
| `DELETE /v1/goals/{id}` | `delete_goal` | `config:write` | Deletes a goal |
| `PUT /v1/settings/branded-terms` | `update_branded_terms` | `config:write` | Replaces the branded search terms for every engine |
| `GET /v1/search/{provider}/sites` | None | A signed-in Admin | Lists the sites the connected account can read, with the one for your primary domain marked `suggested` |
| `PUT /v1/search/{provider}/site` | None | A signed-in Admin | Picks the site to sync, and for Google whether to backfill |
| `PUT /v1/search/{provider}/key` | None | A signed-in Admin, signed in within 15 minutes | Connects with a pasted key, Google's service account JSON key or Bing's API key |
| `DELETE /v1/search/{provider}` | None | A signed-in Admin | Deletes the connection and its credential. The synced numbers stay. |
| `POST /v1/search/google/oauth/start` | None | A signed-in Admin, signed in within 15 minutes | Returns the URL of Google's consent page |
| `GET /auth/gsc/callback` | None | The Admin who started the sign-in | Where Google sends the browser back. The path stays the redirect URI registered on each Google OAuth client. |

`GET /v1/openapi.yaml` lists each route's fields. [Connect agents](agents.md) covers keys and MCP sign-in.
