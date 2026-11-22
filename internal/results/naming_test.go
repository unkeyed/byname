package results_test

import (
	"slices"
	"testing"

	"github.com/unkeyed/byname/internal/checks/checkstest"
	"github.com/unkeyed/byname/internal/config"
	"github.com/unkeyed/byname/internal/results"
)

const namingQuestion = "Is Northwind a good host for a Go API?"

// withNamingQuestion adds a question that names the brand to the seeded
// two, runs a check, and then makes the answers to that question the only
// ones naming Northwind, the way a question that names you gets answers
// that name you back. It returns the run and the question.
func withNamingQuestion(t *testing.T, e *env) (int64, config.Question) {
	t.Helper()
	set, err := e.config.PutQuestions(ctx, config.QuestionsInput{Questions: append(slices.Clone(checkstest.Questions), config.QuestionInput{Group: "Brand", Text: namingQuestion})})
	if err != nil {
		t.Fatal(err)
	}
	q := set.Questions[2]
	if !q.NamesBrand || q.CountInScores || !set.Questions[0].CountInScores {
		t.Fatalf("questions = %+v, want only the one naming Northwind marked and left out of scores", set.Questions)
	}
	run := e.check(t)
	for _, sql := range []string{
		`UPDATE answer SET mentioned = question_id = $2, position = CASE WHEN question_id = $2 THEN 1 END WHERE run_id = $1 AND status <> 'failed'`,
		`DELETE FROM brand_mention bm USING answer a, brand b WHERE bm.answer_id = a.id AND b.id = bm.brand_id AND a.run_id = $1 AND b.kind = 'self' AND a.question_id <> $2`,
		`INSERT INTO brand_mention (answer_id, brand_id, position) SELECT a.id, b.id, 1 FROM answer a, brand b
			WHERE a.run_id = $1 AND a.question_id = $2 AND a.status <> 'failed' AND b.kind = 'self' ON CONFLICT DO NOTHING`,
	} {
		if _, err := e.db.Exec(ctx, sql, run, q.ID); err != nil {
			t.Fatal(err)
		}
	}
	return run, q
}

// A question that names your brand is still asked and its answers shown,
// but its answers count in no score, until someone counts it anyway.
func TestQuestionNamingTheBrandIsLeftOutOfScores(t *testing.T) {
	e := setup(t)
	run, q := withNamingQuestion(t, e)

	summary := func() results.RunSummary {
		t.Helper()
		s, err := e.results.Summary(ctx, nil, results.Scope{})
		if err != nil || len(s) == 0 || s[0].RunID != run {
			t.Fatalf("summary = %+v %v", s, err)
		}
		return s[0]
	}
	if s := summary(); s.Answered != 12 || s.Mentioned != 0 || s.MentionRate == nil || *s.MentionRate != 0 || s.Share == nil || *s.Share != 0 {
		t.Fatalf("summary = %+v, want the 12 answers to the other two questions, none naming you: the question that names you is left out", s)
	}
	rates, err := e.results.Rates(ctx, nil, results.Scope{}, results.ByPair)
	if err != nil {
		t.Fatal(err)
	}
	for _, sr := range rates {
		if p := sr.Points[0]; p.Mentioned != 0 || p.Calls != 2 {
			t.Errorf("%s: %+v, want 2 calls and no mention on the line", sr.Pair.Surface, p)
		}
	}
	comp, err := e.results.Competitors(ctx, nil, results.Scope{}, results.ByPair)
	self := slices.IndexFunc(comp.Brands, func(b results.BrandStanding) bool { return b.Name == "Northwind" })
	if err != nil || comp.Answered != 12 || self < 0 || comp.Brands[self].Mentions != 0 || comp.Brands[self].Rank != 0 {
		t.Fatalf("competitors = %+v %v, want Northwind never named over the 12 counted answers", comp, err)
	}
	gaps, err := e.results.Gaps(ctx, run, results.Scope{})
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range gaps.Gaps {
		if g.Question.ID == q.ID {
			t.Errorf("gaps list the question that names you: %+v", g)
		}
	}
	r, err := e.results.Run(ctx, run)
	if err != nil || r.Succeeded != 18 || r.Scored != 12 || r.Mentioned != 0 {
		t.Fatalf("run = %+v %v, want 18 answered, 12 scored, none naming you", r, err)
	}
	table, err := e.results.RunQuestions(ctx, run, results.Scope{})
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(table.Rows, func(r results.QuestionRow) bool { return r.Question.ID == q.ID })
	if i < 0 || table.Rows[i].Question.CountsInScores || table.Rows[i].Named == 0 {
		t.Fatalf("questions table = %+v, want the question shown with its answers naming you and marked as left out", table.Rows)
	}

	c, err := e.results.Compare(ctx, run, e.check(t))
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, n := range c.Counts {
		total += n
	}
	if len(c.Pairs) != 18 || total != 12 {
		t.Fatalf("compare lists %d pairs and counts %d, want all 18 listed and the 12 that count counted", len(c.Pairs), total)
	}

	if q, err = e.config.SetQuestionScoring(ctx, q.ID, config.QuestionScoringInput{CountInScores: new(true)}); err != nil || !q.CountInScores || !q.NamesBrand {
		t.Fatalf("count it anyway = %+v %v", q, err)
	}
	if s := summary(); s.Answered != 18 || s.Mentioned == 0 {
		t.Fatalf("summary once counted = %+v, want all 18 answers with the question's mentions", s)
	}
	if _, err := e.config.SetQuestionScoring(ctx, q.ID, config.QuestionScoringInput{}); err != nil {
		t.Fatal(err)
	}
	if s := summary(); s.Answered != 12 {
		t.Fatalf("summary back on the default = %+v, want it left out again", s)
	}
}

