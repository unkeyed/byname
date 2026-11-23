package results_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/unkeyed/byname/internal/checks/checkstest"
	"github.com/unkeyed/byname/internal/core"
	"github.com/unkeyed/byname/internal/engines"
	"github.com/unkeyed/byname/internal/results"
)

const reviewPage = "https://reviews.example/go-gateways"

// Claude naming only the competitor, after searching the web, and linking
// to a review site.
const claudeLosesLinking = `{"status_code":20000,"tasks":[{"status_code":20000,"result":[{"model_name":"claude-sonnet-4-5-20250929",
	"fan_out_queries":["zero downtime deploy go api"],
	"items":[{"type":"message","sections":[{"type":"text","text":"Contoso Gateway is the usual choice for this. It drains connections before each deploy.",
	"annotations":[{"url":"` + reviewPage + `"}]}]}]}]}]}`

func TestBrief(t *testing.T) {
	e := setup(t)
	e.treg.SetBody(claudeLive, []byte(claudeLosesLinking))
	run := e.check(t)
	// The review page names only Contoso, and your own page never names you.
	var own string
	if err := e.db.QueryRow(ctx, `WITH own AS (SELECT url FROM citation WHERE is_self ORDER BY url LIMIT 1),
			_ AS (INSERT INTO source_page (url, domain, status, fetched_at, self_present, brands_present)
				SELECT $1, 'reviews.example', 'ok', now(), false, '{Contoso}'::text[]
				UNION ALL SELECT url, 'northwind.example', 'ok', now(), false, '{}' FROM own)
		SELECT url FROM own`, reviewPage).Scan(&own); err != nil {
		t.Fatal(err)
	}
	text := checkstest.Questions[0].Text
	var question int64
	if err := e.db.QueryRow(ctx, `SELECT id FROM question WHERE text = $1`, text).Scan(&question); err != nil {
		t.Fatal(err)
	}

	b, err := e.results.Brief(ctx, question, 0, results.Scope{})
	if err != nil {
		t.Fatal(err)
	}
	var states []results.LineState
	for _, l := range b.Lines {
		states = append(states, l.State)
	}
	want := []results.LineState{results.LineMissed, results.LineMissed, results.LineMissed, results.LineNamed, results.LineNamedLinked, results.LineNamedLinked}
	if b.RunID != run || b.Answered != 6 || b.Named != 3 || b.Linked != 2 || !slices.Equal(states, want) {
		t.Errorf("brief on check %d: %d answered, %d named, %d linked, lines %v; want check %d, 3 of 6 named, 2 linked, lines %v",
			b.RunID, b.Answered, b.Named, b.Linked, states, run, want)
	}
	for i := 1; i < len(b.Lines); i++ {
		if b.Lines[i].State == b.Lines[i-1].State && b.Lines[i].Pair.ID < b.Lines[i-1].Pair.ID {
			t.Errorf("lines %d and %d share a state out of pair order", i-1, i)
		}
	}
	claude := b.Lines[slices.IndexFunc(b.Lines, func(l results.BriefLine) bool { return l.Pair.Surface == engines.ClaudeCode })]
	if claude.State != results.LineMissed || claude.Title != "Claude Code · claude-sonnet-5-5" || !slices.Equal(claude.Brands, []string{"Contoso"}) ||
		!slices.Equal(claude.FanOut, []string{"zero downtime deploy go api"}) || !slices.Equal(claude.Links, []string{reviewPage}) {
		t.Errorf("claude line = %+v; want missed, naming Contoso, its search and the review page", claude)
	}
	if !slices.Equal(b.Instead, []string{"Contoso"}) || !slices.Contains(b.FanOut, "zero downtime deploy go api") {
		t.Errorf("instead %v, searches %v; want Contoso and Claude's search", b.Instead, b.FanOut)
	}
	i := slices.IndexFunc(b.Pages, func(p results.BriefPage) bool { return p.URL == reviewPage })
	if i < 0 || !b.Pages[i].Outreach || b.Pages[i].Answers != 1 || len(b.Pages) > 10 {
		t.Fatalf("pages = %+v; want the review page as an outreach target, at most 10", b.Pages)
	}
	for _, p := range b.Pages {
		if p.URL != reviewPage && p.Outreach {
			t.Errorf("%s is an outreach target without a page check naming Contoso", p.URL)
		}
	}
	for _, s := range []string{text, "Contoso", "“zero downtime deploy go api”", "Named instead, most often first: Contoso.", "4. The AI apps linked to Northwind's own page " + own + ", and its HTML does not name Northwind.",
		"6. Some pages the AI apps read name a competitor and not Northwind: " + reviewPage + "."} {
		if !strings.Contains(b.Prompt, s) {
			t.Errorf("prompt lacks %q:\n%s", s, b.Prompt)
		}
	}
	unrendered(t, b.Prompt)

	gemini, err := e.results.Brief(ctx, question, run, results.Scope{Surface: engines.GeminiCLI})
	if err != nil {
		t.Fatal(err)
	}
	if len(gemini.Lines) != 1 || gemini.Lines[0].State != results.LineNamedLinked || len(gemini.Instead) != 0 {
		t.Errorf("gemini brief = %+v; want one answer that named and linked to you, nobody instead", gemini)
	}
	for _, s := range []string{"Named instead", "Compare Northwind with", "name a competitor and not Northwind", "outreach list", "What the answers said"} {
		if strings.Contains(gemini.Prompt, s) {
			t.Errorf("prompt where every answer named and linked to you has %q:\n%s", s, gemini.Prompt)
		}
	}
	if !strings.Contains(gemini.Prompt, "Named Northwind: 1 of 1 answers.") {
		t.Errorf("gemini prompt lacks its standing:\n%s", gemini.Prompt)
	}
	unrendered(t, gemini.Prompt)

	_, err = e.results.Brief(ctx, question, 0, results.Scope{Surface: engines.ChatGPTApp})
	is(t, err, core.ErrNotFound)
	_, err = e.results.Brief(ctx, question, run+1, results.Scope{})
	is(t, err, core.ErrNotFound)
}

func unrendered(t *testing.T, prompt string) {
	t.Helper()
	for _, s := range []string{"<no value>", "%!"} {
		if strings.Contains(prompt, s) {
			t.Errorf("prompt carries %q:\n%s", s, prompt)
		}
	}
}
