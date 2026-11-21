package panel

import (
	"math"
	"sort"

	"github.com/unkeyed/byname/internal/engines"
)

// Gap is one question's standing on the latest run, for the Gaps page.
type Gap struct {
	Question QuestionID
	// Engines is how many surface model pairs answered (failures excluded).
	Engines int
	// Named is how many of those answers named you.
	Named int
	// Competitors is how many distinct competitors those answers named.
	Competitors int
	// Demand is monthly searches for the topic, or nil when no demand
	// source is configured or it has no figure.
	Demand *int
}

// NewGap summarizes one question's answers from the latest run.
func NewGap(q QuestionID, answers []Scored, brands []Brand, demand *int) Gap {
	kinds := map[BrandID]BrandKind{}
	for _, b := range brands {
		kinds[b.ID] = b.Kind
	}
	g := Gap{Question: q, Demand: demand}
	competitors := map[BrandID]bool{}
	for _, a := range answers {
		if a.Status == engines.StatusFailed {
			continue
		}
		g.Engines++
		if a.Mentioned {
			g.Named++
		}
		for _, m := range a.Mentions {
			if kinds[m.Brand] == KindCompetitor {
				competitors[m.Brand] = true
			}
		}
	}
	g.Competitors = len(competitors)
	return g
}

// Opportunity scores a gap from 0 to 5: the share of engines that did not
// name you times 3, rounded, plus 2 for 1,000 or more monthly searches or 1
// for 300 or more. Without a demand figure the score is the engine part.
func (g Gap) Opportunity() int {
	if g.Engines == 0 {
		return 0
	}
	score := int(math.Round(float64(g.Engines-g.Named) / float64(g.Engines) * 3))
	if g.Demand != nil {
		switch {
		case *g.Demand >= 1000:
			score += 2
		case *g.Demand >= 300:
			score++
		}
	}
	return score
}

// RankGaps keeps the questions where at least one engine did not name you,
// highest opportunity first, ties broken by how many competitors were named.
func RankGaps(gaps []Gap) []Gap {
	var out []Gap
	for _, g := range gaps {
		if g.Named < g.Engines {
			out = append(out, g)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Opportunity() != b.Opportunity() {
			return a.Opportunity() > b.Opportunity()
		}
		if a.Competitors != b.Competitors {
			return a.Competitors > b.Competitors
		}
		return a.Question < b.Question
	})
	return out
}
