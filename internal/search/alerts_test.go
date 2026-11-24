package search_test

import (
	"net/url"
	"testing"
	"time"

	"github.com/unkeyed/byname/internal/notify"
	"github.com/unkeyed/byname/internal/results"
	"github.com/unkeyed/byname/internal/search"
	"github.com/unkeyed/byname/internal/secretbox"
)

// withAlerts makes e's service raise events into a real notify service.
func (e *env) withAlerts() {
	u, _ := url.Parse("http://localhost:8080")
	alerts := notify.New(e.db, results.New(e.db), notify.Options{Now: func() time.Time { return e.now }})
	e.svc = search.New(e.db, search.Options{
		Secrets: secretbox.ForTest(), BaseURLs: map[search.Provider]string{search.Google: e.fake.URL}, PublicURL: u,
		Terms: e.cfg, Alerts: alerts, Now: func() time.Time { return e.now },
	})
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
