# Social

The **Social** page shows the public posts that name you on X, Reddit, Hacker News, LinkedIn, YouTube, Threads and Bluesky. You name the people and accounts to leave out, such as your team and your company's own accounts, so the page and the digest show what everyone else says. A digest of each day or week goes to your notification channels.

Six networks are searched through Treg with your Treg token. Bluesky is searched through its own public API, which needs no key and costs nothing. Without a Treg token, Byname searches Bluesky only.

Social is off until someone turns it on, in onboarding's optional **Social mentions** step or under **Settings > Social**, because the Treg searches cost money. The onboarding step shows what a month costs at the default settings, and, once switched on, the **People to leave out** form with **Our accounts** as the first label. Saving the Treg token starts nothing, and nothing is searched before onboarding is finished, even with social on. An instance upgraded from a version before the switch, which had already searched, keeps social on.

`internal/social` holds all of it, and this page follows that package.

## Where each network's posts come from

Treg has no single search across social networks, so Byname pins one search per network, the same way it pins one endpoint per surface.

| Network | Searched through | Endpoint | Price per call | What a mention is |
|---|---|---|---|---|
| X | Treg | `anyapi.x.search.posts` | $0.00075 | A post. Retweets are left out. |
| Reddit | Treg | `crawl4ai.reddit.search.posts` | $0.0009 | A post, title and text. |
| Hacker News | Treg | `crawl4ai.hackernews.search.stories` | $0.00015 | A story. Comments are not searched. |
| LinkedIn | Treg | `anyapi.linkedin.search.posts` | $0.002 | A post. |
| YouTube | Treg | `crawl4ai.youtube.search.videos` | $0.0009 | A video whose title or description names you. The channel is the author. |
| Threads | Treg | `tikhub.x.threads-web-search-recent` | $0.002 | A post. |
| Bluesky | Bluesky's public API | `app.bsky.feed.searchPosts` on `https://api.bsky.app` | Free | A post. |

The prices are the Treg catalog's, checked on November 22, 2026. Every Treg call sends `X-Treg-Route-Max-Cost`, a ceiling a few times the listed price, so Treg refuses a dearer fallback source instead of charging for it. What a call cost is read from Treg's `X-Treg-Cost-Micro` header. A Treg call without that header is recorded as a missing cost, never as $0. A Bluesky call is recorded as a known $0, which is not a missing cost.

Each network's answer is parsed into one shape: the post, its link, the author's handle and name, the text, when it was posted, and the engagement the network reports. A count the network does not report is empty, not 0. YouTube search gives only a video's age, such as "2 years ago", so its posting time is worked out from the time of the search and marked approximate. YouTube posts are ranked by views. Posts on the other networks are ranked by likes, replies, reposts and score added up.

## Set it up

Anyone can read **Settings > Social**. An Editor or Admin can change it.

- **Search social networks.** The switch. Off by default. With it off, the `social_fetch` job calls nothing at all.
- **Search terms.** With no terms, Byname searches for your brand's name and primary domain from **Settings > General**. You can set up to 5 terms instead. Use the distinctive form, such as your full product name or your domain. A common word on its own matches posts about something else.
- **Networks.** Each network can be turned on or off. All seven start on.
- **Search every.** 1, 3, 6, 12 or 24 hours. The default is 6.
- **Digest.** Daily or weekly, and for weekly the day the week starts. The default is weekly, starting Monday. Days follow the instance's time zone.

The Treg token is set by an Admin under **Settings > Credentials**. See [Treg](engines/treg.md).

## Leave people out

**People to leave out** lists the accounts whose posts Byname hides, grouped by a label such as a person's name or "Our accounts". To give one person handles on several networks, use the same label for each.

1. Under **Leave out an account**, type who it is, pick the network, and give the handle or paste a link to the profile.
2. Click **Leave out**.

Byname stores the handle in lowercase, without `@` and without Reddit's `u/`. A profile link works for every network, such as `https://x.com/name`, `https://www.reddit.com/user/name`, `https://www.linkedin.com/in/name` or `https://bsky.app/profile/name.bsky.social`. A LinkedIn author is known by the slug in their profile link. YouTube search shows only a channel's name, so a YouTube channel is matched by that name with the spaces taken out. A YouTube `@handle` or channel id matches only when it is the same as that name.

