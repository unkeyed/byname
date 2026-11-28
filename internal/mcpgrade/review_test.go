package mcpgrade_test

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/unkeyed/byname/internal/checks/checkstest"
	"github.com/unkeyed/byname/internal/core"
	"github.com/unkeyed/byname/internal/engines/tregtest"
	"github.com/unkeyed/byname/internal/mcpgrade"
)

const goodReview = `{"title":{"score":80,"reason":"Clear."},"description":{"score":60,"reason":"Say what it returns."},"inputs":{"score":40,"reason":"Describe the query."}}`

func (e *env) reviewing(capUSD *float64, reply map[string]string) {
	e.t.Helper()
	st := checkstest.Settings()
	st.SpendCapMonthUSD = capUSD
	if _, err := e.config.PutSettings(e.ctx, st); err != nil {
		e.t.Fatal(err)
	}
	if _, err := e.config.SetCredential(e.ctx, "treg", "treg_test_token", ""); err != nil {
		e.t.Fatal(err)
	}
	e.treg.Reply(func(c tregtest.Call) []byte {
		for name, text := range reply {
			if strings.Contains(c.Sent, "Tool name: "+name+`\n`) {
				return tregtest.LLMBody(text)
			}
		}
		return tregtest.LLMBody(goodReview)
	})
}

func (e *env) graded(version string, names ...string) {
	e.t.Helper()
	r := mcpgrade.Report{Rules: "2026-11", Score: 72, Grade: "B", Categories: []mcpgrade.Category{}}
	for i, n := range names {
		r.Tools = append(r.Tools, mcpgrade.Tool{Name: n, Description: "Does " + n + ".", Params: []mcpgrade.Param{}, Hints: []string{}, Marks: []mcpgrade.ToolMark{},
			Score: 90 - 10*i, Key: n + "-" + version})
	}
	b, err := json.Marshal(r)
	if err != nil {
		e.t.Fatal(err)
	}
	if _, err := e.db.Exec(e.ctx, `INSERT INTO mcp_grade (url, score, grade, rules, report) VALUES ('https://mcp.northwind.example/mcp', 72, 'B', '2026-11', $1)`, b); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) review() mcpgrade.Server {
	e.t.Helper()
	if _, err := e.svc.StartReview(e.ctx); err != nil {
		e.t.Fatal(err)
	}
	if err := e.svc.Tick(e.ctx); err != nil {
		e.t.Fatal(err)
	}
	v, err := e.svc.Server(e.ctx)
	if err != nil {
		e.t.Fatal(err)
	}
	return v
}

func (e *env) asked() []string {
	var out []string
	name := regexp.MustCompile(`Tool name: ([a-z_]+)`)
	for _, c := range e.treg.Calls() {
		if m := name.FindStringSubmatch(c.Sent); m != nil {
			out = append(out, m[1])
		}
	}
	return out
}

