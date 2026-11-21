package engines

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"
)

type seenRequest struct {
	Method, Path string
	Query        map[string][]string
	Header       http.Header
	Body         any
}

// tregServer records each request and answers with respond.
func tregServer(t *testing.T, respond func(w http.ResponseWriter, r *http.Request)) (*httptest.Server, *[]seenRequest) {
	t.Helper()
	var seen []seenRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var body any
		if len(b) > 0 {
			if err := json.Unmarshal(b, &body); err != nil {
				t.Errorf("request body is not JSON: %s", b)
			}
		}
		seen = append(seen, seenRequest{r.Method, r.URL.Path, r.URL.Query(), r.Header.Clone(), body})
		respond(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

func fixture(t *testing.T, family, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", family, name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func jsonValue(t *testing.T, s string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func ask(s Surface, model string, force bool) Request {
	return Request{
		Question: "What should I look for when choosing a project management tool for a small team?",
		Surface:  s, Model: model, ForceWebSearch: force, Location: "US", Language: "en",
		IdempotencyKey: "run-7/q-3/sm-2/1",
	}
}

func TestTregSendsTheProviderRequest(t *testing.T) {
	const q = "What should I look for when choosing a project management tool for a small team?"
	for _, tc := range []struct {
		req    Request
		method string
		tool   string
		query  map[string][]string
		body   string
	}{
		{ask(ClaudeCode, "claude-sonnet-4-5", true), "POST", claudeAPI.ID, nil,
			`[{"user_prompt":"` + q + `","model_name":"claude-sonnet-4-5","web_search":true,"force_web_search":true,"web_search_country_iso_code":"US"}]`},
		{ask(CodexCLI, "gpt-5", false), "POST", chatGPTAPI.ID, nil,
			`[{"user_prompt":"` + q + `","model_name":"gpt-5","web_search":true,"web_search_country_iso_code":"US"}]`},
		{ask(GeminiCLI, "gemini-2.5-pro", true), "POST", geminiAPI.ID, nil,
			`[{"user_prompt":"` + q + `","model_name":"gemini-2.5-pro","web_search":true}]`},
		{ask(PerplexityApp, "sonar", true), "POST", perplexityAPI.ID, nil,
			`[{"user_prompt":"` + q + `","model_name":"sonar","web_search_country_iso_code":"US"}]`},
		{ask(ChatGPTApp, "", false), "POST", chatGPTScraper.ID, nil,
			`[{"keyword":"` + q + `","location_code":2840,"language_code":"en"}]`},
		{ask(GeminiApp, "", false), "POST", geminiScrape.ID, nil,
			`{"prompt":"` + q + `","country":"US","include":{"markdown":true}}`},
		{ask(BingAIMode, "", false), "POST", bingAIMode.ID, nil,
			`{"prompt":"` + q + `","country":"US","include":{"markdown":true}}`},
		{ask(GoogleAIMode, "", false), "POST", googleAIMode.ID, nil,
			`{"prompt":"` + q + `","gl":"US","hl":"en","include":{"markdown":true}}`},
		{ask(GoogleAIOverview, "", false), "POST", googleOrganic.ID, nil,
			`{"query":"` + q + `","gl":"US","hl":"en","include":{"aioverview":{"markdown":true}}}`},
		{ask(Copilot, "", false), "GET", bingCopilot.ID, map[string][]string{"engine": {"bing_copilot"}, "q": {q}}, ""},
		{ask(Brave, "", false), "GET", braveAIMode.ID, map[string][]string{"engine": {"brave_ai_mode"}, "q": {q}}, ""},
	} {
		t.Run(string(tc.req.Surface), func(t *testing.T) {
			srv, seen := tregServer(t, func(w http.ResponseWriter, r *http.Request) {})
			tr := Treg{Client: srv.Client(), BaseURL: srv.URL, Token: "treg_team_token", Org: "org_1"}
			if _, err := tr.Call(context.Background(), tc.req); err != nil {
				t.Fatal(err)
			}
			if len(*seen) != 1 {
				t.Fatalf("sent %d requests, want 1", len(*seen))
			}
			got := (*seen)[0]
			if got.Method != tc.method || got.Path != "/call/"+tc.tool {
				t.Errorf("sent %s %s, want %s /call/%s", got.Method, got.Path, tc.method, tc.tool)
			}
			if got.Header.Get("X-Treg-Token") != "treg_team_token" || got.Header.Get("Authorization") != "" {
				t.Errorf("auth headers: X-Treg-Token=%q Authorization=%q", got.Header.Get("X-Treg-Token"), got.Header.Get("Authorization"))
			}
			if got.Header.Get("X-Treg-Org") != "org_1" {
				t.Errorf("X-Treg-Org = %q", got.Header.Get("X-Treg-Org"))
			}
			if got.Header.Get("Idempotency-Key") != "run-7/q-3/sm-2/1" {
				t.Errorf("Idempotency-Key = %q", got.Header.Get("Idempotency-Key"))
			}
			if tc.query != nil && !reflect.DeepEqual(got.Query, tc.query) {
				t.Errorf("query %v, want %v", got.Query, tc.query)
			}
			var want any
			if tc.body != "" {
				want = jsonValue(t, tc.body)
			}
			if !reflect.DeepEqual(got.Body, want) {
				t.Errorf("body %v\nwant %v", got.Body, want)
			}
		})
	}
}

func TestTregRejectsBadRequestsWithoutCalling(t *testing.T) {
	srv, seen := tregServer(t, func(w http.ResponseWriter, r *http.Request) {})
	tr := Treg{Client: srv.Client(), BaseURL: srv.URL, Token: "t"}
	for name, req := range map[string]Request{
		"developer tool without a model":  ask(ClaudeCode, "", true),
		"app surface with a model":        ask(ChatGPTApp, "gpt-6", false),
		"unknown surface":                 ask("grok_app", "", false),
		"market name instead of code":     {Question: "q?", Surface: GeminiApp, Location: "United States", Language: "en"},
		"scraper without a location code": {Question: "q?", Surface: ChatGPTApp, Location: "ZZ", Language: "en"},
	} {
		if _, err := tr.Call(context.Background(), req); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	if len(*seen) != 0 {
		t.Errorf("sent %d requests for invalid input", len(*seen))
	}
}

func TestTregReadsCostAndCallID(t *testing.T) {
	body := fixture(t, "dataforseo", claudeAPI.ID+".forced-search.synthetic.json")
	usd := func(v float64) *float64 { return &v }
	for _, tc := range []struct {
		name    string
		headers map[string]string
		want    *float64
	}{
		{"metered call", map[string]string{"X-Treg-Cost-Micro": "55000"}, usd(0.055)},
		{"no cost header is missing, not free", nil, nil},
		{"unreadable cost header is missing", map[string]string{"X-Treg-Cost-Micro": "n/a"}, nil},
		{"idempotent replay records the first charge", map[string]string{
			"X-Treg-Idempotent-Replay": "true", "X-Treg-Cost-Micro": "0", "X-Treg-Original-Cost-Micro": "55000",
		}, usd(0.055)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := tregServer(t, func(w http.ResponseWriter, r *http.Request) {
				for k, v := range tc.headers {
					w.Header().Set(k, v)
				}
				w.Header().Set("X-Treg-Call-Id", "call_abc")
				w.Write(body)
			})
			res, err := Treg{Client: srv.Client(), BaseURL: srv.URL}.Call(context.Background(), ask(ClaudeCode, "claude-sonnet-4-5", true))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(res.CostUSD, tc.want) {
				t.Errorf("cost %v, want %v", deref(res.CostUSD), deref(tc.want))
			}
			if res.CallID != "call_abc" || res.Status != StatusOK || string(res.Raw) != string(body) {
				t.Errorf("call id %q, status %s, raw kept %v", res.CallID, res.Status, string(res.Raw) == string(body))
			}
		})
	}
}

func deref(f *float64) any {
	if f == nil {
		return nil
	}
	return *f
}

func TestTregRefusalIsNotAnEngineFailure(t *testing.T) {
	for _, tc := range []struct {
		status    int
		body      string
		code      string
		retry     time.Duration
		retryHead string
	}{
		{402, `{"error":"insufficient_balance","message":"top up","balance_micro":10}`, "insufficient_balance", 0, ""},
		{429, `{"error":"org_rate_limit","message":"slow down"}`, "org_rate_limit", 7 * time.Second, "7"},
		{503, `{"error":"upstream_busy","message":"busy","retry_after_s":3}`, "upstream_busy", 3 * time.Second, ""},
	} {
		t.Run(tc.code, func(t *testing.T) {
			srv, _ := tregServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Treg-Error", "1")
				if tc.retryHead != "" {
					w.Header().Set("Retry-After", tc.retryHead)
				}
				w.WriteHeader(tc.status)
				io.WriteString(w, tc.body)
			})
			_, err := Treg{Client: srv.Client(), BaseURL: srv.URL}.Call(context.Background(), ask(GeminiApp, "", false))
			var refusal *Refusal
			if !errors.As(err, &refusal) {
				t.Fatalf("got %v, want a *Refusal", err)
			}
			if refusal.StatusCode != tc.status || refusal.Code != tc.code || refusal.RetryAfter != tc.retry {
				t.Errorf("got %+v", refusal)
			}
		})
	}
}

