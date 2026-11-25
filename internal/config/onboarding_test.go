package config_test

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/unkeyed/byname/internal/config"
	"github.com/unkeyed/byname/internal/core"
	"github.com/unkeyed/byname/internal/engines"
	"github.com/unkeyed/byname/internal/engines/tregtest"
	"github.com/unkeyed/byname/internal/secretbox"
	"github.com/unkeyed/byname/internal/testdb"
)

const claudeLive = "dataforseo.x.ai-optimization-claude-llm-responses-live"

func newServiceDB(t *testing.T, now time.Time) (*config.Service, *tregtest.Server, *pgxpool.Pool) {
	t.Helper()
	treg := tregtest.New(t)
	db := testdb.New(t)
	return config.New(db, config.Options{Secrets: secretbox.ForTest(), TregBaseURL: treg.URL, Now: func() time.Time { return now }}), treg, db
}

// configure saves everything onboarding asks for except the pair tests:
// two questions, the Treg token, Claude Code on one model and the ChatGPT
// app, and every step but review.
func configure(t *testing.T, svc *config.Service) {
	t.Helper()
	if s, err := svc.Settings(ctx); err != nil || s.Brand.Name == "" {
		if _, err := svc.PutSettings(ctx, settings()); err != nil {
			t.Fatal(err)
		}
	}
	steps := []func() error{
		func() error {
			_, err := svc.PutQuestions(ctx, config.QuestionsInput{Questions: []config.QuestionInput{
				{Group: "Deploying", Text: "How do I deploy a Go API with zero downtime?"},
				{Group: "Deploying", Text: "Which managed hosting platform should I use for a Go API?"},
			}})
			return err
		},
		func() error { _, err := svc.SetCredential(ctx, "treg", "treg_test_token", ""); return err },
		func() error {
			_, err := svc.PutSurface(ctx, engines.ClaudeCode, config.SurfaceInput{Enabled: true, Models: []config.PairInput{{Model: "claude-sonnet-5-5"}}})
			return err
		},
		func() error {
			_, err := svc.PutSurface(ctx, engines.ChatGPTApp, config.SurfaceInput{Enabled: true})
			return err
		},
	}
	for _, step := range steps {
		if err := step(); err != nil {
			t.Fatal(err)
		}
	}
	for _, k := range []config.StepKey{config.StepBrand, config.StepCompetitors, config.StepQuestions, config.StepSurfaces, config.StepSearch, config.StepSchedule} {
		if _, err := svc.SaveStep(ctx, k); err != nil {
			t.Fatal(err)
		}
	}
}

func testAll(t *testing.T, svc *config.Service) {
	t.Helper()
	pairs, err := svc.ActivePairs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range pairs {
		if res, err := svc.TestPair(ctx, p.ID); err != nil || res.Pair.TestStatus != "ok" {
			t.Fatalf("testing %s %s: %+v %v", p.Surface, p.Model, res, err)
		}
	}
}

// onboard sets up and finishes onboarding.
func onboard(t *testing.T, svc *config.Service) {
	t.Helper()
	configure(t, svc)
	testAll(t, svc)
	if _, err := svc.FinishOnboarding(ctx, 0); err != nil {
		t.Fatal(err)
	}
}

