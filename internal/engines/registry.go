package engines

import (
	"net/http"
	"net/url"
)

// Endpoint is one pinned Treg tool and how to talk to it. Several surfaces
// can share an endpoint (Claude Code and the Claude app both use the Claude
// API) while keeping their own fidelity.
type Endpoint struct {
	// ID is the Treg tool id, called as <method> /call/<ID>.
	ID string
	// Family is the provider whose native shapes the endpoint speaks. Its
	// fixtures live in testdata/<Family>/<ID>*.json.
	Family string
	Method string
	Search SearchControl
	// Models is the Treg tool id of the endpoint's free model list, or empty
	// when the endpoint takes no model.
	Models string
	// SampleCostUSD is what one call cost in the Nov 21, 2026 sample, for
	// the estimate before a first check. Token-billed endpoints vary with
	// every answer, so it is only ever shown as an estimate.
	SampleCostUSD *float64
	// Native is the endpoint's path at its own provider, which the scraper
	// route calls with the team's key. NativeModels is the model list's.
	Native       string
	NativeModels string

	build func(Endpoint, Request) (providerRequest, error)
	parse func(raw []byte) Result
}

// providerRequest is the provider's own request: query parameters for a GET,
// a JSON body for a POST.
type providerRequest struct {
	Query url.Values
	Body  any
}

// TakesModel reports whether a Request for this endpoint must name a model.
func (e Endpoint) TakesModel() bool { return e.Models != "" }

// Decode parses a provider's native response body. It never fails: bytes it
// cannot read, or a provider-reported error, give StatusFailed with the
// reason. Raw is set to body.
func (e Endpoint) Decode(body []byte) Result {
	r := e.parse(body)
	r.Raw = body
	return r
}

// Spec is one surface's row in the registry.
type Spec struct {
	Surface  Surface
	Title    string
	Endpoint Endpoint
	// Fidelity is the surface's default label. A model_match surface's pairs
	// get their final label from PairFidelity after the test call.
	Fidelity Fidelity
}

