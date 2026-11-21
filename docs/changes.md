# Changes

The **Changes** page answers one question: did a change you shipped to your site move anything in Google search once Google recrawled the page? Byname logs each merged change to your site, waits for Google's first crawl of the changed page, and then compares the page's search numbers before and after against control pages you leave alone.

Changes is optional. Without a connected repository you can still add changes by hand.

## Connect your site's repository

Byname reads merged pull requests from one GitHub repository, the one your site is built from.

1. On GitHub, create a fine-grained personal access token with read-only access to that one repository: **Contents** and **Pull requests**, both **Read-only**. Give it an expiry date.
2. In Byname, an Admin opens **Settings** and pastes the repository, as `owner/name`, and the token. Byname encrypts the token with `ENCRYPTION_KEY`, never returns it, and shows its expiry date.
3. Add path rules that map changed files to the URLs they build, for example `content/blog/*.md` to `https://www.example.com/blog/*`. A merged pull request that touches no mapped file is ignored.

Byname polls the repository once an hour, inside the container, so a merge shows up within the hour. Polling needs only the token: there is no webhook secret to manage and no public endpoint to protect. Before the token expires, Byname raises a **Token expiring** alert, which only Admins see.

## How a change gets its verdict

Each change records its page, its kind, the pull request, and the merge time. Then:

1. **Recrawl watch.** After each daily Search Console sync, Byname asks Google's URL Inspection API about each changed page that has not been recrawled yet. It records the first crawl after the merge. URL Inspection can read a page's coverage and last crawl. It cannot ask Google to index a page.
2. **Interim read.** 17 days after the first crawl, Byname compares the page's Search Console numbers since the crawl with the period before the merge.
3. **Verdict.** 31 days after the first crawl, it makes the same comparison and records the verdict.

Both reads use a difference-in-differences check: the change in the changed page's numbers, minus the change in the control pages' numbers over the same periods. Seasonality and demand swings move the control pages too, so they cancel out. A change counts as moved when it passes one of these thresholds:

| Measure | Default threshold |
|---|---|
| Average position | 2.5 positions |
| CTR | 0.8 percentage points |
| Impressions | A ratio of 2.7 |

The interim and verdict days and the thresholds are defaults, editable per instance under **Settings**. Each read raises a **Change verdict** alert to the channels subscribed to it. See [Notifications](notifications.md).

## Control pages

Control pages are pages you promise not to change for a while, each with a hold date. They are the baseline every verdict compares against, so pick pages with steady traffic that are like the pages you change. A control page counts until its hold date. Changing a control page early, or having no control pages, weakens every verdict that uses it.

## What the page shows

The **Changes** page lists every change with its page, merge date, Google's first crawl after the merge, the interim and verdict dates, the numbers from before the merge, and the verdict once it is in. Control pages are listed with their hold dates.

Search Console data arrives about three days late, and one page's impressions swing with demand as much as with rank. Read a verdict as evidence about one page, not as proof. [Search Console](search-console.md) explains the sync these reads depend on.