func TestTregProviderErrorIsAFailedResult(t *testing.T) {
	srv, _ := tregServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		io.WriteString(w, `{"success":false,"error":{"code":"UPSTREAM","message":"gemini timed out"}}`)
	})
	res, err := Treg{Client: srv.Client(), BaseURL: srv.URL}.Call(context.Background(), ask(GeminiApp, "", false))
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != StatusFailed || res.Error == "" || len(res.Raw) == 0 || res.CostUSD != nil {
		t.Errorf("got %+v", res)
	}
}

func TestTregConnectionFailureIsAnError(t *testing.T) {
	srv, _ := tregServer(t, func(w http.ResponseWriter, r *http.Request) {})
	srv.Close()
	if _, err := (Treg{Client: srv.Client(), BaseURL: srv.URL}).Call(context.Background(), ask(GeminiApp, "", false)); err == nil {
		t.Error("no error from a closed server")
	}
}

func TestTregResolvesRedirectLinks(t *testing.T) {
	body := fixture(t, "dataforseo", geminiAPI.ID+".reasoning-redirects.synthetic.json")
	srv, _ := tregServer(t, func(w http.ResponseWriter, r *http.Request) { w.Write(body) })
	targets := map[string]string{
		"https://vertexaisearch.cloud.google.com/grounding-api-redirect/AUZIYQExampleToken1": "https://docs.example.org/rate-limiting",
	}
	var looked []string
	resolve := func(_ context.Context, link string) (string, error) {
		looked = append(looked, link)
		return targets[link], nil
	}
	res, err := Treg{Client: srv.Client(), BaseURL: srv.URL, Resolve: resolve}.Call(context.Background(), ask(GeminiCLI, "gemini-2.5-pro", false))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"https://docs.example.org/rate-limiting", "https://www.northwind.example/docs/ratelimit"}
	if !slices.Equal(res.Citations, want) {
		t.Errorf("citations %q, want %q (the resolved link repeats the first and is dropped)", res.Citations, want)
	}
	if len(looked) != 1 {
		t.Errorf("looked up %q, want only the vertexaisearch link", looked)
	}
}

