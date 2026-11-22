# Treg

[Treg](https://treg.to) is the service that runs each question and bills you per answer. It is the default route. One Treg token covers every app in [AI apps](surfaces.md), each through its pinned endpoint, and Treg reports the cost of every call. You need no account with DataForSEO, cloro or SerpApi.

## Add the token

An Admin pastes the Treg team token in onboarding's **AI apps** step, which links to treg.to to get one, or under **Settings > Treg**. Byname encrypts it with `ENCRYPTION_KEY`, sends it only to Treg, and never returns it through any API. A personal Treg token also needs its organization, which Byname sends as `X-Treg-Org`. A team token does not.

Saving the token makes one free call, a read of the Claude Code model list, to prove it works, so a mistyped token shows as refused at once. **Test** under **Settings > Treg** makes the same call again. The token is required while any AI app you turned on uses the Treg route. If every AI app you turned on uses your own key or a gateway, onboarding finishes without it.

## How calls work

- Each call goes to `https://treg.to/call/<endpoint>` with the provider's own request body. Treg adds only headers, so the response is the provider's own shape, and Byname parses it the same way on the scraping provider route.
- Each call carries an idempotency key built from the check, the question, the AI app and model, and the attempt. A retry after a lost response replays the stored answer instead of paying for a new one.
- Treg reports each call's cost in the `X-Treg-Cost-Micro` header. Byname stores it on the answer and adds it to the check's spend, which the spend caps under **Settings > Schedule and limits** read.
- Model lists come from DataForSEO's model-list endpoints through Treg, which cost nothing.

## When Treg refuses a call

Treg can decline a call on its own behalf, for example `402 insufficient_balance` when the balance is empty or `429 org_rate_limit`. Nothing reached the provider and nothing was charged, so Byname does not count it against the endpoint's success rate. The check stops, and Byname raises a **Check refused** alert with Treg's reason. Top up or wait, then use **Check now**.

A provider error that Treg passes through is different. It is stored as a failed answer and counts against that endpoint's success rate on **Settings > Check history**.

## Cost

A check costs questions times each AI app and model you turned on times each endpoint's cost per call. The review step and `GET /v1/estimate` estimate it from each endpoint's past answers, or from the sample cost in [AI apps](surfaces.md#ai-apps-and-their-pinned-endpoints) before the first check. The per-check spend cap stops asking new questions once a check passes it. The monthly cap stops a check from starting, and social searches from calling Treg, once the month's checks and social searches have spent that much.

Treg bills you, not Byname. `GET /v1/costs`, or the `get_costs` tool, gathers every cost figure in one place, in US dollars:

- `per_answer`: what one answer costs in each AI app on its current route, from past answers or the sample cost, with the models turned on. Every model in an AI app costs the same per answer.
- `checks`: one check, the same estimate as `GET /v1/estimate`, how many checks the schedule runs in an average month (0 while paused), and what they cost. **Check now** adds to it.
- `social`: social searches at their settings, one call per network and term each fetch. `treg_month_usd` is what the Treg networks would cost, `bluesky_month_usd` is always 0, and `month_usd` is 0 while social is off.
- `month_estimate_usd`: checks and social searches in an average month.
- `default_month_cap_usd`: the monthly cap finishing onboarding sets when none is set, from `month_estimate_usd`.
- `month`: what the calendar month has spent so far in the instance's time zone, checks and social apart, how many calls reported no cost, and the monthly cap.

**Settings > Schedule and limits** shows the month's estimate and spend, and **Check now** shows what a check will cost before it starts. At the default schedule, every 3 days, the three consumer apps ticked by default and 10 questions cost about $0.09 a check, or about $0.90 a month. Social searches for two terms on all seven networks every 6 hours add about $1.60 a month.

### The default monthly cap

When onboarding finishes and no monthly cap is set, Byname sets one: three times the monthly estimate of checks and social searches, rounded up to a whole dollar, and at least $5.00. A normal month stays well under it, and a mistake, such as a question list ten times longer than meant, stops at it. Onboarding's **Schedule and spend caps** step and its review show the cap finishing will set, and the estimate beside each cap. Change it or remove it under **Settings > Schedule and limits**, or with `PUT /v1/settings`.
