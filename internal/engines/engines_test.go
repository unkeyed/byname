package engines

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestEverySurfaceKeyIsRegisteredOnce(t *testing.T) {
	keys := []Surface{
		ChatGPTApp, GeminiApp, GoogleAIMode, GoogleAIOverview, Copilot, BingAIMode,
		Brave, ClaudeCode, GeminiCLI, CodexCLI, ClaudeApp, PerplexityApp,
	}
	if len(All()) != len(keys) {
		t.Fatalf("registry has %d surfaces, want %d", len(All()), len(keys))
	}
	for _, k := range keys {
		if _, ok := Lookup(k); !ok {
			t.Errorf("surface %s is not registered", k)
		}
	}
	if _, ok := Lookup("grok_app"); ok {
		t.Error("Lookup found an unregistered surface")
	}
}

func TestAppSurfacesTakeNoModelAndToolsDo(t *testing.T) {
	for _, s := range All() {
		app := s.Fidelity == FidelityApp
		if app == s.Endpoint.TakesModel() {
			t.Errorf("%s: fidelity %s but TakesModel=%v", s.Surface, s.Fidelity, s.Endpoint.TakesModel())
		}
		if app != (s.Endpoint.Search == SearchByApp) {
			t.Errorf("%s: fidelity %s but search %s", s.Surface, s.Fidelity, s.Endpoint.Search)
		}
	}
}

// TestPairFidelity is the fidelity table over every surface on every route
// it can take, before a test call and after tests that did and did not
// confirm the model and search.
func TestPairFidelity(t *testing.T) {
	const (
		app    = FidelityApp
		match  = FidelityModelMatch
		approx = FidelityApproximation
		none   = Fidelity("")
	)
	confirmed := &TestOutcome{ModelAccepted: true, ForceAccepted: true, Searched: true}
	noSearch := &TestOutcome{ModelAccepted: true, ForceAccepted: true}
	notForced := &TestOutcome{ModelAccepted: true, Searched: true}
	type want struct{ untested, confirmed, noSearch, notForced Fidelity }
	// Per surface and route: the label with no test, after a test that
	// confirmed everything, after one that did not search, and after one
	// whose forced search was refused. none: the route does not serve it.
	table := map[Surface]map[Route]want{
		ChatGPTApp:       {RouteTreg: {app, app, app, app}, RouteScraper: {app, app, app, app}, RouteProvider: {approx, approx, approx, approx}, RouteGateway: {approx, approx, approx, approx}},
		GeminiApp:        {RouteTreg: {app, app, app, app}, RouteScraper: {app, app, app, app}, RouteProvider: {approx, approx, approx, approx}, RouteGateway: {approx, approx, approx, approx}},
		GoogleAIMode:     {RouteTreg: {app, app, app, app}, RouteScraper: {app, app, app, app}, RouteProvider: {approx, approx, approx, approx}, RouteGateway: {approx, approx, approx, approx}},
		GoogleAIOverview: {RouteTreg: {app, app, app, app}, RouteScraper: {app, app, app, app}, RouteProvider: {approx, approx, approx, approx}, RouteGateway: {approx, approx, approx, approx}},
		Copilot:          {RouteTreg: {app, app, app, app}, RouteScraper: {app, app, app, app}, RouteProvider: {approx, approx, approx, approx}, RouteGateway: {approx, approx, approx, approx}},
		BingAIMode:       {RouteTreg: {app, app, app, app}, RouteScraper: {app, app, app, app}, RouteProvider: {approx, approx, approx, approx}, RouteGateway: {approx, approx, approx, approx}},
		Brave:            {RouteTreg: {app, app, app, app}, RouteScraper: {app, app, app, app}, RouteProvider: {none, none, none, none}, RouteGateway: {approx, approx, approx, approx}},
		// Forceable on every route but the gateway, where the model decides.
		ClaudeCode: {RouteTreg: {match, match, approx, approx}, RouteScraper: {match, match, approx, approx}, RouteProvider: {match, match, approx, approx}, RouteGateway: {match, match, approx, match}},
		CodexCLI:   {RouteTreg: {match, match, approx, approx}, RouteScraper: {match, match, approx, approx}, RouteProvider: {match, match, approx, approx}, RouteGateway: {match, match, approx, match}},
		// Gemini turns search on but cannot force it, on every route.
		GeminiCLI:     {RouteTreg: {match, match, approx, match}, RouteScraper: {match, match, approx, match}, RouteProvider: {match, match, approx, match}, RouteGateway: {match, match, approx, match}},
		ClaudeApp:     {RouteTreg: {approx, approx, approx, approx}, RouteScraper: {approx, approx, approx, approx}, RouteProvider: {approx, approx, approx, approx}, RouteGateway: {approx, approx, approx, approx}},
		PerplexityApp: {RouteTreg: {approx, approx, approx, approx}, RouteScraper: {approx, approx, approx, approx}, RouteProvider: {approx, approx, approx, approx}, RouteGateway: {approx, approx, approx, approx}},
	}
	if len(table) != len(All()) {
		t.Fatalf("table covers %d surfaces, the registry has %d", len(table), len(All()))
	}
	for surface, routes := range table {
		for _, r := range Routes {
			w := routes[r]
			rs, err := RouteOf(surface, r)
			if w.untested == none {
				if err == nil {
					t.Errorf("%s on %s: the route serves it, the table says it does not", surface, r)
				}
				continue
			}
			if err != nil {
				t.Errorf("%s on %s: %v", surface, r, err)
				continue
			}
			for _, c := range []struct {
				name string
				t    *TestOutcome
				want Fidelity
			}{{"untested", nil, w.untested}, {"confirmed", confirmed, w.confirmed}, {"no search", noSearch, w.noSearch}, {"force refused", notForced, w.notForced}} {
				if got := PairFidelity(rs, c.t); got != c.want {
					t.Errorf("%s on %s, %s: got %s, want %s", surface, r, c.name, got, c.want)
				}
			}
			if got := PairFidelity(rs, &TestOutcome{ForceAccepted: true, Searched: true}); rs.Spec.Fidelity == match && got != approx {
				t.Errorf("%s on %s: a model the route does not list got %s, want approximation", surface, r, got)
			}
		}
	}
}

