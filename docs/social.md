# Social

The **Social** page shows the public posts that name you on X, Reddit, Hacker News, LinkedIn, YouTube, Threads and Bluesky. You name the people and accounts to leave out, such as your team and your company's own accounts, so the page and the digest show what everyone else says. A digest of each day or week shows under the bell and goes wherever you send notifications.

Six networks are searched through Treg with your Treg token. Bluesky is searched through its own public API, which needs no key and costs nothing. Without a Treg token, Byname searches Bluesky only.

Social is off until someone turns it on, in onboarding's optional **Social mentions** step or under **Settings > Social**, because the Treg searches cost money. The onboarding step shows what a month costs at the default settings, and, once switched on, the **People to leave out** form with **Our accounts** as the first label. Saving the Treg token starts nothing, and nothing is searched before onboarding is finished, even with social on. An instance upgraded from a version before the switch, which had already searched, keeps social on.

`internal/social` holds all of it, and this page follows that package.

## Where each network's posts come from

Treg has no single search across social networks, so Byname pins one search per network, the same way it pins one endpoint per AI app.

| Network | Searched through | Endpoint | Price per 1,000 searches | What a mention is |
|---|---|---|---|---|
| X | Treg | `anyapi.x.search.posts` | $0.75 | A post. Retweets are left out. |
| Reddit | Treg | `crawl4ai.reddit.search.posts` | $0.90 | A post, title and text. |
| Hacker News | Treg | `crawl4ai.hackernews.search.stories` | $0.15 | A story. Comments are not searched. |
| LinkedIn | Treg | `anyapi.linkedin.search.posts` | $2.00 | A post. |
| YouTube | Treg | `crawl4ai.youtube.search.videos` | $0.90 | A video whose title or description names you. The channel is the author. |
| Threads | Treg | `tikhub.x.threads-web-search-recent` | $2.00 | A post. |
| Bluesky | Bluesky's public API | `app.bsky.feed.searchPosts` on `https://api.bsky.app` | Free | A post. |

The prices are the Treg catalog's, checked on November 22, 2026. Every Treg call sends `X-Treg-Route-Max-Cost`, a ceiling a few times the listed price, so Treg refuses a dearer fallback source instead of charging for it. What a call cost is read from Treg's `X-Treg-Cost-Micro` header. A Treg call without that header is recorded as a missing cost, never as $0. A Bluesky call is recorded as a known $0, which is not a missing cost.

A network's search matches loosely, on stems, similar words and parts of names, so it returns posts that never say what you searched for. Byname keeps a post only when its text or its link names the term as a whole word, in any case: for the term Unkey, "unkey", "@unkey", "#unkey" and "unkey.com" count, and "monkey" or a post that never says Unkey does not.

Each network's answer is parsed into one shape: the post, its link, the author's handle and name, the text, when it was posted, and the engagement the network reports. A count the network does not report is empty, not 0. YouTube search gives only a video's age, such as "2 years ago", so its posting time is worked out from the time of the search and marked approximate. YouTube posts are ranked by views. Posts on the other networks are ranked by likes, replies, reposts and score added up.

## Set it up

Anyone can read **Settings > Social**. An Editor or Admin can change it.

- **Search social networks.** The switch. Off by default. With it off, the `social_fetch` job calls nothing at all.
- **Search terms.** With no terms, Byname searches for your brand's name and primary domain from **Settings > General**. You can set up to 5 terms instead. Use the distinctive form, such as your full product name or your domain. A common word on its own matches posts about something else.
- **Networks.** Each network can be turned on or off. All seven start on.
- **Search every.** 1, 3, 6, 12 or 24 hours. The default is 6.
- **Digest.** Daily or weekly, and for weekly the day the week starts. The default is weekly, starting Monday. Days follow the instance's time zone.

The Treg token is set by an Admin under **Settings > Treg**. Treg is the service that runs each question and bills you per answer. See [Treg](engines/treg.md).

## Leave people out

**People to leave out** lists the accounts whose posts Byname hides, grouped by a label such as a person's name or "Our accounts". To give one person handles on several networks, use the same label for each.

1. Under **Leave out an account**, type who it is, pick the network, and give the handle or paste a link to the profile.
2. Click **Leave out**.

Byname stores the handle in lowercase, without `@` and without Reddit's `u/`. A profile link works for every network, such as `https://x.com/name`, `https://www.reddit.com/user/name`, `https://www.linkedin.com/in/name` or `https://bsky.app/profile/name.bsky.social`. A LinkedIn author is known by the slug in their profile link. YouTube search shows only a channel's name, so a YouTube channel is matched by that name with the spaces taken out. A YouTube `@handle` or channel id matches only when it is the same as that name.

Leaving out hides, it does not delete. Byname stores every post it finds and hides the left-out ones each time it reads, so a new entry hides that account's past posts too, and removing an entry brings them back. The **Social** page has a **Show N hidden** toggle that shows them, marked with their label.

## Filter by topic and subreddit

Your name can mean something else to other people, in another place or another language, and those posts are not about you. **Topics and subreddits** under **Settings > Social** hides them, or keeps the page to what is.

