package engines_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/unkeyed/byname/internal/engines"
	"github.com/unkeyed/byname/internal/engines/providertest"
	"github.com/unkeyed/byname/internal/engines/tregtest"
)

const question = "What's the best way to add rate limiting to a REST API?"

func ask(s engines.Surface, model string, force bool) engines.Request {
	return engines.Request{Question: question, Surface: s, Model: model, ForceWebSearch: force, Location: "US", Language: "en"}
}

func route(t *testing.T, s engines.Surface, r engines.Route) engines.RouteSpec {
	t.Helper()
	rs, err := engines.RouteOf(s, r)
	if err != nil {
		t.Fatal(err)
	}
	return rs
}

// TestEveryOfferedRouteAnswers binds every surface to every route the
// registry offers it, against the fakes, and asks one question.
func TestEveryOfferedRouteAnswers(t *testing.T) {
	ctx := context.Background()
	providers := providertest.New(t)
	treg := tregtest.New(t)
	for _, spec := range engines.All() {
		for _, rs := range engines.Offered(spec.Surface) {
			t.Run(string(spec.Surface)+"/"+string(rs.Route), func(t *testing.T) {
				conn := providers.Conn(rs.Credential())
				if rs.Route == engines.RouteTreg {
					conn = engines.Conn{Secret: "treg-token", BaseURL: treg.URL, Client: treg.Client()}
				}
				a := rs.Bind(conn)
				var model string
				if rs.TakesModel() {
					models, err := a.ListModels(ctx, spec.Surface)
					if err != nil || len(models) == 0 {
						t.Fatalf("model list: %v, %d models", err, len(models))
					}
					model = models[0].Name
				}
				res, err := a.Call(ctx, ask(spec.Surface, model, rs.Search() == engines.SearchForceable))
				if err != nil {
					t.Fatal(err)
				}
				if res.Status != engines.StatusOK || res.Text == "" || len(res.Raw) == 0 {
					t.Errorf("got status %s, error %q, %d bytes of text", res.Status, res.Error, len(res.Text))
				}
			})
		}
	}
	for _, c := range providers.Calls() {
		if !c.Authorized {
			t.Errorf("%s %s %s was sent without the provider's auth", c.Kind, c.Method, c.Path)
		}
	}
}

