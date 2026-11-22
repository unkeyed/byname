// Package instance answers questions about this Byname instance as a
// whole: whether it is set up, and what it costs. REST and MCP both call
// it, so an agent sees what the UI sees.
package instance

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/unkeyed/byname/internal/config"
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

type Service struct {
	version string
	db      *pgxpool.Pool
	config  Config
	social  Social
}

func New(version string, db *pgxpool.Pool, cfg Config, soc Social) *Service {
	return &Service{version: version, db: db, config: cfg, social: soc}
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
	Checks    config.CheckCosts   `json:"checks"`
	Social    social.CostEstimate `json:"social"`
	// MonthEstimateUSD is checks plus social searches in an average month.
	MonthEstimateUSD float64   `json:"month_estimate_usd" jsonschema:"checks.month_usd plus social.month_usd"`
	Month            MonthCost `json:"month"`
}

// MonthCost is what the calendar month so far has spent, in the
// instance's time zone, which the monthly spend cap counts.
type MonthCost struct {
	Start     time.Time `json:"start" jsonschema:"the first moment of the month in the instance's time zone"`
	ChecksUSD float64   `json:"checks_usd" jsonschema:"what this month's checks spent"`
	SocialUSD float64   `json:"social_usd" jsonschema:"what this month's social searches spent"`
	SpentUSD  float64   `json:"spent_usd" jsonschema:"checks_usd plus social_usd, what the monthly cap counts"`
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
	out.MonthEstimateUSD = out.Checks.MonthUSD + out.Social.MonthUSD
	st, err := s.config.Settings(ctx)
	if err != nil {
		return out, err
	}
	loc := st.Schedule().Location
	local := time.Now().In(loc)
	out.Month = MonthCost{Start: time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, loc), CapUSD: st.SpendCapMonthUSD}
	err = s.db.QueryRow(ctx, `SELECT
			coalesce((SELECT sum(spend_usd) FROM run WHERE started_at >= $1), 0)::float8,
			coalesce((SELECT sum(cost_usd) FROM social_call WHERE called_at >= $1), 0)::float8,
			coalesce((SELECT sum(spend_missing_calls) FROM run WHERE started_at >= $1), 0)::int
			+ (SELECT count(*) FROM social_call WHERE called_at >= $1 AND cost_usd IS NULL AND status = 'ok')::int`,
		out.Month.Start).Scan(&out.Month.ChecksUSD, &out.Month.SocialUSD, &out.Month.MissingCostCalls)
	out.Month.SpentUSD = out.Month.ChecksUSD + out.Month.SocialUSD
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
