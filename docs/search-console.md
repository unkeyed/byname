# Search Console

Search Console is Google's free report of how often your site appears in Google results and how many people click. Byname puts its numbers on **Google search > Traffic**, on the same timeline as your AI answers: daily clicks, appearances (impressions), click rate (CTR, clicks divided by appearances) and average position (1 is the top), clicks from searches with your name against searches without it, top pages and searches, and progress toward goals. It reads Search Console straight from Google's API with read-only access. No third party sits in between, and Treg is not involved.

You connect it in onboarding's fifth step, **Search Console**, or later under **Settings > Search Console**. Both offer the same Google sign-in, service account form and property picker. Without a Google OAuth client, onboarding shows only the service account form, with the three steps to follow. Only an Admin can connect it, and connecting needs a sign-in within the last 15 minutes. In onboarding, someone who is not an Admin sees a note to skip the step. The step offers **Skip** until a property is picked, and **Continue** after. There are two ways in:

| Method | You need | Best when |
|---|---|---|
| Sign in with Google | `GSC_GOOGLE_CLIENT_ID` and `GSC_GOOGLE_CLIENT_SECRET` set on the instance | A person who can see the property signs in once |
| Service account | A Google Cloud service account added as a user on the property | You would rather not depend on one person's Google account |

Both feed the same tables, and you can switch later. Byname encrypts the credential, the Google refresh token or the service account key, with `ENCRYPTION_KEY` and never returns it.

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

Set both or neither. Byname refuses to start with only one. Without them, **Settings > Search Console** offers only the service account.

Google documents that an External app whose publishing status is **Testing** gets refresh tokens that expire after 7 days. When that happens, the sync fails and **Settings > Search Console** shows the error. Sign in again, publish the app, use an Internal app, or switch to a service account.

## Connect with Sign in with Google

1. Click **Sign in with Google**. Google shows the consent page. Byname asks for read-only access to Search Console with offline access, so Google returns a refresh token.
2. Sign in with an account that can see the property, and allow access. Google sends you back to the page where you started, onboarding's **Search Console** step or **Settings > Search Console**.
3. Pick the property. Byname lists every property the account can see, and selects the one that matches your primary domain.

Only the Admin who started the sign-in can finish it. If Google sends no refresh token, Byname says so: remove Byname's access at [myaccount.google.com/permissions](https://myaccount.google.com/permissions) and sign in again.

## Connect with a service account

1. In Google Cloud, under **IAM & Admin > Service accounts**, create a service account. It needs no roles in the project.
2. On the service account, under **Keys**, add a key of type **JSON** and download it.
3. In Search Console, open the property's **Settings > Users and permissions** and add the service account's `client_email` as a user with **Restricted** permission. Read access is all Byname needs.
4. In Byname, choose **Or use a service account**, paste the whole JSON key, and pick the property.

Byname accepts a key with `type` `service_account`, a `client_email` and an RSA `private_key`.

## Property kinds

| Kind | Looks like | Covers |
|---|---|---|
| Domain | `sc-domain:example.com` | Every protocol and subdomain of the domain |
| URL prefix | `https://www.example.com/` | Only URLs that start with the prefix |

Pick a domain property when you have one. It counts traffic to every subdomain, the same way a link to any of your domains counts as linking to your site.

## The sync

- The first sync starts within a minute of picking a property. With **Backfill** on, the default, it pulls every day Google keeps, about 16 months. With it off, it starts with the last 28 days.
- After that, Byname syncs once a day, inside the container. A failed sync retries an hour later. Picking another property or reconnecting syncs at once.
- Each sync pulls web search data per day four ways: site totals, by query, by page, and by query and page. It pages through 25,000 rows a request and commits 30 days at a time, so a long backfill that stops resumes where it left off.
- Google marks recent days as not yet final. Byname stores them as partial, using Google's `firstIncompleteDate`, and overwrites them on the next sync. A final day is never replaced by a partial one. Search Console data lags about three days.
- Two replicas never sync at once. The sync takes a Postgres advisory lock.
- After a sync that succeeds, Byname checks goals and 28-day clicks, and raises **Goal reached** and **Google clicks moved** notifications. [Notifications](notifications.md#events) says when each fires.
- After a sync that succeeds, Byname also asks URL Inspection about pages with site changes and works out the change results that are due. See [Site changes](changes.md#recrawl-watch).

**Settings > Search Console** shows the property, the last sync and its error. **Change the connection** deletes the stored credential and stops syncing. The numbers already synced stay on **Google search > Traffic**.

## Branded search terms

A search counts as one with your name when it contains one of your branded terms, ignoring case. Every other search is a search without your name, which the API calls non-brand. By default the terms are your brand's name and aliases, lowercased, and they follow the name and aliases until someone changes them.

You can change them in three places: **Google searches for your name** in onboarding's first step, **Brand and domain**, the same field under **Settings > General**, and **Branded terms** on the brand card of **Google search > Traffic**. Agents use `PUT /v1/settings/branded-terms` or the `update_branded_terms` tool. **Use name and aliases**, or saving an empty list, goes back to the default.

Google hides rare queries, which come back with no query text. Byname counts those clicks as anonymized, never as searches without your name, so `non_brand_clicks`, the clicks from searches without your name, is a floor, not an estimate.

## Goals

A goal is a target for one metric over a trailing window, reached by a due date:

| Field | Values |
|---|---|
| Metric | `clicks`, `impressions` (appearances) or `non_brand_clicks` (clicks from searches without your name) |
| Window | 28 or 90 days |
| Target | A whole number |
| Due date | A date, such as `2030-06-30` |

**Google search > Traffic** shows each goal's progress over its window, ending on the latest final day. Goals live in the database, so a new target needs no deploy. Editors and Admins add them on **Google search > Traffic**, or with `POST /v1/goals` and the `upsert_goal` MCP tool. Adding a goal with the same metric, window and due date as an existing one changes that goal's target.
