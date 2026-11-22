package checks_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/unkeyed/byname/internal/checks"
	"github.com/unkeyed/byname/internal/checks/checkstest"
	"github.com/unkeyed/byname/internal/config"
	"github.com/unkeyed/byname/internal/engines/tregtest"
	"github.com/unkeyed/byname/internal/secretbox"
	"github.com/unkeyed/byname/internal/testdb"
)

const (
	claudeLive     = "dataforseo.x.ai-optimization-claude-llm-responses-live"
	perplexityLive = "dataforseo.x.ai-optimization-perplexity-llm-responses-live"
)

type env struct {
	db     *pgxpool.Pool
	config *config.Service
	checks *checks.Service
	treg   *tregtest.Server
	slept  []time.Duration
}

func setup(t *testing.T, mod func(*config.Settings)) *env {
	t.Helper()
	e := &env{db: testdb.New(t), treg: tregtest.New(t)}
	e.config = config.New(e.db, config.Options{Secrets: secretbox.ForTest(), TregBaseURL: e.treg.URL})
	checkstest.Seed(t, e.config, e.treg, mod)
	e.checks = e.restart()
	return e
}

// restart is a new process on the same database.
func (e *env) restart() *checks.Service {
	return checks.New(e.db, e.config, checks.Options{Sleep: func(_ context.Context, d time.Duration) error {
		e.slept = append(e.slept, d)
		return nil
	}})
}

func (e *env) runNow(t *testing.T) int64 {
	t.Helper()
	run, created, err := e.checks.RunNow(context.Background())
	if err != nil || !created || run.Status != checks.Queued {
		t.Fatalf("run now: %+v created %v: %v", run, created, err)
	}
	return run.ID
}

type runRow struct {
	Status       checks.Status
	Spend        float64
	Missing      int
	Error        string
	RefusalCode  string
	Answers      int
	Failed       int
	JobStatus    string
	JobTrigger   string
	FinishedAtOK bool
}