Leaving out hides, it does not delete. Byname stores every post it finds and hides the left-out ones each time it reads, so a new entry hides that account's past posts too, and removing an entry brings them back. The **Social** page has a **Show N hidden from people you left out** toggle that shows them, marked with their label.

## The search

The scheduler runs the search as the `social_fetch` job, inside the container, every few hours as set. It searches each network that is on for each term, newest first.

- X, LinkedIn, Threads and Bluesky answer in pages. Byname follows the pages back until it reaches the newest post it already has for that network and term, at most 3 pages a term on X, LinkedIn and Bluesky and 2 on Threads.
- Reddit, Hacker News and YouTube answer with their latest posts in one call, 100 on Reddit and Hacker News and 20 on YouTube.
- Each post is stored once, keyed by its network and the network's own post id. Finding a post again updates its engagement and keeps when it was first seen.
- Each call and the posts it found are stored together, so a search cut off midway keeps the calls it finished, and the next search goes on from the newest post stored.

Social searches share the monthly spend cap under **Settings > Limits** with AI checks. Before each Treg call Byname adds up this month's checks and social searches. Once the cap is reached, the Treg networks are skipped and the job ends as `capped`. Bluesky still runs, since it costs nothing. If Treg refuses a call, for example for an empty balance, the Treg networks are skipped and the job ends as `refused`. A provider error on one network is recorded on that network, and the other networks carry on.

Bluesky can start asking for sign-in at any time. When it answers `401` or `403`, Byname marks Bluesky **Unavailable** on **Settings > Social** and the **Social** page, skips it for the rest of that search, and notes it on the job. It tries again at the next search. The posts found so far stay.

## The digest

When a day or week ends, Byname raises the **Social digest** event, once per period. It covers the posts first seen in the period, without the accounts left out at that moment, and has:

- the number of new mentions, and how many more came from left-out accounts,
- the count per network, with each network's most engaged posts,
- the new voices, the accounts whose first mention fell in the period,
- what the period's searches cost, and how many Treg calls reported no cost.

A period in which nothing was searched raises no digest. The digest goes to every channel subscribed to **Social digest** and shows in the app to anyone with `results:read`. See [Notifications](notifications.md). The top of the **Social** page shows the latest period that has ended.

The digest is data only. Byname does not write a summary of it with a model.

## REST and MCP

| REST | MCP tool | Scope |
|---|---|---|
| `GET /v1/social/mentions` | `list_social_mentions` | `results:read` |
| `GET /v1/social/digest` | `get_social_digest` | `results:read` |
| `GET /v1/social/settings` | `get_social_settings` | `results:read` |
| `PUT /v1/social/settings` | `update_social_settings` | `config:write` |
| `GET /v1/social/left-out` | `list_left_out_accounts` | `results:read` |
| `POST /v1/social/left-out` | `leave_out_account` | `config:write` |
| `DELETE /v1/social/left-out/{id}` | `remove_left_out_account` | `config:write` |

`PUT /v1/social/settings` takes `enabled` to turn social on or off. Leave it out to keep the current setting. `next_fetch_at` is null while nothing will be searched: social is off, onboarding is not finished, there is no term, or no network that is on can run without a Treg token.

`GET /v1/social/mentions` takes `network`, `since` (a day or a time), `include_hidden=true` and `limit` (1 to 500, default 100). `GET /v1/social/digest` takes `day`, any day in the period, and without it returns the latest period that has ended. See [Agents](agents.md) for keys and scopes.

## Known gaps

- Hacker News comments are not searched, only stories.
- The X search uses `-is:retweet` to leave retweets out. Byname has not checked against a live call that X's search reads it that way.
- Threads results follow the documented shape of Threads' web search. Treg's catalog example cuts every post, so Byname has not checked the parser against a real Threads answer.
- Sentiment and replying are not part of Byname.
