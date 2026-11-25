package results

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/unkeyed/byname/internal/core"
	"github.com/unkeyed/byname/internal/engines"
	"github.com/unkeyed/byname/internal/panel"
)

// Series is one line on a chart: a pair on one endpoint at one fidelity.
type Series struct {
	Pair     PairRef `json:"pair"`
	Endpoint string  `json:"endpoint"`
	Fidelity string  `json:"fidelity"`
}

type scoredAnswer struct {
	id, runID int64
	started   time.Time
	question  panel.QuestionID
	// counted is false for an answer to a question that names your brand,
	// unless someone counted it anyway: it is shown but scores skip it.
	counted  bool
	series   Series
	reported string
	fanOut   []string
	scored   panel.Scored
}

// scoredAnswers loads answers as panel.Scored, in run order and, within a
// run, in sample order.
func (s *Service) scoredAnswers(ctx context.Context, where string, args ...any) ([]scoredAnswer, error) {
	rows, err := s.db.Query(ctx, `SELECT a.id, a.run_id, coalesce(r.started_at, r.queued_at), a.question_id, q.counts_in_scores,
			m.id, m.surface_key, coalesce(m.model, ''), a.endpoint, a.fidelity, a.reported_model, a.fan_out,
			a.status, a.mentioned, a.cited, coalesce(a.position, 0),
			coalesce(array_agg(bm.brand_id ORDER BY bm.position NULLS LAST, bm.brand_id) FILTER (WHERE bm.brand_id IS NOT NULL), '{}'),
			coalesce(array_agg(coalesce(bm.position, 0) ORDER BY bm.position NULLS LAST, bm.brand_id) FILTER (WHERE bm.brand_id IS NOT NULL), '{}')
		FROM answer a JOIN run r ON r.id = a.run_id JOIN surface_model m ON m.id = a.surface_model_id
		JOIN question q ON q.id = a.question_id
		LEFT JOIN brand_mention bm ON bm.answer_id = a.id
		WHERE `+where+`
		GROUP BY a.id, r.id, m.id, q.id ORDER BY a.run_id, a.sample, a.id`, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (scoredAnswer, error) {
		var a scoredAnswer
		var brands []int64
		var positions []int
		err := r.Scan(&a.id, &a.runID, &a.started, &a.question, &a.counted, &a.series.Pair.ID, &a.series.Pair.Surface, &a.series.Pair.Model,
			&a.series.Endpoint, &a.series.Fidelity, &a.reported, &a.fanOut, &a.scored.Status, &a.scored.Mentioned, &a.scored.Cited,
			&a.scored.Position, &brands, &positions)
		for i, b := range brands {
			a.scored.Mentions = append(a.scored.Mentions, panel.Mention{Brand: panel.BrandID(b), Position: positions[i]})
		}
		return a, err
	})
}

func (s *Service) brands(ctx context.Context) ([]panel.Brand, error) {
	rows, err := s.db.Query(ctx, `SELECT id, name, aliases, domains, kind FROM tracked_brand`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (panel.Brand, error) {
		var b panel.Brand
		return b, r.Scan(&b.ID, &b.Name, &b.Aliases, &b.Domains, &b.Kind)
	})
}

// Standing is one pair's answers to a question in one run, as compare
// shows it: what most of them say, and how many there were.
type Standing struct {
	AnswerID  int64          `json:"answer_id" jsonschema:"the first sample's answer"`
	AnswerIDs []int64        `json:"answer_ids" jsonschema:"every sample's answer, in sample order"`
	Status    engines.Status `json:"status" jsonschema:"failed only when every answer failed"`
	Mentioned bool           `json:"mentioned" jsonschema:"most answers named you"`
	Cited     bool           `json:"cited" jsonschema:"most answers cited your domains"`
	Position  *int           `json:"position" jsonschema:"your best position among the answers that named you; null unless most did"`
	Answered  int            `json:"answered" jsonschema:"answers that did not fail"`
	Named     int            `json:"named" jsonschema:"answers that named you"`
}

// standing is a cell's answers as compare shows them.
func standing(ids []int64, t panel.Tally) Standing {
	return Standing{AnswerID: ids[0], AnswerIDs: ids, Status: t.Status, Mentioned: t.Mentioned(), Cited: t.MostlyCited(),
		Position: nullPosition(t.Position), Answered: t.Answered, Named: t.Named}
}

