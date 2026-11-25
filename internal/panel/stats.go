package panel

import "math"

// z95 is the standard normal quantile for a two-sided 95% interval.
const z95 = 1.959963984540054

// Band is a 95% Wilson score interval on a rate of k in n.
type Band struct {
	Low  float64 `json:"low" jsonschema:"the lowest rate the answers make likely, 0 to 1"`
	High float64 `json:"high" jsonschema:"the highest rate the answers make likely, 0 to 1"`
}

// Wilson is the 95% band on k in n, or nil when n is 0. Unlike the normal
// approximation it stays inside 0 to 1 and is not zero-width at 0 or n.
func Wilson(k, n int) *Band {
	if n == 0 {
		return nil
	}
	p, nf, z2 := float64(k)/float64(n), float64(n), z95*z95
	d := 1 + z2/nf
	center := (p + z2/(2*nf)) / d
	half := z95 * math.Sqrt(p*(1-p)/nf+z2/(4*nf*nf)) / d
	return &Band{Low: max(0, center-half), High: min(1, center+half)}
}

// Cell is one question on one pair: its answers in the earlier run and the
// later one.
type Cell struct {
	BeforeNamed, BeforeAnswered, AfterNamed, AfterAnswered int
}

// Move is whether the share of answers naming you moved between two runs by
// more than chance gives.
type Move struct {
	Cells      int      `json:"cells" jsonschema:"question and pair cells compared, those answered in both runs"`
	BeforeRate *float64 `json:"before_rate" jsonschema:"named over answered across those cells in the earlier run; null when none"`
	AfterRate  *float64 `json:"after_rate" jsonschema:"named over answered across those cells in the later run; null when none"`
	PValue     float64  `json:"p_value" jsonschema:"the chance of a move at least this large if nothing changed, 0 to 1"`
	Real       bool     `json:"real" jsonschema:"true when p_value is under 0.05"`
}

// Shift compares each cell with itself across the two runs, a
// Cochran–Mantel–Haenszel test with continuity correction stratified by
// cell. A question one app always names, or never does, weighs nothing;
// only cells whose answers differ between the runs count. With one answer
// on each side it is McNemar's test on the answers that flipped.
func Shift(cells []Cell) Move {
	var m Move
	var beforeNamed, beforeAnswered, afterNamed, afterAnswered int
	var diff, variance float64
	for _, c := range cells {
		n1, n2 := c.AfterAnswered, c.BeforeAnswered
		if n1 == 0 || n2 == 0 {
			continue
		}
		m.Cells++
		beforeNamed, beforeAnswered = beforeNamed+c.BeforeNamed, beforeAnswered+n2
		afterNamed, afterAnswered = afterNamed+c.AfterNamed, afterAnswered+n1
		N, m1 := float64(n1+n2), float64(c.AfterNamed+c.BeforeNamed)
		diff += float64(c.AfterNamed) - float64(n1)*m1/N
		variance += float64(n1) * float64(n2) * m1 * (N - m1) / (N * N * (N - 1))
	}
	if beforeAnswered > 0 {
		m.BeforeRate = new(float64(beforeNamed) / float64(beforeAnswered))
		m.AfterRate = new(float64(afterNamed) / float64(afterAnswered))
	}
	m.PValue = 1
	if variance > 0 {
		stat := math.Pow(max(0, math.Abs(diff)-0.5), 2) / variance
		m.PValue = math.Erfc(math.Sqrt(stat / 2))
	}
	m.Real = m.PValue < 0.05
	return m
}
