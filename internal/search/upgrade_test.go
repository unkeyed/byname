package search_test

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"testing"
	"time"

	"github.com/unkeyed/byname/internal/config"
	"github.com/unkeyed/byname/internal/db"
	"github.com/unkeyed/byname/internal/notify"
	"github.com/unkeyed/byname/internal/results"
	"github.com/unkeyed/byname/internal/search"
	"github.com/unkeyed/byname/internal/search/searchtest"
	"github.com/unkeyed/byname/internal/secretbox"
	"github.com/unkeyed/byname/internal/testdb"
)

// lastRelease is the last migration v0.2.0 shipped, when Search Console
// was Google's alone in internal/gsc.
const lastRelease = "016_social_filters.sql"

// An instance on v0.2.0 upgrades in place: its Search Console connection,
// numbers, goal, jobs, onboarding and alerts all carry over as Google's,
// and the first sync after the upgrade opens the stored credential and
// raises nothing already raised.
func TestUpgradeKeepsSearchConsoleAsGoogle(t *testing.T) {
	e := newEnvOn(t, testdb.Through(t, lastRelease))
	brand, nonBrand := "fernhollow pricing", "go api hosting"
	key := e.fake.ServiceAccountKey(t, saEmail)
	synced := e.now.Add(-2 * time.Hour)

	// The rows below are what v0.2.0's internal/gsc wrote. Days 3 to 58
	// are final, days 1 and 2 still partial. The 28 days ending on day 3
	// have twice the clicks of the 28 before, all of the gain branded.
	e.exec(`INSERT INTO gsc_connection (method, account, ciphertext, property, property_kind, backfill, connected_at, last_sync_at)
		VALUES ('service_account', $1, $2, $3, 'domain', true, $4, $5)`,
		saEmail, secretbox.ForTest().Seal(key, "gsc:service_account"), domainProperty, e.now.AddDate(0, -1, 0), synced)
	for n := 1; n <= 58; n++ {
		day, final := e.day(n), n >= 3
		clicks, brandClicks, impressions := int64(15), int64(2), int64(800)
		switch {
		case !final:
			clicks, brandClicks, impressions = 14, 1, 300
		case n <= 30:
			clicks, brandClicks, impressions = 30, 15, 1000
		}
		e.exec(`INSERT INTO gsc_site_daily (property, date, clicks, impressions, position, is_final) VALUES ($1, $2, $3, $4, 6, $5)`,
			domainProperty, day, clicks, impressions, final)
		for q, c := range map[string]int64{brand: brandClicks, nonBrand: 10} {
			e.exec(`INSERT INTO gsc_query_daily (property, date, query, clicks, impressions, position, is_final) VALUES ($1, $2, $3, $4, 200, 4, $5)`,
				domainProperty, day, q, c, final)
			e.exec(`INSERT INTO gsc_query_page_daily (property, date, query, page, clicks, impressions, position, is_final) VALUES ($1, $2, $3, $4, $5, 200, 4, $6)`,
				domainProperty, day, q, prefixProperty, c, final)
		}
		e.exec(`INSERT INTO gsc_page_daily (property, date, page, clicks, impressions, position, is_final) VALUES ($1, $2, $3, $4, 500, 5, $5)`,
			domainProperty, day, prefixProperty+"docs", clicks, final)
	}
	var goalID int64
	if err := e.db.QueryRow(e.ctx, `INSERT INTO goal (metric, window_days, target, due_on) VALUES ('clicks', 28, 500, '2027-03-01') RETURNING id`).Scan(&goalID); err != nil {
		t.Fatal(err)
	}
	e.exec(`INSERT INTO job_run (job, trigger, status, started_at, finished_at) VALUES ('gsc_sync', 'scheduled', 'completed', $1, $1)`, synced)
	e.exec(`UPDATE instance_settings SET onboarding_steps = '{brand,competitors,questions,surfaces,search_console}'`)
	e.exec(`INSERT INTO alert (kind, subject, payload) VALUES ('goal_reached', $1, $2)`, fmt.Sprintf("goal/%d/500", goalID),
		`{"goal_id": `+fmt.Sprint(goalID)+`, "metric": "clicks", "window_days": 28, "target": 500, "value": 840, "due_on": "2027-03-01"}`)
	e.exec(`INSERT INTO alert (kind, subject, payload) VALUES ('search_moved', $1, '{"metric": "clicks", "window_days": 28, "before": 420, "after": 840}')`,
		"search/clicks/"+e.day(3))

	if err := db.Migrate(e.ctx, e.db); err != nil {
		t.Fatalf("upgrade from v0.2.0: %v", err)
	}

	st := e.status()
	if !st.Connected || st.Method != search.MethodServiceAccount || st.Account != saEmail || st.Site != domainProperty || st.SiteKind != search.Domain ||
		!st.Backfill || st.LastSyncAt == nil || !st.LastSyncAt.Equal(synced) || st.LastSyncError != "" {
		t.Fatalf("Google after the upgrade = %+v, want the Search Console connection", st)
	}
	if want := (search.Data{DataSite: domainProperty, FirstDay: e.day(58), LatestFinal: e.day(3), PartialDays: []string{e.day(2), e.day(1)}}); !dataEqual(st.Data, want) {
		t.Fatalf("Google's stored data = %+v, want %+v", st.Data, want)
	}

	sum, err := e.svc.Summary(e.ctx, search.Google)
	if err != nil {
		t.Fatal(err)
	}
	cur, prev := sum.Current, sum.Previous
	if sum.Site != domainProperty || cur == nil || prev == nil ||
		cur.From != e.day(30) || cur.To != e.day(3) || cur.Clicks != 840 || cur.Impressions != 28000 || cur.Position != 6 ||
		cur.BrandClicks != 420 || cur.NonBrandClicks != 280 || cur.UnlistedClicks != 140 || cur.DaysWithData != 28 ||
		prev.Clicks != 420 || prev.Impressions != 22400 || prev.BrandClicks != 56 || prev.NonBrandClicks != 280 {
		t.Fatalf("Google summary after the upgrade = %+v, current %+v, previous %+v", sum, cur, prev)
	}
	queries, err := e.svc.TopQueries(e.ctx, search.Google, search.Top{})
	if err != nil {
		t.Fatal(err)
	}
	top := map[string]int64{}
	for _, q := range queries.Queries {
		top[q.Query] = q.Clicks
	}
	if !maps.Equal(top, map[string]int64{brand: 420, nonBrand: 280}) {
		t.Fatalf("Google's top queries after the upgrade = %v", top)
	}

	goals, err := e.svc.Goals(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(goals) != 1 || goals[0].ID != goalID || goals[0].Provider != search.Google || goals[0].Metric != search.MetricClicks ||
		goals[0].Progress == nil || goals[0].Progress.Value != 840 || !goals[0].Progress.Reached {
		t.Fatalf("goals after the upgrade = %+v, want the Search Console goal as Google's", goals)
	}

	ob, err := e.cfg.Onboarding(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if i := slices.IndexFunc(ob.Steps, func(s config.Step) bool { return s.Key == config.StepSearch }); i < 0 || !ob.Steps[i].Saved {
		t.Fatalf("onboarding after the upgrade = %+v, want the search engines step saved", ob.Steps)
	}

	alerts := notify.New(e.db, results.New(e.db), notify.Options{Secrets: secretbox.ForTest()})
	e.svc = e.service(alerts)
	e.fake.SetRows(domainProperty, site(e.day(2), 14, 300), site(e.day(1), 14, 300))
	e.fake.SetFirstIncomplete(e.day(2))
	e.sync()
	if st := e.status(); st.LastSyncError != "" || !st.LastSyncAt.Equal(e.now) {
		t.Fatalf("the first sync after the upgrade: %+v", st)
	}

	list, err := alerts.Alerts(e.ctx, admin, notify.AlertQuery{})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, a := range list.Alerts {
		got = append(got, string(a.Kind)+": "+a.Title+" -> "+a.Path)
	}
	want := []string{
		"search_moved: Google clicks rose over the last 28 days -> /search/traffic?provider=google",
		"goal_reached: Goal reached: Google clicks -> /search/traffic?provider=google",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("alerts after the first sync since the upgrade:\n%v\nwant the two raised before it, as Google's:\n%v", got, want)
	}

	jobs, err := results.New(e.db).Jobs(e.ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	var syncs []string
	for _, j := range jobs {
		syncs = append(syncs, j.Job+"/"+j.Provider+"/"+j.Status)
	}
	if !slices.Equal(syncs, []string{"search_sync/google/completed", "search_sync/google/completed"}) {
		t.Fatalf("jobs after the upgrade = %v, want the old sync and the new one as Google's", syncs)
	}
}

// A Google sign-in v0.2.0 started finishes on the upgraded instance: the
// state it sealed still opens, and Google's code still exchanges.
func TestSignInStartedBeforeUpgradeFinishes(t *testing.T) {
	e := newEnv(t)
	verifier := "v020-verifier-0123456789abcdefghijklmnopqrstuvwxyz"
	raw, err := json.Marshal(map[string]any{"u": admin.UserID, "v": verifier, "e": e.now.Add(10 * time.Minute).Unix(), "r": "onboarding"})
	if err != nil {
		t.Fatal(err)
	}
	state := secretbox.ForTest().Seal(string(raw), "gsc:oauth-state")
	challenge := sha256.Sum256([]byte(verifier))
	consent := e.fake.URL + "/o/oauth2/v2/auth?" + url.Values{
		"client_id": {"test-client"}, "redirect_uri": {"http://localhost:8080/auth/gsc/callback"}, "response_type": {"code"},
		"scope": {searchtest.Scope}, "access_type": {"offline"}, "state": {state},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(challenge[:])}, "code_challenge_method": {"S256"},
	}.Encode()
	back := followConsent(t, consent)

	if o := e.svc.GoogleOAuthOrigin(state); o != (search.OAuthOrigin{Return: search.ReturnOnboarding}) {
		t.Fatalf("the sign-in started at %+v, want onboarding in the first entity, where v0.2.0 started it", o)
	}
	if err := e.svc.FinishGoogleOAuth(e.ctx, admin, search.OAuthCallback{State: back.Query().Get("state"), Code: back.Query().Get("code")}); err != nil {
		t.Fatalf("finishing a sign-in v0.2.0 started: %v", err)
	}
	if st := e.status(); !st.Connected || st.Method != search.MethodOAuth {
		t.Fatalf("Google after the sign-in = %+v", st)
	}
}

func (e *env) exec(sql string, args ...any) {
	e.t.Helper()
	if _, err := e.db.Exec(e.ctx, sql, args...); err != nil {
		e.t.Fatal(err)
	}
}

func dataEqual(a, b search.Data) bool {
	return a.DataSite == b.DataSite && a.FirstDay == b.FirstDay && a.LatestFinal == b.LatestFinal && slices.Equal(a.PartialDays, b.PartialDays)
}