type PairChange struct {
	QuestionID int64         `json:"question_id" jsonschema:"in the after run's question set"`
	PairID     int64         `json:"pair_id"`
	Question   QuestionRef   `json:"question"`
	Pair       PairRef       `json:"pair"`
	Outcome    panel.Outcome `json:"outcome"`
	Before     *Standing     `json:"before"`
	After      *Standing     `json:"after"`
}

// Comparison is every question and pair across two runs, with counts per
// outcome, the net change (gained minus lost) and whether the share of
// answers naming you moved by more than chance gives. A question and pair
// asked several times counts by what most of its answers say; the move
// weighs every answer. Pairs list every
// question, but the counts, the net and the move leave out questions that
// count in no score, such as one that names your brand.
type Comparison struct {
	Before int64                 `json:"before"`
	After  int64                 `json:"after"`
	Counts map[panel.Outcome]int `json:"counts"`
	Net    int                   `json:"net"`
	Move   panel.Move            `json:"move" jsonschema:"whether the share of answers naming you moved by more than chance, each question and pair compared with itself"`
	Pairs  []PairChange          `json:"pairs"`
}

// Compare classifies each question and pair from run before to run after.
// Across question set versions, questions follow the old-to-new map. A
// pair whose endpoint or fidelity changed between the runs is a new series
// and counts as not comparable.
func (s *Service) Compare(ctx context.Context, before, after int64) (Comparison, error) {
	b, err := s.Run(ctx, before)
	if err != nil {
		return Comparison{}, err
	}
	a, err := s.Run(ctx, after)
	if err != nil {
		return Comparison{}, err
	}
	if b.QuestionSetVersion > a.QuestionSetVersion {
		return Comparison{}, fmt.Errorf("%w: run %d asked question set version %d, newer than run %d's version %d; put the older run first",
			core.ErrInvalid, before, b.QuestionSetVersion, after, a.QuestionSetVersion)
	}
	var m panel.QuestionMap
	if b.QuestionSetID != a.QuestionSetID {
		if m, err = s.questionMap(ctx, b.QuestionSetID, a.QuestionSetID); err != nil {
			return Comparison{}, err
		}
	}
	runs := map[int64]panel.Run{before: {}, after: {}}
	answerIDs := map[int64]map[panel.Key][]int64{before: {}, after: {}}
	series := map[int64]map[panel.Key]Series{before: {}, after: {}}
	answers, err := s.scoredAnswers(ctx, `a.run_id = ANY($1)`, []int64{before, after})
	if err != nil {
		return Comparison{}, err
	}
	pairs := map[int64]PairRef{}
	for _, x := range answers {
		pairs[x.series.Pair.ID] = x.series.Pair
		k := panel.Key{Question: x.question, SurfaceModel: panel.SurfaceModelID(x.series.Pair.ID)}
		if _, ok := series[x.runID][k]; !ok {
			series[x.runID][k] = x.series
		}
		runs[x.runID][k] = append(runs[x.runID][k], x.scored)
		answerIDs[x.runID][k] = append(answerIDs[x.runID][k], x.id)
	}
	for k, sr := range series[before] {
		mapped := k
		if m != nil {
			mapped.Question = m[k.Question]
		}
		if later, ok := series[after][mapped]; ok && later != sr {
			delete(runs[before], k)
		}
	}
	c := panel.Compare(runs[before], runs[after], m)
	out := Comparison{Before: before, After: after, Counts: map[panel.Outcome]int{}, Pairs: []PairChange{}}
	for _, o := range []panel.Outcome{panel.Gained, panel.Lost, panel.Moved, panel.Unchanged, panel.NotComparable} {
		out.Counts[o] = 0
	}
	reverse := map[panel.QuestionID]panel.QuestionID{}
	for old, now := range m {
		reverse[now] = old
	}
	var ids []panel.QuestionID
	for _, p := range c.Pairs {
		if !slices.Contains(ids, p.Key.Question) {
			ids = append(ids, p.Key.Question)
		}
	}
	questions, err := s.questionRefs(ctx, ids)
	if err != nil {
		return Comparison{}, err
	}
	var cells []panel.Cell
	for _, p := range c.Pairs {
		pc := PairChange{QuestionID: int64(p.Key.Question), PairID: int64(p.Key.SurfaceModel), Question: questions[p.Key.Question],
			Pair: pairs[int64(p.Key.SurfaceModel)], Outcome: p.Outcome}
		if p.After != nil {
			pc.After = new(standing(answerIDs[after][p.Key], *p.After))
		}
		if p.Before != nil {
			k := p.Key
			if old, ok := reverse[k.Question]; ok {
				k.Question = old
			}
			pc.Before = new(standing(answerIDs[before][k], *p.Before))
		}
		out.Pairs = append(out.Pairs, pc)
		if pc.Question.CountsInScores {
			out.Counts[p.Outcome]++
		}
		if pc.Question.CountsInScores && p.Outcome != panel.NotComparable {
			cells = append(cells, panel.Cell{BeforeNamed: p.Before.Named, BeforeAnswered: p.Before.Answered, AfterNamed: p.After.Named, AfterAnswered: p.After.Answered})
		}
	}
	out.Net = out.Counts[panel.Gained] - out.Counts[panel.Lost]
	out.Move = panel.Shift(cells)
	return out, nil
}

