package panel

import (
	"fmt"
	"sort"
	"strings"

	"github.com/unkeyed/byname/internal/engines"
)

// Key is one question and surface model pair, the unit every delta is
// counted in.
type Key struct {
	Question     QuestionID
	SurfaceModel SurfaceModelID
}

// Run is one run's scored answers, each cell's in sample order.
type Run map[Key][]Scored

// Tally is what one cell's answers say together: one question on one pair
// in one run, asked once per sample. With one answer it reads exactly as
// that answer.
type Tally struct {
	// Status is failed only when every answer failed, and ok when any
	// answer was.
	Status engines.Status
	// Answered counts the answers that did not fail, Named those that named
	// you and Cited those that cited your domains.
	Answered, Named, Cited int
	// Position is your best position among the answers that named you, 0
	// when none did.
	Position int
	// Mentions are the brands most answers named, in the order first named.
	Mentions []Mention
}

// TallyOf reads one cell's answers together.
func TallyOf(answers []Scored) Tally {
	t := Tally{Status: engines.StatusFailed}
	seen := map[BrandID]int{}
	var order []Mention
	for _, a := range answers {
		if a.Status == engines.StatusFailed {
			continue
		}
		if t.Status != engines.StatusOK {
			t.Status = a.Status
		}
		t.Answered++
		if a.Mentioned {
			t.Named++
			if t.Position == 0 || a.Position < t.Position {
				t.Position = a.Position
			}
		}
		if a.Cited {
			t.Cited++
		}
		for _, m := range a.Mentions {
			if seen[m.Brand] == 0 {
				order = append(order, m)
			}
			seen[m.Brand]++
		}
	}
	for _, m := range order {
		if 2*seen[m.Brand] > t.Answered {
			t.Mentions = append(t.Mentions, m)
		}
	}
	return t
}

// Mentioned is whether most of the cell's answers named you.
func (t Tally) Mentioned() bool { return 2*t.Named > t.Answered }

// MostlyCited is whether most of the cell's answers cited your domains.
func (t Tally) MostlyCited() bool { return 2*t.Cited > t.Answered }

// QuestionMap maps an older question set version's questions to the newer
// one's. A nil map means both runs used the same version.
type QuestionMap map[QuestionID]QuestionID

type Outcome string

const (
	Gained    Outcome = "gained"
	Lost      Outcome = "lost"
	Moved     Outcome = "moved"
	Unchanged Outcome = "unchanged"
	// NotComparable: the pair is missing from one run, failed in either, or
	// its question has no mapping into the newer set.
	NotComparable Outcome = "not_comparable"
)

// PairChange is one cell's outcome. Before or After is nil when the cell is
// missing from that run. Key is in the newer run's question ids, except for
// an older question with no mapping, which keeps its own id.
type PairChange struct {
	Key     Key
	Outcome Outcome
	Before  *Tally
	After   *Tally
}

type Comparison struct {
	Pairs  []PairChange
	Counts map[Outcome]int
}

// Net is gained minus lost.
func (c Comparison) Net() int { return c.Counts[Gained] - c.Counts[Lost] }

// Compare classifies every cell across two runs by what most of its
// answers say: gained when most did not name you before and most do now,
// moved when most named you both times at a different best position.
// Cells only compare across question set versions through m.
func Compare(before, after Run, m QuestionMap) Comparison {
	c := Comparison{Counts: map[Outcome]int{}}
	add := func(k Key, b, a *Tally) {
		o := outcome(b, a)
		c.Pairs = append(c.Pairs, PairChange{Key: k, Outcome: o, Before: b, After: a})
		c.Counts[o]++
	}

	mapped := map[Key]*Tally{}
	for k, answers := range before {
		t := TallyOf(answers)
		if m != nil {
			q, ok := m[k.Question]
			if !ok {
				add(k, &t, nil)
				continue
			}
			k.Question = q
		}
		mapped[k] = &t
	}
	for k, answers := range after {
		t := TallyOf(answers)
		add(k, mapped[k], &t)
		delete(mapped, k)
	}
	for k, s := range mapped {
		add(k, s, nil)
	}

	sort.Slice(c.Pairs, func(i, j int) bool {
		a, b := c.Pairs[i].Key, c.Pairs[j].Key
		if a.Question != b.Question {
			return a.Question < b.Question
		}
		return a.SurfaceModel < b.SurfaceModel
	})
	return c
}

func comparable(b, a *Tally) bool {
	return b != nil && a != nil && b.Status != engines.StatusFailed && a.Status != engines.StatusFailed
}

func outcome(b, a *Tally) Outcome {
	switch {
	case !comparable(b, a):
		return NotComparable
	case !b.Mentioned() && a.Mentioned():
		return Gained
	case b.Mentioned() && !a.Mentioned():
		return Lost
	case b.Mentioned() && b.Position != a.Position:
		return Moved
	default:
		return Unchanged
	}
}

// Summarize is the one-line change for one cell between a run and the one
// before it, as the prompt drawer's run history shows it, for example
// "Gained mention at #1 · Northwind cited · Dropped: Contoso, Fabrikam".
// prev is nil when there is no earlier answer.
func Summarize(prev *Tally, cur Tally, brands []Brand) string {
	if cur.Status == engines.StatusFailed {
		return "Failed"
	}
	if !comparable(prev, &cur) {
		return "No earlier answer to compare"
	}
	self, _ := selfBrand(brands)
	var parts []string
	switch outcome(prev, &cur) {
	case Gained:
		parts = append(parts, fmt.Sprintf("Gained mention at #%d", cur.Position))
	case Lost:
		parts = append(parts, fmt.Sprintf("Lost mention (was #%d)", prev.Position))
	case Moved:
		parts = append(parts, fmt.Sprintf("Moved #%d → #%d", prev.Position, cur.Position))
	}
	switch {
	case !prev.MostlyCited() && cur.MostlyCited():
		parts = append(parts, self.Name+" cited")
	case prev.MostlyCited() && !cur.MostlyCited():
		parts = append(parts, self.Name+" no longer cited")
	}
	names := map[BrandID]Brand{}
	for _, b := range brands {
		names[b.ID] = b
	}
	if added := competitorsOnlyIn(cur, prev, names); len(added) > 0 {
		parts = append(parts, "Added: "+strings.Join(added, ", "))
	}
	if dropped := competitorsOnlyIn(*prev, &cur, names); len(dropped) > 0 {
		parts = append(parts, "Dropped: "+strings.Join(dropped, ", "))
	}
	if len(parts) == 0 {
		return "No change"
	}
	return strings.Join(parts, " · ")
}

// competitorsOnlyIn names the competitors a names that b does not, in a's
// order of mention.
func competitorsOnlyIn(a Tally, b *Tally, brands map[BrandID]Brand) []string {
	inB := map[BrandID]bool{}
	for _, m := range b.Mentions {
		inB[m.Brand] = true
	}
	var out []string
	for _, m := range a.Mentions {
		if br := brands[m.Brand]; br.Kind == KindCompetitor && !inB[m.Brand] {
			out = append(out, br.Name)
		}
	}
	return out
}
