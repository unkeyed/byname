package search

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/unkeyed/byname/internal/notify"
)

// Alerts stores and sends events (internal/notify). Emit is idempotent on
// the event's kind and subject.
type Alerts interface {
	Emit(ctx context.Context, e notify.Event) error
}

// A 28-day metric has moved when it differs from the 28 days before by at
// least moveFraction of the earlier value and by at least moveMinClicks,
// so a site with a handful of clicks does not alert on noise.
const (
	moveFraction  = 0.25
	moveMinClicks = 20
)

// moved says whether a metric moved from before to after.
func moved(before, after int64) bool {
	d := math.Abs(float64(after - before))
	return d >= moveMinClicks && d >= moveFraction*float64(before)
}

// movers are the 28-day metrics search_moved watches.
var movers = []Metric{MetricClicks, MetricNonBrandClicks}

// searchAlerts runs after each of p's syncs that succeeded. It raises
// goal_reached once per goal and target, when one of p's goals first
// meets its target, and search_moved once per provider, metric and window
// end day, when 28-day clicks or non-brand clicks moved against the 28
// days before. Both windows must be complete, so a short history never
// compares a part against a whole.
func (s *Service) searchAlerts(ctx context.Context, p Provider) error {
	if s.alerts == nil {
		return nil
	}
	goals, err := s.Goals(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, g := range goals {
		if g.Provider != p || g.Progress == nil || !g.Progress.Reached {
			continue
		}
		errs = append(errs, s.alerts.Emit(ctx, notify.Event{
			Subject: fmt.Sprintf("goal/%d/%d", g.ID, g.Target),
			Payload: notify.GoalReachedPayload{GoalID: g.ID, Metric: string(g.Metric), WindowDays: g.WindowDays,
				Target: float64(g.Target), Value: float64(g.Progress.Value), DueOn: g.DueOn},
		}))
	}
	sum, err := s.Summary(ctx, p)
	if err != nil {
		return errors.Join(append(errs, err)...)
	}
	cur, prev := sum.Current, sum.Previous
	if cur == nil || prev == nil || cur.DaysWithData < cur.Days || prev.DaysWithData < prev.Days {
		return errors.Join(errs...)
	}
	for _, m := range movers {
		before, after := metrics[m](*prev), metrics[m](*cur)
		if !moved(before, after) {
			continue
		}
		errs = append(errs, s.alerts.Emit(ctx, notify.Event{
			Subject: fmt.Sprintf("search/%s/%s/%s", p, m, cur.To),
			Payload: notify.SearchMovedPayload{Metric: string(m), WindowDays: cur.Days, Before: float64(before), After: float64(after)},
		}))
	}
	return errors.Join(errs...)
}
