# Notifications

Byname tells people when something moves: a check finished and some answers started or stopped naming you, a check stopped, a spend cap was reached, a model was paused, a Google search goal was reached, Google clicks moved, a site change got its final result, a day or week of social posts ended, or the GitHub token for site changes is about to expire. Every notification shows in the app behind the bell, beside your name at the bottom of the sidebar or at the top right on a phone. The bell shows how many you have not read. Each notification is a short card that says what happened and links to the page with the details, and opening it marks it read. To also get notifications by email, in Slack or at your own endpoint, an Admin adds where they go under **Settings > Notifications**, or in onboarding's **Schedule and spend caps** step. With nowhere added, notifications stay in the app. The API calls a notification an alert, as in `GET /v1/alerts`.

## Where notifications go

| Where | Stored as | Settings | Secret |
|---|---|---|---|
| Email (SMTP) | `smtp` | Host, port, encryption (`starttls` on 587, `tls` on 465, or `none` for a local relay), username, from, and 1 to 20 recipients | The SMTP password |
| Email (Resend) | `email_api` | From, and 1 to 20 recipients | A Resend API key (`re_...`) |
| Slack | `slack_webhook` | None | The Slack incoming webhook URL |
| Webhook | `webhook` | The URL to post to | A signing secret that Byname makes |

For each place, you pick which notifications go there. Byname encrypts each place's secret with `ENCRYPTION_KEY` and never returns it, except the webhook signing secret, which it shows once when you add the webhook. Adding or editing a place needs a sign-in within the last 15 minutes. **Send test** sends a test notification there at once. The API calls each place a notification channel, as in `GET /v1/notification-channels`.

Webhook and Slack URLs must use `https`, or `http` to `localhost` for a receiver on the same machine. Email uses plain text, with the notification's title as the subject. It ends with a link to the page in Byname, such as **See what changed**, and a link to the notifications page, both built from `PUBLIC_URL`. Slack shows the same two links. Email, Slack and the webhook carry the same title and lines.

### Get a Slack webhook URL

Byname only posts to the URL, so the Slack app needs no bot token and no permissions beyond the `incoming-webhook` scope, which Slack adds for you.

