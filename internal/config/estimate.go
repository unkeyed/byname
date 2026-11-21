package config

import (
	"context"
	"errors"

	"github.com/unkeyed/byname/internal/core"
	"github.com/unkeyed/byname/internal/engines"
)

// CostSource is where a pair's cost per call comes from.
type CostSource string

const (
	// CostFromHistory: the mean reported cost of the endpoint's past answers.
	CostFromHistory CostSource = "history"
	// CostFromSample: the endpoint's sampled cost per call from the registry.
	CostFromSample CostSource = "sample"
	// CostUnknown: no past answer reported a cost and no sample exists.
	CostUnknown CostSource = "unknown"
)

// Estimate is what one check is expected to cost: calls are questions
// times active pairs, and each pair's cost per call is a mean of past
// answers or a sample. Token-billed answers vary with every call, so this is
// only ever an estimate, and pairs with neither a history nor a sample are
// counted as unknown rather than guessed.
type Estimate struct {
	Questions    int           `json:"questions"`
	Pairs        int           `json:"pairs"`
	Calls        int           `json:"calls"`
	KnownUSD     float64       `json:"known_usd" jsonschema:"the sum over pairs with a cost per call"`
	UnknownPairs int           `json:"unknown_pairs"`
	Rows         []EstimateRow `json:"rows"`
}

type EstimateRow struct {
	PairID         int64            `json:"pair_id"`
	Surface        engines.Surface  `json:"surface"`
	Title          string           `json:"title"`
	Model          string           `json:"model"`
	Fidelity       engines.Fidelity `json:"fidelity"`
	Route          engines.Route    `json:"route"`
	Endpoint       string           `json:"endpoint"`
	Calls          int              `json:"calls"`
	CostPerCallUSD *float64         `json:"cost_per_call_usd"`
	Source         CostSource       `json:"source"`
	Samples        int              `json:"samples" jsonschema:"past answers averaged, for history"`
	CostUSD        *float64         `json:"cost_usd"`
}

func (s *Service) Estimate(ctx context.Context) (Estimate, error) {
	out := Estimate{Rows: []EstimateRow{}}
	set, err := s.CurrentQuestions(ctx)
	if err != nil && !errors.Is(err, core.ErrNotFound) {
		return out, err
	}
	out.Questions = len(set.Questions)
	pairs, err := s.ActivePairs(ctx)
	if err != nil {
		return out, err
	}
	states, err := s.surfaceStates(ctx, s.db)
	if err != nil {
		return out, err
	}
	type mean struct {
		usd float64
		n   int
	}
	history := map[string]mean{}
	rows, err := s.db.Query(ctx, `SELECT endpoint, avg(cost_usd)::float8, count(*) FROM answer WHERE cost_usd IS NOT NULL GROUP BY endpoint`)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var e string
		var m mean
		if err := rows.Scan(&e, &m.usd, &m.n); err != nil {
			return out, err
		}
		history[e] = m
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	for _, p := range pairs {
		st := states[p.Surface]
		rs, err := engines.RouteOf(p.Surface, st.route)
		if err != nil {
			continue
		}
		row := EstimateRow{
			PairID: p.ID, Surface: p.Surface, Title: rs.Spec.Title, Model: p.Model, Fidelity: p.Fidelity,
			Route: st.route, Endpoint: rs.EndpointID(st.baseURL), Calls: out.Questions, Source: CostUnknown,
		}
		if m, ok := history[row.Endpoint]; ok {
			row.CostPerCallUSD, row.Source, row.Samples = &m.usd, CostFromHistory, m.n
		} else if sample := rs.SampleCostUSD(); sample != nil {
			row.CostPerCallUSD, row.Source = sample, CostFromSample
		}
		if row.CostPerCallUSD != nil {
			cost := *row.CostPerCallUSD * float64(row.Calls)
			row.CostUSD = &cost
			out.KnownUSD += cost
		} else {
			out.UnknownPairs++
		}
		out.Rows = append(out.Rows, row)
	}
	out.Pairs = len(out.Rows)
	out.Calls = out.Questions * out.Pairs
	return out, nil
}