var (
	chatGPTScraper = Endpoint{
		ID: "dataforseo.x.ai-optimization-chat-gpt-llm-scraper-live-advanced", Family: "dataforseo",
		SampleCostUSD: usd(0.004),
		Native:        "/v3/ai_optimization/chat_gpt/llm_scraper/live/advanced",
		Method:        http.MethodPost, Search: SearchByApp,
		build: dataforseoScraperRequest, parse: parseDataForSEOScraper,
	}
	claudeAPI = Endpoint{
		ID: "dataforseo.x.ai-optimization-claude-llm-responses-live", Family: "dataforseo",
		SampleCostUSD: usd(0.055),
		Native:        "/v3/ai_optimization/claude/llm_responses/live", NativeModels: "/v3/ai_optimization/claude/llm_responses/models",
		Method: http.MethodPost, Search: SearchForceable,
		Models: "dataforseo.x.ai-optimization-claude-llm-responses-models",
		build:  dataforseoLLMRequest(true), parse: parseDataForSEOLLM,
	}
	chatGPTAPI = Endpoint{
		ID: "dataforseo.x.ai-optimization-chat-gpt-llm-responses-live", Family: "dataforseo",
		SampleCostUSD: usd(0.073),
		Native:        "/v3/ai_optimization/chat_gpt/llm_responses/live", NativeModels: "/v3/ai_optimization/chat_gpt/llm_responses/models",
		Method: http.MethodPost, Search: SearchForceable,
		Models: "dataforseo.x.ai-optimization-chat-gpt-llm-responses-models",
		build:  dataforseoLLMRequest(true), parse: parseDataForSEOLLM,
	}
	geminiAPI = Endpoint{
		ID: "dataforseo.x.ai-optimization-gemini-llm-responses-live", Family: "dataforseo",
		SampleCostUSD: usd(0.060),
		Native:        "/v3/ai_optimization/gemini/llm_responses/live", NativeModels: "/v3/ai_optimization/gemini/llm_responses/models",
		Method: http.MethodPost, Search: SearchEnableOnly,
		Models: "dataforseo.x.ai-optimization-gemini-llm-responses-models",
		build:  dataforseoLLMRequest(false), parse: parseDataForSEOLLM,
	}
	perplexityAPI = Endpoint{
		ID: "dataforseo.x.ai-optimization-perplexity-llm-responses-live", Family: "dataforseo",
		SampleCostUSD: usd(0.0061),
		Native:        "/v3/ai_optimization/perplexity/llm_responses/live", NativeModels: "/v3/ai_optimization/perplexity/llm_responses/models",
		Method: http.MethodPost, Search: SearchAlways,
		Models: "dataforseo.x.ai-optimization-perplexity-llm-responses-models",
		build:  dataforseoLLMRequest(true), parse: parseDataForSEOLLM,
	}
	geminiScrape = Endpoint{
		ID: "cloro.ai-search.gemini.scrape", Family: "cloro",
		SampleCostUSD: usd(0.0024),
		Native:        "/v1/monitor/gemini",
		Method:        http.MethodPost, Search: SearchByApp,
		build: cloroPromptRequest, parse: parseCloroAnswer,
	}
	googleAIMode = Endpoint{
		ID: "cloro.google.serp.ai_mode", Family: "cloro",
		SampleCostUSD: usd(0.0024),
		Native:        "/v1/monitor/aimode",
		Method:        http.MethodPost, Search: SearchByApp,
		build: cloroAIModeRequest, parse: parseCloroAnswer,
	}
	googleOrganic = Endpoint{
		ID: "cloro.google.serp.organic", Family: "cloro",
		SampleCostUSD: usd(0.0028),
		Native:        "/v1/monitor/google",
		Method:        http.MethodPost, Search: SearchByApp,
		build: cloroOrganicRequest, parse: parseCloroAIOverview,
	}
	bingAIMode = Endpoint{
		ID: "cloro.bing.serp.ai_mode", Family: "cloro",
		SampleCostUSD: usd(0.0024),
		Native:        "/v1/monitor/bing/aimode",
		Method:        http.MethodPost, Search: SearchByApp,
		build: cloroPromptRequest, parse: parseCloroAnswer,
	}
	bingCopilot = Endpoint{
		ID: "serpapi.x.bing-copilot", Family: "serpapi",
		SampleCostUSD: usd(0.015),
		Native:        "/search.json",
		Method:        http.MethodGet, Search: SearchByApp,
		build: serpapiRequest("bing_copilot"), parse: parseSerpApi,
	}
	braveAIMode = Endpoint{
		ID: "serpapi.x.brave-ai-mode", Family: "serpapi",
		SampleCostUSD: usd(0.015),
		Native:        "/search.json",
		Method:        http.MethodGet, Search: SearchByApp,
		build: serpapiRequest("brave_ai_mode"), parse: parseSerpApi,
	}
)

// registry is the one list of surfaces. Order is the order onboarding shows
// them in: consumer apps and search, developer tools, then approximations.
var registry = []Spec{
	{ChatGPTApp, "ChatGPT app", chatGPTScraper, FidelityApp},
	{GeminiApp, "Gemini app", geminiScrape, FidelityApp},
	{GoogleAIMode, "Google AI Mode", googleAIMode, FidelityApp},
	{GoogleAIOverview, "Google AI Overview", googleOrganic, FidelityApp},
	{Copilot, "Microsoft Copilot", bingCopilot, FidelityApp},
	{BingAIMode, "Bing AI Mode", bingAIMode, FidelityApp},
	{Brave, "Brave AI answers", braveAIMode, FidelityApp},
	{ClaudeCode, "Claude Code", claudeAPI, FidelityModelMatch},
	{GeminiCLI, "Gemini CLI", geminiAPI, FidelityModelMatch},
	{CodexCLI, "Codex CLI", chatGPTAPI, FidelityModelMatch},
	{ClaudeApp, "Claude app", claudeAPI, FidelityApproximation},
	{PerplexityApp, "Perplexity app", perplexityAPI, FidelityApproximation},
}

func usd(v float64) *float64 { return &v }

// Lookup returns the registry row for a surface.
func Lookup(s Surface) (Spec, bool) {
	for _, spec := range registry {
		if spec.Surface == s {
			return spec, true
		}
	}
	return Spec{}, false
}

// All returns every surface in registry order.
func All() []Spec {
	return append([]Spec(nil), registry...)
}