// questionMap maps each question of set older to the question of set newer
// that carries it on, following question_map through every version
// between them.
func (s *Service) questionMap(ctx context.Context, older, newer int64) (panel.QuestionMap, error) {
	rows, err := s.db.Query(ctx, `WITH RECURSIVE lineage (origin, qid) AS (
			SELECT id, id FROM question WHERE set_id = $2
			UNION
			SELECT l.origin, m.old_question_id FROM lineage l JOIN question_map m ON m.new_question_id = l.qid
		)
		SELECT l.qid, l.origin FROM lineage l JOIN question q ON q.id = l.qid WHERE q.set_id = $1`, older, newer)
	if err != nil {
		return nil, err
	}
	m := panel.QuestionMap{}
	for rows.Next() {
		var old, now panel.QuestionID
		if err := rows.Scan(&old, &now); err != nil {
			return nil, err
		}
		m[old] = now
	}
	return m, rows.Err()
}

// Point is one pair's answers to a question in one run: what most of them
// say, and how many there were.
type Point struct {
	RunID         int64          `json:"run_id"`
	RunStartedAt  time.Time      `json:"run_started_at"`
	AnswerID      int64          `json:"answer_id" jsonschema:"the first sample's answer"`
	AnswerIDs     []int64        `json:"answer_ids" jsonschema:"every sample's answer, in sample order"`
	QuestionID    int64          `json:"question_id" jsonschema:"the question as that run asked it, which may be an earlier version's"`
	Status        engines.Status `json:"status" jsonschema:"failed only when every answer failed"`
	Mentioned     bool           `json:"mentioned" jsonschema:"most answers named you"`
	Cited         bool           `json:"cited" jsonschema:"most answers cited your domains"`
	Position      *int           `json:"position" jsonschema:"your best position among the answers that named you; null unless most did"`
	Answered      int            `json:"answered" jsonschema:"answers that did not fail"`
	Named         int            `json:"named" jsonschema:"answers that named you"`
	ReportedModel string         `json:"reported_model" jsonschema:"the model the first sample's answer reported"`
	// Summary is the change since this series' previous run, such as
	// "Gained mention at #1 · Northwind cited · Dropped: Contoso".
	Summary string `json:"summary"`
}

type HistorySeries struct {
	Series
	Answered     int     `json:"answered" jsonschema:"runs whose answers did not all fail"`
	Named        int     `json:"named" jsonschema:"runs where most answers named you"`
	Cited        int     `json:"cited" jsonschema:"runs where most answers cited your domains"`
	BestPosition *int    `json:"best_position" jsonschema:"your best position over the runs most answers named you on; null when there are none"`
	Points       []Point `json:"points"`
}

type History struct {
	QuestionID int64 `json:"question_id"`
	// Question is the question as it reads now, so a view can title itself
	// without fetching an answer.
	Question QuestionRef     `json:"question"`
	Series   []HistorySeries `json:"series"`
}

