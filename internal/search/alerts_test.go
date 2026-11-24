package search_test

import (
	"testing"
	"time"

	"github.com/unkeyed/byname/internal/notify"
	"github.com/unkeyed/byname/internal/results"
	"github.com/unkeyed/byname/internal/search"
	"github.com/unkeyed/byname/internal/search/searchtest"
)

// withAlerts makes e's service raise events into a real notify service.
func (e *env) withAlerts() {
	e.svc = e.service(notify.New(e.db, results.New(e.db), notify.Options{Now: func() time.Time { return e.now }}))
}

// seedHistory gives the property 60 days ending yesterday: 10 clicks a day
// up to 31 days ago, then 20 a day, all on a non-brand query, with the
// last two days partial.
func (e *env) seedHistory() {
	for n := 1; n <= 60; n++ {
		clicks := 20
		if n > 30 {
			clicks = 10
		}
		e.fake.SetRows(prefixProperty, site(e.day(n), clicks, 1000))
	}
	e.fake.SetFirstIncomplete(e.day(2))
}

func TestSearchAlertsFireOncePerGoalAndWindow(t *testing.T) {
	e := newEnv(t)
	e.withAlerts()
	e.seedHistory()
	e.connectServiceAccount(prefixProperty)
	reached, err := e.svc.UpsertGoal(e.ctx, search.GoalInput{Provider: search.Google, Metric: search.MetricClicks, WindowDays: 28, Target: 500, DueOn: "2028-03-15"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.UpsertGoal(e.ctx, search.GoalInput{Provider: search.Google, Metric: search.MetricNonBrandClicks, WindowDays: 28, Target: 5000, DueOn: "2028-03-15"}); err != nil {
		t.Fatal(err)
	}

	e.sync()
	e.sync()
	if n := e.count(`SELECT count(*) FROM alert WHERE kind = 'goal_reached'`); n != 1 {
		t.Fatalf("%d goal_reached alerts after two syncs, want 1 for the clicks goal", n)
	}
	if n := e.count(`SELECT count(*) FROM alert WHERE kind = 'goal_reached' AND (payload->>'goal_id')::bigint = $1`, reached.ID); n != 1 {
		t.Fatal("goal_reached is not for the goal that met its target")
	}
	// Clicks and non-brand clicks both doubled, 280 to 560.
	if n := e.count(`SELECT count(*) FROM alert WHERE kind = 'search_moved' AND payload->>'before' = '280' AND payload->>'after' = '560'`); n != 2 {
		t.Fatalf("%d search_moved alerts after two syncs, want one each for clicks and non-brand clicks", n)
	}

	// A day later the windows end a day later: a new window alerts again,
	// once.
	e.now = e.now.Add(24 * time.Hour)
	e.seedHistory()
	e.sync()
	e.sync()
	if n := e.count(`SELECT count(*) FROM alert WHERE kind = 'search_moved'`); n != 4 {
		t.Fatalf("%d search_moved alerts after a second day's syncs, want 4", n)
	}
	if n := e.count(`SELECT count(*) FROM alert WHERE kind = 'goal_reached'`); n != 1 {
		t.Fatalf("%d goal_reached alerts, want the one", n)
	}
}

func TestSearchDoesNotAlertOnASmallMoveOrAShortHistory(t *testing.T) {
	e := newEnv(t)
	e.withAlerts()
	for n := 1; n <= 40; n++ {
		e.fake.SetRows(prefixProperty, site(e.day(n), 10, 1000))
	}
	e.fake.SetFirstIncomplete(e.day(2))
	e.connectServiceAccount(prefixProperty)
	e.sync()
	if n := e.count(`SELECT count(*) FROM alert`); n != 0 {
		t.Fatalf("%d alerts with 40 days of history and nothing moving, want 0", n)
	}
}

// TestSearchMovedIsPerProvider: Google and Bing windows that end on the
// same day each raise their own search_moved, once.
func TestSearchMovedIsPerProvider(t *testing.T) {
	e := newEnv(t)
	e.now = e.now.AddDate(0, 0, 7)
	e.withAlerts()
	const windowStart = "2026-10-24"
	var days []searchtest.BingDay
	for n := 63; n >= 1; n-- {
		clicks := 10
		if e.day(n) >= windowStart {
			clicks = 20
		}
		e.fake.SetRows(prefixProperty, site(e.day(n), clicks, 1000))
		days = append(days, searchtest.BingDay{Date: e.day(n), Clicks: clicks, Impressions: 1000})
	}
	e.fake.SetFirstIncomplete(e.day(7))
	e.bing.SetTraffic(bingSite, days...)
	var weeks []searchtest.BingWeek
	for n := -1; n <= 7; n++ {
		clicks := 100
		if friday(n) < windowStart {
			clicks = 50
		}
		weeks = append(weeks, searchtest.BingWeek{Date: friday(n), Key: "go api hosting", Clicks: clicks, Impressions: 900, Position: 4})
	}
	e.bing.SetQueries(bingSite, weeks...)
	e.connectServiceAccount(prefixProperty)
	e.connectBing()

	for range 2 {
		if err := e.svc.SyncIfDue(e.ctx); err != nil {
			t.Fatal(err)
		}
		e.now = e.now.Add(25 * time.Hour)
	}
	for _, p := range []search.Provider{search.Google, search.Bing} {
		sum, err := e.svc.Summary(e.ctx, p)
		if err != nil || sum.Current == nil || sum.Current.From != windowStart || sum.Current.To != "2026-11-20" {
			t.Fatalf("%s headline = %+v, %v; want both engines' windows to end on Nov 20", p, sum.Current, err)
		}
	}
	if n := e.count(`SELECT count(*) FROM alert WHERE kind = 'search_moved'`); n != 4 {
		t.Fatalf("%d search_moved alerts, want clicks and non-brand clicks once for each engine", n)
	}
}
