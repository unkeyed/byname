# AI apps

Byname asks your questions where your buyers ask them: the ChatGPT app, a Google results page, Claude Code in a developer's terminal. You pick these AI apps, not models or providers, and each AI app is pinned to the endpoint that comes closest to what a person sees there. This page lists every AI app, what its accuracy label means, and what Byname cannot reach yet.

You choose AI apps in onboarding and under **Settings > AI apps**. `internal/engines/registry.go` is the one list of them, and this page follows it. The code and the API call an AI app a surface, as in `GET /v1/surfaces`.

## Accuracy labels

Every AI app, chart and answer carries one of three labels, which say how close Byname gets to the real thing. Answers with different labels are stored, charted and compared apart, and never averaged together.

| Label | Stored as | What it means |
|---|---|---|
| Real app answer | `app` | Read from the real app or search page by a scraping provider. The most faithful option, used wherever one exists. |
| Same model, not the full tool | `model_match` | For a developer tool that runs in a terminal: an API call with the model your team uses and web search turned on. It does not include the tool's own system prompt or agent loop. |
| Stand-in (not the real app) | `approximation` | The closest option when there is no real app answer, such as the Claude API standing in for the Claude app. Never called the app. |

The label is decided for each AI app and model, in `PairFidelity`:

- An app is a Real app answer when its app is read, through Treg or a scraping provider's own key. On a direct model key or a gateway it becomes a Stand-in, because a model API stands in for the app.
- A developer tool is Same model, not the full tool while the route accepts the model, search was forced (or turned on, where it cannot be forced) and the test call actually searched. Otherwise it is a Stand-in. Until the test call runs, it shows the label it is expected to get.
- The Claude app and the Perplexity app are always Stand-ins.

## AI apps and their pinned endpoints

Each AI app calls one pinned Treg endpoint, never a routed one that picks a provider per call, so checks compare only when the same endpoint answered every time. The endpoint, the model and the label are stored on every answer, and changing any of them starts a new line on the charts. The sample cost is what one call cost in a sample on November 21, 2026, used for the estimate before your first check.

| AI app | Key | Pinned Treg endpoint | Label | Sample cost |
|---|---|---|---|---|
| ChatGPT app | `chatgpt_app` | `dataforseo.x.ai-optimization-chat-gpt-llm-scraper-live-advanced` | Real app answer | $0.004 |
| Gemini app | `gemini_app` | `cloro.ai-search.gemini.scrape` | Real app answer | $0.0024 |
| Google AI Mode | `google_ai_mode` | `cloro.google.serp.ai_mode` | Real app answer | $0.0024 |
| Google AI Overview | `google_ai_overview` | `cloro.google.serp.organic` | Real app answer | $0.0028 |
| Microsoft Copilot | `copilot` | `serpapi.x.bing-copilot` | Real app answer | $0.015 |
| Bing AI Mode | `bing_ai_mode` | `cloro.bing.serp.ai_mode` | Real app answer | $0.0024 |
| Brave AI answers | `brave` | `serpapi.x.brave-ai-mode` | Real app answer | $0.015 |
| Claude Code | `claude_code` | `dataforseo.x.ai-optimization-claude-llm-responses-live` | Same model, not the full tool | $0.055 |
| Gemini CLI | `gemini_cli` | `dataforseo.x.ai-optimization-gemini-llm-responses-live` | Same model, not the full tool | $0.060 |
| Codex CLI | `codex_cli` | `dataforseo.x.ai-optimization-chat-gpt-llm-responses-live` | Same model, not the full tool | $0.073 |
| Claude app | `claude_app` | `dataforseo.x.ai-optimization-claude-llm-responses-live` | Stand-in (not the real app) | $0.055 |
| Perplexity app | `perplexity_app` | `dataforseo.x.ai-optimization-perplexity-llm-responses-live` | Stand-in (not the real app) | $0.0061 |

