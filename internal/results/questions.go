package results

import (
	"cmp"
	"context"
	"slices"

	"github.com/unkeyed/byname/internal/engines"
	"github.com/unkeyed/byname/internal/panel"
)

// Cell is one pair's answer to one question in the questions table.
type Cell struct {
	AnswerID  int64          `json:"answer_id"`
	Status    engines.Status `json:"status"`
	Mentioned bool           `json:"mentioned"`
	Cited     bool           `json:"cited"`
	Position  *int           `json:"position"`
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
	Named        int     `json:"named" jsonschema:"cells that named you"`
	Cited        int     `json:"cited" jsonschema:"cells that cited your domains"`
	BestPosition *int    `json:"best_position" jsonschema:"your best position across the row; null when no cell named you"`
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
	for _, a := range answers {
		i, ok := row[a.Question.ID]
		if !ok {
			i = len(out.Rows)
			row[a.Question.ID] = i
			out.Rows = append(out.Rows, QuestionRow{Question: a.Question, Cells: make([]*Cell, len(out.Columns))})
		}
		r := &out.Rows[i]
		c := &Cell{AnswerID: a.ID, Status: engines.Status(a.Status), Mentioned: a.Mentioned, Cited: a.Cited, Position: a.Position, Outcome: panel.NotComparable}
		if o, ok := outcomes[key{a.Question.ID, a.Pair.ID}]; ok {
			c.Outcome = o
		}
		r.Cells[col[Series{Pair: a.Pair, Endpoint: a.Endpoint, Fidelity: a.Fidelity}]] = c
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
