package results_test

import (
	"slices"
	"testing"

	"github.com/unkeyed/byname/internal/core"
	"github.com/unkeyed/byname/internal/engines"
	"github.com/unkeyed/byname/internal/results"
)

// twoRuns is a first check where Claude Code names Northwind and a second
// where it names only Contoso; every other pair answers the same both times.
func twoRuns(t *testing.T) (*env, int64, int64) {
	t.Helper()
	e := setup(t)
	first := e.check(t)
	e.treg.SetBody(claudeLive, []byte(claudeLoses))
	return e, first, e.check(t)
}

func TestGapsRankingAndVisibleOn(t *testing.T) {
	e, _, second := twoRuns(t)
	all, err := e.results.Gaps(ctx, 0, results.Scope{})
	if err != nil || all.RunID != second || len(all.Gaps) != 2 {
		t.Fatalf("gaps = %+v, %v; want both questions of the latest run", all, err)
	}
	for _, g := range all.Gaps {
		visible := 0
		searched := 0
		for _, p := range g.Pairs {
			if p.Named {
				visible++
			}
			if len(p.FanOut) > 0 {
				searched++
			}
		}
		if len(g.Pairs) != g.Engines || visible != g.Named || g.Named != 3 || g.Engines != 6 {
			t.Errorf("%q: %d pairs, %d named; engines %d, named %d; want visible on 3 of 6, one row per pair", g.Question.Text, len(g.Pairs), visible, g.Engines, g.Named)
		}
		if searched == 0 || searched == len(g.Pairs) {
			t.Errorf("%q: %d of %d pairs carry their searches; want only the endpoints that expose them", g.Question.Text, searched, len(g.Pairs))
		}
		if !slices.IsSortedFunc(g.Pairs, func(a, b results.GapPair) int { return int(a.Pair.ID - b.Pair.ID) }) {
			t.Errorf("%q: pairs out of pair order", g.Question.Text)
		}
	}

	claude, err := e.results.Gaps(ctx, 0, results.Scope{Surface: engines.ClaudeCode})
	if err != nil || len(claude.Gaps) != 2 {
		t.Fatalf("claude gaps = %+v, %v; want both questions, which Claude Code no longer names you on", claude, err)
	}
	if g := claude.Gaps[0]; g.Engines != 1 || g.Named != 0 || g.Opportunity != 3 || !slices.Equal(g.NamedOnMiss, []string{"Contoso"}) {
		t.Errorf("claude gap = %+v; want visible on 0 of 1, opportunity 3, Contoso named instead", g)
	}
	codex, err := e.results.Gaps(ctx, 0, results.Scope{Surface: engines.CodexCLI})
	if err != nil || codex.RunID != second || len(codex.Gaps) != 0 {
		t.Errorf("codex gaps = %+v, %v; want none, Codex names you on both questions", codex, err)
	}
	_, err = e.results.Gaps(ctx, 0, results.Scope{Surface: "nowhere"})
	is(t, err, core.ErrInvalid)
}

func TestPrompts(t *testing.T) {
	e, first, second := twoRuns(t)
	all, err := e.results.Prompts(ctx, nil, results.Scope{})
	if err != nil || all.LatestRunID != second || len(all.Runs) != 2 || len(all.Rows) != 2 {
		t.Fatalf("prompts = %+v, %v; want two questions over two runs", all, err)
	}
	if all.Runs[0].ID != first || all.Rows[0].Question.Text != "How do I add rate limiting to a REST API?" {
		t.Errorf("runs %+v, first row %q; want oldest run first and question order", all.Runs, all.Rows[0].Question.Text)
	}
	row := all.Rows[0]
	if row.Answered != 6 || len(row.NamedBy) != 3 || row.BestPosition == nil {
		t.Errorf("row = %+v; want 3 of 6 pairs naming you on the latest run, with a best position", row)
	}
	if d := row.Dots; d[0].State != results.DotNamed || d[0].Named != 4 || d[1].Named != 3 || d[1].Answered != 6 {
		t.Errorf("dots = %+v %+v; want 4 then 3 of 6 pairs naming you", d[0], d[1])
	}

	claude, err := e.results.Prompts(ctx, nil, results.Scope{Surface: engines.ClaudeCode})
	if err != nil {
		t.Fatal(err)
	}
	c := claude.Rows[0]
	if c.Dots[0].State != results.DotNamed || c.Dots[1].State != results.DotNotNamed || len(c.NamedBy) != 0 || !slices.Equal(c.Instead, []string{"Contoso"}) {
		t.Errorf("claude row = %+v (dots %+v %+v); want named then not named, Contoso instead", c, c.Dots[0], c.Dots[1])
	}

	latest := e.results
	since := all.Runs[1].StartedAt
	only, err := latest.Prompts(ctx, &since, results.Scope{})
	if err != nil || len(only.Runs) != 1 || len(only.Rows[0].Dots) != 1 {
		t.Errorf("since the latest run = %+v, %v; want one dot per row", only, err)
	}
}

