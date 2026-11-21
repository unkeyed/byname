package engines

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
)

// Route is how a surface's calls reach a provider. The values are stored in
// surface.route.
type Route string

const (
	// RouteTreg calls the surface's pinned endpoint through Treg with the
	// one Treg token. The default.
	RouteTreg Route = "treg"
	// RouteScraper calls the same pinned endpoint at its own provider
	// (DataForSEO, cloro or SerpApi) with that provider's key. The answer
	// is the same one Treg would return, so fidelity does not change.
	RouteScraper Route = "scraper"
	// RouteProvider calls a model API directly with its key: OpenAI,
	// Anthropic, Google or Perplexity.
	RouteProvider Route = "provider"
	// RouteGateway calls an OpenAI-compatible gateway at its base URL.
	RouteGateway Route = "gateway"
)

var Routes = []Route{RouteTreg, RouteScraper, RouteProvider, RouteGateway}

// answersFromAPI reports whether the route asks a model API rather than
// reading the app, so an app surface on it is only an approximation.
func (r Route) answersFromAPI() bool { return r == RouteProvider || r == RouteGateway }

// CredentialKind is the kind of key a route calls with. The values are
// stored in credential.kind.
type CredentialKind string

const (
	CredentialTreg       CredentialKind = "treg"
	CredentialOpenAI     CredentialKind = "openai"
	CredentialAnthropic  CredentialKind = "anthropic"
	CredentialPerplexity CredentialKind = "perplexity"
	CredentialGoogle     CredentialKind = "google"
	CredentialDataForSEO CredentialKind = "dataforseo"
	CredentialCloro      CredentialKind = "cloro"
	CredentialSerpApi    CredentialKind = "serpapi"
	CredentialGateway    CredentialKind = "gateway"
)

var CredentialKinds = []CredentialKind{
	CredentialTreg, CredentialOpenAI, CredentialAnthropic, CredentialPerplexity, CredentialGoogle,
	CredentialDataForSEO, CredentialCloro, CredentialSerpApi, CredentialGateway,
}

// CostReport is how an adapter learns what a call cost. A call with no
// reported cost is stored as missing, never as $0 and never as an estimate.
type CostReport string

const (
	// CostMetered: Treg's X-Treg-Cost-Micro header on every call.
	CostMetered CostReport = "metered"
	// CostInBody: the provider's response carries the cost.
	CostInBody CostReport = "in_body"
	// CostWhenReported: some gateways put a cost in the response, some do not.
	CostWhenReported CostReport = "when_reported"
	// CostNone: billed by tokens with no dollar figure in the response.
	CostNone CostReport = "none"
)

// Adapter makes one route's calls.
type Adapter interface {
	Call(ctx context.Context, req Request) (Result, error)
	// ListModels reads the models the route accepts for a surface.
	ListModels(ctx context.Context, s Surface) ([]Model, error)
}

// Conn is what an adapter needs to call: the credential's secret and where
// to send the calls.
type Conn struct {
	Secret string
	// Org is Treg's X-Treg-Org, which only a personal Treg token needs.
	Org string
	// BaseURL is a gateway's base URL. For every other adapter it replaces
	// the provider's public URL, for tests and local fakes.
	BaseURL string
	Client  *http.Client
	// Resolve follows redirect links whose target is not in the URL.
	Resolve Resolver
}

// AdapterSpec is one adapter's row in the registry: the route and key it
// calls with, the surfaces it serves, how it searches and how it reports
// cost.
type AdapterSpec struct {
	Route      Route
	Credential CredentialKind
	// Title names the provider in the UI, such as "OpenAI".
	Title  string
	serves func(Spec) bool
	// Search is how the adapter turns on web search. Empty means the pinned
	// endpoint's, for the routes that call that endpoint.
	Search SearchControl
	Cost   CostReport
	// Endpoint is the id stored on each answer of a provider route.
	Endpoint string
	new      func(RouteSpec, Conn) Adapter
	// probe makes one free call that proves the key works, or is nil when
	// the provider has no free call.
	probe func(context.Context, Conn) error
}

