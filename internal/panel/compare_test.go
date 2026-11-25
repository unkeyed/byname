package panel

import (
	"slices"
	"testing"

	"github.com/unkeyed/byname/internal/engines"
)

func named(pos int, others ...BrandID) Scored {
	s := Scored{Status: engines.StatusOK, Mentioned: true, Position: pos}
	for _, o := range others {
		s.Mentions = append(s.Mentions, Mention{Brand: o})
	}
	return s
}

func notNamed(others ...BrandID) Scored {
	s := Scored{Status: engines.StatusOK}
	for _, o := range others {
		s.Mentions = append(s.Mentions, Mention{Brand: o})
	}
	return s
}

var failedAnswer = Scored{Status: engines.StatusFailed}

func k(q QuestionID, sm SurfaceModelID) Key { return Key{q, sm} }

func TestCompareClassifiesEachPair(t *testing.T) {
	before := Run{
		k(1, 1): {notNamed()},
		k(1, 2): {named(1)},
		k(2, 1): {named(3)},
		k(2, 2): {named(2)},
		k(3, 1): {notNamed()},
		k(3, 2): {named(1)},
		k(4, 1): {named(1)},
		k(5, 1): {{Status: engines.StatusNoAnswer}},
	}
	after := Run{
		k(1, 1): {named(1)},
		k(1, 2): {notNamed()},
		k(2, 1): {named(1)},
		k(2, 2): {named(2)},
		k(3, 1): {notNamed()},
		k(3, 2): {failedAnswer},
		k(5, 1): {named(2)},
		k(6, 1): {named(1)},
	}
	want := map[Key]Outcome{
		k(1, 1): Gained,
		k(1, 2): Lost,
		k(2, 1): Moved,
		k(2, 2): Unchanged,
		k(3, 1): Unchanged,
		k(3, 2): NotComparable,
		k(4, 1): NotComparable,
		k(5, 1): Gained,
		k(6, 1): NotComparable,
	}
	c := Compare(before, after, nil)
	if len(c.Pairs) != len(want) {
		t.Fatalf("%d pairs, want %d", len(c.Pairs), len(want))
	}
	for _, p := range c.Pairs {
		if p.Outcome != want[p.Key] {
			t.Errorf("%v: %s, want %s", p.Key, p.Outcome, want[p.Key])
		}
	}
	counts := map[Outcome]int{Gained: 2, Lost: 1, Moved: 1, Unchanged: 2, NotComparable: 3}
	for o, n := range counts {
		if c.Counts[o] != n {
			t.Errorf("%s: %d, want %d", o, c.Counts[o], n)
		}
	}
	if c.Net() != 1 {
		t.Errorf("net %d, want 1", c.Net())
	}
	for i := 1; i < len(c.Pairs); i++ {
		a, b := c.Pairs[i-1].Key, c.Pairs[i].Key
		if a.Question > b.Question || (a.Question == b.Question && a.SurfaceModel > b.SurfaceModel) {
			t.Errorf("pairs out of order: %v before %v", a, b)
		}
	}
}

func TestCompareAcrossQuestionSetVersions(t *testing.T) {
	before := Run{
		k(3, 1):  {notNamed()},
		k(5, 1):  {named(2)},
		k(12, 1): {named(1)},
		k(9, 1):  {named(1)},
	}
	after := Run{
		k(17, 1): {named(1)},
		k(26, 1): {named(1)},
		k(27, 1): {named(1)},
		k(9, 1):  {named(1)},
	}
	c := Compare(before, after, QuestionMap{3: 17, 5: 26, 12: 27})
	want := map[Key]Outcome{
		k(17, 1): Gained,
		k(26, 1): Moved,
		k(27, 1): Unchanged,
		k(9, 1):  NotComparable,
	}
	got := map[Key][]Outcome{}
	for _, p := range c.Pairs {
		got[p.Key] = append(got[p.Key], p.Outcome)
	}
	for key, o := range want {
		if len(got[key]) == 0 || got[key][0] != o {
			t.Errorf("%v: %v, want %s", key, got[key], o)
		}
	}
	// Question 9 is in both runs but not in the map, so it does not carry
	// over: the old answer and the new one are each not comparable.
	if len(got[k(9, 1)]) != 2 {
		t.Errorf("unmapped question 9: %v, want two not-comparable pairs", got[k(9, 1)])
	}
	if c.Net() != 1 {
		t.Errorf("net %d, want 1", c.Net())
	}
}

