package panel

import (
	"math"
	"testing"
)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestWilsonBand(t *testing.T) {
	if b := Wilson(0, 0); b != nil {
		t.Fatalf("Wilson(0, 0) = %+v, want nil: no answers make no band", b)
	}
	// References from the Wilson formula worked in Python apart from this package.
	for _, c := range []struct {
		k, n      int
		low, high float64
	}{
		{31, 96, 0.23775841402236636, 0.4217016900105605},
		{0, 10, 0, 0.2775327998628892},
		{10, 10, 0.7224672001371107, 1},
	} {
		b := Wilson(c.k, c.n)
		if b == nil || !near(b.Low, c.low) || !near(b.High, c.high) {
			t.Errorf("Wilson(%d, %d) = %+v, want %v to %v", c.k, c.n, b, c.low, c.high)
		}
	}
}

// flips builds one-answer cells: gained started naming you, lost stopped,
// and same answered alike both times.
func flips(gained, lost, sameNamed, sameNot int) []Cell {
	var cells []Cell
	for range gained {
		cells = append(cells, Cell{BeforeNamed: 0, BeforeAnswered: 1, AfterNamed: 1, AfterAnswered: 1})
	}
	for range lost {
		cells = append(cells, Cell{BeforeNamed: 1, BeforeAnswered: 1, AfterNamed: 0, AfterAnswered: 1})
	}
	for range sameNamed {
		cells = append(cells, Cell{BeforeNamed: 1, BeforeAnswered: 1, AfterNamed: 1, AfterAnswered: 1})
	}
	for range sameNot {
		cells = append(cells, Cell{BeforeNamed: 0, BeforeAnswered: 1, AfterNamed: 0, AfterAnswered: 1})
	}
	return cells
}

// With one answer per side, Shift is McNemar's test with continuity
// correction, (|b-c|-1)²/(b+c) on one degree of freedom.
func TestShiftWithOneAnswerIsMcNemar(t *testing.T) {
	for _, c := range []struct {
		name                            string
		gained, lost, sameNamed, sameNo int
		p                               float64
		real                            bool
		before, after                   float64
	}{
		{"three gained, one lost is noise", 3, 1, 20, 76, 0.6170750774519738, false, 0.21, 0.23},
		{"eight gained, none lost is real", 8, 0, 20, 72, 0.013328328780817557, true, 0.20, 0.28},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := Shift(flips(c.gained, c.lost, c.sameNamed, c.sameNo))
			if m.Cells != 100 || !near(m.PValue, c.p) || m.Real != c.real {
				t.Fatalf("Shift = %+v, want 100 cells, p %v, real %v", m, c.p, c.real)
			}
			if m.BeforeRate == nil || m.AfterRate == nil || !near(*m.BeforeRate, c.before) || !near(*m.AfterRate, c.after) {
				t.Fatalf("rates = %v → %v, want %v → %v", m.BeforeRate, m.AfterRate, c.before, c.after)
			}
		})
	}
}

func TestShiftWithNothingToCompare(t *testing.T) {
	m := Shift([]Cell{{BeforeNamed: 1, BeforeAnswered: 1}, {AfterNamed: 1, AfterAnswered: 1}})
	if m.Cells != 0 || m.BeforeRate != nil || m.AfterRate != nil || m.PValue != 1 || m.Real {
		t.Fatalf("Shift over cells answered in one run only = %+v, want no cells, no rates, p 1", m)
	}
	m = Shift(flips(0, 0, 5, 5))
	if m.Cells != 10 || m.PValue != 1 || m.Real {
		t.Fatalf("Shift with no answer flipped = %+v, want p 1", m)
	}
}

// A cell with several answers a side weighs them together. One cell is one
// 2×2 table, where the test is Yates's chi-square scaled by (N-1)/N.
func TestShiftWithSeveralAnswersPerCell(t *testing.T) {
	m := Shift([]Cell{{BeforeNamed: 0, BeforeAnswered: 3, AfterNamed: 3, AfterAnswered: 3}})
	if m.Cells != 1 || !near(m.PValue, 0.13603712811414367) || m.Real {
		t.Fatalf("0 of 3 to 3 of 3 = %+v, want p 0.136, not real", m)
	}
	// The same rise on four questions is real, while a question that names
	// you every time adds answers without adding weight.
	cells := []Cell{{BeforeNamed: 3, BeforeAnswered: 3, AfterNamed: 3, AfterAnswered: 3}}
	for range 4 {
		cells = append(cells, Cell{BeforeNamed: 0, BeforeAnswered: 3, AfterNamed: 3, AfterAnswered: 3})
	}
	m = Shift(cells)
	// Each rising table has a-E 1.5 and V 0.45, so the statistic is (6-0.5)²/1.8.
	if m.Cells != 5 || !near(m.PValue, 4.141188842657834e-05) || !m.Real || !near(*m.BeforeRate, 0.2) || !near(*m.AfterRate, 1) {
		t.Fatalf("four questions 0 of 3 to 3 of 3 = %+v, want p 0.00004, real, 20%% → 100%%", m)
	}
}
