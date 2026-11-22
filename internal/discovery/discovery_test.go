package discovery_test

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/unkeyed/byname/internal/checks"
	"github.com/unkeyed/byname/internal/checks/checkstest"
	"github.com/unkeyed/byname/internal/config"
	"github.com/unkeyed/byname/internal/discovery"
	"github.com/unkeyed/byname/internal/engines"
	"github.com/unkeyed/byname/internal/engines/tregtest"
	"github.com/unkeyed/byname/internal/panel"
	"github.com/unkeyed/byname/internal/results"
	"github.com/unkeyed/byname/internal/secretbox"
	"github.com/unkeyed/byname/internal/testdb"
)

var ctx = context.Background()

type env struct {
	db      *pgxpool.Pool
	treg    *tregtest.Server
	config  *config.Service
	found   *discovery.Service
	results *results.Service
	now     time.Time
}

// setup seeds an instance (Northwind, competitor Contoso alias Contoso
// Gateway, technology Redis) ready to run checks. With find, checks read
// each answer for other brands.
func setup(t *testing.T, mod func(*config.Settings)) *env {
	t.Helper()
	e := &env{db: testdb.New(t), treg: tregtest.New(t), now: time.Now()}
	e.config = config.New(e.db, config.Options{Secrets: secretbox.ForTest(), TregBaseURL: e.treg.URL})
	checkstest.Seed(t, e.config, e.treg, mod)
	e.found = discovery.New(e.db, e.config, discovery.Options{Now: func() time.Time { return e.now }})
	e.results = results.New(e.db)
	return e
}

// check runs one check to the end, reading answers for other brands when
// find is set.
func (e *env) check(t *testing.T, find bool) int64 {
	t.Helper()
	o := checks.Options{Sleep: func(context.Context, time.Duration) error { return nil }}
	if find {
		o.Finder = e.found
	}
	svc := checks.New(e.db, e.config, o)
	run, _, err := svc.RunNow(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Work(ctx); err != nil {
		t.Fatal(err)
	}
	return run.ID
}

func (e *env) setFind(t *testing.T, on bool) {
	t.Helper()
	if _, err := e.found.PutSettings(ctx, discovery.SettingsInput{Find: &on}); err != nil {
		t.Fatal(err)
	}
}

type brandRow struct {
	ID      int64
	Name    string
	Kind    panel.BrandKind
	Domains []string
}

func (e *env) brands(t *testing.T) map[string]brandRow {
	t.Helper()
	rows, err := e.db.Query(ctx, `SELECT id, name, kind, domains FROM brand`)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]brandRow{}
	for rows.Next() {
		var b brandRow
		if err := rows.Scan(&b.ID, &b.Name, &b.Kind, &b.Domains); err != nil {
			t.Fatal(err)
		}
		out[b.Name] = b
	}
	return out
}

