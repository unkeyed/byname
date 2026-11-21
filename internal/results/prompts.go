package results

import (
	"cmp"
	"context"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/unkeyed/byname/internal/engines"
	"github.com/unkeyed/byname/internal/panel"
)

// RunRef is a run as a column header shows it.
type RunRef struct {
	ID        int64     `json:"id"`
	StartedAt time.Time `json:"started_at"`
}

// DotState is how a question did in one run, over the pairs in scope.
type DotState string

const (
	DotNamed    DotState = "named"     // at least one pair named you
	DotCited    DotState = "cited"     // none named you, at least one cited your domains
	DotNotNamed DotState = "not_named" // some pair answered, none named or cited you
	DotFailed   DotState = "failed"    // every call failed
)

// Dot is one question in one run. It counts pairs; it is not an average.
type Dot struct {
	RunID    int64    `json:"run_id"`
	State    DotState `json:"state"`
	Named    int      `json:"named" jsonschema:"pairs that named you"`
	Answered int      `json:"answered" jsonschema:"pairs whose call did not fail"`
}

// PromptRow is one question on the Prompts page.
type PromptRow struct {
	Question QuestionRef `json:"question" jsonschema:"as the latest run asked it"`
	// Answered is how many pairs answered on the latest run, NamedBy the
	// ones that named you.
	Answered     int      `json:"answered"`
	NamedBy      []Series `json:"named_by"`
	BestPosition *int     `json:"best_position" jsonschema:"your best position on the latest run; null when no pair named you"`
	Instead      []string `json:"instead" jsonschema:"competitors named on the latest run by answers that did not name you, most often first"`
	// Dots line up with Prompts.Runs, null where the question was not asked.
	Dots []*Dot `json:"dots"`
}

// Prompts is every question of the latest run in range with its standing
// there and a dot per run over the range, on the pairs in scope.
type Prompts struct {
	Runs        []RunRef    `json:"runs" jsonschema:"runs in range with answers on the pairs in scope, oldest first"`
	LatestRunID int64       `json:"latest_run_id" jsonschema:"0 when no run in range has answers"`
	Rows        []PromptRow `json:"rows" jsonschema:"in question order"`
}

// Prompts lists the latest run's questions over the runs started since
// (nil for all time) on the pairs in scope. Earlier runs' questions follow
// the question map onto the latest run's versions.
func (s *Service) Prompts(ctx context.Context, since *time.Time, sc Scope) (Prompts, error) {
	out := Prompts{Runs: []RunRef{}, Rows: []PromptRow{}}
	if err := sc.check(); err != nil {
		return out, err
	}
	answers, err := s.scoredAnswers(ctx, scopedFilter, since, string(sc.Surface), sc.Model)
	if err != nil || len(answers) == 0 {
		return out, err
	}
	brands, err := s.brands(ctx)
	if err != nil {
		return out, err
	}
	latest := answers[len(answers)-1].runID
	sets, err := s.runSets(ctx, answers)
	if err != nil {
		return out, err
	}
	carry := map[int64]panel.QuestionMap{}
	for _, set := range sets {
		if set != sets[latest] && carry[set] == nil {
			if carry[set], err = s.questionMap(ctx, set, sets[latest]); err != nil {
				return out, err
			}
		}
	}

	runIndex := map[int64]int{}
	for _, a := range answers {
		if _, ok := runIndex[a.runID]; !ok {
			runIndex[a.runID] = len(out.Runs)
			out.Runs = append(out.Runs, RunRef{ID: a.runID, StartedAt: a.started})
		}
	}
	type cell struct{ answered, named, cited, calls int }
	cells := map[panel.QuestionID]map[int64]*cell{}
	var current []panel.QuestionID
	onLatest := map[panel.QuestionID][]scoredAnswer{}
	for _, a := range answers {
		q := a.question
		if set := sets[a.runID]; set != sets[latest] {
			mapped, ok := carry[set][q]
			if !ok {
				continue
			}
			q = mapped
		}
		if a.runID == latest {
			if _, ok := onLatest[q]; !ok {
				current = append(current, q)
			}
			onLatest[q] = append(onLatest[q], a)
		}
		if cells[q] == nil {
			cells[q] = map[int64]*cell{}
		}
		c := cells[q][a.runID]
		if c == nil {
			c = &cell{}
			cells[q][a.runID] = c
		}
		c.calls++
		if a.scored.Status != engines.StatusFailed {
			c.answered++
		}
		if a.scored.Mentioned {
			c.named++
		}
		if a.scored.Cited {
			c.cited++
		}
	}
	order, err := s.questionOrder(ctx, sets[latest])
	if err != nil {
		return out, err
	}
	slices.SortFunc(current, func(a, b panel.QuestionID) int { return cmp.Compare(order[a], order[b]) })
	refs, err := s.questionRefs(ctx, current)
	if err != nil {
		return out, err
	}
	for _, q := range current {
		row := PromptRow{Question: refs[q], NamedBy: []Series{}, Dots: make([]*Dot, len(out.Runs))}
		for run, c := range cells[q] {
			d := &Dot{RunID: run, Named: c.named, Answered: c.answered}
			switch {
			case c.named > 0:
				d.State = DotNamed
			case c.cited > 0:
				d.State = DotCited
			case c.answered > 0:
				d.State = DotNotNamed
			default:
				d.State = DotFailed
			}
			row.Dots[runIndex[run]] = d
		}
		for _, a := range onLatest[q] {
			if a.scored.Status != engines.StatusFailed {
				row.Answered++
			}
			if a.scored.Mentioned {
				row.NamedBy = append(row.NamedBy, a.series)
			}
			if p := a.scored.Position; p > 0 && (row.BestPosition == nil || p < *row.BestPosition) {
				row.BestPosition = &p
			}
		}
		row.Instead = namedInstead(onLatest[q], brands)
		out.Rows = append(out.Rows, row)
	}
	out.LatestRunID = latest
	return out, nil
}