// TestRoutesSendTheDocumentedRequest checks each adapter's method, path,
// auth and body against the provider's documented API, and the cost it
// reads: none for token-billed APIs, the body's for DataForSEO, Perplexity
// and a gateway that reports one.
func TestRoutesSendTheDocumentedRequest(t *testing.T) {
	webSearchUS := `"user_location":{"type":"approximate","country":"US"}`
	for _, tc := range []struct {
		surface engines.Surface
		route   engines.Route
		model   string
		force   bool
		method  string
		path    string
		query   map[string]string
		header  map[string]string
		body    string
		cost    *float64
	}{
		{
			surface: engines.CodexCLI, route: engines.RouteProvider, model: "gpt-4.1", force: true,
			method: "POST", path: "/v1/responses",
			header: map[string]string{"Authorization": "Bearer providertest-key"},
			body:   `{"model":"gpt-4.1","input":"` + question + `","tools":[{"type":"web_search",` + webSearchUS + `}],"tool_choice":"required"}`,
		},
		{
			surface: engines.ChatGPTApp, route: engines.RouteProvider, model: "gpt-4.1",
			method: "POST", path: "/v1/responses",
			body: `{"model":"gpt-4.1","input":"` + question + `","tools":[{"type":"web_search",` + webSearchUS + `}]}`,
		},
		{
			surface: engines.ClaudeCode, route: engines.RouteProvider, model: "claude-sonnet-4-5", force: true,
			method: "POST", path: "/v1/messages",
			header: map[string]string{"x-api-key": "providertest-key", "anthropic-version": "2023-06-01"},
			body: `{"model":"claude-sonnet-4-5","max_tokens":16000,"messages":[{"role":"user","content":"` + question + `"}],` +
				`"tools":[{"type":"web_search_20250305","name":"web_search",` + webSearchUS + `}],"tool_choice":{"type":"tool","name":"web_search"}}`,
		},
		{
			surface: engines.ClaudeApp, route: engines.RouteProvider, model: "claude-sonnet-4-5",
			method: "POST", path: "/v1/messages",
			body: `{"model":"claude-sonnet-4-5","max_tokens":16000,"messages":[{"role":"user","content":"` + question + `"}],` +
				`"tools":[{"type":"web_search_20250305","name":"web_search",` + webSearchUS + `}]}`,
		},
		{
			surface: engines.GeminiCLI, route: engines.RouteProvider, model: "gemini-2.5-pro",
			method: "POST", path: "/v1beta/models/gemini-2.5-pro:generateContent",
			header: map[string]string{"x-goog-api-key": "providertest-key"},
			body:   `{"contents":[{"role":"user","parts":[{"text":"` + question + `"}]}],"tools":[{"google_search":{}}]}`,
		},
		{
			surface: engines.PerplexityApp, route: engines.RouteProvider, model: "sonar",
			method: "POST", path: "/chat/completions",
			header: map[string]string{"Authorization": "Bearer providertest-key"},
			body:   `{"model":"sonar","messages":[{"role":"user","content":"` + question + `"}],"web_search_options":{"user_location":{"country":"US"}}}`,
			cost:   new(0.005066),
		},
		{
			surface: engines.Brave, route: engines.RouteGateway, model: "anthropic/claude-sonnet-4.5",
			method: "POST", path: "/chat/completions",
			header: map[string]string{"Authorization": "Bearer providertest-key"},
			body:   `{"model":"anthropic/claude-sonnet-4.5","messages":[{"role":"user","content":"` + question + `"}]}`,
			cost:   new(0.02341),
		},
		{
			surface: engines.ClaudeCode, route: engines.RouteScraper, model: "claude-sonnet-4-5", force: true,
			method: "POST", path: "/v3/ai_optimization/claude/llm_responses/live",
			header: map[string]string{"Authorization": "Basic cHJvdmlkZXJ0ZXN0LWxvZ2luOnByb3ZpZGVydGVzdC1wYXNzd29yZA=="},
			body:   `[{"user_prompt":"` + question + `","model_name":"claude-sonnet-4-5","web_search":true,"force_web_search":true,"web_search_country_iso_code":"US"}]`,
			cost:   new(0.055),
		},
		{
			surface: engines.ChatGPTApp, route: engines.RouteScraper,
			method: "POST", path: "/v3/ai_optimization/chat_gpt/llm_scraper/live/advanced",
			body: `[{"keyword":"` + question + `","location_code":2840,"language_code":"en"}]`,
			cost: new(0.004),
		},
		{
			surface: engines.GeminiApp, route: engines.RouteScraper,
			method: "POST", path: "/v1/monitor/gemini",
			header: map[string]string{"Authorization": "Bearer providertest-key"},
			body:   `{"prompt":"` + question + `","country":"US","include":{"markdown":true}}`,
		},
		{
			surface: engines.GoogleAIMode, route: engines.RouteScraper,
			method: "POST", path: "/v1/monitor/aimode",
			body: `{"prompt":"` + question + `","gl":"US","hl":"en","include":{"markdown":true}}`,
		},
		{
			surface: engines.GoogleAIOverview, route: engines.RouteScraper,
			method: "POST", path: "/v1/monitor/google",
			body: `{"query":"` + question + `","gl":"US","hl":"en","include":{"aioverview":{"markdown":true}}}`,
		},
		{
			surface: engines.BingAIMode, route: engines.RouteScraper,
			method: "POST", path: "/v1/monitor/bing/aimode",
			body: `{"prompt":"` + question + `","country":"US","include":{"markdown":true}}`,
		},
		{
			surface: engines.Copilot, route: engines.RouteScraper,
			method: "GET", path: "/search.json",
			query: map[string]string{"engine": "bing_copilot", "q": question, "api_key": "providertest-key"},
		},
	} {
		t.Run(string(tc.surface)+"/"+string(tc.route), func(t *testing.T) {
			srv := providertest.New(t)
			rs := route(t, tc.surface, tc.route)
			res, err := rs.Bind(srv.Conn(rs.Credential())).Call(context.Background(), ask(tc.surface, tc.model, tc.force))
			if err != nil {
				t.Fatal(err)
			}
			if res.Status != engines.StatusOK {
				t.Errorf("status %s: %s", res.Status, res.Error)
			}
			calls := srv.Calls()
			if len(calls) != 1 {
				t.Fatalf("%d calls, want 1", len(calls))
			}
			c := calls[0]
			if c.Method != tc.method || c.Path != tc.path || !c.Authorized {
				t.Errorf("sent %s %s (authorized %v), want %s %s", c.Method, c.Path, c.Authorized, tc.method, tc.path)
			}
			for k, v := range tc.header {
				if got := c.Header.Get(k); got != v {
					t.Errorf("header %s = %q, want %q", k, got, v)
				}
			}
			for k, v := range tc.query {
				if got := c.Query.Get(k); got != v {
					t.Errorf("query %s = %q, want %q", k, got, v)
				}
			}
			if tc.body != "" {
				var got, want any
				if err := json.Unmarshal(c.Body, &got); err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal([]byte(tc.body), &want); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("body %s\nwant %s", c.Body, tc.body)
				}
			}
			switch {
			case tc.cost == nil && res.CostUSD != nil:
				t.Errorf("cost %v, want none: %s reports no dollar cost", *res.CostUSD, rs.Adapter.Title)
			case tc.cost != nil && (res.CostUSD == nil || *res.CostUSD != *tc.cost):
				t.Errorf("cost %v, want %v", res.CostUSD, *tc.cost)
			}
		})
	}
}

