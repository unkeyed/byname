package checks_test

import (
	"context"
	"strings"
	"testing"

	"github.com/unkeyed/byname/internal/checks/checkstest"
	"github.com/unkeyed/byname/internal/config"
	"github.com/unkeyed/byname/internal/engines"
	"github.com/unkeyed/byname/internal/engines/providertest"
	"github.com/unkeyed/byname/internal/engines/tregtest"
	"github.com/unkeyed/byname/internal/secretbox"
	"github.com/unkeyed/byname/internal/testdb"
)

// TestEachPairIsAskedOnItsRoute moves the ChatGPT app to a gateway and
// Claude Code to a direct Anthropic key, and checks that a run asks each
// through its own adapter, stores the route's endpoint so each starts a new
// series, labels the gateway's app answers an approximation, and records
// the token-billed calls' cost as missing rather than $0.
func TestEachPairIsAskedOnItsRoute(t *testing.T) {
	ctx := context.Background()
	providers := providertest.New(t)
	e := &env{db: testdb.New(t), treg: tregtest.New(t)}
	e.config = config.New(e.db, config.Options{Secrets: secretbox.ForTest(), TregBaseURL: e.treg.URL,
		BaseURLs: map[engines.CredentialKind]string{engines.CredentialAnthropic: providers.BaseURL(engines.CredentialAnthropic)}})
	checkstest.Seed(t, e.config, e.treg, nil)
	e.checks = e.restart()

	for _, kind := range []engines.CredentialKind{engines.CredentialGateway, engines.CredentialAnthropic} {
		if _, err := e.config.SetCredential(ctx, string(kind), providertest.Secret(kind), ""); err != nil {
			t.Fatal(err)
		}
	}
	gw, err := e.config.PutSurface(ctx, engines.ChatGPTApp, config.SurfaceInput{Enabled: true, Route: engines.RouteGateway,
		BaseURL: providers.BaseURL(engines.CredentialGateway), Models: []config.PairInput{{Model: "openai/gpt-4.1"}}})
	if err != nil {
		t.Fatal(err)
	}
	if gw.Fidelity != engines.FidelityApproximation || gw.Routes[3].Fidelity != engines.FidelityApproximation || gw.Routes[0].Fidelity != engines.FidelityApp {
		t.Fatalf("ChatGPT app on a gateway: label %s, routes %+v; want approximation there and app on Treg", gw.Fidelity, gw.Routes)
	}
	cc, err := e.config.PutSurface(ctx, engines.ClaudeCode, config.SurfaceInput{Enabled: true, Route: engines.RouteProvider,
		Models: []config.PairInput{{Model: "claude-sonnet-4-5-20250929"}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []config.Surface{gw, cc} {
		for _, p := range s.Pairs {
			if !p.Enabled {
				continue
			}
			res, err := e.config.TestPair(ctx, p.ID)
			if err != nil || res.Pair.TestStatus != "ok" {
				t.Fatalf("test %s %s: %+v %v", s.Key, p.Model, res, err)
			}
			want := map[engines.Surface]engines.Fidelity{engines.ChatGPTApp: engines.FidelityApproximation, engines.ClaudeCode: engines.FidelityModelMatch}[s.Key]
			if res.Pair.Fidelity != want {
				t.Errorf("%s after its test: %s, want %s", s.Key, res.Pair.Fidelity, want)
			}
			if s.Key == engines.ClaudeCode && (res.CostReported || res.CostUSD != nil) {
				t.Errorf("a token-billed test reported cost %v, want none", res.CostUSD)
			}
		}
	}

	providers.Reset()
	id := e.runNow(t)
	if err := e.checks.Work(ctx); err != nil {
		t.Fatal(err)
	}
	r := e.run(t, id)
	if r.Status != "completed" || r.Answers != 14 || r.Failed != 0 {
		t.Fatalf("run = %+v, want 2 questions x 7 pairs answered", r)
	}
	rows, err := e.db.Query(ctx, `SELECT m.surface_key, a.endpoint, a.fidelity, a.cost_usd::float8 FROM answer a
		JOIN surface_model m ON m.id = a.surface_model_id WHERE a.run_id = $1 AND m.surface_key IN ('chatgpt_app', 'claude_code')`, id)
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for rows.Next() {
		var surface, endpoint, fidelity string
		var cost *float64
		if err := rows.Scan(&surface, &endpoint, &fidelity, &cost); err != nil {
			t.Fatal(err)
		}
		seen++
		switch surface {
		case "chatgpt_app":
			if !strings.HasPrefix(endpoint, "gateway:") || fidelity != "approximation" || cost == nil {
				t.Errorf("gateway answer: %s %s cost %v, want gateway:<host>, approximation and the gateway's reported cost", endpoint, fidelity, cost)
			}
		case "claude_code":
			if endpoint != "anthropic.messages" || fidelity != "model_match" || cost != nil {
				t.Errorf("Anthropic answer: %s %s cost %v, want anthropic.messages, model_match and no cost", endpoint, fidelity, cost)
			}
		}
	}
	if seen != 4 {
		t.Fatalf("%d answers on the moved surfaces, want 4", seen)
	}
	if r.Missing != 2 {
		t.Errorf("spend_missing_calls = %d, want the 2 Anthropic calls flagged", r.Missing)
	}
	byKind := map[engines.CredentialKind]int{}
	for _, c := range providers.Calls() {
		if c.Method == "POST" && c.Authorized {
			byKind[c.Kind]++
		}
	}
	if byKind[engines.CredentialGateway] != 2 || byKind[engines.CredentialAnthropic] != 2 {
		t.Errorf("provider calls = %v, want 2 to the gateway and 2 to Anthropic", byKind)
	}
	for _, c := range e.treg.Calls() {
		if c.Tool == claudeLive || strings.Contains(c.Tool, "chat-gpt-llm-scraper") {
			t.Errorf("Treg was asked %s, which moved off the Treg route", c.Tool)
		}
	}
}
