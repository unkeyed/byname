package config_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/unkeyed/byname/internal/config"
	"github.com/unkeyed/byname/internal/core"
	"github.com/unkeyed/byname/internal/engines"
	"github.com/unkeyed/byname/internal/engines/tregtest"
	"github.com/unkeyed/byname/internal/panel"
	"github.com/unkeyed/byname/internal/secretbox"
	"github.com/unkeyed/byname/internal/testdb"
)

var ctx = context.Background()

func newService(t *testing.T, now time.Time) (*config.Service, *tregtest.Server) {
	t.Helper()
	treg := tregtest.New(t)
	svc := config.New(testdb.New(t), config.Options{Secrets: secretbox.ForTest(), TregBaseURL: treg.URL, Now: func() time.Time { return now }})
	return svc, treg
}

func settings() config.Settings {
	return config.Settings{
		Brand:     config.SelfBrand{Name: "Northwind", Aliases: []string{"Northwind Cloud", " Northwind  Cloud "}, Domains: []string{"Northwind.example", "www.northwind.example"}},
		MatchMode: panel.MatchWholeWord, Market: "US", Language: "en", TimeZone: "Europe/Berlin",
		CheckEveryDays: 3, CheckTime: "06:00", SpendCapRunUSD: 2, Concurrency: 4, Retries: 1, TimeoutS: 60,
	}
}

func is(t *testing.T, err, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
}

