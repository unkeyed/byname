// Package instance answers questions about this Byname instance as a
// whole: whether it is set up, and what it costs. REST and MCP both call
// it, so an agent sees what the UI sees.
package instance

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/unkeyed/byname/internal/config"
	"github.com/unkeyed/byname/internal/discovery"
	"github.com/unkeyed/byname/internal/social"
)

// Status is what GET /v1/status and the status tool return.
type Status struct {
	Version string `json:"version"`
	// Onboarded is whether someone has finished onboarding. Until then
	// nothing runs on schedule and Run now refuses.
	Onboarded bool `json:"onboarded"`
}

// Config is internal/config: onboarding, the settings and what checks
// cost.
type Config interface {
	Onboarded(context.Context) (bool, error)
	Settings(context.Context) (config.SettingsView, error)
	AnswerCosts(context.Context) ([]config.AnswerCost, error)
	CheckCosts(context.Context) (config.CheckCosts, error)
	FinishOnboarding(ctx context.Context, socialMonthUSD float64) (config.Onboarding, error)
}

// Social is internal/social's cost estimate.
type Social interface {
	CostEstimate(context.Context) (social.CostEstimate, error)
}

// OtherBrands is internal/discovery's settings, with what reading answers
// and looking up logos cost.
type OtherBrands interface {
	Settings(context.Context) (discovery.Settings, error)
}

type Service struct {
	version string
	db      *pgxpool.Pool
	config  Config
	social  Social
	brands  OtherBrands
}

func New(version string, db *pgxpool.Pool, cfg Config, soc Social, brands OtherBrands) *Service {
	return &Service{version: version, db: db, config: cfg, social: soc, brands: brands}
}

func (s *Service) Status(ctx context.Context) (Status, error) {
	done, err := s.config.Onboarded(ctx)
	return Status{Version: s.version, Onboarded: done}, err
}

// Costs is every cost fact the app explains spending with: what an answer
// costs on each surface, what one check costs, what a month of checks and
// of social searches is expected to cost, and what this month has spent
// against the monthly cap. Every figure is in US dollars, and every
// expected figure is an estimate.
type Costs struct {
	PerAnswer []config.AnswerCost `json:"per_answer" jsonschema:"one answer on each surface, on its current route, in onboarding order"`
	Checks    config.CheckCosts   `json:"checks" jsonschema:"each check's estimate includes reading its answers for other brands while that is on"`
	Social    social.CostEstimate `json:"social"`
	// OtherBrands is reading answers for other brands, which checks.check
	// already counts, and looking up logos, at most a set number of new
	// brands a month.
	OtherBrands discovery.Settings `json:"other_brands" jsonschema:"what reading answers for other brands and looking up logos cost, and whether each is on"`
	// MonthEstimateUSD is checks plus social searches in an average month.
	MonthEstimateUSD float64 `json:"month_estimate_usd" jsonschema:"checks.month_usd plus social.month_usd"`
	// DefaultMonthCapUSD is what finishing onboarding sets as the monthly
	// cap when none is set, from month_estimate_usd.
	DefaultMonthCapUSD float64   `json:"default_month_cap_usd" jsonschema:"the monthly spend cap finishing onboarding sets when none is set: three times month_estimate_usd, rounded up to a whole dollar, at least 5"`
	Month              MonthCost `json:"month"`
}

// MonthCost is what the calendar month so far has spent, in the
// instance's time zone, which the monthly spend cap counts.
type MonthCost struct {
	Start     time.Time `json:"start" jsonschema:"the first moment of the month in the instance's time zone"`
	ChecksUSD float64   `json:"checks_usd" jsonschema:"what this month's checks spent on answers"`
	SocialUSD float64   `json:"social_usd" jsonschema:"what this month's social searches spent"`
	BrandsUSD float64   `json:"brands_usd" jsonschema:"what this month spent finding other brands: reading answers, in checks or later, and looking up logos"`
	SpentUSD  float64   `json:"spent_usd" jsonschema:"checks_usd plus social_usd plus brands_usd, what the monthly cap counts"`
	// MissingCostCalls are answered calls that reported no cost, which no
	// figure here can count.
	MissingCostCalls int      `json:"missing_cost_calls" jsonschema:"answered calls this month that reported no cost, so spent_usd is a floor"`
	CapUSD           *float64 `json:"cap_usd" jsonschema:"the monthly spend cap, null for none"`
}

func (s *Service) Costs(ctx context.Context) (Costs, error) {
	var out Costs
	var err error
	if out.PerAnswer, err = s.config.AnswerCosts(ctx); err != nil {
		return out, err
	}
	if out.Checks, err = s.config.CheckCosts(ctx); err != nil {
		return out, err
	}
	if out.Social, err = s.social.CostEstimate(ctx); err != nil {
		return out, err
	}
	if out.OtherBrands, err = s.brands.Settings(ctx); err != nil {
		return out, err
	}
	out.MonthEstimateUSD = out.Checks.MonthUSD + out.Social.MonthUSD
	out.DefaultMonthCapUSD = config.DefaultMonthCap(out.MonthEstimateUSD)
	st, err := s.config.Settings(ctx)
	if err != nil {
		return out, err
	}
	loc := st.Schedule().Location
	local := time.Now().In(loc)
	out.Month = MonthCost{Start: time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, loc), CapUSD: st.SpendCapMonthUSD}
	err = s.db.QueryRow(ctx, `SELECT
			coalesce((SELECT sum(usd) FROM spend WHERE at >= $1 AND source = 'checks'), 0)::float8,
			coalesce((SELECT sum(usd) FROM spend WHERE at >= $1 AND source = 'social'), 0)::float8,
			coalesce((SELECT sum(usd) FROM spend WHERE at >= $1 AND source = 'brands'), 0)::float8,
			coalesce((SELECT sum(spend_missing_calls) FROM run WHERE started_at >= $1), 0)::int
			+ (SELECT count(*) FROM social_call WHERE called_at >= $1 AND cost_usd IS NULL AND status = 'ok')::int
			+ (SELECT count(*) FROM brand_call WHERE called_at >= $1 AND cost_usd IS NULL AND status = 'ok')::int`,
		out.Month.Start).Scan(&out.Month.ChecksUSD, &out.Month.SocialUSD, &out.Month.BrandsUSD, &out.Month.MissingCostCalls)
	out.Month.SpentUSD = out.Month.ChecksUSD + out.Month.SocialUSD + out.Month.BrandsUSD
	return out, err
}

// FinishOnboarding finishes onboarding, setting the default monthly cap
// from checks and social searches at their current settings when none is
// set.
func (s *Service) FinishOnboarding(ctx context.Context) (config.Onboarding, error) {
	est, err := s.social.CostEstimate(ctx)
	if err != nil {
		return config.Onboarding{}, err
	}
	return s.config.FinishOnboarding(ctx, est.MonthUSD)
}