func (e *env) count(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := e.db.QueryRow(ctx, sql, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestCheckReadsEachAnswerForBrandsNobodyTracks(t *testing.T) {
	e := setup(t, nil)
	run := e.check(t, true)

	b := e.brands(t)
	for name, want := range map[string]panel.BrandKind{
		"Northwind": panel.KindSelf, "Contoso": panel.KindCompetitor, "Redis": panel.KindTechnology,
		"Litware Cloud": panel.KindUntracked, "Fabrikam Deploy": panel.KindUntracked,
	} {
		if b[name].Kind != want {
			t.Errorf("%s: kind %q, want %q", name, b[name].Kind, want)
		}
	}
	if len(b) != 5 {
		t.Errorf("brands = %v, want only those five: your brand under its alias, Contoso under its alias and the technology are never untracked", b)
	}
	if !slices.Equal(b["Litware Cloud"].Domains, []string{"litware.example"}) {
		t.Errorf("Litware Cloud domains = %v, want the domain the model gave", b["Litware Cloud"].Domains)
	}

	answers := e.count(t, `SELECT count(*) FROM answer WHERE run_id = $1 AND status = 'ok'`, run)
	if got := e.count(t, `SELECT count(*) FROM brand_call WHERE purpose = 'find' AND run_id = $1 AND status = 'ok'`, run); got != answers || answers == 0 {
		t.Errorf("finds = %d, want one per answered call (%d)", got, answers)
	}
	if got := e.count(t, `SELECT count(*) FROM discovered_mention`); got != 2*answers {
		t.Errorf("found mentions = %d, want both untracked brands in each of %d answers", got, answers)
	}
	var answersUSD, spent float64
	if err := e.db.QueryRow(ctx, `SELECT spend_usd::float8, (SELECT sum(usd)::float8 FROM spend WHERE run_id = $1) FROM run WHERE id = $1`, run).Scan(&answersUSD, &spent); err != nil {
		t.Fatal(err)
	}
	if want := answersUSD + float64(answers)*0.01; spent != want {
		t.Errorf("the check's spend = %v, want its answers %v plus %d reads at $0.01", spent, answersUSD, answers)
	}
}

// The per-check spend cap counts reading answers: with $0.01 an answer and
// $0.01 a read, a $0.03 cap stops after the second answer, which a check
// that read nothing would have kept asking past.
func TestReadingAnswersCountsAgainstTheCheckCap(t *testing.T) {
	e := setup(t, func(s *config.Settings) { s.SpendCapRunUSD = 0.03 })
	run := e.check(t, true)
	var status string
	if err := e.db.QueryRow(ctx, `SELECT status FROM run WHERE id = $1`, run).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if got := e.count(t, `SELECT count(*) FROM answer WHERE run_id = $1`, run); status != "capped" || got != 2 {
		t.Errorf("status %s with %d answers, want capped after 2", status, got)
	}
	if got := e.count(t, `SELECT count(*) FROM brand_call WHERE run_id = $1`, run); got != 1 {
		t.Errorf("reads = %d, want 1: the second answer reached the cap, so it was not read", got)
	}
}

// A read Treg refuses never fails the check: every answer is stored, the
// answer has no found brands, and a pass an hour later reads it again.
func TestAFailedReadNeverFailsTheCheck(t *testing.T) {
	e := setup(t, nil)
	e.treg.Before(func(c tregtest.Call) *tregtest.Refusal {
		if c.Find {
			return &tregtest.Refusal{Status: http.StatusPaymentRequired, Code: "insufficient_balance"}
		}
		return nil
	})
	run := e.check(t, true)
	var status string
	if err := e.db.QueryRow(ctx, `SELECT status FROM run WHERE id = $1`, run).Scan(&status); err != nil {
		t.Fatal(err)
	}
	answers := e.count(t, `SELECT count(*) FROM answer WHERE run_id = $1 AND status = 'ok'`, run)
	if status != "completed" || answers != 2*len(checkstest.Pairs) {
		t.Fatalf("status %s with %d answers, want a completed check with every answer", status, answers)
	}
	if got := e.count(t, `SELECT count(*) FROM brand_call WHERE status = 'failed' AND error LIKE '%insufficient_balance%'`); got != answers {
		t.Errorf("failed reads = %d, want %d, each with Treg's reason", got, answers)
	}
	if got := e.count(t, `SELECT count(*) FROM brand WHERE key IS NOT NULL`); got != 0 {
		t.Errorf("%d brands found from refused reads, want none", got)
	}

	e.treg.Before(nil)
	if err := e.found.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if got := e.count(t, `SELECT count(*) FROM brand_call WHERE status = 'ok'`); got != 0 {
		t.Errorf("a pass read %d answers right away, want none before an hour has passed", got)
	}
	e.now = e.now.Add(2 * time.Hour)
	if err := e.found.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if got := e.count(t, `SELECT count(*) FROM brand_call WHERE status = 'ok' AND run_id IS NULL`); got != answers {
		t.Errorf("the pass read %d answers again, want all %d, outside the check", got, answers)
	}
	if got := e.count(t, `SELECT count(*) FROM job_run WHERE job = 'other_brands' AND trigger = 'scheduled' AND status = 'completed'`); got != 1 {
		t.Errorf("other_brands jobs = %d, want the one pass in Check history", got)
	}
}

// snapshot is every score a reader sees, without the found brands each
// answer lists, which are there to change.
func (e *env) snapshot(t *testing.T, before, after int64) string {
	t.Helper()
	r := e.results
	all := map[string]any{}
	add := func(name string, v any, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		all[name] = v
	}
	sc := results.Scope{}
	v1, err := r.Rates(ctx, nil, sc, results.ByPair)
	add("rates", v1, err)
	v2, err := r.Share(ctx, nil, sc)
	add("share", v2, err)
	v3, err := r.Summary(ctx, nil, sc)
	add("summary", v3, err)
	v4, err := r.Competitors(ctx, nil, sc, results.ByPair)
	add("competitors", v4, err)
	v5, err := r.Competitors(ctx, nil, sc, results.BySurface)
	add("competitors by app", v5, err)
	v6, err := r.Gaps(ctx, after, sc)
	add("gaps", v6, err)
	v7, err := r.RunQuestions(ctx, after, sc)
	add("questions", v7, err)
	v8, err := r.Sources(ctx, nil, sc)
	add("sources", v8, err)
	v9, err := r.Prompts(ctx, nil, sc)
	add("prompts", v9, err)
	v10, err := r.Compare(ctx, before, after)
	add("compare", v10, err)
	v11, err := r.RunAnswers(ctx, after)
	add("answers", v11, err)
	var q int64
	if err := e.db.QueryRow(ctx, `SELECT min(id) FROM question`).Scan(&q); err != nil {
		t.Fatal(err)
	}
	v12, err := r.QuestionHistory(ctx, q, nil)
	add("history", v12, err)
	b, err := json.Marshal(all)
	if err != nil {
		t.Fatal(err)
	}
	var generic any
	if err := json.Unmarshal(b, &generic); err != nil {
		t.Fatal(err)
	}
	var strip func(v any)
	strip = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			delete(x, "others")
			for _, c := range x {
				strip(c)
			}
		case []any:
			for _, c := range x {
				strip(c)
			}
		}
	}
	strip(generic)
	out, _ := json.MarshalIndent(generic, "", " ")
	return string(out)
}