func TestEveryDirectProviderStandsInOnce(t *testing.T) {
	for _, s := range All() {
		n := 0
		for _, a := range Adapters() {
			if a.Route == RouteProvider && a.serves(s) {
				n++
			}
		}
		if n > 1 {
			t.Errorf("%s has %d direct providers, want at most one", s.Surface, n)
		}
		if _, err := RouteOf(s.Surface, RouteTreg); err != nil {
			t.Errorf("%s has no Treg route", s.Surface)
		}
		if _, err := RouteOf(s.Surface, RouteScraper); err != nil {
			t.Errorf("%s: its pinned endpoint's provider has no own-key route", s.Surface)
		}
	}
}

func TestEndpointIDSplitsSeriesByRoute(t *testing.T) {
	seen := map[string]Route{}
	for _, r := range Routes {
		rs, err := RouteOf(ClaudeCode, r)
		if err != nil {
			t.Fatal(err)
		}
		id := rs.EndpointID("https://gateway.example/v1")
		if prev, dup := seen[id]; dup {
			t.Errorf("%s and %s store the same endpoint %q, so their answers would share a series", prev, r, id)
		}
		seen[id] = r
	}
	gw, _ := RouteOf(ClaudeCode, RouteGateway)
	if a, b := gw.EndpointID("https://one.example/v1"), gw.EndpointID("https://two.example/v1"); a == b {
		t.Errorf("two gateways share endpoint %q", a)
	}
}