func TestSummarize(t *testing.T) {
	cited := func(s Scored) Scored { s.Cited = true; return s }
	for _, tc := range []struct {
		name string
		prev *Tally
		cur  Scored
		want string
	}{
		{"gained, cited and competitors dropped", ptr(notNamed(contoso, redis, fabrikam)), cited(named(1)),
			"Gained mention at #1 · Northwind cited · Dropped: Contoso, Fabrikam"},
		{"lost to a new competitor", ptr(named(2)), notNamed(fabrikam),
			"Lost mention (was #2) · Added: Fabrikam"},
		{"moved and no longer cited", ptr(cited(named(3, contoso))), named(1, contoso),
			"Moved #3 → #1 · Northwind no longer cited"},
		{"technologies are not competitors", ptr(named(1)), named(1, redis), "No change"},
		{"nothing changed", ptr(named(1, contoso)), named(1, contoso), "No change"},
		{"first answer", nil, named(1), "No earlier answer to compare"},
		{"earlier answer failed", ptr(failedAnswer), named(1), "No earlier answer to compare"},
		{"this answer failed", ptr(named(1)), failedAnswer, "Failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Summarize(tc.prev, TallyOf([]Scored{tc.cur}), brands); got != tc.want {
				t.Errorf("got  %q\nwant %q", got, tc.want)
			}
		})
	}
}

// ptr is the cell of one answer.
func ptr(s Scored) *Tally {
	t := TallyOf([]Scored{s})
	return &t
}

func TestTallyReadsWhatMostAnswersSay(t *testing.T) {
	cited := func(s Scored) Scored { s.Cited = true; return s }
	noAnswer := Scored{Status: engines.StatusNoAnswer}
	for _, tc := range []struct {
		name                    string
		answers                 []Scored
		status                  engines.Status
		answered, named, nCited int
		mentioned, mostlyCited  bool
		position                int
		mentions                []BrandID
	}{
		{"2 of 3 named you, the best at #1", []Scored{named(2), notNamed(), cited(named(1, contoso))},
			engines.StatusOK, 3, 2, 1, true, false, 1, nil},
		{"1 of 3 is not most", []Scored{cited(named(1)), cited(notNamed()), notNamed()},
			engines.StatusOK, 3, 1, 2, false, true, 1, nil},
		{"half is not most", []Scored{named(1), notNamed()},
			engines.StatusOK, 2, 1, 0, false, false, 1, nil},
		{"a failed answer is left out", []Scored{named(3), failedAnswer},
			engines.StatusOK, 1, 1, 0, true, false, 3, nil},
		{"every answer failed", []Scored{failedAnswer, failedAnswer},
			engines.StatusFailed, 0, 0, 0, false, false, 0, nil},
		{"no AI answer and a failure", []Scored{noAnswer, failedAnswer},
			engines.StatusNoAnswer, 1, 0, 0, false, false, 0, nil},
		{"competitors most answers named", []Scored{notNamed(fabrikam, contoso), notNamed(contoso), named(1)},
			engines.StatusOK, 3, 1, 0, false, false, 1, []BrandID{contoso}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := TallyOf(tc.answers)
			var mentions []BrandID
			for _, m := range got.Mentions {
				mentions = append(mentions, m.Brand)
			}
			if got.Status != tc.status || got.Answered != tc.answered || got.Named != tc.named || got.Cited != tc.nCited ||
				got.Mentioned() != tc.mentioned || got.MostlyCited() != tc.mostlyCited || got.Position != tc.position || !slices.Equal(mentions, tc.mentions) {
				t.Errorf("got %+v (mentioned %v, mostly cited %v), want %s with %d answered, %d named, %d cited, mentioned %v, mostly cited %v, #%d, mentions %v",
					got, got.Mentioned(), got.MostlyCited(), tc.status, tc.answered, tc.named, tc.nCited, tc.mentioned, tc.mostlyCited, tc.position, tc.mentions)
			}
		})
	}
}

func TestCompareWeighsMostOfACellsAnswers(t *testing.T) {
	before := Run{
		k(1, 1): {named(1), notNamed(), notNamed()},
		k(2, 1): {named(3), named(2), notNamed()},
		k(3, 1): {named(2), notNamed(), named(5)},
		k(4, 1): {named(1), named(1), notNamed()},
		k(5, 1): {failedAnswer, failedAnswer, failedAnswer},
	}
	after := Run{
		k(1, 1): {named(2), named(3), notNamed()},
		k(2, 1): {named(1), notNamed(), named(4)},
		k(3, 1): {notNamed(), named(2), named(2)},
		k(4, 1): {named(1), notNamed(), notNamed()},
		k(5, 1): {named(1), failedAnswer, failedAnswer},
	}
	want := map[Key]Outcome{
		k(1, 1): Gained,
		k(2, 1): Moved,
		k(3, 1): Unchanged,
		k(4, 1): Lost,
		k(5, 1): NotComparable,
	}
	for _, p := range Compare(before, after, nil).Pairs {
		if p.Outcome != want[p.Key] {
			t.Errorf("%v: %s, want %s", p.Key, p.Outcome, want[p.Key])
		}
	}
}
