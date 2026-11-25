package results

import (
	"cmp"
	"context"
	"slices"

	"github.com/unkeyed/byname/internal/engines"
	"github.com/unkeyed/byname/internal/panel"
)

// Cell is one pair's answers to one question in the questions table: what
// most of them say, and how many there were.
type Cell struct {
	AnswerID  int64          `json:"answer_id" jsonschema:"the first sample's answer"`
	AnswerIDs []int64        `json:"answer_ids" jsonschema:"every sample's answer, in sample order"`
	Status    engines.Status `json:"status" jsonschema:"failed only when every answer failed"`
	Mentioned bool           `json:"mentioned" jsonschema:"most answers named you"`
	Cited     bool           `json:"cited" jsonschema:"most answers cited your domains"`
	Position  *int           `json:"position" jsonschema:"your best position among the answers that named you"`
	Answered  int            `json:"answered" jsonschema:"answers that did not fail"`
	Named     int            `json:"named" jsonschema:"answers that named you"`
	// Outcome is the change since the previous run: not_comparable when
	// there is none, or the pair changed endpoint or fidelity in between.
	Outcome panel.Outcome `json:"outcome"`
}

type QuestionRow struct {
	Question QuestionRef `json:"question"`
	// Cells line up with the table's columns, null where that pair has no
	// answer to this question.
	Cells        []*Cell `json:"cells"`
	Answered     int     `json:"answered" jsonschema:"cells that did not fail"`
	Named        int     `json:"named" jsonschema:"cells where most answers named you"`
	Cited        int     `json:"cited" jsonschema:"cells where most answers cited your domains"`
	BestPosition *int    `json:"best_position" jsonschema:"your best position in the row's answers; null when none named you"`
	Gained       int     `json:"gained" jsonschema:"cells that gained a mention since the previous run"`
	Lost         int     `json:"lost"`
	Moved        int     `json:"moved"`
}

// QuestionTable is one run as the questions table shows it: a row per
// question and a column per series, with each cell's change since the
// previous run.
type QuestionTable struct {
	RunID int64 `json:"run_id"`
	// PreviousRunID is the run changes count from: the latest earlier run
	// with answers. Null on the first run.
	PreviousRunID *int64        `json:"previous_run_id"`
	Columns       []Series      `json:"columns" jsonschema:"one per pair, endpoint and fidelity in scope, in pair order"`
	Rows          []QuestionRow `json:"rows" jsonschema:"in question order"`
}

// RunQuestions lays out one run's answers on the pairs in scope as the
// questions table.
func (s *Service) RunQuestions(ctx context.Context, runID int64, sc Scope) (QuestionTable, error) {
	if err := sc.check(); err != nil {
		return QuestionTable{}, err
	}
	answers, err := s.RunAnswers(ctx, runID)
	if err != nil {
		return QuestionTable{}, err
	}
	out := QuestionTable{RunID: runID, Columns: []Series{}, Rows: []QuestionRow{}}
	type key struct{ question, pair int64 }
	outcomes := map[key]panel.Outcome{}
	var prev int64
	if err := s.db.QueryRow(ctx, `SELECT coalesce(max(run_id), 0) FROM answer WHERE run_id < $1`, runID).Scan(&prev); err != nil {
		return QuestionTable{}, err
	}
	if prev != 0 {
		out.PreviousRunID = &prev
		c, err := s.Compare(ctx, prev, runID)
		if err != nil {
			return QuestionTable{}, err
		}
		for _, p := range c.Pairs {
			outcomes[key{p.QuestionID, p.PairID}] = p.Outcome
		}
	}

	answers = slices.DeleteFunc(answers, func(a AnswerSummary) bool { return !sc.holds(a.Pair) })
	col := map[Series]int{}
	for _, a := range answers {
		sr := Series{Pair: a.Pair, Endpoint: a.Endpoint, Fidelity: a.Fidelity}
		if _, ok := col[sr]; !ok {
			col[sr] = len(out.Columns)
			out.Columns = append(out.Columns, sr)
		}
	}
	slices.SortStableFunc(out.Columns, func(a, b Series) int { return cmp.Compare(a.Pair.ID, b.Pair.ID) })
	for i, sr := range out.Columns {
		col[sr] = i
	}

	row := map[int64]int{}
	cells := map[[2]int][]AnswerSummary{}
	for _, a := range answers {
		i, ok := row[a.Question.ID]
		if !ok {
			i = len(out.Rows)
			row[a.Question.ID] = i
			out.Rows = append(out.Rows, QuestionRow{Question: a.Question, Cells: make([]*Cell, len(out.Columns))})
		}
		at := [2]int{i, col[Series{Pair: a.Pair, Endpoint: a.Endpoint, Fidelity: a.Fidelity}]}
		cells[at] = append(cells[at], a)
	}
	for at, cell := range cells {
		scored := make([]panel.Scored, len(cell))
		c := &Cell{Outcome: panel.NotComparable}
		for j, a := range cell {
			scored[j] = panel.Scored{Status: engines.Status(a.Status), Mentioned: a.Mentioned, Cited: a.Cited}
			if a.Position != nil {
				scored[j].Position = *a.Position
			}
			c.AnswerIDs = append(c.AnswerIDs, a.ID)
		}
		t := panel.TallyOf(scored)
		c.AnswerID, c.Status, c.Mentioned, c.Cited, c.Position = cell[0].ID, t.Status, t.Mentioned(), t.MostlyCited(), nullPosition(t.Position)
		c.Answered, c.Named = t.Answered, t.Named
		if o, ok := outcomes[key{cell[0].Question.ID, cell[0].Pair.ID}]; ok {
			c.Outcome = o
		}
		r := &out.Rows[at[0]]
		r.Cells[at[1]] = c
		if c.Status != engines.StatusFailed {
			r.Answered++
		}
		if c.Mentioned {
			r.Named++
		}
		if c.Cited {
			r.Cited++
		}
		if c.Position != nil && (r.BestPosition == nil || *c.Position < *r.BestPosition) {
			r.BestPosition = c.Position
		}
		switch c.Outcome {
		case panel.Gained:
			r.Gained++
		case panel.Lost:
			r.Lost++
		case panel.Moved:
			r.Moved++
		}
	}
	return out, nil
}
