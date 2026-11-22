# Scraping providers

Every pinned endpoint belongs to one of three scraping providers: DataForSEO, cloro or SerpApi. If you already have an account with one, an AI app can call the same endpoint at that provider with your key instead of through Treg. The answer is the same one Treg would return, so an app stays a Real app answer.

## Which AI apps each provider serves

| Provider | Credential kind | AI apps | Cost reported |
|---|---|---|---|
| DataForSEO | `dataforseo` | ChatGPT app, Claude Code, Gemini CLI, Codex CLI, Claude app, Perplexity app | Yes, in the response |
| cloro | `cloro` | Gemini app, Google AI Mode, Google AI Overview, Bing AI Mode | No |
| SerpApi | `serpapi` | Microsoft Copilot, Brave AI answers | No |

cloro and SerpApi responses carry no cost. Byname stores each of their answers with its cost flagged as missing, never as $0, so the check shows how many calls reported no cost and the spend caps cannot count them. Keep an eye on those providers' own dashboards.

## Use your key

1. An Admin adds the key under **Settings > Provider keys**. Byname encrypts it and never returns it.
   - DataForSEO: your API login and password as `login:password`, from DataForSEO's API access page.
   - cloro: a cloro API key.
   - SerpApi: a SerpApi API key.
2. Click **Test**. DataForSEO and SerpApi have a free call that proves the key works. cloro has none, so test an AI app that uses it instead.
3. Under **Settings > AI apps**, open the AI app's **Advanced: own key or gateway** options and pick **Your own scraping key**.
4. Save and run the test call.

Byname calls the provider's own API: `https://api.dataforseo.com`, `https://api.cloro.dev` or `https://serpapi.com`. The endpoint stored on each answer becomes `scraper:<pinned endpoint>`, so the AI app starts a new line on the charts even though the accuracy label stays the same.
