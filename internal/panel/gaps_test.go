package panel

import (
	"slices"
	"testing"
)

func searches(n int) *int { return &n }

func TestOpportunity(t *testing.T) {
	for _, tc := range []struct {
		name string
		gap  Gap
		want int
	}{
		{"no engine named you, high demand", Gap{Engines: 4, Named: 0, Demand: searches(1000)}, 5},
		{"3 of 4 missed (2.25 rounds to 2), mid demand", Gap{Engines: 4, Named: 1, Demand: searches(300)}, 3},
		{"2 of 4 missed (1.5 rounds up to 2), low demand", Gap{Engines: 4, Named: 2, Demand: searches(299)}, 2},
		{"1 of 4 missed (0.75 rounds to 1)", Gap{Engines: 4, Named: 3, Demand: searches(999)}, 2},
		{"1 of 3 missed, no demand source", Gap{Engines: 3, Named: 2}, 1},
		{"every engine named you", Gap{Engines: 4, Named: 4, Demand: searches(5000)}, 2},
		{"no answers", Gap{}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.gap.Opportunity(); got != tc.want {
				t.Errorf("got %d, want %d", got, tc.want)
			}
		})
	}
}

func TestNewGapCountsAnswersAndCompetitors(t *testing.T) {
	answers := []Scored{
		named(1, contoso, redis),
		notNamed(contoso, fabrikam),
		notNamed(),
		failedAnswer,
	}
	g := NewGap(7, answers, brands, searches(400))
	want := Gap{Question: 7, Engines: 3, Named: 1, Competitors: 2, Demand: g.Demand}
	if g != want || *g.Demand != 400 {
		t.Errorf("got %+v, want %+v", g, want)
	}
}

func TestRankGaps(t *testing.T) {
	gaps := []Gap{
		{Question: 1, Engines: 4, Named: 4},
		{Question: 2, Engines: 4, Named: 2, Competitors: 1},
		{Question: 3, Engines: 4, Named: 0, Competitors: 0},
		{Question: 4, Engines: 4, Named: 2, Competitors: 5},
		{Question: 5, Engines: 4, Named: 3, Demand: searches(1200)},
	}
	var order []QuestionID
	for _, g := range RankGaps(gaps) {
		order = append(order, g.Question)
	}
	// 3 scores 3 and 5 scores 1+2, tied on competitors, so question id
	// orders them. 4 and 2 score 2, and 4 named more competitors.
	if want := []QuestionID{3, 5, 4, 2}; !slices.Equal(order, want) {
		t.Errorf("order %v, want %v", order, want)
	}
}