func (e *env) run(t *testing.T, id int64) runRow {
	t.Helper()
	var r runRow
	err := e.db.QueryRow(context.Background(), `SELECT r.status, r.spend_usd::float8, r.spend_missing_calls, coalesce(r.error, ''),
			coalesce(r.refusal_code, ''), (SELECT count(*) FROM answer WHERE run_id = r.id),
			(SELECT count(*) FROM answer WHERE run_id = r.id AND status = 'failed'), j.status, j.trigger, r.finished_at IS NOT NULL
		FROM run r JOIN job_run j ON j.run_id = r.id WHERE r.id = $1`, id).
		Scan(&r.Status, &r.Spend, &r.Missing, &r.Error, &r.RefusalCode, &r.Answers, &r.Failed, &r.JobStatus, &r.JobTrigger, &r.FinishedAtOK)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func near(a, b float64) bool { return a-b < 1e-9 && b-a < 1e-9 }

func TestRunCheckStoresEveryAnswer(t *testing.T) {
	ctx := context.Background()
	e := setup(t, func(s *config.Settings) { s.Concurrency = 4 })
	e.treg.SetCost(perplexityLive, nil)

	id := e.runNow(t)
	again, created, err := e.checks.RunNow(ctx)
	if err != nil || created || again.ID != id {
		t.Fatalf("second run now = %+v created %v %v, want the queued run back", again, created, err)
	}
	if err := e.checks.Work(ctx); err != nil {
		t.Fatal(err)
	}

	r := e.run(t, id)
	if r.Status != checks.Completed || r.Answers != 12 || r.Failed != 0 || r.JobStatus != "completed" || r.JobTrigger != "run_now" || !r.FinishedAtOK {
		t.Fatalf("run = %+v, want completed with 2 questions x 6 pairs and its job_run completed", r)
	}
	if !near(r.Spend, 0.10) || r.Missing != 2 {
		t.Fatalf("spend $%v with %d missing, want $0.10 from 10 priced calls and Perplexity's 2 calls counted missing, not $0", r.Spend, r.Missing)
	}
	if n := e.treg.Answers(); n != 12 {
		t.Fatalf("Treg got %d answer calls, want 12", n)
	}
	for _, c := range e.treg.Calls() {
		if !strings.HasSuffix(c.Tool, "-models") && (!strings.HasPrefix(c.IdempotencyKey, fmt.Sprintf("byname/run-%d/q-", id)) || !strings.HasSuffix(c.IdempotencyKey, "/1")) {
			t.Errorf("idempotency key %q, want byname/run-%d/q-<id>/sm-<id>/1", c.IdempotencyKey, id)
		}
	}

	type answer struct {
		surface, model, endpoint, fidelity, reported string
		mentioned, cited                             bool
		position                                     *int
		cost                                         *float64
		mentions, citations                          string
	}
	rows, err := e.db.Query(ctx, `SELECT m.surface_key, coalesce(m.model, ''), a.endpoint, a.fidelity, a.reported_model,
			a.mentioned, a.cited, a.position, a.cost_usd::float8,
			coalesce((SELECT string_agg(b.name || '@' || coalesce(bm.position::text, '-'), ' ' ORDER BY bm.position NULLS LAST)
				FROM brand_mention bm JOIN brand b ON b.id = bm.brand_id WHERE bm.answer_id = a.id), ''),
			coalesce((SELECT string_agg(c.domain || CASE WHEN c.is_self THEN '(self)' ELSE '' END, ' ' ORDER BY c.ord)
				FROM citation c WHERE c.answer_id = a.id), '')
		FROM answer a JOIN surface_model m ON m.id = a.surface_model_id
		JOIN question q ON q.id = a.question_id
		WHERE a.run_id = $1 AND q.ord = 0`, id)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]answer{}
	for rows.Next() {
		var a answer
		if err := rows.Scan(&a.surface, &a.model, &a.endpoint, &a.fidelity, &a.reported, &a.mentioned, &a.cited, &a.position, &a.cost, &a.mentions, &a.citations); err != nil {
			t.Fatal(err)
		}
		got[a.surface] = a
	}
	claude := got["claude_code"]
	if claude.endpoint != claudeLive || claude.fidelity != "model_match" || claude.model != "claude-sonnet-5-5" || claude.reported != "claude-sonnet-4-5-20250929" {
		t.Errorf("claude code answer = %+v, want its endpoint, fidelity, requested and reported model stored", claude)
	}
	if !claude.mentioned || !claude.cited || claude.position == nil || *claude.position != 1 ||
		claude.mentions != "Northwind@1 Contoso@2 Redis@-" || claude.citations != "docs.example.org northwind.example(self)" {
		t.Errorf("claude code answer = %+v, want Northwind #1 and cited, Contoso #2, Redis without a position", claude)
	}
	if claude.cost == nil || !near(*claude.cost, 0.01) {
		t.Errorf("claude code cost = %v, want 0.01", claude.cost)
	}
	perplexity := got["perplexity_app"]
	if perplexity.cost != nil || perplexity.mentioned || perplexity.fidelity != "approximation" || perplexity.endpoint != perplexityLive {
		t.Errorf("perplexity answer = %+v, want no cost, not mentioned, an approximation", perplexity)
	}
	if ov := got["google_ai_overview"]; !ov.mentioned || !ov.cited || ov.fidelity != "app" || ov.model != "" {
		t.Errorf("AI Overview answer = %+v, want Northwind named and cited, labeled app", ov)
	}
	if bing := got["bing_ai_mode"]; bing.mentioned || bing.position != nil || bing.mentions != "" {
		t.Errorf("Bing AI Mode answer = %+v, want nobody on the brand table named", bing)
	}

	var raw string
	if err := e.db.QueryRow(ctx, `SELECT raw->>'status_code' FROM answer WHERE run_id = $1 AND endpoint = $2 LIMIT 1`, id, claudeLive).Scan(&raw); err != nil || raw != "20000" {
		t.Errorf("raw = %q %v, want the provider body stored as jsonb", raw, err)
	}
}

func TestSpendCapStopsQueueing(t *testing.T) {
	ctx := context.Background()
	e := setup(t, func(s *config.Settings) { s.SpendCapRunUSD = 0.025 })
	id := e.runNow(t)
	if err := e.checks.Work(ctx); err != nil {
		t.Fatal(err)
	}
	r := e.run(t, id)
	if n := e.treg.Answers(); n != 3 || r.Answers != 3 {
		t.Fatalf("%d calls and %d answers, want 3: $0.01 each stops queueing once $0.03 passes the $0.025 cap", n, r.Answers)
	}
	if r.Status != checks.Capped || r.JobStatus != "capped" || !strings.Contains(r.Error, "spend cap") {
		t.Fatalf("run = %+v, want capped with the cap named", r)
	}
}

func TestMonthlyCapStopsARunBeforeItStarts(t *testing.T) {
	ctx := context.Background()
	e := setup(t, func(s *config.Settings) { s.SpendCapMonthUSD = new(0.05) })
	first := e.runNow(t)
	if err := e.checks.Work(ctx); err != nil {
		t.Fatal(err)
	}
	if r := e.run(t, first); r.Status != checks.Capped || r.Answers != 5 || !strings.Contains(r.Error, "monthly") {
		t.Fatalf("first run = %+v, want capped by the month after 5 calls", r)
	}
	second := e.runNow(t)
	if err := e.checks.Work(ctx); err != nil {
		t.Fatal(err)
	}
	if r := e.run(t, second); r.Status != checks.Capped || r.Answers != 0 {
		t.Fatalf("second run = %+v, want capped before asking anything", r)
	}
}

