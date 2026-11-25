package results_test

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

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
	checkstest.Seed(t, e.config, e.treg, nil)
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
	// Two answers that stopped naming you, against none that started, is
	// McNemar's (|0-2|-1)²/2 = 0.5, well within chance.
	if m := c.Move; m.Cells != 12 || m.Real || math.Abs(m.PValue-0.4795001221869535) > 1e-9 || math.Abs(*m.BeforeRate-*m.AfterRate-2.0/12) > 1e-9 {
		t.Fatalf("move = %+v, want 12 cells, 2 of 12 fewer named, p 0.48, not real", m)
	}

	gaps, err := e.results.Gaps(ctx, 0, results.Scope{})
	if err != nil || gaps.RunID != second || len(gaps.Gaps) != 2 {
		t.Fatalf("gaps = %+v %v, want both questions of the latest run", gaps, err)
	}
	g := gaps.Gaps[0]
	if g.Engines != 6 || g.Named != 3 || g.Opportunity != 2 || len(g.NamedOnMiss) != 1 || g.NamedOnMiss[0] != "Contoso" || g.Demand != nil {
		t.Fatalf("gap = %+v, want 3 of 6 named you, opportunity 2, Contoso named where you were not", g)
	}

	share, err := e.results.Share(ctx, nil, results.Scope{})
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
		{Group: "Deploying", Text: "How should I deploy a public REST API?", Replaces: []int64{v1.Questions[0].ID}},
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
	if h.Question.ID != v2.Questions[0].ID || h.Question.Text != v2.Questions[0].Text {
		t.Errorf("history question = %+v, want the current version %+v", h.Question, v2.Questions[0])
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

	rates, err := e.results.Rates(ctx, nil, results.Scope{}, results.ByPair)
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

const claudeModels = `{"status_code":20000,"tasks":[{"status_code":20000,"result":[
	{"model_name":"claude-sonnet-5-5","web_search_supported":true},{"model_name":"claude-opus-5-5","web_search_supported":true}]}]}`

// TestScreens checks what the overview, questions table and competitors
// screens read, with Claude Code asked on two models: both name Northwind
// first and Contoso second on the first run, and only Contoso on the second.
func TestScreens(t *testing.T) {
	e := setup(t)
	e.treg.SetBody("dataforseo.x.ai-optimization-claude-llm-responses-models", []byte(claudeModels))
	if _, err := e.config.PutSurface(ctx, engines.ClaudeCode, config.SurfaceInput{Enabled: true,
		Models: []config.PairInput{{Model: "claude-sonnet-5-5"}, {Model: "claude-opus-5-5"}}}); err != nil {
		t.Fatal(err)
	}
	first := e.check(t)
	e.treg.SetBody(claudeLive, []byte(claudeLoses))
	second := e.check(t)
	claude := results.Scope{Surface: engines.ClaudeCode}

	t.Run("rates by surface combine a surface's models", func(t *testing.T) {
		lines, err := e.results.Rates(ctx, nil, results.Scope{}, results.BySurface)
		if err != nil {
			t.Fatal(err)
		}
		if len(lines) != 6 {
			t.Fatalf("%d lines, want one per surface", len(lines))
		}
		var cc *results.RateSeries
		for i := range lines {
			if lines[i].Pair.Surface == engines.ClaudeCode {
				cc = &lines[i]
			}
		}
		if cc == nil || cc.Pair.ID != 0 || cc.Pair.Model != "" || len(cc.Models) != 2 || cc.Models[0] != "claude-opus-5-5" {
			t.Fatalf("claude code line = %+v, want no pair and both models", cc)
		}
		p := cc.Points
		if len(p) != 2 || p[0].Answered != 4 || p[0].Mentioned != 4 || *p[0].MentionRate != 1 || p[1].Mentioned != 0 || *p[1].MentionRate != 0 {
			t.Fatalf("claude code points = %+v, want 4 of 4 named then 0 of 4", p)
		}
		pairs, err := e.results.Rates(ctx, nil, claude, results.ByPair)
		if err != nil || len(pairs) != 2 || pairs[0].Pair.ID == pairs[1].Pair.ID || len(pairs[0].Models) != 1 {
			t.Fatalf("claude code by pair = %+v %v, want a line per model", pairs, err)
		}
		opus, err := e.results.Rates(ctx, nil, results.Scope{Model: "claude-opus-5-5"}, results.ByPair)
		if err != nil || len(opus) != 1 || opus[0].Pair.Model != "claude-opus-5-5" {
			t.Fatalf("opus only = %+v %v", opus, err)
		}
		_, err = e.results.Rates(ctx, nil, results.Scope{Surface: "myspace"}, results.ByPair)
		is(t, err, core.ErrInvalid)
		_, err = e.results.Rates(ctx, nil, results.Scope{}, "engine")
		is(t, err, core.ErrInvalid)
	})

	t.Run("summary counts pairs and your share", func(t *testing.T) {
		runs, err := e.results.Summary(ctx, nil, claude)
		if err != nil || len(runs) != 2 {
			t.Fatalf("summary = %+v %v", runs, err)
		}
		r1, r2 := runs[0], runs[1]
		if r1.RunID != first || r1.Answered != 4 || r1.Mentioned != 4 || r1.Cited != 4 || r1.Mentions != 8 || *r1.Share != 0.5 {
			t.Fatalf("run 1 = %+v, want 4 of 4 named and cited, half of 8 mentions yours", r1)
		}
		if r2.Mentioned != 0 || *r2.MentionRate != 0 || r2.Mentions != 4 || *r2.Share != 0 {
			t.Fatalf("run 2 = %+v, want none named and Contoso's 4 mentions", r2)
		}
		// Wilson's band on 4 of 4 is 51% to 100%, and on 0 of 4 0% to 49%.
		if b := r1.MentionBand; b == nil || math.Abs(b.Low-0.5101091635454027) > 1e-9 || b.High != 1 {
			t.Errorf("run 1 mention band = %+v, want 0.51 to 1", b)
		}
		if b := r2.MentionBand; b == nil || b.Low != 0 || math.Abs(b.High-0.4898908364545973) > 1e-9 {
			t.Errorf("run 2 mention band = %+v, want 0 to 0.49", b)
		}
	})

	t.Run("competitors rank brands and fill the heatmap", func(t *testing.T) {
		c, err := e.results.Competitors(ctx, nil, claude, results.ByPair)
		if err != nil {
			t.Fatal(err)
		}
		if c.Runs != 2 || c.Answered != 8 || c.Mentions != 12 || c.BrandsNamed != 2 || len(c.Brands) != 2 {
			t.Fatalf("competitors = %+v, want 2 runs, 8 answers, Contoso 8 and Northwind 4 mentions", c)
		}
		if b := c.Brands[0]; b.Name != "Contoso" || b.Rank != 1 || b.Mentions != 8 {
			t.Errorf("first = %+v, want Contoso ranked 1", b)
		}
		if b := c.Brands[1]; b.Name != "Northwind" || b.Rank != 2 || b.Mentions != 4 || b.Share != 4.0/12 {
			t.Errorf("second = %+v, want Northwind ranked 2 with a third of mentions", b)
		}
		if len(c.Columns) != 2 || c.Columns[0].Answered != 4 {
			t.Fatalf("columns = %+v, want one per Claude model with 4 answers", c.Columns)
		}
		for _, cell := range c.Columns[0].Cells {
			if cell.BrandID == c.Brands[1].BrandID && (cell.Named != 2 || cell.Rate != 0.5) {
				t.Errorf("Northwind cell = %+v, want named in 2 of 4", cell)
			}
		}
		none, err := e.results.Competitors(ctx, new(time.Now().Add(time.Hour)), results.Scope{}, results.ByPair)
		if err != nil || none.Runs != 0 || len(none.Brands) != 1 || none.Brands[0].Rank != 0 || none.BrandsNamed != 0 {
			t.Fatalf("no runs in range = %+v %v, want only you, unranked", none, err)
		}
	})

	t.Run("questions table", func(t *testing.T) {
		tbl, err := e.results.RunQuestions(ctx, second, claude)
		if err != nil {
			t.Fatal(err)
		}
		if tbl.PreviousRunID == nil || *tbl.PreviousRunID != first || len(tbl.Columns) != 2 || len(tbl.Rows) != 2 {
			t.Fatalf("table = %+v, want 2 questions by 2 Claude models, changes since run %d", tbl, first)
		}
		for _, row := range tbl.Rows {
			if row.Named != 0 || row.Lost != 2 || row.Answered != 2 || row.BestPosition != nil || row.Question.Text == "" {
				t.Errorf("row = %+v, want both models lost", row)
			}
			for _, c := range row.Cells {
				if c == nil || c.Outcome != panel.Lost {
					t.Errorf("cell = %+v, want lost", c)
				}
			}
		}
		all, err := e.results.RunQuestions(ctx, first, results.Scope{})
		if err != nil || all.PreviousRunID != nil || len(all.Columns) != 7 {
			t.Fatalf("first run table = %+v %v, want 7 columns and no previous run", all, err)
		}
		row := all.Rows[0]
		if row.Named != 5 || row.BestPosition == nil || *row.BestPosition != 1 || row.Cells[0].Outcome != panel.NotComparable {
			t.Fatalf("first row = %+v, want named by both Claude models, Codex, Gemini and AI Overview at best #1, nothing to compare", row)
		}
		_, err = e.results.RunQuestions(ctx, 999999, results.Scope{})
		is(t, err, core.ErrNotFound)
	})

	t.Run("compare names each question and pair", func(t *testing.T) {
		c, err := e.results.Compare(ctx, first, second)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range c.Pairs {
			if p.Question.ID != p.QuestionID || p.Question.Text == "" || p.Pair.ID != p.PairID || p.Pair.Surface == "" {
				t.Fatalf("pair change = %+v, want its question and pair", p)
			}
		}
	})

	t.Run("history sums each series", func(t *testing.T) {
		v1, _ := e.config.CurrentQuestions(ctx)
		h, err := e.results.QuestionHistory(ctx, v1.Questions[0].ID, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range h.Series {
			if s.Pair.Surface != engines.ClaudeCode {
				continue
			}
			if s.Answered != 2 || s.Named != 1 || s.Cited != 1 || s.BestPosition == nil || *s.BestPosition != 1 {
				t.Errorf("%s history = %+v, want named and cited once at #1 in 2 runs", s.Pair.Model, s)
			}
		}
	})
}

// A pair asked each question three times compares, charts and ranks by
// what most of its answers say, and the move weighs every answer.
func TestACellOfSeveralAnswersReadsAsMostOfThem(t *testing.T) {
	e := setup(t)
	set, err := e.config.CurrentQuestions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	q1, q2 := set.Questions[0].ID, set.Questions[1].ID
	claude, perplexity := e.pairID(t, engines.ClaudeCode), e.pairID(t, engines.PerplexityApp)
	run := func() int64 {
		var id int64
		if err := e.db.QueryRow(ctx, `INSERT INTO run (question_set_id, trigger, status) VALUES ($1, 'run_now', 'completed') RETURNING id`, set.ID).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	// answers stores one cell's samples: n for named, - for not, x for failed.
	answers := func(run, question, pair int64, samples string) {
		for i, a := range samples {
			status, named := "ok", a == 'n'
			if a == 'x' {
				status = "failed"
			}
			var position *int
			if named {
				position = new(1)
			}
			if _, err := e.db.Exec(ctx, `INSERT INTO answer (run_id, question_id, surface_model_id, sample, endpoint, fidelity, status, attempts, mentioned, cited, position)
				VALUES ($1, $2, $3, $4, 'endpoint', 'model_match', $5, 1, $6, false, $7)`, run, question, pair, i+1, status, named, position); err != nil {
				t.Fatal(err)
			}
		}
	}
	before, after := run(), run()
	answers(before, q1, claude, "---")
	answers(after, q1, claude, "nnn")
	answers(before, q1, perplexity, "---")
	answers(after, q1, perplexity, "nn-")
	answers(before, q2, claude, "n--")
	answers(after, q2, claude, "nnn")
	answers(before, q2, perplexity, "nnn")
	answers(after, q2, perplexity, "nnx")

	c, err := e.results.Compare(ctx, before, after)
	if err != nil {
		t.Fatal(err)
	}
	if c.Counts[panel.Gained] != 3 || c.Counts[panel.Unchanged] != 1 || c.Net != 3 {
		t.Fatalf("counts %v net %d, want 3 cells most answers newly named you and 1 unchanged", c.Counts, c.Net)
	}
	for _, p := range c.Pairs {
		if p.QuestionID == q2 && p.PairID == perplexity {
			if a := p.After; a == nil || a.Answered != 2 || a.Named != 2 || !a.Mentioned || len(a.AnswerIDs) != 3 || a.AnswerID != a.AnswerIDs[0] {
				t.Errorf("after = %+v, want 2 of 2 answered named, all 3 answers listed", a)
			}
		}
	}
	// 10 of 11 answers name you against 4 of 12 a check before. Stratified
	// by cell, statsmodels' StratifiedTable.test_null_odds(correction=True)
	// gives 7.2, p 0.00729. Read as one answer per cell it would be 3
	// flips, McNemar's p 0.248.
	if m := c.Move; m.Cells != 4 || !m.Real || math.Abs(m.PValue-0.007290358091535665) > 1e-9 ||
		math.Abs(*m.BeforeRate-4.0/12) > 1e-9 || math.Abs(*m.AfterRate-10.0/11) > 1e-9 {
		t.Fatalf("move = %+v, want 4 cells, 4 of 12 named before and 10 of 11 after, p 0.0073, real", m)
	}

	h, err := e.results.QuestionHistory(ctx, q1, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range h.Series {
		if s.Pair.ID != perplexity {
			continue
		}
		if len(s.Points) != 2 || s.Answered != 2 || s.Named != 1 {
			t.Fatalf("perplexity history = %+v, want one point per check, named on the later one", s)
		}
		if p := s.Points[1]; p.Answered != 3 || p.Named != 2 || !p.Mentioned || len(p.AnswerIDs) != 3 || p.Summary != "Gained mention at #1" {
			t.Errorf("later point = %+v, want 2 of 3 named, all 3 answers listed, gained", p)
		}
	}

	table, err := e.results.RunQuestions(ctx, after, results.Scope{})
	if err != nil || len(table.Columns) != 2 || len(table.Rows) != 2 {
		t.Fatalf("questions table = %+v %v, want 2 questions on 2 pairs", table, err)
	}
	if r := table.Rows[0]; r.Answered != 2 || r.Named != 2 || r.Gained != 2 {
		t.Errorf("first row = %+v, want both cells answered, mostly named and gained", r)
	}
	for i, sr := range table.Columns {
		c := table.Rows[1].Cells[i]
		if sr.Pair.ID == perplexity && (c.Answered != 2 || c.Named != 2 || !c.Mentioned || len(c.AnswerIDs) != 3 || c.AnswerID != c.AnswerIDs[0] || c.Status != engines.StatusOK) {
			t.Errorf("perplexity cell = %+v, want 2 of 2 answered named, all 3 answers listed", c)
		}
	}

	gaps, err := e.results.Gaps(ctx, before, results.Scope{})
	if err != nil || len(gaps.Gaps) != 2 {
		t.Fatalf("gaps = %+v %v, want both questions of the earlier check", gaps, err)
	}
	for _, g := range gaps.Gaps {
		if g.Question.ID != q2 {
			continue
		}
		if g.Engines != 2 || g.Named != 1 || len(g.Pairs) != 2 {
			t.Fatalf("gap = %+v, want 2 pairs, most answers naming you on 1", g)
		}
		if p := g.Pairs[0]; p.Pair.ID != claude || p.Named || p.Answered != 3 || p.NamedAnswers != 1 || len(p.AnswerIDs) != 3 {
			t.Errorf("claude = %+v, want 1 of 3 named, not most", p)
		}
	}

	prompts, err := e.results.Prompts(ctx, nil, results.Scope{})
	if err != nil || len(prompts.Rows) != 2 || len(prompts.Runs) != 2 {
		t.Fatalf("prompts = %+v %v, want 2 questions over 2 checks", prompts, err)
	}
	if r := prompts.Rows[0]; r.Answered != 2 || len(r.NamedBy) != 2 {
		t.Errorf("first prompt = %+v, want 2 pairs answered and both named you, each listed once", r)
	}
	if d := prompts.Rows[1].Dots[0]; d == nil || d.Answered != 2 || d.Named != 1 || d.State != results.DotNamed {
		t.Errorf("second prompt's first dot = %+v, want 2 pairs answered, most answers named you on 1", d)
	}

	b, err := e.results.Brief(ctx, q1, after, results.Scope{})
	if err != nil || len(b.Lines) != 2 || b.Answered != 6 || b.Named != 5 {
		t.Fatalf("brief = %+v %v, want one line per pair, 5 of 6 answers named you", b, err)
	}
	for _, l := range b.Lines {
		if l.Pair.ID == perplexity && (l.Answered != 3 || l.Named != 2 || l.State != results.LineNamed || len(l.AnswerIDs) != 3 || l.AnswerID != l.AnswerIDs[0]) {
			t.Errorf("perplexity line = %+v, want 2 of 3 named, named most of the time, all 3 answers listed", l)
		}
	}
	for _, want := range []string{"got 6 answers, 3 from each app and model.", "That is what most of its 3 answers did: 2 named"} {
		if !strings.Contains(b.Prompt, want) {
			t.Errorf("brief prompt does not say %q:\n%s", want, b.Prompt)
		}
	}
	if b, err := e.results.Brief(ctx, q2, after, results.Scope{}); err != nil || b.Answered != 5 || !strings.Contains(b.Prompt, "got 5 answers, several from some apps and models.") {
		t.Errorf("brief on a cell with a failed answer = %+v %v, want 5 answers, several from some apps and models", b, err)
	}
}
