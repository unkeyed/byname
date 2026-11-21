package results_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/unkeyed/byname/internal/checks"
	"github.com/unkeyed/byname/internal/checks/checkstest"
	"github.com/unkeyed/byname/internal/config"
	"github.com/unkeyed/byname/internal/core"
	"github.com/unkeyed/byname/internal/engines"
	"github.com/unkeyed/byname/internal/engines/tregtest"
	"github.com/unkeyed/byname/internal/panel"
	"github.com/unkeyed/byname/internal/results"
	"github.com/unkeyed/byname/internal/secretbox"
	"github.com/unkeyed/byname/internal/testdb"
)

var ctx = context.Background()

const (
	claudeLive     = "dataforseo.x.ai-optimization-claude-llm-responses-live"
	perplexityLive = "dataforseo.x.ai-optimization-perplexity-llm-responses-live"
)

// Claude naming only the competitor, citing nothing.
const claudeLoses = `{"status_code":20000,"tasks":[{"status_code":20000,"result":[{"model_name":"claude-sonnet-4-5-20250929",
	"items":[{"type":"message","sections":[{"type":"text","text":"Contoso Gateway is the usual choice for this."}]}]}]}]}`

type env struct {
	db      *pgxpool.Pool
	config  *config.Service
	checks  *checks.Service
	results *results.Service
	treg    *tregtest.Server
}

func setup(t *testing.T) *env {
	t.Helper()
	e := &env{db: testdb.New(t), treg: tregtest.New(t)}
	e.config = config.New(e.db, config.Options{Secrets: secretbox.ForTest(), TregBaseURL: e.treg.URL})
	checkstest.Seed(t, e.config, nil)
	e.checks = checks.New(e.db, e.config, checks.Options{})
	e.results = results.New(e.db)
	e.treg.SetCost(perplexityLive, nil)
	return e
}