// scopedFilter is scopedWhere for scoredAnswers, which adds its own WHERE.
var scopedFilter = strings.TrimPrefix(strings.TrimSpace(scopedWhere), "WHERE ")

// namedInstead is the competitors named by the answers that did not name
// you, most often first, then by name.
func namedInstead(answers []scoredAnswer, brands []panel.Brand) []string {
	byID := map[panel.BrandID]panel.Brand{}
	for _, b := range brands {
		byID[b.ID] = b
	}
	seen := map[panel.BrandID]int{}
	for _, a := range answers {
		if a.scored.Mentioned || a.scored.Status == engines.StatusFailed {
			continue
		}
		for _, m := range a.scored.Mentions {
			if byID[m.Brand].Kind == panel.KindCompetitor {
				seen[m.Brand]++
			}
		}
	}
	ids := slices.Collect(maps.Keys(seen))
	slices.SortFunc(ids, func(a, b panel.BrandID) int {
		return cmp.Or(cmp.Compare(seen[b], seen[a]), cmp.Compare(byID[a].Name, byID[b].Name))
	})
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = byID[id].Name
	}
	return out
}

// runSets is the question set each run of answers asked.
func (s *Service) runSets(ctx context.Context, answers []scoredAnswer) (map[int64]int64, error) {
	var ids []int64
	for _, a := range answers {
		if !slices.Contains(ids, a.runID) {
			ids = append(ids, a.runID)
		}
	}
	rows, err := s.db.Query(ctx, `SELECT id, question_set_id FROM run WHERE id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	out := map[int64]int64{}
	for rows.Next() {
		var run, set int64
		if err := rows.Scan(&run, &set); err != nil {
			return nil, err
		}
		out[run] = set
	}
	return out, rows.Err()
}

func (s *Service) questionOrder(ctx context.Context, set int64) (map[panel.QuestionID]int, error) {
	rows, err := s.db.Query(ctx, `SELECT id, ord FROM question WHERE set_id = $1`, set)
	if err != nil {
		return nil, err
	}
	out := map[panel.QuestionID]int{}
	for rows.Next() {
		var id panel.QuestionID
		var ord int
		if err := rows.Scan(&id, &ord); err != nil {
			return nil, err
		}
		out[id] = ord
	}
	return out, rows.Err()
}