func (e *env) count(sql string, args ...any) int {
	e.t.Helper()
	var n int
	if err := e.db.QueryRow(e.ctx, sql, args...).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

func TestReviewScoresEachDefinitionOnce(t *testing.T) {
	e := newEnv(t)
	e.reviewing(nil, map[string]string{"fetch": `{"title":{"score":100,"reason":""},"description":{"score":100,"reason":""},"inputs":{"score":100,"reason":""}}`})
	e.graded("v1", "search", "fetch", "list")

	v, err := e.svc.Server(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if r := v.Review; r.Status != "none" || !r.HasToken || r.Tools != 3 || r.Reviewed != 0 || r.Score != nil || r.EstimateTools != 3 || math.Abs(r.EstimateUSD-0.009) > 1e-9 {
		t.Fatalf("before a review: %+v, want three tools at the $0.003 sample price", r)
	}
	if v, err = e.svc.StartReview(e.ctx); err != nil || v.Review.Status != "queued" {
		t.Fatalf("StartReview = %+v, %v", v.Review, err)
	}
	if again, err := e.svc.StartReview(e.ctx); err != nil || again.Review.Status != "queued" || e.count(`SELECT count(*) FROM job_run WHERE job = 'mcp_review'`) != 1 {
		t.Fatalf("asking again queued a second review: %+v, %v", again.Review, err)
	}
	if err := e.svc.Tick(e.ctx); err != nil {
		t.Fatal(err)
	}
	if v, err = e.svc.Server(e.ctx); err != nil {
		t.Fatal(err)
	}
	r := v.Review
	if r.Status != "completed" || r.Reviewed != 3 || r.EstimateTools != 0 || r.CostUSD == nil || *r.CostUSD != 0.03 || r.Score == nil || *r.Score != 71 {
		t.Fatalf("after the review: %+v, want 3 reviewed at $0.01 each, the mean of 56, 100 and 56", r)
	}
	if got := e.asked(); !slices.Equal(got, []string{"list", "fetch", "search"}) {
		t.Errorf("asked about %v, want the worst rule score first", got)
	}
	search := v.Latest.Tools[0].Review
	if search == nil || *search != (mcpgrade.ToolReview{Score: 56,
		Title:       mcpgrade.ReviewPart{Score: 80, Reason: "Clear."},
		Description: mcpgrade.ReviewPart{Score: 60, Reason: "Say what it returns."},
		Inputs:      mcpgrade.ReviewPart{Score: 40, Reason: "Describe the query."}}) {
		t.Errorf("search's review = %+v", search)
	}
	if v.Latest.Score != 72 {
		t.Errorf("the review moved the grade's score to %d", v.Latest.Score)
	}
	key := regexp.MustCompile(`^byname/mcp-review/search-v1/2026-11/1$`)
	if c := e.treg.Calls()[2]; !key.MatchString(c.IdempotencyKey) {
		t.Errorf("idempotency key %q", c.IdempotencyKey)
	}
	if _, err := e.svc.StartReview(e.ctx); !errors.Is(err, core.ErrInvalid) {
		t.Errorf("StartReview with every tool reviewed = %v, want ErrInvalid", err)
	}

	e.treg.Reset()
	e.graded("v1", "search", "fetch", "list")
	if _, err := e.db.Exec(e.ctx, `UPDATE mcp_grade SET report = jsonb_set(report, '{tools,2,key}', '"list-v2"') WHERE id = (SELECT max(id) FROM mcp_grade)`); err != nil {
		t.Fatal(err)
	}
	if v, _ = e.svc.Server(e.ctx); v.Review.Reviewed != 2 || v.Review.EstimateTools != 1 || v.Latest.Tools[2].Review != nil {
		t.Fatalf("a newer grade with list changed: %+v", v.Review)
	}
	v = e.review()
	if got := e.asked(); !slices.Equal(got, []string{"list"}) || v.Review.Reviewed != 3 || *v.Review.CostUSD != 0.01 {
		t.Errorf("reviewing the changed tool asked about %v, then %+v; want only list", got, v.Review)
	}
	var spent float64
	if err := e.db.QueryRow(e.ctx, `SELECT sum(usd)::float8 FROM spend WHERE source = 'mcp_review'`).Scan(&spent); err != nil || spent != 0.04 {
		t.Errorf("spend counts %v of reviews, %v; want the four paid calls", spent, err)
	}
}

func TestReviewStopsAtTheMonthlyCap(t *testing.T) {
	e := newEnv(t)
	e.reviewing(new(0.025), nil)
	e.graded("v1", "a", "b", "c", "d", "e")
	v := e.review()
	if v.Review.Status != "capped" || v.Review.Reviewed != 3 || !strings.Contains(v.Review.Error, "this month has spent $0.03; the monthly spend cap is $0.03") {
		t.Fatalf("review = %+v, want capped after 3 calls of $0.01 crossed the $0.025 cap", v.Review)
	}
	if _, err := e.svc.StartReview(e.ctx); !errors.Is(err, core.ErrInvalid) || !strings.Contains(err.Error(), "monthly spend cap") {
		t.Errorf("StartReview over the cap = %v, want ErrInvalid naming the cap", err)
	}
}

func TestReviewStopsWhenTregRefusesAndResumes(t *testing.T) {
	e := newEnv(t)
	e.reviewing(nil, nil)
	e.graded("v1", "a", "b", "c")
	calls := 0
	e.treg.Before(func(tregtest.Call) *tregtest.Refusal {
		if calls++; calls == 2 {
			return &tregtest.Refusal{Status: http.StatusPaymentRequired, Code: "insufficient_balance"}
		}
		return nil
	})
	v := e.review()
	if v.Review.Status != "refused" || v.Review.Reviewed != 1 || !strings.Contains(v.Review.Error, "insufficient_balance") {
		t.Fatalf("review = %+v, want refused after one", v.Review)
	}
	if n := e.count(`SELECT count(*) FROM mcp_review WHERE status = 'refused' AND cost_usd IS NULL AND tool_name = 'b'`); n != 1 {
		t.Errorf("%d refused rows for b, want one that cost nothing", n)
	}
	e.treg.Before(nil)
	e.treg.Reset()
	v = e.review()
	if got := e.asked(); v.Review.Status != "completed" || v.Review.Reviewed != 3 || !slices.Equal(got, []string{"b", "a"}) {
		t.Fatalf("resumed review asked %v and ended %+v; want b and a, then all three reviewed", got, v.Review)
	}
	if k := e.treg.Calls()[0].IdempotencyKey; !strings.HasSuffix(k, "/b-v1/2026-11/1") {
		t.Errorf("the refused tool's retry key %q, want attempt 1 again: a refusal is no attempt", k)
	}
}

func TestReviewReplyThatIsNotJSONIsAPaidFailure(t *testing.T) {
	e := newEnv(t)
	e.reviewing(nil, map[string]string{"b": "I would rather not."})
	e.graded("v1", "a", "b", "c")
	v := e.review()
	if v.Review.Status != "completed" || v.Review.Reviewed != 2 || v.Review.EstimateTools != 1 || v.Latest.Tools[1].Review != nil {
		t.Fatalf("review = %+v, want the other two reviewed and b left to do", v.Review)
	}
	if n := e.count(`SELECT count(*) FROM mcp_review WHERE tool_name = 'b' AND status = 'failed' AND cost_usd = 0.01 AND error LIKE '%no JSON object%'`); n != 1 {
		t.Errorf("%d failed rows for b, want one with its cost and why", n)
	}
	e.reviewing(nil, nil)
	e.treg.Reset()
	e.review()
	if k := e.treg.Calls()[0].IdempotencyKey; !strings.HasSuffix(k, "/b-v1/2026-11/2") {
		t.Errorf("the retry's key %q, want attempt 2, a new paid call", k)
	}
}

func TestStartReviewNeedsAGradeToolsAndAToken(t *testing.T) {
	e := newEnv(t)
	if _, err := e.svc.StartReview(e.ctx); !errors.Is(err, core.ErrInvalid) {
		t.Errorf("StartReview without a grade = %v, want ErrInvalid", err)
	}
	e.graded("v1")
	if _, err := e.svc.StartReview(e.ctx); !errors.Is(err, core.ErrInvalid) {
		t.Errorf("StartReview of a grade with no tools = %v, want ErrInvalid", err)
	}
	e.graded("v1", "a")
	if _, err := e.svc.StartReview(e.ctx); !errors.Is(err, core.ErrInvalid) || !strings.Contains(err.Error(), "Treg token") {
		t.Errorf("StartReview without a Treg token = %v, want ErrInvalid naming the token", err)
	}
	if n := e.count(`SELECT count(*) FROM job_run`); n != 0 {
		t.Errorf("%d jobs queued by refused starts", n)
	}
}

func TestTickFailsAReviewCutOffMidway(t *testing.T) {
	e := newEnv(t)
	e.reviewing(nil, nil)
	e.graded("v1", "a")
	if _, err := e.db.Exec(e.ctx, `INSERT INTO job_run (job, trigger, status, started_at) VALUES ('mcp_review', 'run_now', 'running', now())`); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Tick(e.ctx); err != nil {
		t.Fatal(err)
	}
	v, _ := e.svc.Server(e.ctx)
	if v.Review.Status != "failed" || !strings.Contains(v.Review.Error, "reviewing again does the rest") || len(e.treg.Calls()) != 0 {
		t.Errorf("a review left running = %+v after %d calls, want failed with how to resume", v.Review, len(e.treg.Calls()))
	}
}

func TestReviewRecordsACallCutOffByShutdown(t *testing.T) {
	e := newEnv(t)
	e.reviewing(nil, nil)
	e.graded("v1", "a", "b")
	if _, err := e.svc.StartReview(e.ctx); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(e.ctx)
	e.treg.Reply(func(tregtest.Call) []byte {
		cancel()
		return tregtest.LLMBody(goodReview)
	})
	if err := e.svc.Tick(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Tick = %v, want context.Canceled", err)
	}
	e.reviewing(nil, nil)
	if err := e.svc.Tick(e.ctx); err != nil {
		t.Fatal(err)
	}
	if v := e.review(); v.Review.Status != "completed" || v.Review.Reviewed != 2 {
		t.Fatalf("review after the shutdown = %+v, want both reviewed", v.Review)
	}
	var spent float64
	if err := e.db.QueryRow(e.ctx, `SELECT sum(usd)::float8 FROM spend WHERE source = 'mcp_review'`).Scan(&spent); err != nil || spent != 0.02 {
		t.Errorf("spend counts %v of reviews, %v; want the call shutdown cut off counted once", spent, err)
	}
	for _, c := range e.treg.Calls() {
		if !strings.HasSuffix(c.IdempotencyKey, "/1") {
			t.Errorf("key %q, want every tool on its first attempt", c.IdempotencyKey)
		}
	}
}

func TestReviewRecoversAPaidAnswerThatWasLost(t *testing.T) {
	e := newEnv(t)
	e.reviewing(nil, nil)
	e.graded("v1", "a", "b")
	e.treg.Lose(func(c tregtest.Call) bool { return strings.Contains(c.Sent, `Tool name: b\n`) })
	if _, err := e.svc.StartReview(e.ctx); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Tick(e.ctx); err == nil {
		t.Fatal("Tick = nil, want the lost answer's error")
	}
	if v, _ := e.svc.Server(e.ctx); v.Review.Status != "failed" || e.count(`SELECT count(*) FROM mcp_review`) != 0 {
		t.Fatalf("review = %+v, want failed with nothing recorded for the lost answer", v.Review)
	}
	e.treg.Lose(nil)
	e.treg.Reset()
	if v := e.review(); v.Review.Status != "completed" || v.Review.Reviewed != 2 {
		t.Fatalf("review = %+v, want both reviewed", v.Review)
	}
	if c := e.treg.Calls()[0]; !strings.HasSuffix(c.IdempotencyKey, "/b-v1/2026-11/1") || !c.Replay {
		t.Errorf("b's retry key %q, replay %v; want attempt 1 again so Treg replays the paid answer", c.IdempotencyKey, c.Replay)
	}
	var spent float64
	if err := e.db.QueryRow(e.ctx, `SELECT sum(usd)::float8 FROM spend WHERE source = 'mcp_review'`).Scan(&spent); err != nil || spent != 0.02 {
		t.Errorf("spend counts %v of reviews, %v; want b's lost answer counted once", spent, err)
	}
}

func TestConcurrentStartsQueueOneReview(t *testing.T) {
	e := newEnv(t)
	e.reviewing(nil, nil)
	e.graded("v1", "a")
	errs := make(chan error, 8)
	var wg sync.WaitGroup
	for range cap(errs) {
		wg.Go(func() {
			_, err := e.svc.StartReview(e.ctx)
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if n := e.count(`SELECT count(*) FROM job_run WHERE job = 'mcp_review'`); n != 1 {
		t.Errorf("%d reviews queued by concurrent starts, want one", n)
	}
}