func (e *env) check(t *testing.T) int64 {
	t.Helper()
	run, _, err := e.checks.RunNow(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.checks.Work(ctx); err != nil {
		t.Fatal(err)
	}
	return run.ID
}

func (e *env) pairID(t *testing.T, s engines.Surface) int64 {
	t.Helper()
	var id int64
	if err := e.db.QueryRow(ctx, `SELECT id FROM surface_model WHERE surface_key = $1`, s).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestRunsAnswersAndCompare(t *testing.T) {
	e := setup(t)
	first := e.check(t)
	if !t.Run("runs", func(t *testing.T) {
		runs, err := e.results.Runs(ctx, results.RunsQuery{})
		if err != nil || len(runs) != 1 {
			t.Fatalf("runs = %+v %v", runs, err)
		}
		r := runs[0]
		if r.Status != "completed" || r.Calls != 12 || r.Succeeded != 12 || r.Mentioned != 8 || r.Cited != 6 || r.SpendMissingCalls != 2 || r.QuestionSetVersion != 1 {
			t.Fatalf("run = %+v; want 12 calls, Northwind named by Claude, Codex, Gemini and AI Overview and cited by Claude, Gemini and AI Overview, on both questions", r)
		}
		if len(r.Endpoints) != 6 {
			t.Fatalf("endpoints = %+v, want one per endpoint", r.Endpoints)
		}
		for _, ep := range r.Endpoints {
			if ep.SuccessRate != 1 || ep.Calls != 2 {
				t.Errorf("%s: %+v, want 2 calls all succeeded", ep.Endpoint, ep)
			}
			if ep.Endpoint == perplexityLive && (ep.MissingCost != 2 || ep.CostPerCall != nil) {
				t.Errorf("perplexity: %+v, want 2 missing costs and no cost per call", ep)
			}
			if ep.Endpoint == claudeLive && (ep.CostPerCall == nil || *ep.CostPerCall != 0.01) {
				t.Errorf("claude: %+v, want $0.01 per call", ep)
			}
		}
	}) {
		return
	}

	answers, err := e.results.RunAnswers(ctx, first)
	if err != nil || len(answers) != 12 {
		t.Fatalf("answers = %d %v", len(answers), err)
	}
	var claude results.AnswerSummary
	for _, a := range answers {
		if a.Endpoint == claudeLive {
			claude = a
			break
		}
	}
	if len(claude.Mentions) != 3 || claude.Mentions[0].Name != "Northwind" || claude.Mentions[2].Position != nil || claude.CitationCount != 2 {
		t.Fatalf("claude answer = %+v, want Northwind, Contoso, then Redis without a position, and 2 citations", claude)
	}
	full, err := e.results.Answer(ctx, claude.ID)
	if err != nil || full.Text == "" || len(full.Raw) == 0 || len(full.Citations) != 2 || !full.Citations[1].IsSelf {
		t.Fatalf("answer = %+v %v, want text, raw JSON and citations", full, err)
	}
	_, err = e.results.Answer(ctx, 999999)
	is(t, err, core.ErrNotFound)

	e.treg.SetBody(claudeLive, []byte(claudeLoses))
	second := e.check(t)
	c, err := e.results.Compare(ctx, first, second)
	if err != nil {
		t.Fatal(err)
	}
	if c.Counts[panel.Lost] != 2 || c.Counts[panel.Unchanged] != 10 || c.Net != -2 || len(c.Pairs) != 12 {
		t.Fatalf("compare counts %v net %d over %d pairs, want Claude lost on both questions and the rest unchanged", c.Counts, c.Net, len(c.Pairs))
	}

	gaps, err := e.results.Gaps(ctx, 0)
	if err != nil || gaps.RunID != second || len(gaps.Gaps) != 2 {
		t.Fatalf("gaps = %+v %v, want both questions of the latest run", gaps, err)
	}
	g := gaps.Gaps[0]
	if g.Engines != 6 || g.Named != 3 || g.Opportunity != 2 || len(g.NamedOnMiss) != 1 || g.NamedOnMiss[0] != "Contoso" || g.Demand != nil {
		t.Fatalf("gap = %+v, want 3 of 6 named you, opportunity 2, Contoso named where you were not", g)
	}

	share, err := e.results.Share(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	claudePair := e.pairID(t, engines.ClaudeCode)
	for _, s := range share {
		if s.Pair.ID != claudePair {
			continue
		}
		p := s.Points[0]
		if p.Mentions != 4 || len(p.Brands) != 2 || p.Brands[0].Share != 0.5 {
			t.Fatalf("claude share on run 1 = %+v, want Northwind and Contoso at half each and Redis left out", p)
		}
		if p := s.Points[1]; p.Mentions != 2 || len(p.Brands) != 1 || p.Brands[0].Name != "Contoso" || p.Brands[0].Share != 1 {
			t.Fatalf("claude share on run 2 = %+v, want all Contoso", p)
		}
	}
}

func TestSeriesFollowQuestionVersionsAndSplitOnFidelity(t *testing.T) {
	e := setup(t)
	first := e.check(t)
	v1, _ := e.config.CurrentQuestions(ctx)

	e.treg.SetBody(claudeLive, []byte(claudeLoses))
	second := e.check(t)

	edit := config.QuestionsInput{Questions: []config.QuestionInput{
		{Group: "Rate limiting", Text: "How should I rate limit a public REST API?", Replaces: []int64{v1.Questions[0].ID}},
		checkstest.Questions[1],
	}}
	v2, err := e.config.PutQuestions(ctx, edit)
	if err != nil {
		t.Fatal(err)
	}
	claudePair := e.pairID(t, engines.ClaudeCode)
	if _, err := e.db.Exec(ctx, `UPDATE surface_model SET fidelity = 'approximation' WHERE id = $1`, claudePair); err != nil {
		t.Fatal(err)
	}
	third := e.check(t)

	h, err := e.results.QuestionHistory(ctx, v2.Questions[0].ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	var claude []results.HistorySeries
	for _, s := range h.Series {
		if s.Pair.ID == claudePair {
			claude = append(claude, s)
		}
	}
	if len(claude) != 2 || len(claude[0].Points) != 2 || claude[0].Fidelity != "model_match" || len(claude[1].Points) != 1 || claude[1].Fidelity != "approximation" {
		t.Fatalf("claude history = %+v, want runs 1 and 2 as model_match and run 3 as its own approximation series", claude)
	}
	if got := claude[0].Points[1].Summary; got != "Lost mention (was #1) · Northwind no longer cited" {
		t.Errorf("summary = %q", got)
	}
	if claude[0].Points[0].QuestionID != v1.Questions[0].ID || claude[1].Points[0].QuestionID != v2.Questions[0].ID {
		t.Errorf("history points = %+v, want the version 1 question for runs 1 and 2", claude)
	}

	c, err := e.results.Compare(ctx, second, third)
	if err != nil {
		t.Fatal(err)
	}
	if c.Counts[panel.NotComparable] != 2 || c.Counts[panel.Gained] != 0 {
		t.Fatalf("compare across the fidelity change: %v, want Claude's two pairs not comparable", c.Counts)
	}
	c, err = e.results.Compare(ctx, first, third)
	if err != nil || c.Counts[panel.Unchanged] != 10 {
		t.Fatalf("compare across question versions: %v %v, want the 10 other pairs matched through the map", c.Counts, err)
	}
	_, err = e.results.Compare(ctx, third, first)
	is(t, err, core.ErrInvalid)

	rates, err := e.results.Rates(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rates) != 7 {
		t.Fatalf("%d rate series, want 6 pairs plus Claude's approximation series", len(rates))
	}
	for _, r := range rates {
		if r.Pair.ID == claudePair && r.Fidelity == "model_match" {
			if len(r.Points) != 2 || *r.Points[0].MentionRate != 1 || *r.Points[1].MentionRate != 0 || *r.Points[0].CitationRate != 1 {
				t.Fatalf("claude rates = %+v, want mention rate 1 then 0", r.Points)
			}
		}
	}
	jobs, err := e.results.Jobs(ctx, 0)
	if err != nil || len(jobs) != 3 || jobs[0].RunID == nil || *jobs[0].RunID != third || jobs[0].SpendUSD == nil {
		t.Fatalf("jobs = %+v %v", jobs, err)
	}
}

func is(t *testing.T, err, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
}
