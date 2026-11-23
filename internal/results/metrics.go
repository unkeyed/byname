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

// RatePoint is one series in one run. Rates are over answered calls
// (failed ones excluded) and null when none answered.
type RatePoint struct {
	RunID        int64     `json:"run_id"`
	RunStartedAt time.Time `json:"run_started_at"`
	Calls        int       `json:"calls"`
	Answered     int       `json:"answered"`
	Mentioned    int       `json:"mentioned"`
	Cited        int       `json:"cited"`
	MentionRate  *float64  `json:"mention_rate"`
	CitationRate *float64  `json:"citation_rate"`
}

type RateSeries struct {
	Series
	// Models are the models the line combines: one on a pair line, none on
	// an app surface, which picks its own.
	Models []string    `json:"models"`
	Points []RatePoint `json:"points"`
}

// Rates is the mention rate and citation rate per run for each line, one
// per pair or per surface (by), over the pairs in scope. since limits it to
// runs started then or later; nil is all time.
func (s *Service) Rates(ctx context.Context, since *time.Time, sc Scope, by Grouping) ([]RateSeries, error) {
	if by == "" {
		by = ByPair
	}
	if err := errors.Join(sc.check(), by.check()); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(ctx, `SELECT `+lineKey+`, a.run_id, coalesce(r.started_at, r.queued_at), min(m.id), `+lineModels+`,
			count(*), count(*) FILTER (WHERE a.status <> 'failed'), count(*) FILTER (WHERE a.mentioned), count(*) FILTER (WHERE a.cited)`+
		scopedFrom+scopedWhere+counted+`GROUP BY 1, 2, 3, 4, 5, 6, 7 ORDER BY 6`, scopeArgs(since, sc, by)...)
	if err != nil {
		return nil, err
	}
	out := []RateSeries{}
	first := map[Series]int64{}
	index := map[Series]int{}
	for rows.Next() {
		var sr Series
		var p RatePoint
		var pair int64
		var models []string
		if err := rows.Scan(&sr.Pair.ID, &sr.Pair.Surface, &sr.Pair.Model, &sr.Endpoint, &sr.Fidelity,
			&p.RunID, &p.RunStartedAt, &pair, &models, &p.Calls, &p.Answered, &p.Mentioned, &p.Cited); err != nil {
			return nil, err
		}
		if p.Answered > 0 {
			p.MentionRate = new(float64(p.Mentioned) / float64(p.Answered))
			p.CitationRate = new(float64(p.Cited) / float64(p.Answered))
		}
		i, ok := index[sr]
		if !ok {
			i = len(out)
			index[sr] = i
			first[sr] = pair
			out = append(out, RateSeries{Series: sr, Models: []string{}, Points: []RatePoint{}})
		}
		first[sr] = min(first[sr], pair)
		for _, m := range models {
			if !slices.Contains(out[i].Models, m) {
				out[i].Models = append(out[i].Models, m)
			}
		}
		out[i].Points = append(out[i].Points, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Lines follow pair order; a pair's series stay in the order they began.
	slices.SortStableFunc(out, func(a, b RateSeries) int { return cmp.Compare(first[a.Series], first[b.Series]) })
	for i := range out {
		slices.Sort(out[i].Models)
	}
	return out, nil
}

// BrandShare is one brand's share of the brand mentions in a series' run.
type BrandShare struct {
	BrandID int64           `json:"brand_id"`
	Name    string          `json:"name"`
	Kind    panel.BrandKind `json:"kind" jsonschema:"self or competitor; technologies never count"`
	// Mentions is how many answers named the brand.
	Mentions int     `json:"mentions"`
	Share    float64 `json:"share"`
}

type SharePoint struct {
	RunID        int64        `json:"run_id"`
	RunStartedAt time.Time    `json:"run_started_at"`
	Answered     int          `json:"answered"`
	Mentions     int          `json:"mentions" jsonschema:"mentions of you and your competitors, the share's denominator"`
	Brands       []BrandShare `json:"brands" jsonschema:"most mentioned first"`
}

type ShareSeries struct {
	Series
	Points []SharePoint `json:"points"`
}

// Share is each brand's share of mentions against your competitors, per
// run for each pair's series in scope. Technologies are left out.
func (s *Service) Share(ctx context.Context, since *time.Time, sc Scope) ([]ShareSeries, error) {
	rates, err := s.Rates(ctx, since, sc, ByPair)
	if err != nil {
		return nil, err
	}
	type key struct {
		series Series
		run    int64
	}
	brands := map[key][]BrandShare{}
	rows, err := s.db.Query(ctx, `SELECT `+lineKey+`, a.run_id, b.id, b.name, b.kind, count(*)`+scopedFrom+`
			JOIN brand_mention bm ON bm.answer_id = a.id JOIN brand b ON b.id = bm.brand_id AND b.kind <> 'technology'`+
		scopedWhere+counted+`GROUP BY 1, 2, 3, 4, 5, 6, 7 ORDER BY count(*) DESC, b.name`, scopeArgs(since, sc, ByPair)...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var k key
		var b BrandShare
		if err := rows.Scan(&k.series.Pair.ID, &k.series.Pair.Surface, &k.series.Pair.Model, &k.series.Endpoint, &k.series.Fidelity,
			&k.run, &b.BrandID, &b.Name, &b.Kind, &b.Mentions); err != nil {
			return nil, err
		}
		brands[k] = append(brands[k], b)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]ShareSeries, len(rates))
	for i, rs := range rates {
		out[i] = ShareSeries{Series: rs.Series, Points: make([]SharePoint, len(rs.Points))}
		for j, p := range rs.Points {
			bs := brands[key{rs.Series, p.RunID}]
			total := 0
			for _, b := range bs {
				total += b.Mentions
			}
			for k := range bs {
				bs[k].Share = float64(bs[k].Mentions) / float64(total)
			}
			out[i].Points[j] = SharePoint{RunID: p.RunID, RunStartedAt: p.RunStartedAt, Answered: p.Answered, Mentions: total, Brands: append([]BrandShare{}, bs...)}
		}
	}
	return out, nil
}

// RunSummary is one run's headline over the pairs in scope: how many
// question and pair answers named or cited you, and your share of the
// brand mentions. It counts pairs; it is not an average of line rates.
// Answers to questions that name your brand are left out unless counted.
type RunSummary struct {
	RunID        int64     `json:"run_id"`
	RunStartedAt time.Time `json:"run_started_at"`
	Answered     int       `json:"answered" jsonschema:"question and pair answers that did not fail"`
	Mentioned    int       `json:"mentioned" jsonschema:"answers that named you"`
	Cited        int       `json:"cited" jsonschema:"answers that cited your domains"`
	MentionRate  *float64  `json:"mention_rate" jsonschema:"mentioned over answered; null when none answered"`
	CitationRate *float64  `json:"citation_rate" jsonschema:"cited over answered; null when none answered"`
	Mentions     int       `json:"mentions" jsonschema:"mentions of you and your competitors, the share's denominator"`
	Share        *float64  `json:"share" jsonschema:"your mentions over mentions; null when no brand was named"`
}

// Summary is the headline per run over the pairs in scope, oldest run first.
func (s *Service) Summary(ctx context.Context, since *time.Time, sc Scope) ([]RunSummary, error) {
	if err := sc.check(); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(ctx, `SELECT a.run_id, coalesce(r.started_at, r.queued_at),
			count(*) FILTER (WHERE a.status <> 'failed'), count(*) FILTER (WHERE a.mentioned), count(*) FILTER (WHERE a.cited),
			coalesce(sum(n.brands), 0)::int, coalesce(sum(n.self), 0)::int`+scopedFrom+`
			CROSS JOIN LATERAL (SELECT count(*) AS brands, count(*) FILTER (WHERE b.kind = 'self') AS self
				FROM brand_mention bm JOIN brand b ON b.id = bm.brand_id AND b.kind <> 'technology' WHERE bm.answer_id = a.id) n`+
		scopedWhere+counted+`GROUP BY 1, 2 ORDER BY 1`, scopeArgs(since, sc, "")[:3]...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (RunSummary, error) {
		var x RunSummary
		var self int
		if err := r.Scan(&x.RunID, &x.RunStartedAt, &x.Answered, &x.Mentioned, &x.Cited, &x.Mentions, &self); err != nil {
			return x, err
		}
		if x.Answered > 0 {
			x.MentionRate = new(float64(x.Mentioned) / float64(x.Answered))
			x.CitationRate = new(float64(x.Cited) / float64(x.Answered))
		}
		if x.Mentions > 0 {
			x.Share = new(float64(self) / float64(x.Mentions))
		}
		return x, nil
	})
}

// Gap is a question where at least one pair did not name you on a run.
type Gap struct {
	Question QuestionRef `json:"question"`
	// Engines is how many pairs answered; Named how many named you, the N
	// of "visible on N of M".
	Engines     int `json:"engines"`
	Named       int `json:"named"`
	Competitors int `json:"competitors" jsonschema:"distinct competitors the answers named"`
	// Opportunity is 0 to 5: the share of pairs that did not name you
	// times 3, plus up to 2 for search demand, which no source provides yet.
	Opportunity int       `json:"opportunity"`
	Demand      *int      `json:"demand" jsonschema:"monthly searches; null until a demand source is configured"`
	NamedOnMiss []string  `json:"named_on_miss" jsonschema:"competitors named by the answers that did not name you, most often first"`
	Pairs       []GapPair `json:"pairs" jsonschema:"each pair's answer, in pair order"`
}

// GapPair is one pair's answer to a gap's question.
type GapPair struct {
	Series
	AnswerID int64          `json:"answer_id"`
	Status   engines.Status `json:"status"`
	Named    bool           `json:"named"`
	Position *int           `json:"position"`
	// FanOut is the web searches the engine ran for the question, empty
	// where the endpoint does not expose them.
	FanOut []string `json:"fan_out"`
	// Links are the pages the answer linked to, in order, leaving out your
	// own site's.
	Links []string `json:"links" jsonschema:"the pages the answer linked to, in order, without your own site's"`
}

type Gaps struct {
	RunID int64 `json:"run_id" jsonschema:"the run ranked; 0 when no run has answers on the pairs in scope"`
	Gaps  []Gap `json:"gaps"`
}

// Gaps ranks the questions some pair in scope did not name you on, highest
// opportunity first, for one run, or the latest run with answers in scope
// when runID is 0. A question that names your brand is no gap, unless it
// is counted in scores anyway.
func (s *Service) Gaps(ctx context.Context, runID int64, sc Scope) (Gaps, error) {
	if err := sc.check(); err != nil {
		return Gaps{}, err
	}
	if runID == 0 {
		err := s.db.QueryRow(ctx, `SELECT coalesce(max(a.run_id), 0) FROM answer a JOIN surface_model m ON m.id = a.surface_model_id
			WHERE ($1::text = '' OR m.surface_key = $1) AND ($2::text = '' OR m.model = $2)`, sc.Surface, sc.Model).Scan(&runID)
		if err != nil || runID == 0 {
			return Gaps{Gaps: []Gap{}}, err
		}
	} else if _, err := s.Run(ctx, runID); err != nil {
		return Gaps{}, err
	}
	brands, err := s.brands(ctx)
	if err != nil {
		return Gaps{}, err
	}
	answers, err := s.scoredAnswers(ctx, `a.run_id = $1`, runID)
	if err != nil {
		return Gaps{}, err
	}
	answers = slices.DeleteFunc(answers, func(a scoredAnswer) bool { return !a.counted || !sc.holds(a.series.Pair) })
	byQuestion := map[panel.QuestionID][]scoredAnswer{}
	var order []panel.QuestionID
	for _, a := range answers {
		if _, ok := byQuestion[a.question]; !ok {
			order = append(order, a.question)
		}
		byQuestion[a.question] = append(byQuestion[a.question], a)
	}
	var gaps []panel.Gap
	for _, q := range order {
		var scored []panel.Scored
		for _, a := range byQuestion[q] {
			scored = append(scored, a.scored)
		}
		gaps = append(gaps, panel.NewGap(q, scored, brands, nil))
	}
	questions, err := s.questionRefs(ctx, order)
	if err != nil {
		return Gaps{}, err
	}
	links, err := s.answerLinks(ctx, answers)
	if err != nil {
		return Gaps{}, err
	}
	out := Gaps{RunID: runID, Gaps: []Gap{}}
	for _, g := range panel.RankGaps(gaps) {
		row := Gap{Question: questions[g.Question], Engines: g.Engines, Named: g.Named, Competitors: g.Competitors,
			Opportunity: g.Opportunity(), Demand: g.Demand, NamedOnMiss: namedInstead(byQuestion[g.Question], brands), Pairs: []GapPair{}}
		for _, a := range byQuestion[g.Question] {
			row.Pairs = append(row.Pairs, GapPair{Series: a.series, AnswerID: a.id, Status: a.scored.Status, Named: a.scored.Mentioned,
				Position: nullPosition(a.scored.Position), FanOut: a.fanOut, Links: append([]string{}, links[a.id]...)})
		}
		slices.SortStableFunc(row.Pairs, func(a, b GapPair) int { return cmp.Compare(a.Pair.ID, b.Pair.ID) })
		out.Gaps = append(out.Gaps, row)
	}
	return out, nil
}

// answerLinks is each answer's linked pages, in order, without your own
// site's.
func (s *Service) answerLinks(ctx context.Context, answers []scoredAnswer) (map[int64][]string, error) {
	ids := make([]int64, len(answers))
	for i, a := range answers {
		ids[i] = a.id
	}
	rows, err := s.db.Query(ctx, `SELECT answer_id, url FROM citation WHERE answer_id = ANY($1) AND NOT is_self ORDER BY answer_id, ord`, ids)
	if err != nil {
		return nil, err
	}
	out := map[int64][]string{}
	var id int64
	var url string
	_, err = pgx.ForEachRow(rows, []any{&id, &url}, func() error {
		out[id] = append(out[id], url)
		return nil
	})
	return out, err
}

func (s *Service) questionRefs(ctx context.Context, ids []panel.QuestionID) (map[panel.QuestionID]QuestionRef, error) {
	rows, err := s.db.Query(ctx, `SELECT id, group_name, text, counts_in_scores FROM question WHERE id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	refs, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (QuestionRef, error) {
		var q QuestionRef
		return q, r.Scan(&q.ID, &q.Group, &q.Text, &q.CountsInScores)
	})
	out := map[panel.QuestionID]QuestionRef{}
	for _, q := range refs {
		out[panel.QuestionID(q.ID)] = q
	}
	if err == nil && len(out) != len(ids) {
		err = fmt.Errorf("%w: a question of the run is missing", core.ErrNotFound)
	}
	return out, err
}
