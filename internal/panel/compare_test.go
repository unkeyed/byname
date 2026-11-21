package panel

import (
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
		k(1, 1): notNamed(),
		k(1, 2): named(1),
		k(2, 1): named(3),
		k(2, 2): named(2),
		k(3, 1): notNamed(),
		k(3, 2): named(1),
		k(4, 1): named(1),
		k(5, 1): {Status: engines.StatusNoAnswer},
	}
	after := Run{
		k(1, 1): named(1),
		k(1, 2): notNamed(),
		k(2, 1): named(1),
		k(2, 2): named(2),
		k(3, 1): notNamed(),
		k(3, 2): failedAnswer,
		k(5, 1): named(2),
		k(6, 1): named(1),
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
		k(3, 1):  notNamed(),
		k(5, 1):  named(2),
		k(12, 1): named(1),
		k(9, 1):  named(1),
	}
	after := Run{
		k(17, 1): named(1),
		k(26, 1): named(1),
		k(27, 1): named(1),
		k(9, 1):  named(1),
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
		prev *Scored
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
		{"earlier answer failed", &failedAnswer, named(1), "No earlier answer to compare"},
		{"this answer failed", ptr(named(1)), failedAnswer, "Failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Summarize(tc.prev, tc.cur, brands); got != tc.want {
				t.Errorf("got  %q\nwant %q", got, tc.want)
			}
		})
	}
}

func ptr(s Scored) *Scored { return &s }