// Whether a question names the brand follows the brand: renaming it, or a
// competitor whose longer name covers yours, changes it. A person's choice
// carries on into the next question set version and covers the earlier.
func TestNamingFollowsTheBrandAndChoicesCarryOn(t *testing.T) {
	e := setup(t)
	_, q := withNamingQuestion(t, e)
	current := func() []config.Question {
		t.Helper()
		set, err := e.config.CurrentQuestions(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return set.Questions
	}

	if _, err := e.config.UpsertBrand(ctx, config.BrandInput{Name: "Northwind Host", Kind: "competitor"}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.config.PutQuestions(ctx, config.QuestionsInput{Questions: []config.QuestionInput{
		checkstest.Questions[0], checkstest.Questions[1], {Group: "Brand", Text: namingQuestion}, {Group: "Brand", Text: "Is Northwind Host cheaper than Contoso?"},
	}}); err != nil {
		t.Fatal(err)
	}
	if qs := current(); !qs[2].NamesBrand || qs[3].NamesBrand {
		t.Fatalf("questions = %+v, want the competitor Northwind Host not counted as naming you", qs)
	}

	s := checkstest.Settings()
	s.Brand.Name, s.Brand.Aliases = "Southwind", nil
	if _, err := e.config.PutSettings(ctx, s); err != nil {
		t.Fatal(err)
	}
	if qs := current(); qs[2].NamesBrand || !qs[2].CountInScores {
		t.Fatalf("after renaming the brand: %+v, want the question no longer naming it and counted", qs[2])
	}
	s.Brand.Name = "Northwind"
	if _, err := e.config.PutSettings(ctx, s); err != nil {
		t.Fatal(err)
	}

	latest := current()[2]
	if _, err := e.config.SetQuestionScoring(ctx, q.ID, config.QuestionScoringInput{CountInScores: new(true)}); err == nil {
		t.Fatal("set on a question from an earlier set, want an error naming the current one")
	}
	if _, err := e.config.SetQuestionScoring(ctx, latest.ID, config.QuestionScoringInput{CountInScores: new(true)}); err != nil {
		t.Fatal(err)
	}
	var earlier bool
	if err := e.db.QueryRow(ctx, `SELECT counts_in_scores FROM question WHERE id = $1`, q.ID).Scan(&earlier); err != nil || !earlier {
		t.Fatalf("the earlier version counts = %v %v, want the choice to cover it", earlier, err)
	}
	if _, err := e.config.PutQuestions(ctx, config.QuestionsInput{Questions: []config.QuestionInput{
		checkstest.Questions[0], {Group: "Brand", Text: namingQuestion},
	}, Retired: []int64{current()[1].ID, current()[3].ID}}); err != nil {
		t.Fatal(err)
	}
	if qs := current(); !qs[1].NamesBrand || !qs[1].CountInScores {
		t.Fatalf("next version = %+v, want the choice to count it carried on", qs[1])
	}
}

func TestCheckNaming(t *testing.T) {
	e := setup(t)
	got, err := e.config.CheckNaming(ctx, config.NamingCheckInput{
		Questions: []string{"Is Northwind Cloud good?", "Contoso Gateway alternatives for Redis", "northwind pricing"},
		Aliases:   []string{"Tally", " stripe ", "Northwind", "Tally Up"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Brand != "Northwind" {
		t.Errorf("brand = %q", got.Brand)
	}
	q := got.Questions
	if !q[0].NamesBrand || !slices.Equal(q[0].Brands, []string{"Northwind"}) ||
		q[1].NamesBrand || !slices.Equal(q[1].Brands, []string{"Contoso", "Redis"}) || q[2].NamesBrand {
		t.Errorf("questions = %+v, want only the first naming you, whole words and case as answers are scored", q)
	}
	for i, want := range []bool{true, true, false, false} {
		if got.Aliases[i].CommonWord != want {
			t.Errorf("%q common word = %v, want %v", got.Aliases[i].Alias, got.Aliases[i].CommonWord, want)
		}
	}
}