func TestEngineNeverMixesFidelities(t *testing.T) {
	e, _, second := twoRuns(t)
	pair := e.pairID(t, engines.ClaudeCode)
	var fidelity string
	if err := e.db.QueryRow(ctx, `SELECT fidelity FROM answer WHERE surface_model_id = $1 LIMIT 1`, pair).Scan(&fidelity); err != nil {
		t.Fatal(err)
	}
	// A third run in which the same pair's answers were recorded as an
	// approximation, naming you first: a new series, never folded into the
	// model_match line.
	if _, err := e.db.Exec(ctx, `WITH r AS (INSERT INTO run (question_set_id, trigger, status, started_at)
			SELECT question_set_id, 'run_now', 'completed', now() + interval '1 hour' FROM run WHERE id = $1 RETURNING id)
		INSERT INTO answer (run_id, question_id, surface_model_id, endpoint, fidelity, status, attempts, mentioned, cited, position)
		SELECT r.id, a.question_id, a.surface_model_id, a.endpoint, 'approximation', 'ok', 1, true, false, 1
		FROM answer a, r WHERE a.run_id = $1 AND a.surface_model_id = $2`, second, pair); err != nil {
		t.Fatal(err)
	}

	for _, pairID := range []int64{pair, 0} {
		match, err := e.results.Engine(ctx, nil, results.Line{Surface: engines.ClaudeCode, PairID: pairID, Endpoint: claudeLive, Fidelity: fidelity})
		if err != nil {
			t.Fatal(err)
		}
		if match.Runs != 2 || match.LatestRunID != second || match.Answered != 2 || match.Named != 0 || *match.Visibility != 0 || match.VisiblePrompts != 0 || match.Prompts != 2 {
			t.Errorf("pair %d %s line = runs %d latest %d, %d of %d named, visible in %d of %d; want the two real runs, not named on the latest",
				pairID, fidelity, match.Runs, match.LatestRunID, match.Named, match.Answered, match.VisiblePrompts, match.Prompts)
		}
		if match.AveragePosition != nil || len(match.NotMentioned) != 2 || !slices.Equal(match.NotMentioned[0].Instead, []string{"Contoso"}) {
			t.Errorf("pair %d %s line: average %v, not mentioned %+v; want no position and Contoso named instead on both", pairID, fidelity, match.AveragePosition, match.NotMentioned)
		}
		if p := match.Points; len(p) != 2 || *p[0].MentionRate != 1 || *p[1].MentionRate != 0 {
			t.Errorf("pair %d %s points = %+v; want visibility 100%% then 0%%", pairID, fidelity, p)
		}
		var self, contoso results.BrandStanding
		for _, b := range match.Ranking {
			switch b.Name {
			case "Northwind":
				self = b
			case "Contoso":
				contoso = b
			}
		}
		if self.Mentions != 2 || contoso.Mentions != 4 || contoso.Rank != 1 || self.Rank != 2 {
			t.Errorf("pair %d %s ranking = %+v; want Contoso 4 mentions #1, Northwind 2 #2", pairID, fidelity, match.Ranking)
		}

		approx, err := e.results.Engine(ctx, nil, results.Line{Surface: engines.ClaudeCode, PairID: pairID, Endpoint: claudeLive, Fidelity: "approximation"})
		if err != nil {
			t.Fatal(err)
		}
		if approx.Runs != 1 || approx.Named != 2 || *approx.Visibility != 1 || approx.VisiblePrompts != 2 || *approx.AveragePosition != 1 || len(approx.Visible) != 2 {
			t.Errorf("pair %d approximation line = %+v; want only the third run, named first on both questions", pairID, approx)
		}
	}
	_, err := e.results.Engine(ctx, nil, results.Line{Surface: engines.ClaudeCode, Endpoint: claudeLive, Fidelity: "averaged"})
	is(t, err, core.ErrInvalid)
}

