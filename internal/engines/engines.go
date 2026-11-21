// Package engines asks one question on one surface and turns the provider's
// answer into a Result.
//
// A surface is a place people ask (the ChatGPT app, Google AI Mode, Claude
// Code). Each surface is pinned to one Treg endpoint in the registry table in
// registry.go, and each endpoint has one request builder and one parser for
// its provider's native shape. Parsers do no I/O: they read bytes and return
// a Result. Redirect links whose target is not in the URL are resolved by the
// adapter through an injected Resolver.
package engines

import "context"

// Surface is a place people ask. The values are stored in surface.key and
// never change once shipped.
type Surface string

const (
	ChatGPTApp       Surface = "chatgpt_app"
	GeminiApp        Surface = "gemini_app"
	GoogleAIMode     Surface = "google_ai_mode"
	GoogleAIOverview Surface = "google_ai_overview"
	Copilot          Surface = "copilot"
	BingAIMode       Surface = "bing_ai_mode"
	Brave            Surface = "brave"
	ClaudeCode       Surface = "claude_code"
	GeminiCLI        Surface = "gemini_cli"
	CodexCLI         Surface = "codex_cli"
	ClaudeApp        Surface = "claude_app"
	PerplexityApp    Surface = "perplexity_app"
)

// Fidelity says how close an answer is to what a person sees on the surface.
// Answers of different fidelity are stored and charted apart.
type Fidelity string

const (
	// FidelityApp is the real app or search page, read by a scraping provider.
	FidelityApp Fidelity = "app"
	// FidelityModelMatch is an API call with the model the team uses and the
	// same web search, standing in for a developer tool.
	FidelityModelMatch Fidelity = "model_match"
	// FidelityApproximation is the closest option when neither of the above
	// holds, such as the Claude API standing in for the Claude app.
	FidelityApproximation Fidelity = "approximation"
)

// SearchControl is how an endpoint lets the caller turn on web search.
type SearchControl string

const (
	// SearchByApp: a scraped app decides for itself whether to search.
	SearchByApp SearchControl = "by_app"
	// SearchForceable: web_search is on and force_web_search makes the model
	// search, on the models that accept it.
	SearchForceable SearchControl = "forceable"
	// SearchEnableOnly: web_search can be turned on but not forced (Gemini).
	SearchEnableOnly SearchControl = "enable_only"
	// SearchAlways: the model searches on every call (Perplexity sonar).
	SearchAlways SearchControl = "always"
)

// Request is one question for one surface. Question is the raw question
// text: never the brand name and never a system prompt that mentions it.
type Request struct {
	Question string
	Surface  Surface
	// Model is the model to ask, for surfaces whose endpoint takes one, and
	// empty for app surfaces, where the app decides.
	Model          string
	ForceWebSearch bool
	// Location is an ISO 3166-1 alpha-2 country code, such as "US".
	Location string
	// Language is an ISO 639-1 code, such as "en".
	Language string
	// IdempotencyKey is built by the runner from the run, question, surface
	// model and attempt, so a retry after a lost response replays the stored
	// answer instead of paying for a new one. Empty sends none.
	IdempotencyKey string
}

// Status is the outcome of one call.
type Status string

const (
	StatusOK Status = "ok"
	// StatusNoAnswer means the surface showed no AI answer for the query,
	// such as a Google search with no AI Overview. It is a valid result: the
	// surface did not name anyone.
	StatusNoAnswer Status = "no_answer"
	// StatusFailed means the provider gave no usable answer. Error says why.
	StatusFailed Status = "failed"
)

// Result is one answer as a person would read it, plus what it cost.
type Result struct {
	Status Status
	Error  string
	// Text is the answer text. Reasoning items are never part of it.
	Text string
	// ReportedModel is the exact model name the endpoint reported, if any.
	ReportedModel string
	// CostUSD is nil when the call reported no cost. It is never 0 for
	// "unknown", so a missing cost cannot pass for a free call.
	CostUSD *float64
	// Citations are the cited URLs in answer order, deduplicated, with
	// Google redirect links replaced by their targets where known.
	Citations []string
	// FanOut is the engine's own web searches, when the endpoint exposes them.
	FanOut []string
	// CallID is Treg's id for the call (X-Treg-Call-Id).
	CallID string
	// Raw is the response body exactly as received.
	Raw []byte
}

// Searched reports whether the answer shows the engine searched the web.
func (r Result) Searched() bool {
	return len(r.FanOut) > 0 || len(r.Citations) > 0
}

// Adapter sends a Request to a surface's pinned endpoint over one route.
type Adapter interface {
	Call(ctx context.Context, req Request) (Result, error)
}

// TestOutcome is what a surface and model pair's test call showed.
type TestOutcome struct {
	// ModelAccepted: the model is on the endpoint's live model list.
	ModelAccepted bool
	// ForceAccepted: force_web_search was sent and the endpoint did not
	// refuse it.
	ForceAccepted bool
	// Searched is the test call's Result.Searched.
	Searched bool
}

// PairFidelity is the fidelity of a surface and model pair after its test
// call. App surfaces stay app and stand-ins stay approximation. A developer
// tool is model_match only when the endpoint accepts the model, search was
// forced (or turned on, where it cannot be forced) and the call searched.
// Before the first test, a pair shows the surface's default Fidelity.
func PairFidelity(s Spec, t TestOutcome) Fidelity {
	if s.Fidelity != FidelityModelMatch {
		return s.Fidelity
	}
	if t.ModelAccepted && s.Endpoint.Search.on(t.ForceAccepted) && t.Searched {
		return FidelityModelMatch
	}
	return FidelityApproximation
}

func (c SearchControl) on(forceAccepted bool) bool {
	switch c {
	case SearchForceable:
		return forceAccepted
	case SearchEnableOnly, SearchAlways:
		return true
	default:
		return false
	}
}