func TestHeadResolverReadsLocation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			t.Errorf("method %s, want HEAD", r.Method)
		}
		if r.URL.Path == "/grounding-api-redirect/abc" {
			http.Redirect(w, r, "https://blog.example.net/post", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	resolve := HeadResolver(srv.Client())
	got, err := resolve(context.Background(), srv.URL+"/grounding-api-redirect/abc")
	if err != nil || got != "https://blog.example.net/post" {
		t.Errorf("got %q, %v", got, err)
	}
	if _, err := resolve(context.Background(), srv.URL+"/plain"); err == nil {
		t.Error("a page that does not redirect resolved")
	}
}

func TestTregListModels(t *testing.T) {
	body := fixture(t, "dataforseo", claudeAPI.Models+".json")
	srv, seen := tregServer(t, func(w http.ResponseWriter, r *http.Request) { w.Write(body) })
	tr := Treg{Client: srv.Client(), BaseURL: srv.URL, Token: "t"}
	models, err := tr.ListModels(context.Background(), ClaudeApp)
	if err != nil {
		t.Fatal(err)
	}
	if want := []Model{{Name: "claude-sonnet-5-5", WebSearch: true}}; !reflect.DeepEqual(models, want) {
		t.Errorf("models %+v, want %+v", models, want)
	}
	if got := (*seen)[0]; got.Method != "GET" || got.Path != "/call/"+claudeAPI.Models || got.Body != nil {
		t.Errorf("sent %s %s with body %v", got.Method, got.Path, got.Body)
	}
	if _, err := tr.ListModels(context.Background(), GoogleAIMode); err == nil {
		t.Error("an app surface returned a model list")
	}
}