func TestNothingRunsBeforeOnboardingIsFinished(t *testing.T) {
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	svc, _ := newService(t, now)

	state, err := svc.Onboarding(ctx)
	if err != nil || state.FinishedAt != nil || len(state.Steps) != 8 || state.Steps[0].Saved || len(state.Missing) == 0 {
		t.Fatalf("fresh: %+v %v, want eight unsaved steps and a list of what is missing", state, err)
	}
	if !strings.Contains(strings.Join(state.Missing, "; "), "save the AI apps step") {
		t.Fatalf("missing = %q, want each unsaved required step named as written", state.Missing)
	}
	if done, err := svc.Onboarded(ctx); err != nil || done {
		t.Fatalf("onboarded = %v %v on a fresh instance", done, err)
	}
	_, err = svc.FinishOnboarding(ctx, 0)
	is(t, err, core.ErrInvalid)
	_, err = svc.SaveStep(ctx, config.StepReview)
	is(t, err, core.ErrNotFound)

	configure(t, svc)
	if s, _ := svc.Settings(ctx); s.NextCheckAt != nil {
		t.Fatalf("next check = %v with onboarding unfinished, want none so the scheduler never claims one", s.NextCheckAt)
	}
	if err := svc.Ready(ctx); !errors.Is(err, core.ErrInvalid) || !strings.Contains(err.Error(), "finished onboarding") {
		t.Fatalf("ready = %v, want onboarding named as missing, so Run now refuses", err)
	}
	state, _ = svc.Onboarding(ctx)
	if next := state.FirstScheduledCheck; next == nil || next.LocalDate != "2026-11-22" || next.LocalTime != "06:00" || next.DaysAway != 1 || next.ThenEveryDays != 3 {
		t.Fatalf("first scheduled check = %+v, want tomorrow 06:00 Berlin, then every 3 days, before finishing", next)
	}
	if len(state.Missing) != 1 || !strings.Contains(state.Missing[0], "Claude Code on claude-sonnet-5-5") || !strings.Contains(state.Missing[0], "ChatGPT app") {
		t.Fatalf("missing = %q, want only the untested pairs", state.Missing)
	}
	_, err = svc.FinishOnboarding(ctx, 0)
	if !errors.Is(err, core.ErrInvalid) || !strings.Contains(err.Error(), "passing test") {
		t.Fatalf("finish before testing: %v, want a refusal naming the tests", err)
	}

	testAll(t, svc)
	state, err = svc.FinishOnboarding(ctx, 0)
	if err != nil || state.FinishedAt == nil || !state.Steps[len(state.Steps)-1].Saved || len(state.Missing) != 0 || state.FirstScheduledCheck != nil {
		t.Fatalf("finish: %+v %v", state, err)
	}
	s, _ := svc.Settings(ctx)
	berlin, _ := time.LoadLocation("Europe/Berlin")
	if s.NextCheckAt == nil || !s.NextCheckAt.Equal(time.Date(2026, 11, 22, 6, 0, 0, 0, berlin)) {
		t.Fatalf("next check = %v after finishing, want tomorrow 06:00 Berlin", s.NextCheckAt)
	}
	if err := svc.Ready(ctx); err != nil {
		t.Fatalf("ready after finishing: %v", err)
	}
	again, err := svc.FinishOnboarding(ctx, 0)
	if err != nil || !again.FinishedAt.Equal(*state.FinishedAt) {
		t.Fatalf("finishing twice: %+v %v, want the first finish kept", again, err)
	}
}

func TestPairTestSetsFidelity(t *testing.T) {
	svc, treg := newService(t, time.Now())
	configure(t, svc)
	pair := func() config.Pair {
		t.Helper()
		pairs, _ := svc.ActivePairs(ctx)
		for _, p := range pairs {
			if p.Surface == engines.ClaudeCode {
				return p
			}
		}
		t.Fatal("no Claude Code pair")
		return config.Pair{}
	}
	if p := pair(); p.Fidelity != engines.FidelityModelMatch || p.LastTestedAt != nil {
		t.Fatalf("untested: %+v, want the expected label and no test", p)
	}

	res, err := svc.TestPair(ctx, pair().ID)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != engines.StatusOK || !res.Searched || !res.Cited || res.Citations == 0 || !res.CostReported || res.CostUSD == nil || *res.CostUSD != 0.01 {
		t.Fatalf("forced search: %+v, want an answer that searched and cited, with Treg's cost", res)
	}
	if p := pair(); p.Fidelity != engines.FidelityModelMatch || p.TestStatus != "ok" || p.TestSearched == nil || !*p.TestSearched || p.LastTestedAt == nil {
		t.Fatalf("after a test that searched: %+v, want model_match confirmed", p)
	}
	last := treg.Calls()[len(treg.Calls())-1]
	if last.Tool != claudeLive || last.IdempotencyKey != "" {
		t.Fatalf("test call = %+v, want one call to the pinned endpoint", last)
	}

	treg.SetBody(claudeLive, []byte(`{"status_code":20000,"tasks":[{"status_code":20000,"result":[{"model_name":"claude-sonnet-5-5",
		"items":[{"type":"message","sections":[{"type":"text","text":"Put a token bucket in front of the API."}]}]}]}]}`))
	res, err = svc.TestPair(ctx, pair().ID)
	if err != nil || res.Searched || res.Pair.Fidelity != engines.FidelityApproximation || pair().Fidelity != engines.FidelityApproximation {
		t.Fatalf("a test that did not search: %+v %v, want the pair dropped to approximation", res, err)
	}

	treg.SetBody(claudeLive, []byte(`{"status_code":40501,"status_message":"Invalid Field: 'force_web_search'."}`))
	res, err = svc.TestPair(ctx, pair().ID)
	if err != nil || res.Status != engines.StatusFailed || !strings.Contains(res.Pair.TestStatus, "40501") || pair().Fidelity != engines.FidelityApproximation {
		t.Fatalf("a failed test: %+v %v, want the failure recorded and the label left alone", res, err)
	}
	if state, _ := svc.Onboarding(ctx); !strings.Contains(strings.Join(state.Missing, " "), "Claude Code") {
		t.Fatalf("missing = %q, want the failed pair named", state.Missing)
	}
	_, err = svc.TestPair(ctx, 99999)
	is(t, err, core.ErrNotFound)
}