// QuestionHistory is one question's answers across runs, one series per
// pair, endpoint and fidelity and one point per run in each, following the question back through the
// earlier versions it carries on. since limits it to runs started then or
// later; nil is all time.
func (s *Service) QuestionHistory(ctx context.Context, questionID int64, since *time.Time) (History, error) {
	var q QuestionRef
	err := s.db.QueryRow(ctx, `SELECT id, group_name, text, counts_in_scores FROM question WHERE id = $1`, questionID).Scan(&q.ID, &q.Group, &q.Text, &q.CountsInScores)
	if errors.Is(err, pgx.ErrNoRows) {
		return History{}, fmt.Errorf("%w: no question %d", core.ErrNotFound, questionID)
	}
	if err != nil {
		return History{}, err
	}
	brands, err := s.brands(ctx)
	if err != nil {
		return History{}, err
	}
	answers, err := s.scoredAnswers(ctx, `a.question_id IN (
			WITH RECURSIVE lineage (qid) AS (
				SELECT $1::bigint
				UNION
				SELECT m.old_question_id FROM lineage l JOIN question_map m ON m.new_question_id = l.qid
			) SELECT qid FROM lineage)
		AND ($2::timestamptz IS NULL OR coalesce(r.started_at, r.queued_at) >= $2)`, questionID, since)
	if err != nil {
		return History{}, err
	}
	type runSeries struct {
		Series
		run int64
	}
	out := History{QuestionID: questionID, Question: q, Series: []HistorySeries{}}
	index := map[Series]int{}
	prev := map[Series]*panel.Tally{}
	for _, cell := range cellsBy(answers, func(a scoredAnswer) runSeries { return runSeries{a.series, a.runID} }) {
		x := cell[0]
		i, ok := index[x.series]
		if !ok {
			i = len(out.Series)
			index[x.series] = i
			out.Series = append(out.Series, HistorySeries{Series: x.series, Points: []Point{}})
		}
		t := panel.TallyOf(scoredOf(cell))
		hs := &out.Series[i]
		if t.Status != engines.StatusFailed {
			hs.Answered++
		}
		if t.Mentioned() {
			hs.Named++
		}
		if t.MostlyCited() {
			hs.Cited++
		}
		if p := t.Position; p > 0 && (hs.BestPosition == nil || p < *hs.BestPosition) {
			hs.BestPosition = &p
		}
		hs.Points = append(hs.Points, Point{
			RunID: x.runID, RunStartedAt: x.started, AnswerID: x.id, AnswerIDs: idsOf(cell), QuestionID: int64(x.question), Status: t.Status,
			Mentioned: t.Mentioned(), Cited: t.MostlyCited(), Position: nullPosition(t.Position), Answered: t.Answered, Named: t.Named,
			ReportedModel: x.reported, Summary: panel.Summarize(prev[x.series], t, brands),
		})
		prev[x.series] = &t
	}
	// A pair's series stay in the order they began, oldest first.
	slices.SortStableFunc(out.Series, func(a, b HistorySeries) int { return cmp.Compare(a.Pair.ID, b.Pair.ID) })
	return out, nil
}

// cellsBy groups answers into cells by key, in the order each key first
// appears, each cell's answers in the order given.
func cellsBy[K comparable](answers []scoredAnswer, key func(scoredAnswer) K) [][]scoredAnswer {
	var out [][]scoredAnswer
	at := map[K]int{}
	for _, a := range answers {
		k := key(a)
		i, ok := at[k]
		if !ok {
			i = len(out)
			at[k] = i
			out = append(out, nil)
		}
		out[i] = append(out[i], a)
	}
	return out
}

// bySeries keys a question's answers in one run into cells.
func bySeries(a scoredAnswer) Series { return a.series }

// scoredOf is a cell's answers as panel scores them.
func scoredOf(cell []scoredAnswer) []panel.Scored {
	out := make([]panel.Scored, len(cell))
	for i, a := range cell {
		out[i] = a.scored
	}
	return out
}

func idsOf(cell []scoredAnswer) []int64 {
	out := make([]int64, len(cell))
	for i, a := range cell {
		out[i] = a.id
	}
	return out
}

func nullPosition(p int) *int {
	if p == 0 {
		return nil
	}
	return &p
}
