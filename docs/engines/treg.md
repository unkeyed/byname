# Treg

[Treg](https://treg.to) is the default route. One Treg token runs every surface in [Where people ask](surfaces.md), each through its pinned endpoint, and Treg reports the cost of every call. You need no account with DataForSEO, cloro or SerpApi.

## Add the token

An Admin pastes the Treg team token in onboarding's **Where people ask** step, or under **Settings > Credentials**. Byname encrypts it with `ENCRYPTION_KEY`, sends it only to Treg, and never returns it through any API. A personal Treg token also needs its organization, which Byname sends as `X-Treg-Org`. A team token does not.

**Test** under **Settings > Credentials** makes one free call, a read of the Claude Code model list, to prove the token works. The token is required while any enabled surface uses the Treg route. If every enabled surface uses your own key or a gateway, onboarding finishes without it.

## How calls work

- Each call goes to `https://treg.to/call/<endpoint>` with the provider's own request body. Treg adds only headers, so the response is the provider's own shape, and Byname parses it the same way on the scraping provider route.
- Each call carries an idempotency key built from the run, the question, the pair and the attempt. A retry after a lost response replays the stored answer instead of paying for a new one.
- Treg reports each call's cost in the `X-Treg-Cost-Micro` header. Byname stores it on the answer and adds it to the run's spend, which the spend caps under **Settings > Limits** read.
- Model lists come from DataForSEO's model-list endpoints through Treg, which cost nothing.

## When Treg refuses a call

Treg can decline a call on its own behalf, for example `402 insufficient_balance` when the balance is empty or `429 org_rate_limit`. Nothing reached the provider and nothing was charged, so Byname does not count it against the endpoint's success rate. The check stops, and Byname raises a **Check refused** alert with Treg's reason. Top up or wait, then use **Run now**.

A provider error that Treg passes through is different. It is stored as a failed answer and counts against that endpoint's success rate on the **Runs** page.

## Cost

A check costs questions times enabled pairs times each endpoint's cost per call. The review step and `GET /v1/estimate` estimate it from each endpoint's past answers, or from the sample cost in [Where people ask](surfaces.md#surfaces-and-their-pinned-endpoints) before the first check. The per-check spend cap stops queueing new calls once a check passes it. The optional monthly cap stops a check from starting once the month's checks have spent that much.