func TestSettingsScheduleTheNextCheck(t *testing.T) {
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	svc, _ := newService(t, now)

	before, err := svc.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if before.NextCheckAt != nil || before.Brand.Name != "" || before.CheckEveryDays != 3 || before.Concurrency != 8 {
		t.Fatalf("defaults = %+v; want nothing scheduled, no brand, every 3 days, concurrency 8", before)
	}

	got, err := svc.PutSettings(ctx, settings())
	if err != nil {
		t.Fatal(err)
	}
	if got.Brand.Name != "Northwind" || len(got.Brand.Aliases) != 1 || len(got.Brand.Domains) != 1 || got.Brand.Domains[0] != "northwind.example" {
		t.Errorf("brand = %+v, want aliases and domains normalized and deduplicated", got.Brand)
	}
	if got.NextCheckAt != nil {
		t.Fatalf("next check = %v before onboarding is finished, want none", got.NextCheckAt)
	}
	onboard(t, svc)
	got, err = svc.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	berlin, _ := time.LoadLocation("Europe/Berlin")
	want := time.Date(2026, 11, 22, 6, 0, 0, 0, berlin)
	if got.NextCheckAt == nil || !got.NextCheckAt.Equal(want) {
		t.Fatalf("next check = %v, want %v: 06:00 Berlin has passed today", got.NextCheckAt, want)
	}

	cheaper := settings()
	cheaper.SpendCapRunUSD = 1
	got, err = svc.PutSettings(ctx, cheaper)
	if err != nil || !got.NextCheckAt.Equal(want) || got.SpendCapRunUSD != 1 {
		t.Fatalf("after a cap change: %+v %v; the next check must not move", got, err)
	}

	later := cheaper
	later.CheckTime = "18:30"
	got, err = svc.PutSettings(ctx, later)
	if err != nil || !got.NextCheckAt.Equal(time.Date(2026, 11, 21, 18, 30, 0, 0, berlin)) {
		t.Fatalf("after a time change: next %v %v, want today 18:30 Berlin", got.NextCheckAt, err)
	}

	paused, err := svc.SetPaused(ctx, true)
	if err != nil || !paused.ChecksPaused || paused.NextCheckAt == nil {
		t.Fatalf("pause: %+v %v", paused, err)
	}

	for name, mod := range map[string]func(*config.Settings){
		"no brand":         func(s *config.Settings) { s.Brand.Name = "" },
		"brand with a URL": func(s *config.Settings) { s.Brand.Domains = []string{"https://northwind.example/docs"} },
		"market":           func(s *config.Settings) { s.Market = "usa" },
		"time zone":        func(s *config.Settings) { s.TimeZone = "Mars/Olympus" },
		"every 31 days":    func(s *config.Settings) { s.CheckEveryDays = 31 },
		"concurrency 33":   func(s *config.Settings) { s.Concurrency = 33 },
		"timeout 10":       func(s *config.Settings) { s.TimeoutS = 10 },
		"retries 4":        func(s *config.Settings) { s.Retries = 4 },
		"zero cap":         func(s *config.Settings) { s.SpendCapRunUSD = 0 },
		"check time":       func(s *config.Settings) { s.CheckTime = "6am" },
	} {
		in := settings()
		mod(&in)
		if _, err := svc.PutSettings(ctx, in); !errors.Is(err, core.ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
}

func TestNextCheck(t *testing.T) {
	s := config.Schedule{Days: 3, Hour: 6, Location: time.UTC}
	at := func(d, h int) time.Time { return time.Date(2026, 11, d, h, 0, 0, 0, time.UTC) }
	for _, tc := range []struct {
		name string
		now  time.Time
		last *time.Time
		want time.Time
	}{
		{"first, before the time", at(21, 5), nil, at(21, 6)},
		{"first, after the time", at(21, 7), nil, at(22, 6)},
		{"days after the last check", at(21, 7), new(at(20, 6)), at(23, 6)},
		{"just claimed", at(21, 6), new(at(21, 6)), at(24, 6)},
		{"claimed late in the day", at(21, 23), new(at(21, 23)), at(24, 6)},
		{"last check long ago", at(21, 7), new(at(1, 6)), at(22, 6)},
	} {
		if got := config.NextCheck(tc.now, tc.last, s); !got.Equal(tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestBrandsUpsertByName(t *testing.T) {
	svc, _ := newService(t, time.Now())
	if _, err := svc.PutSettings(ctx, settings()); err != nil {
		t.Fatal(err)
	}
	c, err := svc.UpsertBrand(ctx, config.BrandInput{Name: "Contoso", Aliases: []string{"Contoso Gateway"}, Domains: []string{"contoso.example"}})
	if err != nil || c.Kind != panel.KindCompetitor {
		t.Fatalf("upsert: %+v %v, want a competitor by default", c, err)
	}
	again, err := svc.UpsertBrand(ctx, config.BrandInput{Name: "contoso", Kind: panel.KindTechnology})
	if err != nil || again.ID != c.ID || again.Name != "contoso" || again.Kind != panel.KindTechnology || len(again.Aliases) != 0 {
		t.Fatalf("upsert by the same name ignoring case: %+v %v, want the same row replaced", again, err)
	}
	_, err = svc.UpsertBrand(ctx, config.BrandInput{Name: "NORTHWIND"})
	is(t, err, core.ErrConflict)
	_, err = svc.UpsertBrand(ctx, config.BrandInput{Name: "Fabrikam", Kind: panel.KindSelf})
	is(t, err, core.ErrInvalid)

	f, _ := svc.UpsertBrand(ctx, config.BrandInput{Name: "Fabrikam"})
	_, err = svc.UpdateBrand(ctx, f.ID, config.BrandInput{Name: "Contoso"})
	is(t, err, core.ErrConflict)
	renamed, err := svc.UpdateBrand(ctx, f.ID, config.BrandInput{Name: "Fabrikam Labs"})
	if err != nil || renamed.Name != "Fabrikam Labs" {
		t.Fatalf("rename: %+v %v", renamed, err)
	}

	brands, _ := svc.Brands(ctx)
	if len(brands) != 3 || brands[0].Kind != panel.KindSelf {
		t.Fatalf("brands = %+v, want 3 with your own first", brands)
	}
	is(t, svc.DeleteBrand(ctx, brands[0].ID), core.ErrNotFound)
	if err := svc.DeleteBrand(ctx, f.ID); err != nil {
		t.Fatal(err)
	}
}

func q(group, text string, replaces ...int64) config.QuestionInput {
	return config.QuestionInput{Group: group, Text: text, Replaces: replaces}
}

func TestQuestionSetsVersionEveryChange(t *testing.T) {
	svc, _ := newService(t, time.Now())
	_, err := svc.CurrentQuestions(ctx)
	is(t, err, core.ErrNotFound)

	v1, err := svc.PutQuestions(ctx, config.QuestionsInput{Questions: []config.QuestionInput{
		q("Rate limiting", "How do I rate limit a REST API?"),
		q("Rate limiting", "What is the best rate limiting service?"),
		q("Auth", "How should I issue API keys to customers?"),
	}})
	if err != nil || v1.Version != 1 || len(v1.Questions) != 3 {
		t.Fatalf("v1 = %+v %v", v1, err)
	}
	same, err := svc.PutQuestions(ctx, config.QuestionsInput{Questions: []config.QuestionInput{
		q("Rate limiting", "How do I rate limit a REST API?"),
		q("Rate limiting", "What is the best rate limiting service?"),
		q("Auth", "How should I issue API keys to customers?"),
	}})
	if err != nil || same.ID != v1.ID {
		t.Fatalf("resending the set made version %d, want it unchanged: %v", same.Version, err)
	}

	edit := config.QuestionsInput{Questions: []config.QuestionInput{
		q("Rate limiting", "how do i rate-limit a REST API"),
		q("Rate limiting", "Which rate limiting service is best for a startup?"),
		q("Billing", "How do I bill customers per API call?"),
	}}
	_, err = svc.PutQuestions(ctx, edit)
	if !errors.Is(err, core.ErrInvalid) || !strings.Contains(err.Error(), "replaces") {
		t.Fatalf("an edit without a map: %v, want ErrInvalid asking for replaces or retired", err)
	}
	edit.Questions[1].Replaces = []int64{v1.Questions[1].ID}
	edit.Retired = []int64{v1.Questions[2].ID}
	v2, err := svc.PutQuestions(ctx, edit)
	if err != nil || v2.Version != 2 {
		t.Fatalf("v2 = %+v %v", v2, err)
	}
	wantCarried := map[int64]int64{v1.Questions[0].ID: v2.Questions[0].ID, v1.Questions[1].ID: v2.Questions[1].ID}
	if len(v2.Carried) != 2 || len(v2.Retired) != 1 || v2.Retired[0] != v1.Questions[2].ID {
		t.Fatalf("carried %+v retired %v", v2.Carried, v2.Retired)
	}
	for _, l := range v2.Carried {
		if wantCarried[l.From] != l.To {
			t.Errorf("question %d carried into %d, want %d", l.From, l.To, wantCarried[l.From])
		}
	}

	for name, in := range map[string]config.QuestionsInput{
		"empty":     {},
		"too short": {Questions: []config.QuestionInput{q("A", "Why?")}},
		"duplicate": {Questions: []config.QuestionInput{q("A", "How do I bill per call?"), q("B", "how do I bill per call")}},
		"no group":  {Questions: []config.QuestionInput{q("", "How do I bill per call?")}},
		"replaces an old version's question": {Questions: []config.QuestionInput{
			q("A", "A brand new question here", v1.Questions[2].ID)}},
	} {
		if _, err := svc.PutQuestions(ctx, in); !errors.Is(err, core.ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
	sets, err := svc.QuestionSets(ctx)
	if err != nil || len(sets) != 2 || sets[0].Version != 2 || sets[0].Questions != 3 {
		t.Fatalf("sets = %+v %v", sets, err)
	}
}

func TestSurfacesCheckModelsAgainstTheLiveList(t *testing.T) {
	svc, treg := newService(t, time.Now())

	_, err := svc.PutSurface(ctx, engines.ClaudeCode, config.SurfaceInput{Enabled: true, Models: []config.PairInput{{Model: "claude-sonnet-5-5"}}})
	is(t, err, core.ErrInvalid)

	app, err := svc.PutSurface(ctx, engines.ChatGPTApp, config.SurfaceInput{Enabled: true})
	if err != nil || len(app.Pairs) != 1 || app.Pairs[0].Model != "" || app.Pairs[0].Fidelity != engines.FidelityApp {
		t.Fatalf("app surface: %+v %v, want one pair with no model", app, err)
	}
	_, err = svc.PutSurface(ctx, engines.ChatGPTApp, config.SurfaceInput{Enabled: true, Models: []config.PairInput{{Model: "gpt-6"}}})
	is(t, err, core.ErrInvalid)
	_, err = svc.PutSurface(ctx, "myspace", config.SurfaceInput{Enabled: true})
	is(t, err, core.ErrNotFound)

	if _, err := svc.SetCredential(ctx, "treg", "treg_test_token", ""); err != nil {
		t.Fatal(err)
	}
	_, err = svc.PutSurface(ctx, engines.ClaudeCode, config.SurfaceInput{Enabled: true, Models: []config.PairInput{{Model: "claude-opus"}}})
	if !errors.Is(err, core.ErrInvalid) || !strings.Contains(err.Error(), "claude-sonnet-5-5") {
		t.Fatalf("a model off the list: %v, want ErrInvalid naming what the list has", err)
	}
	cc, err := svc.PutSurface(ctx, engines.ClaudeCode, config.SurfaceInput{Enabled: true, Models: []config.PairInput{{Model: "claude-sonnet-5-5"}}})
	if err != nil || len(cc.Pairs) != 1 || !cc.Pairs[0].ForceWebSearch || cc.Pairs[0].Fidelity != engines.FidelityModelMatch {
		t.Fatalf("claude code: %+v %v, want one pair forcing search, labeled model_match until tested", cc, err)
	}
	_, err = svc.PutSurface(ctx, engines.GeminiCLI, config.SurfaceInput{Enabled: true, Models: []config.PairInput{{Model: "gemini-4-argon", ForceWebSearch: new(true)}}})
	is(t, err, core.ErrInvalid)
	_, err = svc.PutSurface(ctx, engines.GeminiCLI, config.SurfaceInput{Enabled: true, Models: []config.PairInput{}})
	is(t, err, core.ErrInvalid)

	off, err := svc.PutSurface(ctx, engines.ClaudeCode, config.SurfaceInput{Enabled: false})
	if err != nil || off.Enabled || !off.Pairs[0].Enabled {
		t.Fatalf("turning a surface off: %+v %v, want its models kept", off, err)
	}
	if _, err := svc.PutSurface(ctx, engines.ClaudeCode, config.SurfaceInput{Enabled: true}); err != nil {
		t.Fatal(err)
	}
	active, _ := svc.ActivePairs(ctx)
	if len(active) != 2 {
		t.Fatalf("active pairs = %+v, want ChatGPT app and Claude Code", active)
	}
	if err := svc.Ready(ctx); !errors.Is(err, core.ErrInvalid) || !strings.Contains(err.Error(), "brand") || !strings.Contains(err.Error(), "questions") {
		t.Fatalf("ready = %v, want the brand and questions missing", err)
	}

	treg.SetBody("dataforseo.x.ai-optimization-claude-llm-responses-models",
		[]byte(`{"status_code":20000,"tasks":[{"status_code":20000,"result":[{"model_name":"claude-sonnet-6","web_search_supported":true}]}]}`))
	if err := svc.RefreshModelsIfDue(ctx); err != nil {
		t.Fatal(err)
	}
	surfaces, _ := svc.Surfaces(ctx)
	var paused config.Pair
	for _, s := range surfaces {
		if s.Key == engines.ClaudeCode {
			paused = s.Pairs[0]
		}
	}
	if paused.Model != "claude-sonnet-5-5" || !strings.Contains(paused.PausedReason, "left Claude Code's model list") {
		t.Fatalf("after the model left the list: %+v, want the same model, paused with a reason", paused)
	}
	if active, _ := svc.ActivePairs(ctx); len(active) != 1 {
		t.Fatalf("active pairs = %+v, want the paused pair skipped", active)
	}
	before := len(treg.Calls())
	if err := svc.RefreshModelsIfDue(ctx); err != nil || len(treg.Calls()) != before {
		t.Fatalf("a second refresh the same day called Treg %d times (%v), want none", len(treg.Calls())-before, err)
	}
}

func TestCredentialsAreNeverReturned(t *testing.T) {
	svc, treg := newService(t, time.Now())
	creds, err := svc.Credentials(ctx)
	if err != nil || len(creds) != len(engines.CredentialKinds) || creds[0].Kind != engines.CredentialTreg || creds[0].Set {
		t.Fatalf("before: %+v %v, want every kind unset, Treg first", creds, err)
	}
	_, err = svc.TestCredential(ctx, "treg")
	is(t, err, core.ErrInvalid)
	_, err = svc.SetCredential(ctx, "dataforseo", "no-colon", "")
	is(t, err, core.ErrInvalid)
	_, err = svc.SetCredential(ctx, "openai", "sk-live", "an-org")
	is(t, err, core.ErrInvalid)
	_, err = svc.SetCredential(ctx, "keyring", "x", "")
	is(t, err, core.ErrNotFound)

	c, err := svc.SetCredential(ctx, "treg", "treg_live_secret", "")
	if err != nil || !c.Set {
		t.Fatalf("set: %+v %v", c, err)
	}
	if _, err := svc.SetCredential(ctx, "anthropic", "sk-ant-live-secret", ""); err != nil {
		t.Fatal(err)
	}
	c, err = svc.TestCredential(ctx, "treg")
	if err != nil || c.LastTestStatus != "ok" || c.LastTestedAt == nil {
		t.Fatalf("test: %+v %v", c, err)
	}
	if calls := treg.Calls(); calls[len(calls)-1].Token != "treg_live_secret" || !strings.HasSuffix(calls[len(calls)-1].Tool, "-models") {
		t.Fatalf("test call = %+v, want a free model list call with the stored token", calls[len(calls)-1])
	}
	treg.Before(func(tregtest.Call) *tregtest.Refusal { return &tregtest.Refusal{Status: 401, Code: "invalid_token"} })
	c, err = svc.TestCredential(ctx, "treg")
	if err != nil || !strings.Contains(c.LastTestStatus, "invalid_token") {
		t.Fatalf("failed test: %+v %v, want the refusal recorded", c, err)
	}
	creds, err = svc.Credentials(ctx)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(creds)
	for _, secret := range []string{"treg_live_secret", "sk-ant-live-secret"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("credentials carry a secret: %s", raw)
		}
	}
	if err := svc.DeleteCredential(ctx, "anthropic"); err != nil {
		t.Fatal(err)
	}
	is(t, svc.DeleteCredential(ctx, "anthropic"), core.ErrNotFound)
}