The ChatGPT app, the Gemini app and Google AI Mode are ticked by default in onboarding. They are what most buyers use, and each answer costs well under a cent. No developer tool is ticked: they cost about ten times more an answer and matter only when developers are your buyers. Token-billed endpoints, the developer tools and stand-ins, cost a different amount on every answer, so the review step labels the cost per check as an estimate. `GET /v1/costs` gives the cost of one answer in every AI app. See [Treg](treg.md#cost).

## Models

Apps take no model. The app decides, and Byname stores the model name when the endpoint reports one. Developer tools and the stand-ins take one or more models each, picked from the endpoint's live model list. Each model you pick is charted on its own line with its own label, so the questions asked per check are questions times each AI app and model.

How web search is turned on depends on the endpoint:

| Endpoint | Search |
|---|---|
| Claude and ChatGPT LLM responses | `web_search` on, and `force_web_search` on the models that accept it. Some models refuse forced search, and become a Stand-in. |
| Gemini LLM responses | `web_search` on. It cannot be forced. |
| Perplexity LLM responses | The `sonar` models search on every call. |
| App scrapes | The app decides for itself. |

Model lists are read for free and refreshed once a day, and an hour after a failed refresh. If a model you picked leaves its list, Byname stops asking it, shows a notice and raises a **Model paused** alert. It never switches to another model on its own. Every answer stores the exact model name the endpoint reported, such as a dated snapshot name, so a change shows on the charts. Picking a dated name pins the version.

## What every answer stores

- The answer text, with reasoning items dropped.
- Every brand the answer named with its position, and every link, with Google redirect links resolved to their targets.
- The AI app's own web searches, where the endpoint exposes them. The Claude, ChatGPT and Gemini LLM responses do. Perplexity and the app scrapes do not.
- The raw response, exactly as received, until raw response retention removes it.
- The cost, or a flag that the call reported none. A missing cost is never stored as $0.

Each endpoint's success rate over recent checks shows on **Settings > Check history**, so a flaky provider reads as a provider problem, not as AI apps naming you less. The question sent is always the raw question text, never your brand name and never a system prompt that mentions it.

## Routes

Every AI app works through [Treg](treg.md), the service that runs each question and bills you per answer, with one token. You can move any AI app to another route under **Settings > AI apps**, in its **Advanced: own key or gateway** options. **Don't use Treg?** opens them for every AI app at once.

| Route | Calls | Label for an app | Page |
|---|---|---|---|
| Treg | The pinned endpoint through Treg | Real app answer | [Treg](treg.md) |
| Scraping provider | The same pinned endpoint at DataForSEO, cloro or SerpApi, with your key | Real app answer | [Scraping providers](scraping-providers.md) |
| Model provider | OpenAI, Anthropic, Google Gemini or Perplexity directly, with your key | Stand-in | [Model providers](model-providers.md) |
| Gateway | An OpenAI-compatible gateway at its base URL | Stand-in | [Gateways](gateways.md) |

Moving an AI app to another route resets each model's test, and the stored endpoint changes, so the AI app starts a new line on the charts.

## Known gaps

- No app scrape exists for the Claude app or the Perplexity app, so both are Stand-ins. When a faithful endpoint appears, the pin changes and a new line starts.
- No endpoint exists for Grok, Meta AI or DeepSeek.
- Google shows an AI Overview for some queries and not others. A search with none is stored as a `no_answer` result, which counts as not naming anyone.
- Treg's catalog marks the Copilot endpoint's parameter names as unconfirmed. `engine=bing_copilot` and `q` work.
- The ChatGPT app scrape takes a DataForSEO location code. Byname maps 17 markets: AU, BR, CA, DE, ES, FR, GB, IE, IN, IT, JP, MX, NL, NZ, SE, SG and US. With any other market, ChatGPT app calls fail.
- Byname never sets temperature, and does not know the providers' defaults. It does not know whether the app scrapes run signed in or with personalization, so a signed-in person may see a different answer.
