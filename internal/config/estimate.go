package config

import (
	"context"
	"errors"
	"math"
	"slices"

	"github.com/jackc/pgx/v5"

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
	KnownUSD     float64       `json:"known_usd" jsonschema:"the sum over pairs with a cost per call, plus reading the answers for other brands when that is on"`
	UnknownPairs int           `json:"unknown_pairs"`
	Rows         []EstimateRow `json:"rows"`
	// FindBrands is reading each answer for the brands it names that you
	// don't track, which a check does right after storing it.
	FindBrands *FindEstimate `json:"find_brands" jsonschema:"reading each answer for other brands; null while it is off or no Treg token is stored"`
}

// FindEstimate is what reading a check's answers for other brands costs,
// counting every call as answered.
type FindEstimate struct {
	Answers          int     `json:"answers"`
	CostPerAnswerUSD float64 `json:"cost_per_answer_usd" jsonschema:"the mean of past reads, or the sampled price before any"`
	CostUSD          float64 `json:"cost_usd"`
}

// FindCostPerAnswer is what reading one answer for other brands costs: the
// mean of the reads that reported a cost, or the tool's sample price
// before any did.
func FindCostPerAnswer(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}) (float64, error) {
	var mean *float64
	err := q.QueryRow(ctx, `SELECT avg(cost_usd)::float8 FROM brand_call WHERE purpose = 'find' AND cost_usd IS NOT NULL`).Scan(&mean)
	if mean == nil {
		return engines.BrandFinder.SampleCostUSD, err
	}
	return *mean, err
}

