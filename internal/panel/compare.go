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

// Run is one run's scored answers.
type Run map[Key]Scored

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

// PairChange is one pair's outcome. Before or After is nil when the pair is
// missing from that run. Key is in the newer run's question ids, except for
// an older question with no mapping, which keeps its own id.
type PairChange struct {
	Key     Key
	Outcome Outcome
	Before  *Scored
	After   *Scored
}

type Comparison struct {
	Pairs  []PairChange
	Counts map[Outcome]int
}

// Net is gained minus lost.
func (c Comparison) Net() int { return c.Counts[Gained] - c.Counts[Lost] }

// Compare classifies every pair across two runs. Pairs only compare across
// question set versions through m.
func Compare(before, after Run, m QuestionMap) Comparison {
	c := Comparison{Counts: map[Outcome]int{}}
	add := func(k Key, b, a *Scored) {
		o := outcome(b, a)
		c.Pairs = append(c.Pairs, PairChange{Key: k, Outcome: o, Before: b, After: a})
		c.Counts[o]++
	}

	mapped := map[Key]*Scored{}
	for k, s := range before {
		if m != nil {
			q, ok := m[k.Question]
			if !ok {
				add(k, &s, nil)
				continue
			}
			k.Question = q
		}
		mapped[k] = &s
	}
	for k, s := range after {
		add(k, mapped[k], &s)
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

func comparable(b, a *Scored) bool {
	return b != nil && a != nil && b.Status != engines.StatusFailed && a.Status != engines.StatusFailed
}

func outcome(b, a *Scored) Outcome {
	switch {
	case !comparable(b, a):
		return NotComparable
	case !b.Mentioned && a.Mentioned:
		return Gained
	case b.Mentioned && !a.Mentioned:
		return Lost
	case b.Mentioned && b.Position != a.Position:
		return Moved
	default:
		return Unchanged
	}
}

// Summarize is the one-line change for one pair between a run and the one
// before it, as the prompt drawer's run history shows it, for example
// "Gained mention at #1 · Northwind cited · Dropped: Contoso, Fabrikam".
// prev is nil when there is no earlier answer.
func Summarize(prev *Scored, cur Scored, brands []Brand) string {
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
	case !prev.Cited && cur.Cited:
		parts = append(parts, self.Name+" cited")
	case prev.Cited && !cur.Cited:
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
func competitorsOnlyIn(a Scored, b *Scored, brands map[BrandID]Brand) []string {
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
