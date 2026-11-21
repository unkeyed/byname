# Model providers

A surface can call a model provider's API directly with your own key: OpenAI, Anthropic, Google Gemini or Perplexity. A model API is not the app, so an app surface on this route is an Approximation. A developer tool stays Same model and search while the provider accepts the model and its test call searched.

## Which surfaces each provider stands in for

Each provider serves the surfaces whose model it runs. Brave AI answers has no direct provider.

| Provider | Credential kind | Surfaces | API | Web search | Cost reported |
|---|---|---|---|---|---|
| OpenAI | `openai` | ChatGPT app, Microsoft Copilot, Bing AI Mode, Codex CLI | Responses API, `POST /v1/responses` | The hosted `web_search` tool, which can be forced | No |
| Anthropic | `anthropic` | Claude Code, Claude app | Messages API, `POST /v1/messages` | The `web_search` tool, which can be forced | No |
| Google Gemini | `google` | Gemini app, Google AI Mode, Google AI Overview, Gemini CLI | `generateContent` | The `google_search` tool, which cannot be forced | No |
| Perplexity | `perplexity` | Perplexity app | Chat completions | Every call searches | Yes, in the response |

OpenAI, Anthropic and Google bill by tokens and put no dollar figure in the response. Byname stores those answers with their cost flagged as missing, never as $0. The run shows how many calls reported no cost, and the spend caps cannot count them, so set a spending limit at the provider too.

On this route every surface takes a model, an app surface included, because the API needs one. Byname reads the provider's model list with your key and offers it in the surface's model chips.

## Use your key

1. An Admin adds the key under **Settings > Credentials**: an OpenAI key (`sk-...`), an Anthropic key (`sk-ant-...`), a Gemini API key from Google AI Studio, or a Perplexity key (`pplx-...`). Byname encrypts it and never returns it.
2. Click **Test**. OpenAI, Anthropic and Google have a free call that proves the key works. Perplexity has none, so test a surface that uses it instead.
3. Under **Settings > Where people ask**, open the surface's **Advanced: own key or gateway** options, pick **Your own provider key**, and choose at least one model.
4. Save and run each pair's test call. The test shows the status, whether the answer searched and cited anything, and the fidelity label it confirms.

The endpoint stored on each answer becomes the provider's API, such as `openai.responses`, so the surface starts a new series.