func everySurface(Spec) bool { return true }

func surfaces(ss ...Surface) func(Spec) bool {
	return func(s Spec) bool { return slices.Contains(ss, s.Surface) }
}

func endpointFamily(family string) func(Spec) bool {
	return func(s Spec) bool { return s.Endpoint.Family == family }
}

// adapters is the one list of adapters. Each direct provider stands in for
// the surfaces whose model it serves; Brave has none.
var adapters = []AdapterSpec{
	{Route: RouteTreg, Credential: CredentialTreg, Title: "Treg", serves: everySurface, Cost: CostMetered, new: newTreg, probe: probeTreg},
	{Route: RouteScraper, Credential: CredentialDataForSEO, Title: "DataForSEO", serves: endpointFamily("dataforseo"), Cost: CostInBody, new: newScraper, probe: probeDataForSEO},
	{Route: RouteScraper, Credential: CredentialCloro, Title: "cloro", serves: endpointFamily("cloro"), Cost: CostNone, new: newScraper},
	{Route: RouteScraper, Credential: CredentialSerpApi, Title: "SerpApi", serves: endpointFamily("serpapi"), Cost: CostNone, new: newScraper, probe: probeSerpApi},
	{Route: RouteProvider, Credential: CredentialOpenAI, Title: "OpenAI", serves: surfaces(ChatGPTApp, Copilot, BingAIMode, CodexCLI),
		Search: SearchForceable, Cost: CostNone, Endpoint: "openai.responses", new: newOpenAI, probe: probeOpenAI},
	{Route: RouteProvider, Credential: CredentialAnthropic, Title: "Anthropic", serves: surfaces(ClaudeCode, ClaudeApp),
		Search: SearchForceable, Cost: CostNone, Endpoint: "anthropic.messages", new: newAnthropic, probe: probeAnthropic},
	{Route: RouteProvider, Credential: CredentialGoogle, Title: "Google Gemini", serves: surfaces(GeminiApp, GoogleAIMode, GoogleAIOverview, GeminiCLI),
		Search: SearchEnableOnly, Cost: CostNone, Endpoint: "google.generate-content", new: newGoogle, probe: probeGoogle},
	{Route: RouteProvider, Credential: CredentialPerplexity, Title: "Perplexity", serves: surfaces(PerplexityApp),
		Search: SearchAlways, Cost: CostInBody, Endpoint: "perplexity.chat-completions", new: newPerplexity},
	{Route: RouteGateway, Credential: CredentialGateway, Title: "OpenAI-compatible gateway", serves: everySurface,
		Search: SearchByModel, Cost: CostWhenReported, new: newGateway, probe: probeGateway},
}

// Adapters returns every adapter row in registry order.
func Adapters() []AdapterSpec { return append([]AdapterSpec(nil), adapters...) }

// RouteSpec is one surface on one route: what it calls, how it searches,
// and the fidelity its answers can have.
type RouteSpec struct {
	Spec    Spec
	Route   Route
	Adapter AdapterSpec
}

// RouteOf returns a surface on a route, or an error when the route does not
// serve the surface (Brave has no direct provider).
func RouteOf(s Surface, r Route) (RouteSpec, error) {
	spec, ok := Lookup(s)
	if !ok {
		return RouteSpec{}, fmt.Errorf("engines: unknown surface %q", s)
	}
	for _, a := range adapters {
		if a.Route == r && a.serves(spec) {
			return RouteSpec{Spec: spec, Route: r, Adapter: a}, nil
		}
	}
	return RouteSpec{}, fmt.Errorf("engines: %s has no %s route", spec.Title, r)
}

// Offered lists the routes a surface can take, Treg first.
func Offered(s Surface) []RouteSpec {
	var out []RouteSpec
	for _, r := range Routes {
		if rs, err := RouteOf(s, r); err == nil {
			out = append(out, rs)
		}
	}
	return out
}

func (rs RouteSpec) Credential() CredentialKind { return rs.Adapter.Credential }