func TestCitedSources(t *testing.T) {
	e := setup(t)
	run := e.check(t)
	var other, own string
	if err := e.db.QueryRow(ctx, `SELECT (SELECT url FROM citation WHERE NOT is_self ORDER BY url LIMIT 1), (SELECT url FROM citation WHERE is_self ORDER BY url LIMIT 1)`).Scan(&other, &own); err != nil {
		t.Fatal(err)
	}
	// A cited site that becomes Contoso's own.
	var rival string
	if err := e.db.QueryRow(ctx, `WITH c AS (SELECT url, domain FROM citation WHERE NOT is_self AND domain <> (SELECT domain FROM citation WHERE url = $1 LIMIT 1) ORDER BY url LIMIT 1),
			b AS (UPDATE brand SET domains = domains || (SELECT domain FROM c) WHERE name = 'Contoso') SELECT url FROM c`, other).Scan(&rival); err != nil {
		t.Fatalf("no second cited site: %v", err)
	}
	if _, err := e.db.Exec(ctx, `INSERT INTO source_page (url, domain, status, fetched_at, self_present, brands_present) VALUES
		($1, 'x', 'ok', now(), false, '{Contoso,Redis}'), ($2, 'y', 'ok', now(), false, '{Contoso}'), ($3, 'z', 'ok', now(), false, '{Contoso}')`, other, own, rival); err != nil {
		t.Fatal(err)
	}
	var citations, answered int
	if err := e.db.QueryRow(ctx, `SELECT (SELECT count(*) FROM citation), (SELECT count(*) FROM answer WHERE status <> 'failed')`).Scan(&citations, &answered); err != nil {
		t.Fatal(err)
	}

	got, err := e.results.Sources(ctx, nil, results.Scope{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Citations != citations || got.Answered != answered || got.PageCount != len(got.Pages) || got.Citing == 0 {
		t.Errorf("sources = %d citations over %d answered (%d citing); want %d over %d", got.Citations, got.Answered, got.Citing, citations, answered)
	}
	pages := map[string]results.SourcePage{}
	for _, p := range got.Pages {
		pages[p.URL] = p
	}
	if p := pages[other]; p.Check == nil || !p.Outreach || !slices.Equal(p.Check.Competitors, []string{"Contoso"}) {
		t.Errorf("%s = %+v; want an outreach target naming Contoso and not you", other, p)
	}
	if p := pages[own]; p.Check == nil || p.Outreach || !p.IsSelf {
		t.Errorf("%s = %+v; want your own page, never an outreach target", own, p)
	}
	if p := pages[rival]; p.Check == nil || p.Outreach {
		t.Errorf("%s = %+v; want a competitor's own page, never an outreach target", rival, p)
	}
	for _, p := range got.Pages {
		if p.URL != other && p.URL != own && p.URL != rival && p.Check != nil {
			t.Errorf("%s has a check before it was fetched", p.URL)
		}
	}
	domainCitations := 0
	for _, d := range got.Domains {
		domainCitations += d.Citations
	}
	if domainCitations != citations {
		t.Errorf("domains add up to %d citations, want %d", domainCitations, citations)
	}

	claude, err := e.results.Sources(ctx, nil, results.Scope{Surface: engines.ClaudeCode})
	if err != nil || claude.Citations == 0 || claude.Citations >= citations {
		t.Errorf("claude sources = %d citations, %v; want only Claude Code's", claude.Citations, err)
	}

	answers, err := e.results.RunAnswers(ctx, run)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range answers {
		full, err := e.results.Answer(ctx, a.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range full.Citations {
			if (c.URL == other || c.URL == own || c.URL == rival) != (c.Page != nil) {
				t.Errorf("answer %d cites %s with page %+v; want the page check exactly where one was stored", a.ID, c.URL, c.Page)
			}
		}
	}
}