1. At [api.slack.com/apps](https://api.slack.com/apps), click **Create New App**, then **From scratch**. Name it Byname and pick your workspace.
2. Under **Basic Information > Display Information**, upload [Byname's icon](https://byname.dev/slack-icon.png) (512 by 512 pixels).
3. Open **Incoming Webhooks**, turn them on, click **Add New Webhook to Workspace**, and pick the channel.
4. Copy the URL, which starts with `https://hooks.slack.com/services/`, and paste it into a Slack place in Byname.

If your workspace only lets admins install apps, Slack asks an admin to approve it first. Each webhook posts to one channel, so add another webhook and another Slack place for a second channel.

## Events

`internal/notify/events.go` is the one list of events. **Settings > Notifications** lists them in this order.

| Event | Kind | Raised when | Seen in the app by |
|---|---|---|---|
| Check completed | `check_completed` | A check finished. It says how many answers started naming you, stopped naming you or moved your position since the previous check, how many answers named you, the spend, and the questions that changed most. Its link opens **AI answers > What changed** for the two checks. | Anyone with `results:read` |
| Check refused | `check_refused` | Treg, the service that runs each question and bills you per answer, refused a request, for example for an empty balance, and the check stopped. | Anyone with `results:read` |
| Check failed | `check_failed` | A check could not start, or could not save its answers. | Anyone with `results:read` |
| Spend cap reached | `spend_cap_reached` | A check stopped asking questions when it reached the spend cap for one check or for the month. | Anyone with `results:read` |
| Model paused | `pair_paused` | An AI app stopped offering a model you picked, so Byname stopped asking it. | Anyone with `results:read` |
| Goal reached | `goal_reached` | After a Search Console sync, a Google search goal's progress over its window first meets its target. | Anyone with `results:read` |
| Google clicks moved | `search_moved` | After a Search Console sync, clicks over the last 28 days, or clicks from searches without your name, differ from the 28 days before by at least 25% of the earlier number and by at least 20 clicks. Both 28-day windows must have data for every day. | Anyone with `results:read` |
| Change result | `change_verdict` | A site change got its final result against your comparison pages, 31 days after Google first saw the change by default. The first result raises nothing. Its link opens the change on **Google search > Site changes**. See [Site changes](changes.md). | Anyone with `results:read` |
| Token expiring | `token_expiring` | The hourly GitHub poll, or **Test** under **Settings > Site changes**, finds that the token expires within 14 days. | Admins only |
| Social digest | `social_digest` | A day or week of social searches ended. It says how many new posts named you and on which networks, how many people posted about you for the first time with at most three names, and how many posts from left-out accounts are not counted, then quotes the five posts people engaged with most. It never says what the searches cost: see **Settings > Schedule and limits** for spend. Its link opens the **Social** page at that period. See [Social](social.md). | Anyone with `results:read` |
| Your page was linked | `page_linked` | A check's answers were the first to link to a page you marked as answering a missed question. It names the AI apps that linked to it and how many answers did. Its link opens **AI answers > Missed questions > Handled**. See [Handled questions](handled.md). | Anyone with `results:read` |

Each event has a natural key, its kind and what it happened to, such as one check. Raising the same event twice stores and sends it once. For the events below, the key decides how often each one fires:

| Event | Fires once per |
|---|---|
| `goal_reached` | Goal and target. Raising a goal's target lets it fire again. |
| `search_moved` | Metric and the last day of the 28-day window. While the move holds, the next day's sync raises it again. |
| `change_verdict` | Change. |
| `token_expiring` | Repository and token expiry date. A new token with a new expiry date warns again. |
| `social_digest` | Day or week, such as `week/2026-11-16`. A period in which nothing was searched raises none. |
| `page_linked` | Marked page. Removing the mark and marking the page again lets it fire again. |

## Delivery and retries

Byname sends from a background job, never inside a request. A delivery that fails is tried again after 30 seconds, 2 minutes, 8 minutes and 32 minutes, five attempts in all. Any `2xx` answer counts as delivered. Each attempt is logged with its status and error, and **Settings > Notifications** shows the log. The log never records a place's URL, because a Slack webhook URL is a secret.

## Receive a webhook

A webhook gets a `POST` to its URL with a JSON body:

```json
{
	"id": "alert-12/channel-3",
	"kind": "check_completed",
	"title": "Check #42: 3 answers started naming you, 1 answer stopped naming you",
	"lines": ["AI apps named you in 31 of 96 answers.", "Since check #41, 3 answers started naming you, 1 answer stopped naming you and 2 answers moved your position."],
	"url": "https://byname.example.com/ai/what-changed?before=41&after=42",
	"payload": {"run_id": 42, "previous_run_id": 41, "gained": 3, "lost": 1, "moved": 2, "net": 2, "named": 31, "answered": 96},
	"created_at": "2026-11-21T06:14:09Z"
}
```

`payload` is the event's own data, and its fields depend on `kind`. Its field names keep the API's words, such as `run_id` for the check and `gained` and `lost` for answers that started and stopped naming you. Its field names keep the API's words, such as `run_id` for the check and `gained` and `lost` for answers that started and stopped naming you. `id` is the same on every retry of one delivery, so use it to ignore a repeat. Each request carries these headers:

| Header | Value |
|---|---|
| `X-Byname-Event` | The event kind, such as `check_completed` |
| `X-Byname-Delivery` | The delivery id, the same as `id` |
| `X-Byname-Signature` | `t=<unix seconds>,v1=<hex HMAC-SHA256>` |
| `User-Agent` | `Byname` |

### Verify the signature

The signature is the hex HMAC-SHA256 of `<t>.<raw body>`, keyed with the webhook's signing secret, which starts with `whsec_`. The timestamp is signed too, so refuse a request whose `t` is more than a few minutes away from now, and a captured request cannot be replayed later. Check the raw body before you parse it.

In Node.js:

```js
import crypto from "node:crypto";

// header is the X-Byname-Signature value, body the raw request body as a string.
export function verifyByname(secret, header, body, toleranceSeconds = 300) {
	const parts = Object.fromEntries(header.split(",").map((p) => p.trim().split("=", 2)));
	const t = Number(parts.t);
	if (!Number.isInteger(t) || !parts.v1) return false;
	if (Math.abs(Date.now() / 1000 - t) > toleranceSeconds) return false;
	const expected = crypto.createHmac("sha256", secret).update(`${t}.${body}`).digest("hex");
	const given = Buffer.from(parts.v1, "hex");
	return given.length === 32 && crypto.timingSafeEqual(given, Buffer.from(expected, "hex"));
}
```

In Go:

```go
func verifyByname(secret, header string, body []byte, tolerance time.Duration) bool {
	var ts, sig string
	for _, part := range strings.Split(header, ",") {
		k, v, _ := strings.Cut(strings.TrimSpace(part), "=")
		switch k {
		case "t":
			ts = v
		case "v1":
			sig = v
		}
	}
	unix, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || sig == "" {
		return false
	}
	if d := time.Since(time.Unix(unix, 0)); d > tolerance || d < -tolerance {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts + "."))
	mac.Write(body)
	return hmac.Equal([]byte(sig), []byte(hex.EncodeToString(mac.Sum(nil))))
}
```

Answer with any `2xx` status once the request checks out. Anything else, or no answer within 30 seconds, counts as a failed attempt.