- **Leave out a topic.** Posts that mention it are hidden. A topic is a word or phrase, matched as a whole word in any case: `india` matches "India", "#india" and "India," but not "Indiana".
- **Show only these topics.** Posts that mention none of them are hidden, on every network.
- **Leave out a subreddit.** Reddit posts in it are hidden. Give its name, `r/name` or a link to it.
- **Show only these subreddits.** Reddit posts in other subreddits are hidden. Posts on other networks are not affected.

Setting a topic or subreddit that is already set changes what it does. On the **Social** page, **Hide posts like this** under a post leaves out a topic from it, with the words you selected in the post filled in, or the post's subreddit.

Filters hide like the left-out list: at read time, past posts included, and removing a filter brings its posts back. A hidden post shows under **Show N hidden** marked with why, such as **Filtered: topic india** or **Filtered: r/india**. The digest leaves them out of its counts.

## The search

The scheduler runs the search as the `social_fetch` job, inside the container, every few hours as set. It searches each network that is on for each term, newest first.

- X, LinkedIn, Threads and Bluesky answer in pages. Byname follows the pages back until it reaches the newest post it already has for that network and term, at most 3 pages a term on X, LinkedIn and Bluesky and 2 on Threads.
- Reddit, Hacker News and YouTube answer with their latest posts in one call, 100 on Reddit and Hacker News and 20 on YouTube.
- Each post is stored once, keyed by its network and the network's own post id. Finding a post again updates its engagement and keeps when it was first seen.
- Each call and the posts it found are stored together, so a search cut off midway keeps the calls it finished, and the next search goes on from the newest post stored.

Social searches share the monthly spend cap under **Settings > Schedule and limits** with AI checks. Before each Treg call Byname adds up this month's checks and social searches. Once the cap is reached, the Treg networks are skipped and the job ends as `capped`. Bluesky still runs, since it costs nothing. If Treg refuses a call, for example for an empty balance, the Treg networks are skipped and the job ends as `refused`. A provider error on one network is recorded on that network, and the other networks carry on.

Bluesky can start asking for sign-in at any time. When it answers `401` or `403`, Byname marks Bluesky **Unavailable** on **Settings > Social** and the **Social** page, skips it for the rest of that search, and notes it on the job. It tries again at the next search. The posts found so far stay.

## The digest

When a day or week ends, Byname raises the **Social digest** notification, once per period. It covers the posts first seen in the period, without the accounts, topics and subreddits left out at that moment. In the app it is a card with the count, each network's share, how many people posted about you for the first time and the top three posts. By email or in Slack it reads as a few sentences, then the top five posts, for example:

> 12 new posts named you, Nov 16 to 22
>
> 5 were on X, 4 on Reddit and 3 on Hacker News.
> 9 people posted about you for the first time, among them @dana, u/ledgerfan and @lee.
> 10 posts you left out are not counted.
> The posts people engaged with most:
> u/ledgerfan on Reddit, 120 upvotes and comments: “Northwind is the one” https://reddit.com/r/invoicing/1
> @dana on X, 40 likes, replies and reposts: “Northwind sorted our invoices” https://x.com/dana/status/1
>
> See all posts on Social: https://byname.example.com/social?digest=2026-11-16
> Every notification: https://byname.example.com/notifications

The digest leaves out what the searches cost. **Settings > Schedule and limits** shows spend.

The event's payload carries the same facts as numbers: the new mentions and how many more came from left-out accounts, the count per network with each network's most engaged posts, the new people, the accounts whose first mention fell in the period, and what the period's searches cost, with how many Treg calls reported no cost.

A period in which nothing was searched raises no digest. The digest goes wherever **Settings > Notifications** sends **Social digest**, and shows under the bell to anyone with `results:read`. See [Notifications](notifications.md). The top of the **Social** page shows the latest period that has ended, or the period a digest links to, with **Latest digest** to go back.

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
| `GET /v1/social/filters` | `list_social_filters` | `results:read` |
| `POST /v1/social/filters` | `add_social_filter` | `config:write` |
| `DELETE /v1/social/filters/{id}` | `remove_social_filter` | `config:write` |

`PUT /v1/social/settings` takes `enabled` to turn social on or off. Leave it out to keep the current setting. `next_fetch_at` is null while nothing will be searched: social is off, onboarding is not finished, there is no term, or no network that is on can run without a Treg token.

`GET /v1/social/mentions` takes `network`, `since` (a day or a time), `include_hidden=true` and `limit` (1 to 500, default 100). Each mention carries `left_out_as`, the label hiding its author, and `filtered_by`, why a filter hides it. `POST /v1/social/filters` takes `kind` (`topic` or `subreddit`), `mode` (`leave_out` or `only`) and `value`. `GET /v1/social/digest` takes `day`, any day in the period, and without it returns the latest period that has ended. See [Agents](agents.md) for keys and scopes.

## Known gaps

- Hacker News comments are not searched, only stories.
- The X search uses `-is:retweet` to leave retweets out. Byname has not checked against a live call that X's search reads it that way.
- Threads results follow the documented shape of Threads' web search. Treg's catalog example cuts every post, so Byname has not checked the parser against a real Threads answer.
- Sentiment and replying are not part of Byname.