// findEstimate prices reading calls answers, or is nil when reading is off
// or no Treg token is stored.
func (s *Service) findEstimate(ctx context.Context, calls int) (*FindEstimate, error) {
	var on bool
	if err := s.db.QueryRow(ctx, `SELECT find_brands AND EXISTS (SELECT 1 FROM credential WHERE kind = $1) FROM instance_settings`,
		engines.CredentialTreg).Scan(&on); err != nil || !on {
		return nil, err
	}
	per, err := FindCostPerAnswer(ctx, s.db)
	return &FindEstimate{Answers: calls, CostPerAnswerUSD: per, CostUSD: per * float64(calls)}, err
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

// callCost is one endpoint's cost per call and where it comes from.
type callCost struct {
	usd     *float64
	source  CostSource
	samples int
}

// callCosts is the mean reported cost of each endpoint's past answers.
type callCosts map[string]meanCost

type meanCost struct {
	usd float64
	n   int
}

func (s *Service) callCosts(ctx context.Context) (callCosts, error) {
	out := callCosts{}
	rows, err := s.db.Query(ctx, `SELECT endpoint, avg(cost_usd)::float8, count(*) FROM answer WHERE cost_usd IS NOT NULL GROUP BY endpoint`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var e string
		var m meanCost
		if err := rows.Scan(&e, &m.usd, &m.n); err != nil {
			return nil, err
		}
		out[e] = m
	}
	return out, rows.Err()
}

// of prices one call to endpoint on route rs: the mean of its past
// answers, or else the route's sampled cost, or unknown.
func (c callCosts) of(rs engines.RouteSpec, endpoint string) callCost {
	if m, ok := c[endpoint]; ok {
		return callCost{usd: &m.usd, source: CostFromHistory, samples: m.n}
	}
	if sample := rs.SampleCostUSD(); sample != nil {
		return callCost{usd: sample, source: CostFromSample}
	}
	return callCost{source: CostUnknown}
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
	costs, err := s.callCosts(ctx)
	if err != nil {
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
			Route: st.route, Endpoint: rs.EndpointID(st.baseURL), Calls: out.Questions,
		}
		c := costs.of(rs, row.Endpoint)
		row.CostPerCallUSD, row.Source, row.Samples = c.usd, c.source, c.samples
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
	if out.FindBrands, err = s.findEstimate(ctx, out.Calls); err != nil {
		return out, err
	}
	if out.FindBrands != nil {
		out.KnownUSD += out.FindBrands.CostUSD
	}
	return out, nil
}

// AnswerCost is what one answer costs on a surface on its current route,
// priced the way Estimate prices a pair. The price is the endpoint's, so
// every model on the surface costs the same per answer.
type AnswerCost struct {
	Surface          engines.Surface `json:"surface"`
	Title            string          `json:"title"`
	Group            SurfaceGroup    `json:"group"`
	Route            engines.Route   `json:"route"`
	Endpoint         string          `json:"endpoint"`
	Enabled          bool            `json:"enabled"`
	DefaultOn        bool            `json:"default_on" jsonschema:"ticked by default in onboarding"`
	Models           []string        `json:"models" jsonschema:"the models turned on, each charged this per answer; empty on an app surface"`
	CostPerAnswerUSD *float64        `json:"cost_per_answer_usd" jsonschema:"null when no past answer reported a cost and the route has no sampled price"`
	Source           CostSource      `json:"source" jsonschema:"history (the mean of past answers), sample (the registry's sampled price) or unknown"`
	Samples          int             `json:"samples" jsonschema:"past answers averaged, for history"`
}

// AnswerCosts prices one answer on every surface, on or off, in the
// order onboarding shows them.
func (s *Service) AnswerCosts(ctx context.Context) ([]AnswerCost, error) {
	surfaces, err := s.Surfaces(ctx)
	if err != nil {
		return nil, err
	}
	costs, err := s.callCosts(ctx)
	if err != nil {
		return nil, err
	}
	out := []AnswerCost{}
	for _, sf := range surfaces {
		rs, err := engines.RouteOf(sf.Key, sf.Route)
		if err != nil {
			continue
		}
		c := costs.of(rs, sf.Endpoint)
		row := AnswerCost{Surface: sf.Key, Title: sf.Title, Group: sf.Group, Route: sf.Route, Endpoint: sf.Endpoint, Enabled: sf.Enabled,
			DefaultOn: sf.DefaultOn, Models: []string{}, CostPerAnswerUSD: c.usd, Source: c.source, Samples: c.samples}
		for _, p := range sf.Pairs {
			if p.Enabled && p.Model != "" && !slices.Contains(row.Models, p.Model) {
				row.Models = append(row.Models, p.Model)
			}
		}
		out = append(out, row)
	}
	return out, nil
}

// daysPerMonth is the average calendar month.
const daysPerMonth = 365.25 / 12

// CheckCosts is what checks cost in a month at the current schedule.
type CheckCosts struct {
	Check          Estimate `json:"check" jsonschema:"the estimated cost of one check, as GET /v1/estimate gives it"`
	CheckEveryDays int      `json:"check_every_days"`
	Paused         bool     `json:"paused" jsonschema:"scheduled checks are paused, so none runs on its own"`
	ChecksPerMonth float64  `json:"checks_per_month" jsonschema:"scheduled checks in an average month; 0 while paused. Check now adds to it"`
	MonthUSD       float64  `json:"month_usd" jsonschema:"one check's known cost times checks_per_month"`
}

func (s *Service) CheckCosts(ctx context.Context) (CheckCosts, error) {
	est, err := s.Estimate(ctx)
	if err != nil {
		return CheckCosts{}, err
	}
	st, err := s.Settings(ctx)
	if err != nil {
		return CheckCosts{}, err
	}
	out := CheckCosts{Check: est, CheckEveryDays: st.CheckEveryDays, Paused: st.ChecksPaused}
	if !st.ChecksPaused {
		out.ChecksPerMonth = daysPerMonth / float64(st.CheckEveryDays)
	}
	out.MonthUSD = est.KnownUSD * out.ChecksPerMonth
	return out, nil
}

// DefaultMonthCap is the monthly spend cap onboarding sets when none is
// set: three times the monthly estimate, rounded up to a whole dollar, and
// at least $5, so a normal month never reaches it and a runaway one stops.
func DefaultMonthCap(monthUSD float64) float64 {
	cents := math.Round(3 * monthUSD * 100)
	return max(5, math.Ceil(cents/100))
}