// Brands nobody tracks never change a score: every result a reader sees is
// the same before past answers are read for them, after, and with one of
// them hidden.
func TestUntrackedBrandsLeaveEveryScoreUnchanged(t *testing.T) {
	e := setup(t, nil)
	e.setFind(t, false)
	before, after := e.check(t, false), e.check(t, false)
	want := e.snapshot(t, before, after)

	e.setFind(t, true)
	b, err := e.found.StartBackfill(ctx)
	if err != nil || b.Unread != 4*len(checkstest.Pairs) {
		t.Fatalf("backfill = %+v, %v; want every answer of both checks unread", b, err)
	}
	if err := e.found.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if got := e.count(t, `SELECT count(*) FROM discovered_mention`); got == 0 {
		t.Fatal("the pass found no brands, so this test proves nothing")
	}
	if got := e.snapshot(t, before, after); got != want {
		t.Errorf("scores changed once untracked brands were found:\n%s", diff(want, got))
	}
	if err := e.found.Hide(ctx, e.brands(t)["Litware Cloud"].ID); err != nil {
		t.Fatal(err)
	}
	if got := e.snapshot(t, before, after); got != want {
		t.Errorf("scores changed once a found brand was hidden:\n%s", diff(want, got))
	}
}

func diff(a, b string) string {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			lo := max(0, i-300)
			return "want ..." + a[lo:min(len(a), i+300)] + "\n got ..." + b[lo:min(len(b), i+300)]
		}
	}
	return "lengths differ"
}

