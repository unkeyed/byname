package search

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/unkeyed/byname/internal/core"
)

// Metric is what a goal counts over its window.
type Metric string

const (
	MetricClicks         Metric = "clicks"
	MetricImpressions    Metric = "impressions"
	MetricNonBrandClicks Metric = "non_brand_clicks"
)

// metrics reads each metric from a period's numbers.
var metrics = map[Metric]func(Period) int64{
	MetricClicks:         func(p Period) int64 { return p.Clicks },
	MetricImpressions:    func(p Period) int64 { return p.Impressions },
	MetricNonBrandClicks: func(p Period) int64 { return p.NonBrandClicks },
}

// GoalWindows are the trailing windows a goal can count over, in days.
var GoalWindows = []int{28, 90}

// GoalInput is a goal: reach Target on Metric from one provider over a
// trailing WindowDays window by DueOn.
type GoalInput struct {
	Provider   Provider `json:"provider" jsonschema:"the search engine the goal counts, such as google or bing"`
	Metric     Metric   `json:"metric" jsonschema:"clicks, impressions or non_brand_clicks"`
	WindowDays int      `json:"window_days" jsonschema:"the trailing window, 28 or 90 days; whole weeks for Bing, so 90 counts 91"`
	Target     int64    `json:"target" jsonschema:"the number to reach over the window"`
	DueOn      string   `json:"due_on" jsonschema:"the date to reach it by, such as 2028-03-15"`
}

func (in GoalInput) validate() error {
	if _, err := lookup(in.Provider); err != nil {
		return err
	}
	if _, ok := metrics[in.Metric]; !ok {
		return fmt.Errorf("%w: metric must be clicks, impressions or non_brand_clicks, got %q", core.ErrInvalid, in.Metric)
	}
	if !slices.Contains(GoalWindows, in.WindowDays) {
		return fmt.Errorf("%w: window_days must be 28 or 90, got %d", core.ErrInvalid, in.WindowDays)
	}
	if in.Target < 1 || in.Target > 1e12 {
		return fmt.Errorf("%w: target must be a whole number from 1 to 1,000,000,000,000", core.ErrInvalid)
	}
	_, err := parseDay("due_on", in.DueOn)
	return err
}

// Goal is a stored goal with its progress.
type Goal struct {
	ID int64 `json:"id"`
	GoalInput
	Progress *Progress `json:"progress" jsonschema:"null until the provider's data is synced"`
}

// Progress is a goal's metric over the trailing window that ends on the
// provider's latest final day.
type Progress struct {
	From     string  `json:"from"`
	To       string  `json:"to"`
	Value    int64   `json:"value"`
	Fraction float64 `json:"fraction" jsonschema:"value over target; 1 or more once reached"`
	Reached  bool    `json:"reached"`
	// DaysWithData is less than the window while history is short.
	DaysWithData int `json:"days_with_data"`
}

// Goals lists every provider's goals by due date with their progress.
// None exist until someone adds one.
func (s *Service) Goals(ctx context.Context) ([]Goal, error) {
	rows, err := s.db.Query(ctx, `SELECT id, provider, metric, window_days, target, to_char(due_on, 'YYYY-MM-DD') FROM goal
		ORDER BY due_on, window_days, array_position(ARRAY['impressions', 'clicks', 'non_brand_clicks'], metric), provider, id`)
	if err != nil {
		return nil, err
	}
	goals, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Goal, error) {
		var g Goal
		return g, r.Scan(&g.ID, &g.Provider, &g.Metric, &g.WindowDays, &g.Target, &g.DueOn)
	})
	if err != nil {
		return nil, err
	}
	if err := s.progress(ctx, goals); err != nil {
		return nil, err
	}
	return goals, nil
}

// progress fills in each goal's progress, one period per provider and
// window length.
func (s *Service) progress(ctx context.Context, goals []Goal) error {
	type key struct {
		p    Provider
		days int
	}
	scopes := map[Provider]*scope{}
	periods := map[key]Period{}
	for i, g := range goals {
		sc, seen := scopes[g.Provider]
		if !seen {
			got, ok, err := s.scope(ctx, g.Provider)
			if err != nil {
				return err
			}
			if ok {
				sc = &got
			}
			scopes[g.Provider] = sc
		}
		if sc == nil {
			continue
		}
		k := key{g.Provider, g.WindowDays}
		p, done := periods[k]
		if !done {
			var err error
			if p, err = s.period(ctx, *sc, window(sc.anchor, g.WindowDays, sc.spec.StepDays)); err != nil {
				return err
			}
			periods[k] = p
		}
		v := metrics[g.Metric](p)
		goals[i].Progress = &Progress{From: p.From, To: p.To, Value: v, Fraction: float64(v) / float64(g.Target), Reached: v >= g.Target, DaysWithData: p.DaysWithData}
	}
	return nil
}

func (s *Service) goal(ctx context.Context, id int64) (Goal, error) {
	goals, err := s.Goals(ctx)
	if err != nil {
		return Goal{}, err
	}
	for _, g := range goals {
		if g.ID == id {
			return g, nil
		}
	}
	return Goal{}, fmt.Errorf("%w: no goal %d", core.ErrNotFound, id)
}

// UpsertGoal adds a goal, or sets the target of the goal with the same
// provider, metric, window and due date.
func (s *Service) UpsertGoal(ctx context.Context, in GoalInput) (Goal, error) {
	if err := in.validate(); err != nil {
		return Goal{}, err
	}
	var id int64
	err := s.db.QueryRow(ctx, `INSERT INTO goal (provider, metric, window_days, target, due_on) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (provider, metric, window_days, due_on) DO UPDATE SET target = excluded.target, updated_at = now()
		RETURNING id`, in.Provider, in.Metric, in.WindowDays, in.Target, in.DueOn).Scan(&id)
	if err != nil {
		return Goal{}, err
	}
	return s.goal(ctx, id)
}

// UpdateGoal replaces goal id.
func (s *Service) UpdateGoal(ctx context.Context, id int64, in GoalInput) (Goal, error) {
	if err := in.validate(); err != nil {
		return Goal{}, err
	}
	tag, err := s.db.Exec(ctx, `UPDATE goal SET provider = $2, metric = $3, window_days = $4, target = $5, due_on = $6, updated_at = $7 WHERE id = $1`,
		id, in.Provider, in.Metric, in.WindowDays, in.Target, in.DueOn, s.now())
	if pg := (*pgconn.PgError)(nil); errors.As(err, &pg) && pg.Code == "23505" {
		return Goal{}, fmt.Errorf("%w: a %s goal for %s over %d days due %s already exists", core.ErrConflict, in.Provider, in.Metric, in.WindowDays, in.DueOn)
	}
	if err != nil {
		return Goal{}, err
	}
	if tag.RowsAffected() == 0 {
		return Goal{}, fmt.Errorf("%w: no goal %d", core.ErrNotFound, id)
	}
	return s.goal(ctx, id)
}

// DeleteGoal deletes goal id.
func (s *Service) DeleteGoal(ctx context.Context, id int64) error {
	tag, err := s.db.Exec(ctx, `DELETE FROM goal WHERE id = $1`, id)
	if err == nil && tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: no goal %d", core.ErrNotFound, id)
	}
	return err
}
