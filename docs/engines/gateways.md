# Gateways

Any AI app can call an OpenAI-compatible gateway, such as OpenRouter, Vercel AI Gateway or Cloudflare AI Gateway. Byname sends a chat completion to the gateway's base URL with the model you pick. None of these is a tested integration yet, so run each model's test call and read its result before you rely on it.

A gateway calls a model API, not the app, so an app on this route is a Stand-in (not the real app). A developer tool stays Same model, not the full tool only if the test call shows the model searched.

## How the gateway route works

- Byname sends `POST <base URL>/chat/completions` with the model and the question as the only message, and reads models from `GET <base URL>/models`.
- The request carries no search option, because gateways have no common one. Whether the model searches is the model's decision. Pick a model that searches on its own, such as one with web search built in, and check the test call.
- Links come from the message's `url_citation` annotations, then from a `search_results` or `citations` list where the gateway passes one through. Reasoning that a model puts ahead of its answer inside `<think>` tags is dropped.
- Some gateways report a cost in the response and some do not. A call with no reported cost is flagged as missing, never stored as $0.
- The endpoint stored on each answer is `gateway:<host>`, so two gateways, or one gateway at a new URL, are separate lines on the charts.

## Use a gateway

1. An Admin adds the gateway's API key under **Settings > Provider keys**, as the **OpenAI-compatible gateway** key, and clicks **Test**. The test lists the gateway's models with the key.
2. Under **Settings > AI apps**, open the AI app's **Advanced: own key or gateway** options and pick **OpenAI-compatible gateway**.
3. Enter the gateway's base URL, such as `https://openrouter.ai/api/v1`, and choose at least one model.
4. Save and run each model's test call.

One gateway key serves every AI app on the gateway route. Each AI app keeps its own base URL.