// A pass reads every unread past answer once, costs what the estimate
// said, shows in Check history, and leaves nothing to read.
func TestBackfillReadsPastAnswersOnce(t *testing.T) {
	e := setup(t, nil)
	e.setFind(t, false)
	e.check(t, false)
	if _, err := e.found.StartBackfill(ctx); err == nil {
		t.Fatal("a pass started with reading off")
	}
	e.setFind(t, true)
	b, err := e.found.Backfill(ctx)
	if err != nil || b.Unread != 2*len(checkstest.Pairs) || b.CostUSD != float64(b.Unread)*engines.BrandFinder.SampleCostUSD || !b.Ready {
		t.Fatalf("backfill = %+v, %v; want every answer unread at the sample price", b, err)
	}
	first, err := e.found.StartBackfill(ctx)
	if err != nil || first.Latest == nil || first.Latest.Status != "queued" {
		t.Fatalf("start = %+v, %v; want a queued pass", first, err)
	}
	again, err := e.found.StartBackfill(ctx)
	if err != nil || again.Latest.JobID != first.Latest.JobID {
		t.Fatalf("starting again = %+v, %v; want the same pass", again, err)
	}
	if err := e.found.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	done, err := e.found.Backfill(ctx)
	if err != nil || done.Unread != 0 || done.Latest.Status != "completed" || done.Latest.Read != b.Unread || done.Latest.SpentUSD != float64(b.Unread)*0.01 {
		t.Fatalf("after the pass: %+v %+v, %v; want everything read once at the fake's $0.01", done, done.Latest, err)
	}
	if _, err := e.found.StartBackfill(ctx); err == nil {
		t.Error("a pass started with nothing left to read")
	}
	jobs, err := e.results.Jobs(ctx, 10)
	i := slices.IndexFunc(jobs, func(j results.Job) bool { return j.ID == done.Latest.JobID })
	if err != nil || i < 0 || jobs[i].Job != "other_brands" || jobs[i].SpendUSD == nil || *jobs[i].SpendUSD != done.Latest.SpentUSD {
		t.Errorf("Check history = %+v, %v; want the pass with what it spent", jobs, err)
	}
}

// The monthly spend cap, which checks and social share, stops a pass.
func TestBackfillStopsAtTheMonthlyCap(t *testing.T) {
	e := setup(t, nil)
	e.setFind(t, false)
	e.check(t, false)
	e.setFind(t, true)
	st, _ := e.config.Settings(ctx)
	var spent float64
	if err := e.db.QueryRow(ctx, `SELECT sum(usd)::float8 FROM spend`).Scan(&spent); err != nil {
		t.Fatal(err)
	}
	in := settingsInput(st)
	in.SpendCapMonthUSD = new(spent + 0.025)
	if _, err := e.config.PutSettings(ctx, in); err != nil {
		t.Fatal(err)
	}
	if _, err := e.found.StartBackfill(ctx); err != nil {
		t.Fatal(err)
	}
	if err := e.found.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	b, _ := e.found.Backfill(ctx)
	if b.Latest.Status != "capped" || b.Latest.Read != 3 {
		t.Errorf("pass = %+v, want capped after 3 reads of $0.01 crossed the $0.025 left", b.Latest)
	}
}

func settingsInput(v config.SettingsView) config.Settings {
	return config.Settings{Brand: v.Brand, MatchMode: v.MatchMode, Market: v.Market, Language: v.Language, TimeZone: v.TimeZone,
		CheckEveryDays: v.CheckEveryDays, CheckTime: v.CheckTime, SpendCapRunUSD: v.SpendCapRunUSD, SpendCapMonthUSD: v.SpendCapMonthUSD,
		Concurrency: v.Concurrency, Retries: v.Retries, TimeoutS: v.TimeoutS, RawRetentionDays: v.RawRetentionDays}
}