func TestMovingRouteResetsTestsAndLabels(t *testing.T) {
	svc, _ := newService(t, time.Now())
	configure(t, svc)
	testAll(t, svc)
	_, err := svc.PutSurface(ctx, engines.ChatGPTApp, config.SurfaceInput{Enabled: true, Route: engines.RouteProvider, Models: []config.PairInput{{Model: "gpt-5"}}})
	if !errors.Is(err, core.ErrInvalid) || !strings.Contains(err.Error(), "OpenAI") {
		t.Fatalf("moving to a provider with no key: %v, want a refusal naming the key", err)
	}
	_, err = svc.PutSurface(ctx, engines.Brave, config.SurfaceInput{Enabled: true, Route: engines.RouteProvider})
	is(t, err, core.ErrInvalid)
	_, err = svc.PutSurface(ctx, engines.ChatGPTApp, config.SurfaceInput{Enabled: true, BaseURL: "https://gateway.example/v1"})
	is(t, err, core.ErrInvalid)
}

func TestEstimate(t *testing.T) {
	svc, _, db := newServiceDB(t, time.Now())
	est, err := svc.Estimate(ctx)
	if err != nil || est.Calls != 0 || len(est.Rows) != 0 {
		t.Fatalf("empty instance: %+v %v", est, err)
	}
	onboard(t, svc)

	est, err = svc.Estimate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if est.Questions != 2 || est.Pairs != 2 || est.Calls != 4 || est.UnknownPairs != 0 {
		t.Fatalf("estimate = %+v, want 2 questions x 2 pairs", est)
	}
	near := func(a, b float64) bool { return math.Abs(a-b) < 1e-9 }
	for _, r := range est.Rows {
		want := map[engines.Surface]float64{engines.ClaudeCode: 0.055, engines.ChatGPTApp: 0.004}[r.Surface]
		if r.Source != config.CostFromSample || r.CostPerCallUSD == nil || !near(*r.CostPerCallUSD, want) || !near(*r.CostUSD, want*2) {
			t.Errorf("%s: %+v, want the sampled %v per call times 2 calls", r.Surface, r, want)
		}
	}
	find := 4 * engines.BrandFinder.SampleCostUSD
	if est.FindBrands == nil || est.FindBrands.Answers != 4 || !near(est.FindBrands.CostUSD, find) {
		t.Errorf("find brands = %+v, want 4 answers read at the sampled price", est.FindBrands)
	}
	if !near(est.KnownUSD, 2*0.055+2*0.004+find) {
		t.Errorf("known = %v, want the sum of both pairs and reading their answers", est.KnownUSD)
	}

	if _, err := db.Exec(ctx, `UPDATE instance_settings SET samples = 3`); err != nil {
		t.Fatal(err)
	}
	if est, err = svc.Estimate(ctx); err != nil || est.Samples != 3 || est.Calls != 12 || est.FindBrands == nil || est.FindBrands.Answers != 12 ||
		!near(est.KnownUSD, 3*(2*0.055+2*0.004+find)) {
		t.Errorf("asking each question 3 times: %+v %v, want 12 answers, each paid for and read", est, err)
	}
	for _, r := range est.Rows {
		if r.Calls != 6 {
			t.Errorf("%s: %d calls, want 2 questions x 3 times", r.Surface, r.Calls)
		}
	}
	if _, err := db.Exec(ctx, `UPDATE instance_settings SET samples = 1`); err != nil {
		t.Fatal(err)
	}

	if _, err := db.Exec(ctx, `WITH r AS (INSERT INTO run (question_set_id, trigger, status) SELECT id, 'run_now', 'completed' FROM question_set RETURNING id)
		INSERT INTO answer (run_id, question_id, surface_model_id, endpoint, fidelity, status, attempts, mentioned, cited, cost_usd)
		SELECT r.id, q.id, m.id, $1, 'model_match', 'ok', 1, false, false, c.cost
		FROM r, question q, surface_model m, (VALUES (0.02::numeric, 0), (0.04, 1)) AS c(cost, n)
		WHERE m.surface_key = 'claude_code' AND q.ord = 0 AND c.n = 0
		   OR m.surface_key = 'claude_code' AND q.ord = 1 AND c.n = 1`, claudeLive); err != nil {
		t.Fatal(err)
	}
	est, _ = svc.Estimate(ctx)
	for _, r := range est.Rows {
		if r.Surface == engines.ClaudeCode && (r.Source != config.CostFromHistory || r.Samples != 2 || !near(*r.CostPerCallUSD, 0.03)) {
			t.Errorf("after two answers that reported $0.02 and $0.04: %+v, want their mean $0.03 from history", r)
		}
	}
	if !near(est.KnownUSD, 2*0.03+2*0.004+find) {
		t.Errorf("known = %v after history", est.KnownUSD)
	}

	if _, err := db.Exec(ctx, `UPDATE instance_settings SET find_brands = false`); err != nil {
		t.Fatal(err)
	}
	if est, _ = svc.Estimate(ctx); est.FindBrands != nil || !near(est.KnownUSD, 2*0.03+2*0.004) {
		t.Errorf("with reading answers off: find %+v, known %v, want neither counted", est.FindBrands, est.KnownUSD)
	}
}