func TestUnwrapRedirect(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"https://www.google.com/url?q=https://a.example/x&sa=U", "https://a.example/x"},
		{"https://www.google.com/url?sa=t&url=https%3A%2F%2Fb.example%2Fy%3Fz%3D1", "https://b.example/y?z=1"},
		{"https://google.co.uk/url?q=https://c.example/", "https://c.example/"},
		{"https://www.google.com/url?q=not-a-url", "https://www.google.com/url?q=not-a-url"},
		{"https://www.google.com/search?q=https://d.example", "https://www.google.com/search?q=https://d.example"},
		{"https://notgoogle.example/url?q=https://e.example", "https://notgoogle.example/url?q=https://e.example"},
		{"https://docs.example.org/rate-limiting", "https://docs.example.org/rate-limiting"},
	} {
		if got := unwrapRedirect(tc.in); got != tc.want {
			t.Errorf("unwrapRedirect(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestDecodeEdgeCases(t *testing.T) {
	read := func(t *testing.T, family, name string) []byte {
		t.Helper()
		b, err := os.ReadFile(filepath.Join("testdata", family, name))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	for _, tc := range []struct {
		name  string
		e     Endpoint
		body  func(t *testing.T) []byte
		check func(t *testing.T, r Result)
	}{
		{
			name: "gpt-5 reasoning item is dropped from the text",
			e:    chatGPTAPI,
			body: func(t *testing.T) []byte {
				return read(t, "dataforseo", chatGPTAPI.ID+".reasoning.synthetic.json")
			},
			check: func(t *testing.T, r Result) {
				if strings.Contains(r.Text, "Contoso") || !strings.Contains(r.Text, "Northwind Cloud") {
					t.Errorf("text kept reasoning or lost the message: %q", r.Text)
				}
				if r.Searched() {
					t.Error("an answer with no citations and no fan-out reports Searched")
				}
			},
		},
		{
			name: "gemini-2.5-pro reasoning is dropped and redirect links resolve",
			e:    geminiAPI,
			body: func(t *testing.T) []byte {
				return read(t, "dataforseo", geminiAPI.ID+".reasoning-redirects.synthetic.json")
			},
			check: func(t *testing.T, r Result) {
				if strings.Contains(r.Text, "Contoso") {
					t.Errorf("text kept reasoning: %q", r.Text)
				}
				want := []string{
					"https://docs.example.org/rate-limiting",
					"https://www.northwind.example/docs/ratelimit",
					"https://vertexaisearch.cloud.google.com/grounding-api-redirect/AUZIYQExampleToken1",
				}
				if !slices.Equal(r.Citations, want) {
					t.Errorf("citations %q, want %q", r.Citations, want)
				}
			},
		},
		{
			name: "a Google search without an AI Overview is a no-answer result",
			e:    googleOrganic,
			body: func(t *testing.T) []byte { return read(t, "cloro", googleOrganic.ID+".json") },
			check: func(t *testing.T, r Result) {
				if r.Status != StatusNoAnswer || r.Text != "" || len(r.Citations) != 0 {
					t.Errorf("got %+v", r)
				}
			},
		},
		{
			name: "a refused forced search is a failure with DataForSEO's reason",
			e:    chatGPTAPI,
			body: func(t *testing.T) []byte {
				return read(t, "dataforseo", chatGPTAPI.ID+".force-refused.synthetic.json")
			},
			check: func(t *testing.T, r Result) {
				if r.Status != StatusFailed || !strings.Contains(r.Error, "40501") {
					t.Errorf("got %+v", r)
				}
			},
		},
		{
			name: "bytes that are not JSON fail without panicking",
			e:    claudeAPI,
			body: func(*testing.T) []byte { return []byte("<html>502 Bad Gateway</html>") },
			check: func(t *testing.T, r Result) {
				if r.Status != StatusFailed || r.Error == "" {
					t.Errorf("got %+v", r)
				}
			},
		},
		{
			name: "cloro reporting failure",
			e:    geminiScrape,
			body: func(*testing.T) []byte {
				return []byte(`{"success":false,"error":{"code":"EXTRACTION_FAILED","message":"no answer rendered"}}`)
			},
			check: func(t *testing.T, r Result) {
				if r.Status != StatusFailed || !strings.Contains(r.Error, "EXTRACTION_FAILED") {
					t.Errorf("got %+v", r)
				}
			},
		},
		{
			name: "serpapi reporting an error",
			e:    bingCopilot,
			body: func(*testing.T) []byte {
				return []byte(`{"search_metadata":{"status":"Error"},"error":"Copilot didn't return any results."}`)
			},
			check: func(t *testing.T, r Result) {
				if r.Status != StatusFailed || !strings.Contains(r.Error, "any results") {
					t.Errorf("got %+v", r)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := tc.body(t)
			r := tc.e.Decode(body)
			if string(r.Raw) != string(body) {
				t.Error("Raw is not the body as received")
			}
			if r.CostUSD != nil {
				t.Errorf("a parser reported cost %v; cost comes only from the route", *r.CostUSD)
			}
			tc.check(t, r)
		})
	}
}