// Track makes a found brand a competitor and scores every past answer
// again: an answer that named it first now has it at #1 and you at #2.
func TestTrackScoresPastAnswersAgain(t *testing.T) {
	e := setup(t, nil)
	run := e.check(t, true)
	var answer int64
	if err := e.db.QueryRow(ctx, `UPDATE answer SET text = 'Litware Cloud is the usual pick, then Northwind.'
		WHERE id = (SELECT min(id) FROM answer WHERE run_id = $1 AND status = 'ok') RETURNING id`, run).Scan(&answer); err != nil {
		t.Fatal(err)
	}
	litware := e.brands(t)["Litware Cloud"]
	others, err := e.results.OtherBrands(ctx, nil, results.Scope{})
	if err != nil || len(others.Brands) != 2 || others.Read != others.Answered {
		t.Fatalf("other brands = %+v, %v; want both found brands and every answer read", others, err)
	}

	got, err := e.found.Track(ctx, litware.ID)
	if err != nil || got.Kind != panel.KindCompetitor || got.ID != litware.ID {
		t.Fatalf("track = %+v, %v; want the same brand as a competitor", got, err)
	}
	var position int
	if err := e.db.QueryRow(ctx, `SELECT position FROM answer WHERE id = $1`, answer).Scan(&position); err != nil || position != 2 {
		t.Errorf("your position in the rewritten answer = %d, %v; want 2, behind the newly tracked brand", position, err)
	}
	if n := e.count(t, `SELECT count(*) FROM brand_mention WHERE brand_id = $1 AND position = 1`, litware.ID); n != 1 {
		t.Errorf("Litware Cloud named first in %d answers, want the one that names it", n)
	}
	if n := e.count(t, `SELECT count(*) FROM discovered_mention WHERE brand_id = $1`, litware.ID); n != 0 {
		t.Errorf("%d found mentions left for a tracked brand, want none", n)
	}
	comp, _ := e.results.Competitors(ctx, nil, results.Scope{}, results.ByPair)
	if !slices.ContainsFunc(comp.Brands, func(b results.BrandStanding) bool { return b.BrandID == litware.ID && b.Mentions == 1 }) {
		t.Errorf("competitors = %+v, want Litware Cloud with its one mention", comp.Brands)
	}
	others, _ = e.results.OtherBrands(ctx, nil, results.Scope{})
	if len(others.Brands) != 1 || others.Brands[0].Name != "Fabrikam Deploy" {
		t.Errorf("other brands after tracking = %+v, want only Fabrikam Deploy", others.Brands)
	}
	if _, err := e.found.Track(ctx, litware.ID); err == nil {
		t.Error("tracking a competitor again succeeded, want not found")
	}
}

// Adding a competitor in Settings under a found brand's name adopts it,
// keeping its id and logo; the next read never lists it again.
func TestAddingACompetitorAdoptsTheFoundBrand(t *testing.T) {
	e := setup(t, nil)
	e.check(t, true)
	fabrikam := e.brands(t)["Fabrikam Deploy"]
	b, err := e.config.UpsertBrand(ctx, config.BrandInput{Name: "Fabrikam Deploy", Kind: panel.KindCompetitor})
	if err != nil || b.ID != fabrikam.ID || b.Kind != panel.KindCompetitor {
		t.Fatalf("upsert = %+v, %v; want brand %d as a competitor", b, err, fabrikam.ID)
	}
	if _, err := e.config.UpsertBrand(ctx, config.BrandInput{Name: "Litware", Kind: panel.KindCompetitor}); err != nil {
		t.Fatal(err)
	}
	if _, ok := e.brands(t)["Litware Cloud"]; ok {
		t.Error("tracking Litware left the found Litware Cloud listed, want it covered")
	}
	e.check(t, true)
	if n := e.count(t, `SELECT count(*) FROM brand WHERE key IS NOT NULL`); n != 0 {
		t.Errorf("%d brands listed as untracked after a second check, want none: both are tracked", n)
	}
}