// Search is how this route turns on web search for the surface.
func (rs RouteSpec) Search() SearchControl {
	if rs.Adapter.Search != "" {
		return rs.Adapter.Search
	}
	return rs.Spec.Endpoint.Search
}

// TakesModel reports whether the surface's pairs name a model on this
// route. A model API always needs one, even standing in for an app.
func (rs RouteSpec) TakesModel() bool {
	if rs.Route.answersFromAPI() {
		return true
	}
	return rs.Spec.Endpoint.TakesModel()
}

// EndpointID is stored on each answer, so changing route, provider or
// gateway starts a new series.
func (rs RouteSpec) EndpointID(baseURL string) string {
	switch rs.Route {
	case RouteTreg:
		return rs.Spec.Endpoint.ID
	case RouteScraper:
		return string(RouteScraper) + ":" + rs.Spec.Endpoint.ID
	case RouteGateway:
		host := baseURL
		if u, err := url.Parse(baseURL); err == nil && u.Host != "" {
			host = u.Host
		}
		return string(RouteGateway) + ":" + host
	}
	return rs.Adapter.Endpoint
}

// SampleCostUSD is what one call cost when sampled, for the estimate shown
// before the first check, or nil when no sample exists for this route.
func (rs RouteSpec) SampleCostUSD() *float64 {
	if rs.Route == RouteTreg || rs.Route == RouteScraper {
		return rs.Spec.Endpoint.SampleCostUSD
	}
	return nil
}

// Bind returns the adapter that makes this route's calls with c.
func (rs RouteSpec) Bind(c Conn) Adapter { return rs.Adapter.new(rs, c) }

// ErrNoProbe means the provider has no free call that proves a key works;
// a pair's test call proves it instead.
var ErrNoProbe = errors.New("engines: this provider has no free call to test a key with; test a surface that uses it")

// Probe makes one free call with a key of kind and returns why it failed.
func Probe(ctx context.Context, kind CredentialKind, c Conn) error {
	for _, a := range adapters {
		if a.Credential == kind {
			if a.probe == nil {
				return ErrNoProbe
			}
			return a.probe(ctx, c)
		}
	}
	return fmt.Errorf("engines: unknown credential kind %q", kind)
}

// TestOutcome is what a surface and model pair's test call showed.
type TestOutcome struct {
	// ModelAccepted: the model is on the route's live model list, or the
	// surface takes no model.
	ModelAccepted bool
	// ForceAccepted: force_web_search was sent and the endpoint did not
	// refuse it.
	ForceAccepted bool
	// Searched is the test call's Result.Searched.
	Searched bool
}

// PairFidelity is the one place fidelity is decided. An app surface is App
// answer when its app is read (Treg or the scraping provider's own key)
// and an Approximation when a model API stands in (a direct key or a
// gateway). A developer tool is Same model and search on any route while
// the route accepts the model, search was forced (or turned on, where it
// cannot be forced) and the test call searched; otherwise an
// Approximation. A stand-in surface is always an Approximation. With no
// test yet (t nil), a pair shows the label it is expected to get.
func PairFidelity(rs RouteSpec, t *TestOutcome) Fidelity {
	f := rs.Spec.Fidelity
	if f == FidelityApp && rs.Route.answersFromAPI() {
		return FidelityApproximation
	}
	if f != FidelityModelMatch || t == nil {
		return f
	}
	if t.ModelAccepted && rs.Search().on(t.ForceAccepted) && t.Searched {
		return FidelityModelMatch
	}
	return FidelityApproximation
}

func newTreg(_ RouteSpec, c Conn) Adapter {
	return Treg{Client: c.Client, BaseURL: c.BaseURL, Token: c.Secret, Org: c.Org, Resolve: c.Resolve}
}

func probeTreg(ctx context.Context, c Conn) error {
	_, err := Treg{Client: c.Client, BaseURL: c.BaseURL, Token: c.Secret, Org: c.Org}.ListModels(ctx, ClaudeCode)
	return err
}
