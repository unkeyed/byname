# Notifications

Byname tells people when something moves: a check finished with gains and losses, a check stopped, a spend cap was reached, a model was paused, a search goal was reached, search clicks moved, a site change got its verdict, or the GitHub token for site changes is about to expire. Every event shows in the app under **Alerts**. To also get it by email, in Slack or at your own endpoint, an Admin adds notification channels under **Settings > Notifications**, or in onboarding's **Schedule, limits and alerts** step. With no channel, alerts stay in the app.

## Channels

| Kind | Stored as | Settings | Secret |
|---|---|---|---|
| Email (SMTP) | `smtp` | Host, port, encryption (`starttls` on 587, `tls` on 465, or `none` for a local relay), username, from, and 1 to 20 recipients | The SMTP password |
| Email (Resend) | `email_api` | From, and 1 to 20 recipients | A Resend API key (`re_...`) |
| Slack | `slack_webhook` | None | The Slack incoming webhook URL |
| Webhook | `webhook` | The URL to post to | A signing secret that Byname makes |

Each channel subscribes to the events it should get. Byname encrypts each channel's secret with `ENCRYPTION_KEY` and never returns it, except the webhook signing secret, which it shows once when you add the channel. Adding or editing a channel needs a sign-in within the last 15 minutes. **Send test** sends a test message through the channel at once.

Webhook and Slack URLs must use `https`, or `http` to `localhost` for a receiver on the same machine. Email uses plain text, with the event's title as the subject and a link to the page in Byname, built from `PUBLIC_URL`.

## Events

`internal/notify/events.go` is the one list of events. Settings lists them in this order.

| Event | Kind | Raised when | Seen in the app by |
|---|---|---|---|
| Check completed | `check_completed` | A check finished. It carries gains, losses and moves since the previous check, the net change, how many answers named you, the spend, and the questions that changed most. | Anyone with `results:read` |
| Check refused | `check_refused` | Treg refused a call, for example for an empty balance, and the check stopped. | Anyone with `results:read` |
| Check failed | `check_failed` | A check could not run or could not store its answers. | Anyone with `results:read` |
| Spend cap reached | `spend_cap_reached` | A check stopped queueing calls at the per-check or monthly spend cap. | Anyone with `results:read` |
| Model paused | `pair_paused` | A model left its surface's live model list, so Byname stopped asking it. | Anyone with `results:read` |
| Goal reached | `goal_reached` | After a Search Console sync, a search goal's progress over its window first meets its target. | Anyone with `results:read` |
| Search moved | `search_moved` | After a Search Console sync, 28-day clicks or 28-day non-brand clicks differ from the 28 days before by at least 25% of the earlier number and by at least 20 clicks. Both 28-day windows must have data for every day. | Anyone with `results:read` |
| Change verdict | `change_verdict` | A site change got its final read against the control pages, 31 days after Google's first crawl by default. The interim read raises nothing. See [Changes](changes.md). | Anyone with `results:read` |
| Token expiring | `token_expiring` | The hourly GitHub poll, or **Test** under **Settings > Site changes**, finds that the token expires within 14 days. | Admins only |
| Social digest | `social_digest` | A day or week of social searches ended. It carries the new mentions per network with the most engaged posts, the new voices and what the searches cost, without the accounts left out. See [Social](social.md). | Anyone with `results:read` |

Each event has a natural key, its kind and what it happened to, such as one run. Raising the same event twice stores and sends it once. For the five events that do not come from a check, the key decides how often each one fires:

| Event | Fires once per |
|---|---|
| `goal_reached` | Goal and target. Raising a goal's target lets it fire again. |
| `search_moved` | Metric and the last day of the 28-day window. While the move holds, the next day's sync raises it again. |
| `change_verdict` | Change. |
| `token_expiring` | Repository and token expiry date. A new token with a new expiry date warns again. |
| `social_digest` | Day or week, such as `week/2026-11-16`. A period in which nothing was searched raises none. |

## Delivery and retries

Byname sends from a background job, never inside a request. A delivery that fails is tried again after 30 seconds, 2 minutes, 8 minutes and 32 minutes, five attempts in all. Any `2xx` answer counts as delivered. Each attempt is logged with its status and error, and **Settings > Notifications** shows the log. The log never records a channel's URL, because a Slack webhook URL is a secret.

## Receive a webhook

A webhook channel sends `POST` to its URL with a JSON body:

```json
{
	"id": "alert-12/channel-3",
	"kind": "check_completed",
	"title": "Check #42: 3 gained, 1 lost",
	"lines": ["Named in 31 of 96 answers.", "Since check #41: 3 gained, 1 lost, 2 moved, net +2."],
	"url": "https://byname.example.com/compare?before=41&after=42",
	"payload": {"run_id": 42, "previous_run_id": 41, "gained": 3, "lost": 1, "moved": 2, "net": 2, "named": 31, "answered": 96},
	"created_at": "2026-11-21T06:14:09Z"
}
```

`payload` is the event's own data, and its fields depend on `kind`. `id` is the same on every retry of one delivery, so use it to ignore a repeat. Each request carries these headers:

| Header | Value |
|---|---|
| `X-Byname-Event` | The event kind, such as `check_completed` |
| `X-Byname-Delivery` | The delivery id, the same as `id` |
| `X-Byname-Signature` | `t=<unix seconds>,v1=<hex HMAC-SHA256>` |
| `User-Agent` | `Byname` |

### Verify the signature

The signature is the hex HMAC-SHA256 of `<t>.<raw body>`, keyed with the channel's signing secret, which starts with `whsec_`. The timestamp is signed too, so refuse a request whose `t` is more than a few minutes away from now, and a captured request cannot be replayed later. Check the raw body before you parse it.

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