func TestHiddenBrandsLeaveTheListUntilUnhidden(t *testing.T) {
	e := setup(t, nil)
	e.check(t, true)
	fabrikam := e.brands(t)["Fabrikam Deploy"]
	if err := e.found.Hide(ctx, fabrikam.ID); err != nil {
		t.Fatal(err)
	}
	others, _ := e.results.OtherBrands(ctx, nil, results.Scope{})
	hidden, _ := e.results.HiddenBrands(ctx)
	if len(others.Brands) != 1 || others.Hidden != 1 || len(hidden) != 1 || hidden[0].BrandID != fabrikam.ID || hidden[0].Named == 0 {
		t.Fatalf("others %+v hidden %+v, want Fabrikam Deploy moved to the hidden list with its mentions", others, hidden)
	}
	answers, _ := e.results.RunAnswers(ctx, others.LatestRun)
	for _, a := range answers {
		if slices.ContainsFunc(a.Others, func(o results.OtherMention) bool { return o.BrandID == fabrikam.ID }) {
			t.Fatalf("answer %d lists the hidden brand", a.ID)
		}
	}
	e.check(t, true)
	if e.brands(t)["Fabrikam Deploy"].Kind != panel.KindHidden {
		t.Error("a later read put the hidden brand back on the list")
	}
	if err := e.found.Unhide(ctx, fabrikam.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.found.Unhide(ctx, fabrikam.ID); err != nil {
		t.Errorf("unhiding twice: %v, want no error", err)
	}
	others, _ = e.results.OtherBrands(ctx, nil, results.Scope{})
	if len(others.Brands) != 2 || others.Hidden != 0 {
		t.Errorf("after unhiding: %+v, want both brands back", others)
	}
	if err := e.found.Hide(ctx, e.brands(t)["Northwind"].ID); err == nil {
		t.Error("hiding your own brand succeeded, want not found")
	}
}

// The list counts answers in range, the latest check against the one
// before, and the questions a brand was named for most.
func TestOtherBrandsCountsAndChange(t *testing.T) {
	e := setup(t, nil)
	first := e.check(t, true)
	o, err := e.results.OtherBrands(ctx, nil, results.Scope{})
	if err != nil || o.LatestRun != first || o.PreviousRun != 0 || o.Brands[0].Change != nil {
		t.Fatalf("first check: %+v, %v; want no change before a second check", o, err)
	}
	e.treg.SetFindBody(findReply(t, `{"brands":[{"name":"Fabrikam Deploy","kind":"brand","domain":""}]}`))
	second := e.check(t, true)
	o, _ = e.results.OtherBrands(ctx, nil, results.Scope{})
	per := 2 * len(checkstest.Pairs)
	byName := map[string]results.OtherBrand{}
	for _, b := range o.Brands {
		byName[b.Name] = b
	}
	f, l := byName["Fabrikam Deploy"], byName["Litware Cloud"]
	if o.LatestRun != second || o.PreviousRun != first || o.Answered != 2*per || o.Read != 2*per {
		t.Fatalf("other brands = %+v, want both checks counted", o)
	}
	if f.Named != 2*per || f.Latest != per || f.Change == nil || *f.Change != 0 || o.Brands[0].Name != "Fabrikam Deploy" {
		t.Errorf("Fabrikam Deploy = %+v, want named in every answer, first, unchanged", f)
	}
	if l.Named != per || l.Latest != 0 || l.Previous != per || l.Change == nil || *l.Change != -per {
		t.Errorf("Litware Cloud = %+v, want named only in the first check, down %d", l, per)
	}
	if len(f.Questions) != 2 || f.Questions[0].Named != len(checkstest.Pairs)*2 {
		t.Errorf("questions = %+v, want both questions with every answer", f.Questions)
	}
	since := e.now.Add(time.Hour)
	o, _ = e.results.OtherBrands(ctx, &since, results.Scope{})
	if o.Answered != 0 || len(o.Brands) != 0 {
		t.Errorf("a range with no check: %+v, want nothing", o)
	}
}

// findReply wraps the model's reply text in DataForSEO's answer shape.
func findReply(t *testing.T, text string) []byte {
	t.Helper()
	b, err := json.Marshal(map[string]any{"status_code": 20000, "tasks": []any{map[string]any{"status_code": 20000,
		"result": []any{map[string]any{"model_name": "claude-haiku-4-5", "items": []any{map[string]any{"type": "message",
			"sections": []any{map[string]any{"type": "text", "text": text}}}}}}}}})
	if err != nil {
		t.Fatal(err)
	}
	return b
}