func TestMonthlyCapCountsSocialSearches(t *testing.T) {
	ctx := context.Background()
	e := setup(t, func(s *config.Settings) { s.SpendCapMonthUSD = new(0.05) })
	if _, err := e.db.Exec(ctx, `INSERT INTO social_call (network, route, term, tool, status, cost_usd, called_at)
		VALUES ('x', 'treg', 'northwind', 'anyapi.x.search.posts', 'ok', 0.05, now())`); err != nil {
		t.Fatal(err)
	}
	id := e.runNow(t)
	if err := e.checks.Work(ctx); err != nil {
		t.Fatal(err)
	}
	if r := e.run(t, id); r.Status != checks.Capped || r.Answers != 0 || !strings.Contains(r.Error, "social searches") {
		t.Fatalf("run = %+v, want capped before asking anything, since social searches spent the month's cap", r)
	}
}

func TestTregRefusalStopsTheRun(t *testing.T) {
	ctx := context.Background()
	e := setup(t, nil)
	var mu sync.Mutex
	answers := 0
	e.treg.Before(func(c tregtest.Call) *tregtest.Refusal {
		if strings.HasSuffix(c.Tool, "-models") {
			return nil
		}
		mu.Lock()
		defer mu.Unlock()
		if answers++; answers > 2 {
			return &tregtest.Refusal{Status: 402, Code: "insufficient_balance"}
		}
		return nil
	})
	id := e.runNow(t)
	if err := e.checks.Work(ctx); err != nil {
		t.Fatal(err)
	}
	r := e.run(t, id)
	if r.Status != checks.Refused || r.RefusalCode != "insufficient_balance" || r.JobStatus != "refused" {
		t.Fatalf("run = %+v, want refused with Treg's code", r)
	}
	if r.Answers != 2 || r.Failed != 0 || e.treg.Answers() != 3 {
		t.Fatalf("%d answers, %d failed, %d calls; want the 2 before the refusal, no failed answer, and no call after it", r.Answers, r.Failed, e.treg.Answers())
	}
}

func TestRateLimitBacksOff(t *testing.T) {
	ctx := context.Background()
	e := setup(t, nil)
	refused := false
	e.treg.Before(func(c tregtest.Call) *tregtest.Refusal {
		if c.Tool == claudeLive && !refused {
			refused = true
			return &tregtest.Refusal{Status: 429, Code: "org_rate_limit"}
		}
		return nil
	})
	id := e.runNow(t)
	if err := e.checks.Work(ctx); err != nil {
		t.Fatal(err)
	}
	if r := e.run(t, id); r.Status != checks.Completed || r.Answers != 12 || r.Failed != 0 || len(e.slept) != 1 {
		t.Fatalf("run = %+v after %d waits, want completed after waiting once", r, len(e.slept))
	}
}

func TestKilledRunResumes(t *testing.T) {
	e := setup(t, func(s *config.Settings) { s.Concurrency = 1 })
	e.treg.SetCost(perplexityLive, nil)
	ctx, kill := context.WithCancel(context.Background())
	var mu sync.Mutex
	calls := 0
	e.treg.Before(func(c tregtest.Call) *tregtest.Refusal {
		mu.Lock()
		defer mu.Unlock()
		if !strings.HasSuffix(c.Tool, "-models") {
			if calls++; calls == 5 {
				kill()
			}
		}
		return nil
	})
	id := e.runNow(t)
	if err := e.checks.Work(ctx); err != context.Canceled {
		t.Fatalf("work = %v, want context.Canceled", err)
	}
	if r := e.run(t, id); r.Status != checks.Running || r.Answers != 4 {
		t.Fatalf("after the kill: %+v, want running with the 4 answers stored before it", r)
	}

	after := e.restart()
	if err := after.Work(context.Background()); err != nil {
		t.Fatal(err)
	}
	r := e.run(t, id)
	if r.Status != checks.Completed || r.Answers != 12 || r.Failed != 0 || !near(r.Spend, 0.10) || r.Missing != 2 {
		t.Fatalf("after resuming: %+v, want completed with 12 answers, $0.10 and 2 missing", r)
	}
	replays := 0
	for _, c := range e.treg.Calls() {
		if c.Replay {
			replays++
		}
	}
	if fresh := e.treg.Answers(); fresh != 12 || replays != 1 {
		t.Fatalf("%d paid calls and %d replays, want 12 and 1: the killed call is replayed by its idempotency key, not paid twice", fresh, replays)
	}
	var runs int
	if err := e.db.QueryRow(context.Background(), `SELECT count(*) FROM run`).Scan(&runs); err != nil || runs != 1 {
		t.Fatalf("%d runs, want 1", runs)
	}
}
