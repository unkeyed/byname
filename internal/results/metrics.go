package results

import (
	"cmp"
	"context"
	"fmt"
	"maps"
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
	Points []RatePoint `json:"points"`
}

// Rates is the mention rate and citation rate per run for each series.
// since limits it to runs started then or later; nil is all time.
func (s *Service) Rates(ctx context.Context, since *time.Time) ([]RateSeries, error) {
	rows, err := s.db.Query(ctx, `SELECT m.id, m.surface_key, coalesce(m.model, ''), a.endpoint, a.fidelity,
			a.run_id, coalesce(r.started_at, r.queued_at), count(*), count(*) FILTER (WHERE a.status <> 'failed'),
			count(*) FILTER (WHERE a.mentioned), count(*) FILTER (WHERE a.cited)
		FROM answer a JOIN run r ON r.id = a.run_id JOIN surface_model m ON m.id = a.surface_model_id
		WHERE $1::timestamptz IS NULL OR coalesce(r.started_at, r.queued_at) >= $1
		GROUP BY m.id, a.endpoint, a.fidelity, a.run_id, r.id
		ORDER BY m.id, min(a.run_id) OVER (PARTITION BY m.id, a.endpoint, a.fidelity), a.endpoint, a.fidelity, a.run_id`, since)
	if err != nil {
		return nil, err
	}
	out := []RateSeries{}
	for rows.Next() {
		var sr Series
		var p RatePoint
		if err := rows.Scan(&sr.Pair.ID, &sr.Pair.Surface, &sr.Pair.Model, &sr.Endpoint, &sr.Fidelity,
			&p.RunID, &p.RunStartedAt, &p.Calls, &p.Answered, &p.Mentioned, &p.Cited); err != nil {
			return nil, err
		}
		if p.Answered > 0 {
			p.MentionRate = new(float64(p.Mentioned) / float64(p.Answered))
			p.CitationRate = new(float64(p.Cited) / float64(p.Answered))
		}
		if n := len(out); n == 0 || out[n-1].Series != sr {
			out = append(out, RateSeries{Series: sr, Points: []RatePoint{}})
		}
		out[len(out)-1].Points = append(out[len(out)-1].Points, p)
	}
	return out, rows.Err()
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
// run for each series. Technologies are left out.
func (s *Service) Share(ctx context.Context, since *time.Time) ([]ShareSeries, error) {
	rates, err := s.Rates(ctx, since)
	if err != nil {
		return nil, err
	}
	type key struct {
		series Series
		run    int64
	}
	brands := map[key][]BrandShare{}
	rows, err := s.db.Query(ctx, `SELECT m.id, m.surface_key, coalesce(m.model, ''), a.endpoint, a.fidelity, a.run_id,
			b.id, b.name, b.kind, count(*)
		FROM answer a JOIN run r ON r.id = a.run_id JOIN surface_model m ON m.id = a.surface_model_id
		JOIN brand_mention bm ON bm.answer_id = a.id JOIN brand b ON b.id = bm.brand_id AND b.kind <> 'technology'
		WHERE $1::timestamptz IS NULL OR coalesce(r.started_at, r.queued_at) >= $1
		GROUP BY m.id, a.endpoint, a.fidelity, a.run_id, b.id
		ORDER BY count(*) DESC, b.name`, since)
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

// Gap is a question where at least one pair did not name you on a run.
type Gap struct {
	Question QuestionRef `json:"question"`
	// Engines is how many pairs answered; Named how many named you.
	Engines     int `json:"engines"`
	Named       int `json:"named"`
	Competitors int `json:"competitors" jsonschema:"distinct competitors the answers named"`
	// Opportunity is 0 to 5: the share of pairs that did not name you
	// times 3, plus up to 2 for search demand, which no source provides yet.
	Opportunity int      `json:"opportunity"`
	Demand      *int     `json:"demand" jsonschema:"monthly searches; null until a demand source is configured"`
	NamedOnMiss []string `json:"named_on_miss" jsonschema:"competitors named by the answers that did not name you, most often first"`
	FanOut      []string `json:"fan_out" jsonschema:"the web searches the engines ran for the question, where exposed"`
}

type Gaps struct {
	RunID int64 `json:"run_id" jsonschema:"the run ranked; 0 when no run has answers yet"`
	Gaps  []Gap `json:"gaps"`
}

// Gaps ranks the questions some pair did not name you on, highest
// opportunity first, for one run, or the latest run with answers when
// runID is 0.
func (s *Service) Gaps(ctx context.Context, runID int64) (Gaps, error) {
	if runID == 0 {
		err := s.db.QueryRow(ctx, `SELECT coalesce(max(run_id), 0) FROM answer`).Scan(&runID)
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
	kinds := map[panel.BrandID]panel.Brand{}
	for _, b := range brands {
		kinds[b.ID] = b
	}
	answers, err := s.scoredAnswers(ctx, `a.run_id = $1`, runID)
	if err != nil {
		return Gaps{}, err
	}
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
	out := Gaps{RunID: runID, Gaps: []Gap{}}
	for _, g := range panel.RankGaps(gaps) {
		row := Gap{Question: questions[g.Question], Engines: g.Engines, Named: g.Named, Competitors: g.Competitors,
			Opportunity: g.Opportunity(), Demand: g.Demand, NamedOnMiss: []string{}, FanOut: []string{}}
		seen := map[panel.BrandID]int{}
		for _, a := range byQuestion[g.Question] {
			for _, f := range a.fanOut {
				if !slices.Contains(row.FanOut, f) {
					row.FanOut = append(row.FanOut, f)
				}
			}
			if a.scored.Mentioned || a.scored.Status == engines.StatusFailed {
				continue
			}
			for _, m := range a.scored.Mentions {
				if kinds[m.Brand].Kind == panel.KindCompetitor {
					seen[m.Brand]++
				}
			}
		}
		named := slices.Collect(maps.Keys(seen))
		slices.SortFunc(named, func(a, b panel.BrandID) int {
			return cmp.Or(cmp.Compare(seen[b], seen[a]), cmp.Compare(kinds[a].Name, kinds[b].Name))
		})
		for _, id := range named {
			row.NamedOnMiss = append(row.NamedOnMiss, kinds[id].Name)
		}
		out.Gaps = append(out.Gaps, row)
	}
	return out, nil
}

func (s *Service) questionRefs(ctx context.Context, ids []panel.QuestionID) (map[panel.QuestionID]QuestionRef, error) {
	rows, err := s.db.Query(ctx, `SELECT id, group_name, text FROM question WHERE id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	refs, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (QuestionRef, error) {
		var q QuestionRef
		return q, r.Scan(&q.ID, &q.Group, &q.Text)
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