func TestDefaultMonthCap(t *testing.T) {
	for _, c := range []struct{ month, want float64 }{
		{0, 5}, {1.2, 5}, {1.67, 6}, {4.0 / 3, 5}, {10.0 / 3, 10}, {12.34, 38},
	} {
		if got := config.DefaultMonthCap(c.month); got != c.want {
			t.Errorf("DefaultMonthCap(%v) = %v, want %v", c.month, got, c.want)
		}
	}
}

// Finishing onboarding sets a monthly cap of three times the monthly
// estimate, social searches included, when none is set, and keeps one that
// is.
func TestFinishingSetsADefaultMonthlyCap(t *testing.T) {
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	svc, _ := newService(t, now)
	configure(t, svc)
	testAll(t, svc)
	costs, err := svc.CheckCosts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	perCheck := 2*0.055 + 2*0.004 + 4*engines.BrandFinder.SampleCostUSD
	if costs.ChecksPerMonth != 365.25/12/3 || math.Abs(costs.MonthUSD-perCheck*costs.ChecksPerMonth) > 1e-9 {
		t.Fatalf("check costs = %+v, want %v a check about ten times a month", costs, perCheck)
	}
	if _, err := svc.FinishOnboarding(ctx, 2); err != nil {
		t.Fatal(err)
	}
	s, _ := svc.Settings(ctx)
	if want := math.Ceil(3 * (costs.MonthUSD + 2)); s.SpendCapMonthUSD == nil || *s.SpendCapMonthUSD != want {
		t.Fatalf("monthly cap = %v, want %v: three times checks plus social, rounded up", s.SpendCapMonthUSD, want)
	}

	other, _ := newService(t, now)
	in := settings()
	in.SpendCapMonthUSD = new(50.0)
	if _, err := other.PutSettings(ctx, in); err != nil {
		t.Fatal(err)
	}
	configure(t, other)
	testAll(t, other)
	if _, err := other.FinishOnboarding(ctx, 0); err != nil {
		t.Fatal(err)
	}
	if s, _ := other.Settings(ctx); *s.SpendCapMonthUSD != 50 {
		t.Fatalf("monthly cap = %v, want the $50 already set kept", *s.SpendCapMonthUSD)
	}
}
