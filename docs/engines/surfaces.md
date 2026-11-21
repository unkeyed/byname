# Where people ask

Byname asks your questions where your buyers ask them: the ChatGPT app, a Google results page, Claude Code in a developer's terminal. You pick these surfaces, not engines or providers, and each surface is pinned to the endpoint that comes closest to what a person sees there. This page lists every surface, what its fidelity label means, and what Byname cannot reach yet.

You choose surfaces in onboarding and under **Settings > Where people ask**. `internal/engines/registry.go` is the one list of surfaces, and this page follows it.

## Fidelity labels

Every surface, chart and answer carries one of three labels. Answers with different labels are stored, charted and compared apart, and never averaged together.

| Label | Stored as | What it means |
|---|---|---|
| App answer | `app` | Read from the real app or search page by a scraping provider. The most faithful option, used wherever one exists. |
| Same model and search | `model_match` | For a developer tool that runs in a terminal: an API call with the model your team uses and web search turned on. It does not include the tool's own system prompt or agent loop. |
| Approximation | `approximation` | The closest option when there is no app answer, such as the Claude API standing in for the Claude app. Never called the app. |

Fidelity is decided per surface and model pair, in `PairFidelity`:

- An app surface is an App answer when its app is read, through Treg or a scraping provider's own key. On a direct model key or a gateway it becomes an Approximation, because a model API stands in for the app.
- A developer tool is Same model and search while the route accepts the model, search was forced (or turned on, where it cannot be forced) and the pair's test call actually searched. Otherwise it is an Approximation. Until the test call runs, a pair shows the label it is expected to get.
- A stand-in surface, the Claude app or the Perplexity app, is always an Approximation.

## Surfaces and their pinned endpoints

Each surface calls one pinned Treg endpoint, never a routed one that picks a provider per call, so runs compare only when the same endpoint answered every time. The endpoint, the model and the fidelity are stored on every answer, and changing any of them starts a new series. The sample cost is what one call cost in a sample on November 21, 2026, used for the estimate before your first check.

| Surface | Key | Pinned Treg endpoint | Fidelity | Sample cost |
|---|---|---|---|---|
| ChatGPT app | `chatgpt_app` | `dataforseo.x.ai-optimization-chat-gpt-llm-scraper-live-advanced` | App answer | $0.004 |
| Gemini app | `gemini_app` | `cloro.ai-search.gemini.scrape` | App answer | $0.0024 |
| Google AI Mode | `google_ai_mode` | `cloro.google.serp.ai_mode` | App answer | $0.0024 |
| Google AI Overview | `google_ai_overview` | `cloro.google.serp.organic` | App answer | $0.0028 |
| Microsoft Copilot | `copilot` | `serpapi.x.bing-copilot` | App answer | $0.015 |
| Bing AI Mode | `bing_ai_mode` | `cloro.bing.serp.ai_mode` | App answer | $0.0024 |
| Brave AI answers | `brave` | `serpapi.x.brave-ai-mode` | App answer | $0.015 |
| Claude Code | `claude_code` | `dataforseo.x.ai-optimization-claude-llm-responses-live` | Same model and search | $0.055 |
| Gemini CLI | `gemini_cli` | `dataforseo.x.ai-optimization-gemini-llm-responses-live` | Same model and search | $0.060 |
| Codex CLI | `codex_cli` | `dataforseo.x.ai-optimization-chat-gpt-llm-responses-live` | Same model and search | $0.073 |
| Claude app | `claude_app` | `dataforseo.x.ai-optimization-claude-llm-responses-live` | Approximation | $0.055 |
| Perplexity app | `perplexity_app` | `dataforseo.x.ai-optimization-perplexity-llm-responses-live` | Approximation | $0.0061 |

The ChatGPT app, the Gemini app, Google AI Mode and Claude Code are ticked by default in onboarding. Token-billed endpoints, the developer tools and stand-ins, cost a different amount on every answer, so the review step labels the cost per check as an estimate.

## Models

App surfaces take no model. The app decides, and Byname stores the model name when the endpoint reports one. Developer tools and the stand-ins take one or more models each, picked from the endpoint's live model list. Each selected model makes a surface and model pair with its own series and its own fidelity label, so calls per check are questions times pairs.

How web search is turned on depends on the endpoint:

| Endpoint | Search |
|---|---|
| Claude and ChatGPT LLM responses | `web_search` on, and `force_web_search` on the models that accept it. Some models refuse forced search, and their pair becomes an Approximation. |
| Gemini LLM responses | `web_search` on. It cannot be forced. |
| Perplexity LLM responses | The `sonar` models search on every call. |
| App scrapes | The app decides for itself. |

Model lists are read for free and refreshed once a day, and an hour after a failed refresh. If a selected model leaves its list, Byname pauses that pair with a notice and raises a **Model paused** alert. It never switches to another model on its own. Every answer stores the exact model name the endpoint reported, such as a dated snapshot name, so a change shows on the charts. Picking a dated name pins the version.

## What every answer stores

- The answer text, with reasoning items dropped.
- Every brand mention with its position, and every citation, with Google redirect links resolved to their targets.
- The engine's own web searches, where the endpoint exposes them. The Claude, ChatGPT and Gemini LLM responses do. Perplexity and the app scrapes do not.
- The raw response, exactly as received, until raw response retention removes it.
- The cost, or a flag that the call reported none. A missing cost is never stored as $0.

Each endpoint's success rate over recent runs shows on the **Runs** page, so a flaky provider reads as a provider problem, not a drop in visibility. The question sent is always the raw question text, never your brand name and never a system prompt that mentions it.

## Routes

Every surface works through [Treg](treg.md) with one token. You can move any surface to another route under **Settings > Where people ask**, in the surface's **Advanced: own key or gateway** options. **Don't use Treg?** opens them for every surface at once.

| Route | Calls | Fidelity on an app surface | Page |
|---|---|---|---|
| Treg | The pinned endpoint through Treg | App answer | [Treg](treg.md) |
| Scraping provider | The same pinned endpoint at DataForSEO, cloro or SerpApi, with your key | App answer | [Scraping providers](scraping-providers.md) |
| Model provider | OpenAI, Anthropic, Google Gemini or Perplexity directly, with your key | Approximation | [Model providers](model-providers.md) |
| Gateway | An OpenAI-compatible gateway at its base URL | Approximation | [Gateways](gateways.md) |

Moving a surface to another route resets each pair's test, and the stored endpoint changes, so the surface starts a new series.

## Known gaps

- No app scrape exists for the Claude app or the Perplexity app, so both are Approximations. When a faithful endpoint appears, the pin changes and a new series starts.
- No endpoint exists for Grok, Meta AI or DeepSeek.
- Google shows an AI Overview for some queries and not others. A search with none is stored as a `no_answer` result, which counts as not naming anyone.
- Treg's catalog marks the Copilot endpoint's parameter names as unconfirmed. `engine=bing_copilot` and `q` work.
- The ChatGPT app scrape takes a DataForSEO location code. Byname maps 17 markets: AU, BR, CA, DE, ES, FR, GB, IE, IN, IT, JP, MX, NL, NZ, SE, SG and US. With any other market, ChatGPT app calls fail.
- Byname never sets temperature, and does not know the providers' defaults. It does not know whether the app scrapes run signed in or with personalization, so a signed-in person may see a different answer.
