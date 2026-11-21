// Package checkstest sets up an instance ready to run checks against a fake
// Treg, for tests of the runner, the scheduler and the API.
package checkstest

import (
	"context"
	"testing"

	"github.com/unkeyed/byname/internal/config"
	"github.com/unkeyed/byname/internal/engines"
	"github.com/unkeyed/byname/internal/panel"
)

// Settings brand the instance Northwind (northwind.example), ask in the US
// in English, and run one call at a time.
func Settings() config.Settings {
	return config.Settings{
		Brand:     config.SelfBrand{Name: "Northwind", Aliases: []string{"Northwind Cloud"}, Domains: []string{"northwind.example"}},
		MatchMode: panel.MatchWholeWord, Market: "US", Language: "en", TimeZone: "UTC",
		CheckEveryDays: 3, CheckTime: "06:00", SpendCapRunUSD: 5, Concurrency: 1, Retries: 1, TimeoutS: 30,
	}
}

// Questions are the two questions every seeded run asks.
var Questions = []config.QuestionInput{
	{Group: "Rate limiting", Text: "How do I add rate limiting to a REST API?"},
	{Group: "Rate limiting", Text: "Which managed rate limiting service should I use?"},
}

// Pairs are the six pairs a seeded run asks each question on: four that
// take a model and two app surfaces.
var Pairs = map[engines.Surface]string{
	engines.ClaudeCode:       "claude-sonnet-5-5",
	engines.CodexCLI:         "gpt-6.1-sol",
	engines.GeminiCLI:        "gemini-4-argon",
	engines.PerplexityApp:    "sonar-reasoning-pro",
	engines.GoogleAIOverview: "",
	engines.BingAIMode:       "",
}

// Seed configures cfg with Settings (changed by mod), the brands Contoso
// (competitor) and Redis (technology), the Treg token, Questions and Pairs.
func Seed(t testing.TB, cfg *config.Service, mod func(*config.Settings)) {
	t.Helper()
	ctx := context.Background()
	s := Settings()
	if mod != nil {
		mod(&s)
	}
	must := func(_ any, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(cfg.PutSettings(ctx, s))
	must(cfg.UpsertBrand(ctx, config.BrandInput{Name: "Contoso", Aliases: []string{"Contoso Gateway"}, Domains: []string{"contoso.example"}, Kind: panel.KindCompetitor}))
	must(cfg.UpsertBrand(ctx, config.BrandInput{Name: "Redis", Kind: panel.KindTechnology}))
	must(cfg.SetTregToken(ctx, "treg_test_token", ""))
	must(cfg.PutQuestions(ctx, config.QuestionsInput{Questions: Questions}))
	for _, spec := range engines.All() {
		model, ok := Pairs[spec.Surface]
		if !ok {
			continue
		}
		in := config.SurfaceInput{Enabled: true}
		if model != "" {
			in.Models = []config.PairInput{{Model: model}}
		}
		must(cfg.PutSurface(ctx, spec.Surface, in))
	}
}