func TestGatewayWithoutCostLeavesItMissing(t *testing.T) {
	srv := providertest.New(t)
	body, err := json.Marshal(map[string]any{
		"model":   "perplexity/sonar",
		"choices": []any{map[string]any{"message": map[string]any{"content": "Northwind Cloud limits requests per key."}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	srv.Respond(engines.CredentialGateway, "POST", "/chat/completions", http.StatusOK, body)
	rs := route(t, engines.ClaudeCode, engines.RouteGateway)
	res, err := rs.Bind(srv.Conn(engines.CredentialGateway)).Call(context.Background(), ask(engines.ClaudeCode, "perplexity/sonar", false))
	if err != nil || res.Status != engines.StatusOK || res.CostUSD != nil {
		t.Errorf("got %+v, %v; want an answer with no cost", res, err)
	}
}

// TestProviderErrorsAreFailedResults: a provider's HTTP error is an answer
// that failed, with the provider's reason, never a Go error and never a cost.
func TestProviderErrorsAreFailedResults(t *testing.T) {
	for _, tc := range []struct {
		surface engines.Surface
		route   engines.Route
		model   string
		reason  string
	}{
		{engines.CodexCLI, engines.RouteProvider, "gpt-4.1", "provider HTTP 401: openai: invalid_api_key"},
		{engines.ClaudeCode, engines.RouteProvider, "claude-sonnet-4-5", "provider HTTP 401: anthropic: authentication_error: invalid x-api-key"},
		{engines.GeminiCLI, engines.RouteProvider, "gemini-2.5-pro", "provider HTTP 400: google: 400 INVALID_ARGUMENT"},
		{engines.PerplexityApp, engines.RouteProvider, "sonar", "provider HTTP 401: perplexity:"},
		{engines.ClaudeApp, engines.RouteGateway, "anthropic/claude-sonnet-4.5", "provider HTTP 401: gateway: 401: No auth credentials found"},
		{engines.ClaudeCode, engines.RouteScraper, "claude-sonnet-4-5", "provider HTTP 401: dataforseo 40100"},
		{engines.GeminiApp, engines.RouteScraper, "", "provider HTTP 401: cloro:"},
		{engines.Brave, engines.RouteScraper, "", "provider HTTP 401: serpapi: Invalid API key"},
	} {
		t.Run(string(tc.surface)+"/"+string(tc.route), func(t *testing.T) {
			srv := providertest.New(t)
			rs := route(t, tc.surface, tc.route)
			conn := srv.Conn(rs.Credential())
			conn.Secret = "wrong:key"
			res, err := rs.Bind(conn).Call(context.Background(), ask(tc.surface, tc.model, false))
			if err != nil {
				t.Fatal(err)
			}
			if res.Status != engines.StatusFailed || !strings.HasPrefix(res.Error, tc.reason) {
				t.Errorf("got %s %q, want failed %q", res.Status, res.Error, tc.reason)
			}
			if len(res.Raw) == 0 || res.CostUSD != nil {
				t.Errorf("raw %d bytes, cost %v", len(res.Raw), res.CostUSD)
			}
		})
	}
}

// captureTransport answers every request with an empty 200 and keeps the
// URLs it was asked for.
type captureTransport struct{ urls []string }

func (c *captureTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	c.urls = append(c.urls, r.URL.String())
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("{}")), Header: http.Header{}, Request: r}, nil
}

// TestEmptyBaseURLCallsThePublicAPI: with no Conn.BaseURL each adapter calls
// its provider's public URL, and a BaseURL with a path prefix replaces it.
func TestEmptyBaseURLCallsThePublicAPI(t *testing.T) {
	for _, tc := range []struct {
		surface engines.Surface
		route   engines.Route
		model   string
		want    string
	}{
		{engines.CodexCLI, engines.RouteProvider, "gpt-4.1", "https://api.openai.com/v1/responses"},
		{engines.ClaudeCode, engines.RouteProvider, "claude-sonnet-4-5", "https://api.anthropic.com/v1/messages"},
		{engines.GeminiCLI, engines.RouteProvider, "gemini-2.5-pro", "https://generativelanguage.googleapis.com/v1beta/models/gemini-2.5-pro:generateContent"},
		{engines.PerplexityApp, engines.RouteProvider, "sonar", "https://api.perplexity.ai/chat/completions"},
		{engines.ClaudeCode, engines.RouteScraper, "claude-sonnet-4-5", "https://api.dataforseo.com/v3/ai_optimization/claude/llm_responses/live"},
		{engines.GoogleAIMode, engines.RouteScraper, "", "https://api.cloro.dev/v1/monitor/aimode"},
		{engines.Brave, engines.RouteScraper, "", "https://serpapi.com/search.json?"},
	} {
		rs := route(t, tc.surface, tc.route)
		for _, base := range []string{"", "http://localhost:59103/fake"} {
			ct := &captureTransport{}
			conn := engines.Conn{Secret: "login:password", BaseURL: base, Client: &http.Client{Transport: ct}}
			if _, err := rs.Bind(conn).Call(context.Background(), ask(tc.surface, tc.model, false)); err != nil {
				t.Fatal(err)
			}
			want := tc.want
			if base != "" {
				_, path, _ := strings.Cut(strings.TrimPrefix(want, "https://"), "/")
				want = base + "/" + path
			}
			if len(ct.urls) != 1 || !strings.HasPrefix(ct.urls[0], want) {
				t.Errorf("%s on %s with base %q called %q, want %s", tc.surface, tc.route, base, ct.urls, want)
			}
		}
	}
	rs := route(t, engines.ClaudeCode, engines.RouteGateway)
	if _, err := rs.Bind(engines.Conn{Secret: "k"}).Call(context.Background(), ask(engines.ClaudeCode, "m", false)); err == nil {
		t.Error("a gateway with no base URL made a call")
	}
}

func TestRequestsTheRouteCannotServeAreRefused(t *testing.T) {
	srv := providertest.New(t)
	for _, tc := range []struct {
		name    string
		surface engines.Surface
		route   engines.Route
		req     engines.Request
	}{
		{"a model API standing in for an app needs a model", engines.ChatGPTApp, engines.RouteProvider, ask(engines.ChatGPTApp, "", false)},
		{"a gateway needs a model", engines.Copilot, engines.RouteGateway, ask(engines.Copilot, "", false)},
		{"a scraped app takes no model", engines.GeminiApp, engines.RouteScraper, ask(engines.GeminiApp, "gemini-2.5-pro", false)},
		{"Gemini cannot force search", engines.GeminiCLI, engines.RouteProvider, ask(engines.GeminiCLI, "gemini-2.5-pro", true)},
		{"Perplexity always searches", engines.PerplexityApp, engines.RouteProvider, ask(engines.PerplexityApp, "sonar", true)},
		{"a gateway cannot force search", engines.ClaudeCode, engines.RouteGateway, ask(engines.ClaudeCode, "openai/gpt-4.1", true)},
		{"another surface", engines.ClaudeCode, engines.RouteProvider, ask(engines.ClaudeApp, "claude-sonnet-4-5", false)},
		{"an empty question", engines.CodexCLI, engines.RouteProvider, engines.Request{Surface: engines.CodexCLI, Model: "gpt-4.1", Location: "US", Language: "en"}},
	} {
		rs := route(t, tc.surface, tc.route)
		if _, err := rs.Bind(srv.Conn(rs.Credential())).Call(context.Background(), tc.req); err == nil {
			t.Errorf("%s: no error", tc.name)
		}
	}
	rs := route(t, engines.ClaudeCode, engines.RouteScraper)
	conn := srv.Conn(engines.CredentialDataForSEO)
	conn.Secret = "no-colon"
	_, err := rs.Bind(conn).Call(context.Background(), ask(engines.ClaudeCode, "claude-sonnet-4-5", false))
	if err == nil || !strings.Contains(err.Error(), "login:password") {
		t.Errorf("a DataForSEO secret without login:password gave %v", err)
	}
	if n := len(srv.Calls()); n != 0 {
		t.Errorf("refused requests reached the provider %d times", n)
	}
}

func TestGeminiRedirectLinksResolve(t *testing.T) {
	srv := providertest.New(t)
	conn := srv.Conn(engines.CredentialGoogle)
	conn.Resolve = func(_ context.Context, link string) (string, error) {
		if strings.HasSuffix(link, "Token1") {
			return "https://docs.example.org/rate-limiting", nil
		}
		return "", errors.New("no redirect")
	}
	rs := route(t, engines.GeminiCLI, engines.RouteProvider)
	res, err := rs.Bind(conn).Call(context.Background(), ask(engines.GeminiCLI, "gemini-2.5-pro", false))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"https://docs.example.org/rate-limiting",
		"https://vertexaisearch.cloud.google.com/grounding-api-redirect/AUZIYQExampleToken2",
	}
	if !slices.Equal(res.Citations, want) {
		t.Errorf("citations %q, want %q", res.Citations, want)
	}
	if strings.Contains(res.Text, "Contoso") || !res.Searched() {
		t.Errorf("thought part kept or no search: %+v", res)
	}
}

func TestModelLists(t *testing.T) {
	srv := providertest.New(t)
	ctx := context.Background()
	for _, tc := range []struct {
		surface engines.Surface
		route   engines.Route
		want    []string
	}{
		{engines.CodexCLI, engines.RouteProvider, []string{"gpt-4.1", "gpt-4.1-mini", "gpt-5", "gpt-5.4-mini"}},
		{engines.ClaudeCode, engines.RouteProvider, []string{"claude-opus-5-5", "claude-sonnet-4-5-20250929", "claude-haiku-4-5-20251001"}},
		{engines.GeminiCLI, engines.RouteProvider, []string{"gemini-2.5-pro", "gemini-2.5-flash"}},
		{engines.PerplexityApp, engines.RouteProvider, []string{"sonar", "sonar-pro", "sonar-reasoning-pro", "sonar-deep-research"}},
		{engines.Brave, engines.RouteGateway, []string{"openai/gpt-4.1", "anthropic/claude-sonnet-4.5", "perplexity/sonar"}},
	} {
		rs := route(t, tc.surface, tc.route)
		models, err := rs.Bind(srv.Conn(rs.Credential())).ListModels(ctx, tc.surface)
		if err != nil {
			t.Fatalf("%s on %s: %v", tc.surface, tc.route, err)
		}
		var names []string
		for _, m := range models {
			names = append(names, m.Name)
		}
		if !slices.Equal(names, tc.want) {
			t.Errorf("%s on %s: %q, want %q", tc.surface, tc.route, names, tc.want)
		}
	}
	dfs := route(t, engines.ClaudeCode, engines.RouteScraper)
	models, err := dfs.Bind(srv.Conn(engines.CredentialDataForSEO)).ListModels(ctx, engines.ClaudeCode)
	if err != nil || len(models) == 0 || !models[0].WebSearch {
		t.Errorf("DataForSEO's own Claude model list: %v, %+v", err, models)
	}
	cloro := route(t, engines.GeminiApp, engines.RouteScraper)
	if _, err := cloro.Bind(srv.Conn(engines.CredentialCloro)).ListModels(ctx, engines.GeminiApp); err == nil {
		t.Error("a scraped app listed models")
	}
}

func TestProbes(t *testing.T) {
	srv := providertest.New(t)
	ctx := context.Background()
	for _, kind := range engines.CredentialKinds {
		if kind == engines.CredentialTreg {
			continue
		}
		t.Run(string(kind), func(t *testing.T) {
			before := len(srv.Calls())
			err := engines.Probe(ctx, kind, srv.Conn(kind))
			if kind == engines.CredentialCloro || kind == engines.CredentialPerplexity {
				if !errors.Is(err, engines.ErrNoProbe) || len(srv.Calls()) != before {
					t.Errorf("got %v after %d calls, want ErrNoProbe and no call", err, len(srv.Calls())-before)
				}
				return
			}
			if err != nil {
				t.Errorf("right key: %v", err)
			}
			wrong := srv.Conn(kind)
			wrong.Secret = "wrong:key"
			if err := engines.Probe(ctx, kind, wrong); err == nil {
				t.Error("wrong key passed the probe")
			}
		})
	}
}
