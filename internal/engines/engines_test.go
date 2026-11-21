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

func TestPairFidelity(t *testing.T) {
	claudeCode, _ := Lookup(ClaudeCode)
	geminiCLI, _ := Lookup(GeminiCLI)
	codex, _ := Lookup(CodexCLI)
	claudeApp, _ := Lookup(ClaudeApp)
	chatgpt, _ := Lookup(ChatGPTApp)
	all := TestOutcome{ModelAccepted: true, ForceAccepted: true, Searched: true}

	for _, tc := range []struct {
		name string
		spec Spec
		got  TestOutcome
		want Fidelity
	}{
		{"claude code forced and searched", claudeCode, all, FidelityModelMatch},
		{"claude code allowed but did not search", claudeCode, TestOutcome{ModelAccepted: true, ForceAccepted: true}, FidelityApproximation},
		{"model not on the list", claudeCode, TestOutcome{ForceAccepted: true, Searched: true}, FidelityApproximation},
		{"codex gpt-5 refuses forced search", codex, TestOutcome{ModelAccepted: true, Searched: true}, FidelityApproximation},
		{"codex gpt-4.1 forced and searched", codex, all, FidelityModelMatch},
		{"gemini search turned on counts without force", geminiCLI, TestOutcome{ModelAccepted: true, Searched: true}, FidelityModelMatch},
		{"gemini that did not search", geminiCLI, TestOutcome{ModelAccepted: true}, FidelityApproximation},
		{"claude app stays an approximation", claudeApp, all, FidelityApproximation},
		{"app surface stays app", chatgpt, TestOutcome{}, FidelityApp},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := PairFidelity(tc.spec, tc.got); got != tc.want {
				t.Errorf("got %s, want %s", got, tc.want)
			}
		})
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
