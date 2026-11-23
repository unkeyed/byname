package results_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/unkeyed/byname/internal/config"
	"github.com/unkeyed/byname/internal/core"
	"github.com/unkeyed/byname/internal/engines"
	"github.com/unkeyed/byname/internal/results"
)

// claudeLinks is Claude answering every question with text, linking to a
// page on Northwind's blog the way AI apps do: with www., a trailing slash
// and a tracking parameter.
func claudeLinks(text string) []byte {
	return fmt.Appendf(nil, `{"status_code":20000,"tasks":[{"status_code":20000,"result":[{"model_name":"claude-sonnet-4-5-20250929",
	"items":[{"type":"message","sections":[{"type":"text","text":%q,
	"annotations":[{"title":"Zero downtime deploys","url":"https://www.northwind.example/blog/x/?ref=chatgpt"}]}]}]}]}]}`, text)
}

func handled(t *testing.T, e *env) []results.HandledPage {
	t.Helper()
	h, err := e.results.Handled(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return h.Pages
}

func gapPages(t *testing.T, e *env, question int64) []results.MarkedPage {
	t.Helper()
	gaps, err := e.results.Gaps(ctx, 0, results.Scope{})
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range gaps.Gaps {
		if g.Question.ID == question {
			return g.Pages
		}
	}
	t.Fatalf("question %d is not missed on the latest check", question)
	return nil
}

// A page marked on a question counts every later answer that links to it,
// in any spelling of its address and under any question, and none from
// before it was marked. It shows how the question named you on the check
// before it was marked and on the latest one, and follows its question
// into a new question set.
func TestHandledPagesTrackLaterLinks(t *testing.T) {
	e := setup(t)
	e.treg.SetBody(claudeLive, claudeLinks("Contoso Gateway is the usual choice for this."))
	first := e.check(t)
	set, err := e.config.CurrentQuestions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	q1, q2 := set.Questions[0].ID, set.Questions[1].ID

	page, err := e.config.MarkHandled(ctx, core.Actor{}, q1, config.QuestionPageInput{URL: "northwind.example/blog/x"})
	if err != nil {
		t.Fatal(err)
	}
	again, err := e.config.MarkHandled(ctx, core.Actor{}, q1, config.QuestionPageInput{URL: "https://www.northwind.example/blog/x/"})
	if err != nil || again.ID != page.ID || !again.AddedAt.Equal(page.AddedAt) {
		t.Fatalf("marking the same page again = %+v, %v; want the first mark, %+v", again, err, page)
	}
	for _, bad := range []string{"", "ftp://northwind.example/x", "https://", "northwind.example/" + strings.Repeat("a", 2000)} {
		_, err := e.config.MarkHandled(ctx, core.Actor{}, q1, config.QuestionPageInput{URL: bad})
		is(t, err, core.ErrInvalid)
	}
	_, err = e.config.MarkHandled(ctx, core.Actor{}, q2+100, config.QuestionPageInput{URL: "northwind.example/blog/x"})
	is(t, err, core.ErrNotFound)

	pages := handled(t, e)
	if len(pages) != 1 {
		t.Fatalf("handled = %+v, want the one page", pages)
	}
	p := pages[0]
	if p.Linked.Answers != 0 || p.Linked.FirstRunID != nil {
		t.Errorf("before any later check, linked = %+v; want none, the first check's links came before the mark", p.Linked)
	}
	if p.Before == nil || p.Before.RunID != first || p.Before.Answered != 6 || p.Before.Named != 3 || p.Now != nil {
		t.Errorf("before %+v, now %+v; want 3 of 6 named on the first check and no check since", p.Before, p.Now)
	}
	if got, err := e.results.LinkedFirstTime(ctx, first); err != nil || len(got) != 0 {
		t.Errorf("first linked on the first check = %+v, %v; want none", got, err)
	}

	e.treg.SetBody(claudeLive, claudeLinks("Northwind Cloud is the usual choice for this."))
	second := e.check(t)
	p = handled(t, e)[0]
	claude := results.PairRef{ID: e.pairID(t, engines.ClaudeCode), Surface: engines.ClaudeCode, Model: "claude-sonnet-5-5"}
	if p.Linked.Answers != 2 || p.Linked.Questions != 2 || !slices.Equal(p.Linked.Pairs, []results.PairRef{claude}) ||
		*p.Linked.FirstRunID != second || *p.Linked.LatestRunID != second {
		t.Errorf("linked = %+v; want Claude Code's answers to both questions on the second check", p.Linked)
	}
	if p.Before.RunID != first || p.Now == nil || p.Now.RunID != second || p.Now.Answered != 6 || p.Now.Named != 4 {
		t.Errorf("before %+v, now %+v; want 3 of 6 on the first check, then 4 of 6", p.Before, p.Now)
	}
	got, err := e.results.LinkedFirstTime(ctx, second)
	if err != nil || len(got) != 1 || got[0].PageID != page.ID || got[0].Answers != 2 || !slices.Equal(got[0].Pairs, []results.PairRef{claude}) {
		t.Errorf("first linked on the second check = %+v, %v; want the page, in 2 Claude Code answers", got, err)
	}
	if pages := gapPages(t, e, q1); len(pages) != 1 || pages[0].ID != page.ID || pages[0].Linked != 2 {
		t.Errorf("the question's row carries %+v, want the page, linked in 2 answers", pages)
	}
	if pages := gapPages(t, e, q2); len(pages) != 0 {
		t.Errorf("the other question's row carries %+v, want none", pages)
	}

	third := e.check(t)
	if got, err := e.results.LinkedFirstTime(ctx, third); err != nil || len(got) != 0 {
		t.Errorf("first linked on the third check = %+v, %v; want none, the second check linked first", got, err)
	}
	if p := handled(t, e)[0]; p.Linked.Answers != 4 || *p.Linked.FirstRunID != second || *p.Linked.LatestRunID != third {
		t.Errorf("linked = %+v; want 4 answers, first on the second check, latest on the third", p.Linked)
	}

	if _, err := e.config.PutQuestions(ctx, config.QuestionsInput{Questions: []config.QuestionInput{
		{Group: "Deploying", Text: set.Questions[0].Text},
		{Group: "Deploying", Text: "Which PaaS gives a Go API the fastest cold starts?"},
	}, Retired: []int64{q2}}); err != nil {
		t.Fatal(err)
	}
	fourth := e.check(t)
	next, err := e.config.CurrentQuestions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	q1 = next.Questions[0].ID
	p = handled(t, e)[0]
	if p.Question.ID != q1 || p.Before.RunID != first || p.Now.RunID != fourth {
		t.Errorf("after a new question set, question %d, before %+v, now %+v; want question %d, the first check, then the fourth", p.Question.ID, p.Before, p.Now, q1)
	}
	if pages := gapPages(t, e, q1); len(pages) != 1 || pages[0].ID != page.ID {
		t.Errorf("the carried-on question's row carries %+v, want the page", pages)
	}

	if err := e.config.UnmarkPage(ctx, page.ID); err != nil {
		t.Fatal(err)
	}
	if pages := handled(t, e); len(pages) != 0 {
		t.Errorf("after unmarking, handled = %+v", pages)
	}
	is(t, e.config.UnmarkPage(ctx, page.ID), core.ErrNotFound)
}
