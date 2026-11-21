package gsc_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/unkeyed/byname/internal/config"
	"github.com/unkeyed/byname/internal/core"
	"github.com/unkeyed/byname/internal/gsc"
	"github.com/unkeyed/byname/internal/gsc/gsctest"
	"github.com/unkeyed/byname/internal/secretbox"
	"github.com/unkeyed/byname/internal/testdb"
)

const (
	domainProperty = "sc-domain:fernhollow.dev"
	prefixProperty = "https://www.fernhollow.dev/"
	saEmail        = "byname@fernhollow-search.iam.gserviceaccount.com"
)

var admin = core.Actor{Via: core.ViaSession, ID: "1", UserID: 1, Name: "admin@fernhollow.dev", Role: core.Admin, Scopes: core.Scopes}

type env struct {
	t    *testing.T
	ctx  context.Context
	db   *pgxpool.Pool
	svc  *gsc.Service
	cfg  *config.Service
	fake *gsctest.Server
	now  time.Time
}

func newEnv(t *testing.T) *env {
	t.Helper()
	pool := testdb.New(t)
	fake := gsctest.New(t)
	fake.AddSite(domainProperty, "siteOwner")
	fake.AddSite(prefixProperty, "siteFullUser")
	fake.AddSite("https://blog.otherproject.io/", "siteRestrictedUser")
	fake.AddSite("https://unverified.example/", "siteUnverifiedUser")
	cfg := config.New(pool, config.Options{Secrets: secretbox.ForTest()})
	e := &env{t: t, ctx: context.Background(), db: pool, cfg: cfg, fake: fake,
		now: time.Date(2026, 11, 21, 9, 0, 0, 0, time.UTC)}
	_, err := cfg.PutSettings(e.ctx, config.Settings{
		Brand:  config.SelfBrand{Name: "Fernhollow", Aliases: []string{"Fern Hollow"}, Domains: []string{"fernhollow.dev"}},
		Market: "US", Language: "en", TimeZone: "UTC", CheckEveryDays: 3, CheckTime: "06:00",
		SpendCapRunUSD: 5, Concurrency: 8, Retries: 1, TimeoutS: 180,
	})
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse("http://localhost:8080")
	e.svc = gsc.New(pool, gsc.Options{
		Secrets: secretbox.ForTest(), Endpoints: gsc.GoogleEndpoints(fake.URL),
		Client: gsc.OAuthClient{ID: "test-client", Secret: "test-secret"}, PublicURL: u,
		Terms: cfg, Now: func() time.Time { return e.now },
	})
	return e
}

// day is today (Nov 21 in Pacific Time at e.now) minus n days.
func (e *env) day(n int) string { return e.now.AddDate(0, 0, -n).Format(time.DateOnly) }

func (e *env) connectServiceAccount(property string) {
	e.t.Helper()
	if _, err := e.svc.ConnectServiceAccount(e.ctx, e.fake.ServiceAccountKey(e.t, saEmail)); err != nil {
		e.t.Fatal(err)
	}
	if _, err := e.svc.SetProperty(e.ctx, gsc.PropertyInput{Property: property}); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) sync() {
	e.t.Helper()
	if err := e.svc.Sync(e.ctx); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) status() gsc.Status {
	e.t.Helper()
	st, err := e.svc.Status(e.ctx)
	if err != nil {
		e.t.Fatal(err)
	}
	return st
}

// site is one day of site numbers spread over one page and one query.
func site(date string, clicks, impressions int) gsctest.Row {
	return gsctest.Row{Date: date, Query: "rate limiting api", Page: prefixProperty + "docs", Clicks: clicks, Impressions: impressions, Position: 7.5}
}

func (e *env) count(sql string, args ...any) int64 {
	e.t.Helper()
	var n int64
	if err := e.db.QueryRow(e.ctx, sql, args...).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

// followConsent opens Google's consent URL and returns where the fake sends
// the browser back to, without following it.
func followConsent(t *testing.T, consent string) *url.URL {
	t.Helper()
	c := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := c.Get(consent)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	back, err := url.Parse(res.Header.Get("Location"))
	if res.StatusCode != http.StatusFound || err != nil {
		t.Fatalf("consent page answered %d to %q", res.StatusCode, res.Header.Get("Location"))
	}
	return back
}

func TestOAuthStoresTheRefreshTokenSealed(t *testing.T) {
	e := newEnv(t)
	consent, err := e.svc.StartOAuth(admin, gsc.ReturnSettings)
	if err != nil {
		t.Fatal(err)
	}
	q := must(url.Parse(consent)).Query()
	for k, want := range map[string]string{"scope": gsc.Scope, "access_type": "offline", "prompt": "consent", "code_challenge_method": "S256",
		"redirect_uri": "http://localhost:8080/auth/gsc/callback", "client_id": "test-client"} {
		if q.Get(k) != want {
			t.Errorf("consent URL %s = %q, want %q", k, q.Get(k), want)
		}
	}
	back := followConsent(t, consent)
	if back.Path != "/auth/gsc/callback" {
		t.Fatalf("Google sent the browser to %s", back)
	}
	cb := gsc.OAuthCallback{State: back.Query().Get("state"), Code: back.Query().Get("code")}

	other := admin
	other.UserID = 2
	if err := e.svc.FinishOAuth(e.ctx, other, cb); !errors.Is(err, core.ErrInvalid) {
		t.Fatalf("another admin finishing the sign-in: %v, want ErrInvalid", err)
	}
	if err := e.svc.FinishOAuth(e.ctx, admin, cb); err != nil {
		t.Fatal(err)
	}
	var sealed string
	if err := e.db.QueryRow(e.ctx, `SELECT ciphertext FROM gsc_connection WHERE method = 'oauth'`).Scan(&sealed); err != nil {
		t.Fatal(err)
	}
	refresh, err := secretbox.ForTest().Open(sealed, "gsc:oauth")
	if err != nil || !strings.HasPrefix(refresh, "1//") {
		t.Fatalf("stored credential opens to %q, %v; want the refresh token Google issued", refresh, err)
	}
	if strings.Contains(sealed, refresh) {
		t.Fatal("the refresh token is stored in the clear")
	}
	st := e.status()
	if !st.Connected || st.Method != gsc.MethodOAuth || st.Property != "" {
		t.Fatalf("status after sign-in = %+v", st)
	}
	if strings.Contains(fmt.Sprintf("%+v", st), refresh) {
		t.Fatal("the status carries the refresh token")
	}

	props, err := e.svc.Properties(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	var urls []string
	var suggested string
	for _, p := range props {
		urls = append(urls, p.URL)
		if p.Suggested {
			suggested = p.URL
		}
	}
	if !slices.Equal(urls, []string{domainProperty, prefixProperty, "https://blog.otherproject.io/"}) || suggested != domainProperty {
		t.Fatalf("properties %v suggested %q; want the readable ones with the domain property suggested", urls, suggested)
	}
	e.fake.SetRows(prefixProperty, site(e.day(5), 3, 30))
	if _, err := e.svc.SetProperty(e.ctx, gsc.PropertyInput{Property: prefixProperty}); err != nil {
		t.Fatal(err)
	}
	e.sync()
	if st := e.status(); st.PropertyKind != gsc.URLPrefix || st.LatestFinal != e.day(5) {
		t.Fatalf("after syncing the URL-prefix property: %+v", st)
	}
	var refreshed bool
	for _, form := range e.fake.TokenCalls() {
		refreshed = refreshed || (form.Get("grant_type") == "refresh_token" && form.Get("refresh_token") == refresh)
	}
	if !refreshed {
		t.Fatal("the sync did not get its access token with the stored refresh token")
	}
}

func TestOAuthRefusals(t *testing.T) {
	e := newEnv(t)
	e.fake.DenyConsent = true
	back := followConsent(t, must(e.svc.StartOAuth(admin, gsc.ReturnSettings)))
	err := e.svc.FinishOAuth(e.ctx, admin, gsc.OAuthCallback{State: back.Query().Get("state"), Error: back.Query().Get("error")})
	if !errors.Is(err, core.ErrInvalid) || !strings.Contains(err.Error(), "access_denied") {
		t.Fatalf("denied consent: %v", err)
	}
	e.fake.DenyConsent = false
	e.fake.OmitRefreshToken = true
	back = followConsent(t, must(e.svc.StartOAuth(admin, gsc.ReturnSettings)))
	if err := e.svc.FinishOAuth(e.ctx, admin, gsc.OAuthCallback{State: back.Query().Get("state"), Code: back.Query().Get("code")}); err == nil {
		t.Fatal("a sign-in without a refresh token was accepted")
	}
	if e.status().Connected {
		t.Fatal("a refused sign-in left a connection")
	}
	forged := gsc.OAuthCallback{State: "sb1:forged", Code: "4/abc"}
	if err := e.svc.FinishOAuth(e.ctx, admin, forged); !errors.Is(err, core.ErrInvalid) {
		t.Fatalf("forged state: %v", err)
	}
	noClient := gsc.New(e.db, gsc.Options{Secrets: secretbox.ForTest(), Terms: e.cfg})
	if _, err := noClient.StartOAuth(admin, gsc.ReturnSettings); !errors.Is(err, core.ErrInvalid) || !strings.Contains(err.Error(), "GSC_GOOGLE_CLIENT_ID") {
		t.Fatalf("Sign in with Google without a client: %v", err)
	}
}

func TestServiceAccount(t *testing.T) {
	e := newEnv(t)
	if _, err := e.svc.ConnectServiceAccount(e.ctx, `{"type":"authorized_user"}`); !errors.Is(err, core.ErrInvalid) {
		t.Fatalf("not a service account key: %v", err)
	}
	stranger := gsctest.New(t).ServiceAccountKey(t, "stranger@elsewhere.iam.gserviceaccount.com")
	if _, err := e.svc.ConnectServiceAccount(e.ctx, stranger); !errors.Is(err, core.ErrUpstream) {
		t.Fatalf("a key Google does not know: %v, want ErrUpstream", err)
	}
	st, err := e.svc.ConnectServiceAccount(e.ctx, e.fake.ServiceAccountKey(t, saEmail))
	if err != nil {
		t.Fatal(err)
	}
	if st.Method != gsc.MethodServiceAccount || st.Account != saEmail {
		t.Fatalf("status = %+v", st)
	}
	if _, err := e.svc.SetProperty(e.ctx, gsc.PropertyInput{Property: "https://unverified.example/"}); !errors.Is(err, core.ErrInvalid) {
		t.Fatalf("a property the account cannot read: %v", err)
	}
	if _, err := e.svc.SetProperty(e.ctx, gsc.PropertyInput{Property: "fernhollow.dev"}); !errors.Is(err, core.ErrInvalid) {
		t.Fatalf("a bare domain: %v", err)
	}
	st, err = e.svc.SetProperty(e.ctx, gsc.PropertyInput{Property: domainProperty})
	if err != nil || st.PropertyKind != gsc.Domain || !st.Backfill {
		t.Fatalf("pick the domain property: %+v, %v", st, err)
	}
	var grants []string
	for _, form := range e.fake.TokenCalls() {
		grants = append(grants, form.Get("grant_type"))
	}
	if !slices.Contains(grants, "urn:ietf:params:oauth:grant-type:jwt-bearer") {
		t.Fatalf("token grants %v, want the JWT bearer grant", grants)
	}
	if err := e.svc.Disconnect(e.ctx); err != nil || e.status().Connected {
		t.Fatalf("disconnect: %v", err)
	}
}

func TestFinalAndPartialDays(t *testing.T) {
	e := newEnv(t)
	for n := 6; n >= 1; n-- {
		e.fake.SetRows(domainProperty, site(e.day(n), 10, 100))
	}
	e.fake.SetRows(domainProperty, site(e.day(2), 4, 40), gsctest.Row{Date: e.day(2), Query: "fernhollow outage", Page: prefixProperty, Clicks: 2, Impressions: 5, Position: 1})
	e.fake.SetFirstIncomplete(e.day(2))
	e.connectServiceAccount(domainProperty)
	e.sync()

	st := e.status()
	if st.LatestFinal != e.day(3) || !slices.Equal(st.PartialDays, []string{e.day(2), e.day(1)}) || st.LastSyncAt == nil {
		t.Fatalf("after the first sync: latest final %s, partial %v; want %s and the last two days", st.LatestFinal, st.PartialDays, e.day(3))
	}
	if n := e.count(`SELECT count(*) FROM gsc_query_daily WHERE query = 'fernhollow outage'`); n != 1 {
		t.Fatalf("partial query rows = %d", n)
	}

	// A day later Google has finalized day 2 with other numbers and dropped
	// a query it had counted.
	e.fake.SetRows(domainProperty, site(e.day(2), 9, 90))
	e.fake.SetRows(domainProperty, site(e.day(1), 11, 110))
	e.fake.SetFirstIncomplete(e.day(1))
	e.now = e.now.Add(25 * time.Hour)
	if err := e.svc.SyncIfDue(e.ctx); err != nil {
		t.Fatal(err)
	}
	var clicks int64
	var final bool
	if err := e.db.QueryRow(e.ctx, `SELECT clicks, is_final FROM gsc_site_daily WHERE date = $1`, e.day(3)).Scan(&clicks, &final); err != nil {
		t.Fatal(err)
	}
	if clicks != 9 || !final {
		t.Fatalf("the once partial day: %d clicks final %v, want 9 and final", clicks, final)
	}
	if n := e.count(`SELECT count(*) FROM gsc_query_daily WHERE query = 'fernhollow outage'`); n != 0 {
		t.Fatal("a partial day's row that Google dropped survived the next sync")
	}
	if st := e.status(); st.LatestFinal != e.day(3) || !slices.Equal(st.PartialDays, []string{e.day(2)}) {
		t.Fatalf("after the second sync: latest final %s partial %v", st.LatestFinal, st.PartialDays)
	}
	if n := e.count(`SELECT count(*) FROM job_run WHERE job = 'gsc_sync' AND status = 'completed'`); n != 2 {
		t.Fatalf("completed gsc_sync jobs = %d, want 2", n)
	}
}

func TestResyncIsIdempotent(t *testing.T) {
	e := newEnv(t)
	for n := 40; n >= 1; n-- {
		e.fake.SetRows(domainProperty, site(e.day(n), n, 10*n),
			gsctest.Row{Date: e.day(n), Query: "fernhollow docs", Page: prefixProperty + "docs", Clicks: 2, Impressions: 9, Position: 1.2},
			gsctest.Row{Date: e.day(n), Page: prefixProperty, Clicks: 1, Impressions: 3, Position: 4})
	}
	e.fake.SetFirstIncomplete(e.day(2))
	e.connectServiceAccount(domainProperty)
	snapshot := func() string {
		var out string
		err := e.db.QueryRow(e.ctx, `SELECT string_agg(t, '|' ORDER BY t) FROM (
			SELECT concat_ws(',', 's', date, clicks, impressions, position, is_final) t FROM gsc_site_daily
			UNION ALL SELECT concat_ws(',', 'q', date, query, clicks, impressions, position, is_final) FROM gsc_query_daily
			UNION ALL SELECT concat_ws(',', 'p', date, page, clicks, impressions, position, is_final) FROM gsc_page_daily
			UNION ALL SELECT concat_ws(',', 'qp', date, query, page, clicks, impressions, position, is_final) FROM gsc_query_page_daily) x`).Scan(&out)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	e.sync()
	first := snapshot()
	e.sync()
	e.sync()
	if again := snapshot(); again != first || first == "" {
		t.Fatalf("a re-sync changed the stored rows:\n%s\n%s", first, again)
	}
}

func TestPagingPast25000Rows(t *testing.T) {
	e := newEnv(t)
	const n = 30001
	rows := make([]gsctest.Row, n)
	for i := range rows {
		rows[i] = gsctest.Row{Date: e.day(3), Query: fmt.Sprintf("query %d", i), Page: fmt.Sprintf("%spage/%d", prefixProperty, i%40), Clicks: i % 3, Impressions: 1 + i%7, Position: 9}
	}
	e.fake.SetRows(domainProperty, rows...)
	e.connectServiceAccount(domainProperty)
	e.sync()
	if got := e.count(`SELECT count(*) FROM gsc_query_page_daily`); got != n {
		t.Fatalf("query and page rows stored = %d, want %d", got, n)
	}
	if got := e.count(`SELECT count(*) FROM gsc_query_daily`); got != n {
		t.Fatalf("query rows stored = %d, want %d", got, n)
	}
	var starts []int
	for _, q := range e.fake.Queries() {
		if slices.Equal(q.Dimensions, []string{"date", "query", "page"}) && q.StartDate <= e.day(3) && q.EndDate >= e.day(3) {
			starts = append(starts, q.StartRow)
			if q.RowLimit != 25000 || q.DataState != "all" {
				t.Errorf("request asked rowLimit %d dataState %q, want 25000 and all", q.RowLimit, q.DataState)
			}
		}
	}
	if !slices.Equal(starts, []int{0, 25000}) {
		t.Fatalf("startRow sequence %v, want [0 25000]", starts)
	}
}

func TestBrandAndNonBrand(t *testing.T) {
	e := newEnv(t)
	for n := 3; n <= 30; n++ {
		e.fake.SetRows(domainProperty,
			gsctest.Row{Date: e.day(n), Query: "fernhollow pricing", Page: prefixProperty + "pricing", Clicks: 10, Impressions: 100, Position: 1},
			gsctest.Row{Date: e.day(n), Query: "Fern Hollow login", Page: prefixProperty, Clicks: 5, Impressions: 20, Position: 1},
			gsctest.Row{Date: e.day(n), Query: "rate limiting api", Page: prefixProperty + "docs", Clicks: 7, Impressions: 300, Position: 6},
			gsctest.Row{Date: e.day(n), Query: "", Page: prefixProperty + "docs", Clicks: 4, Impressions: 80, Position: 9},
		)
	}
	e.connectServiceAccount(domainProperty)
	e.sync()
	sum, err := e.svc.Summary(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	c := sum.Current
	if c == nil || c.From != e.day(30) || c.To != e.day(3) {
		t.Fatalf("headline window = %+v", c)
	}
	if c.Clicks != 26*28 || c.BrandClicks != 15*28 || c.NonBrandClicks != 7*28 || c.AnonymizedClicks != 4*28 {
		t.Fatalf("headline clicks %d brand %d non-brand %d anonymized %d; want per day 26, 15, 7 and 4 over 28 days",
			c.Clicks, c.BrandClicks, c.NonBrandClicks, c.AnonymizedClicks)
	}
	top, err := e.svc.TopQueries(e.ctx, gsc.Top{NonBrand: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(top.Queries) != 1 || top.Queries[0].Query != "rate limiting api" || top.Queries[0].Branded {
		t.Fatalf("top non-brand queries = %+v", top.Queries)
	}
	all, err := e.svc.TopQueries(e.ctx, gsc.Top{})
	if err != nil || len(all.Queries) != 3 || all.Queries[0].Query != "fernhollow pricing" || !all.Queries[0].Branded {
		t.Fatalf("top queries = %+v, %v", all.Queries, err)
	}
	split, err := e.svc.Brand(e.ctx, gsc.Range{})
	if err != nil {
		t.Fatal(err)
	}
	if len(split.Points) != 1 || split.Points[0] != (gsc.BrandPoint{Date: e.day(3), Clicks: 26 * 28, BrandClicks: 15 * 28, NonBrandClicks: 7 * 28, AnonymizedClicks: 4 * 28}) {
		t.Fatalf("brand split = %+v, want one full 28-day window", split.Points)
	}
	if !slices.Equal(split.Terms, []string{"fernhollow", "fern hollow"}) {
		t.Fatalf("default branded terms = %v", split.Terms)
	}

	if _, err := e.cfg.SetBrandedTerms(e.ctx, []string{"Rate Limiting"}); err != nil {
		t.Fatal(err)
	}
	sum, _ = e.svc.Summary(e.ctx)
	if sum.Current.BrandClicks != 7*28 || sum.Current.NonBrandClicks != 15*28 || sum.Current.AnonymizedClicks != 4*28 {
		t.Fatalf("with the terms changed: brand %d non-brand %d anonymized %d", sum.Current.BrandClicks, sum.Current.NonBrandClicks, sum.Current.AnonymizedClicks)
	}
	v, err := e.cfg.SetBrandedTerms(e.ctx, nil)
	if err != nil || !v.BrandedTermsDefault || !slices.Equal(v.BrandedTerms, []string{"fernhollow", "fern hollow"}) {
		t.Fatalf("back to the default terms: %v %v, %v", v.BrandedTerms, v.BrandedTermsDefault, err)
	}

	pages, err := e.svc.TopPages(e.ctx, gsc.Top{Limit: 2})
	if err != nil || len(pages.Pages) != 2 || pages.Pages[0].Page != prefixProperty+"docs" || pages.Pages[0].Clicks != 11*28 {
		t.Fatalf("top pages = %+v, %v", pages.Pages, err)
	}
	day, err := e.svc.PageDaily(e.ctx, prefixProperty+"pricing", e.now.AddDate(0, 0, -5), e.now.AddDate(0, 0, -3))
	if err != nil || len(day) != 3 || day[0].Date != e.day(5) || day[0].Clicks != 10 || day[0].CTR != 0.1 {
		t.Fatalf("page daily = %+v, %v", day, err)
	}
}

func TestGoals(t *testing.T) {
	e := newEnv(t)
	goals, err := e.svc.Goals(e.ctx)
	if err != nil || len(goals) != 0 {
		t.Fatalf("goals on a new instance = %v, %v; want none", goals, err)
	}
	if _, err := e.svc.UpsertGoal(e.ctx, gsc.GoalInput{Metric: "sessions", WindowDays: 28, Target: 1, DueOn: "2027-01-31"}); !errors.Is(err, core.ErrInvalid) {
		t.Fatalf("unknown metric: %v", err)
	}
	if _, err := e.svc.UpsertGoal(e.ctx, gsc.GoalInput{Metric: gsc.MetricClicks, WindowDays: 30, Target: 1, DueOn: "2027-01-31"}); !errors.Is(err, core.ErrInvalid) {
		t.Fatalf("a 30-day window: %v", err)
	}
	g, err := e.svc.UpsertGoal(e.ctx, gsc.GoalInput{Metric: gsc.MetricClicks, WindowDays: 28, Target: 1000, DueOn: "2027-01-31"})
	if err != nil || g.Progress != nil {
		t.Fatalf("a goal before any data: %+v, %v", g, err)
	}

	// 100 days of 10 clicks and 100 impressions, the last two partial with
	// far more, which no goal may count.
	for n := 1; n <= 100; n++ {
		r := site(e.day(n), 10, 100)
		if n <= 2 {
			r.Clicks, r.Impressions = 5000, 50000
		}
		e.fake.SetRows(domainProperty, r)
	}
	e.fake.SetFirstIncomplete(e.day(2))
	e.connectServiceAccount(domainProperty)
	e.sync()

	same, err := e.svc.UpsertGoal(e.ctx, gsc.GoalInput{Metric: gsc.MetricClicks, WindowDays: 28, Target: 200, DueOn: "2027-01-31"})
	if err != nil || same.ID != g.ID || same.Target != 200 {
		t.Fatalf("the same goal again: %+v, %v; want goal %d with the new target", same, err, g.ID)
	}
	p := same.Progress
	if p == nil || p.Value != 280 || !p.Reached || p.To != e.day(3) || p.From != e.day(30) || p.DaysWithData != 28 || p.Fraction != 1.4 {
		t.Fatalf("28-day clicks progress = %+v", p)
	}
	imp, err := e.svc.UpsertGoal(e.ctx, gsc.GoalInput{Metric: gsc.MetricImpressions, WindowDays: 90, Target: 10000, DueOn: "2027-01-31"})
	if err != nil || imp.Progress.Value != 9000 || imp.Progress.Reached || imp.Progress.From != e.day(92) {
		t.Fatalf("90-day impressions progress = %+v, %v", imp.Progress, err)
	}
	nb, err := e.svc.UpsertGoal(e.ctx, gsc.GoalInput{Metric: gsc.MetricNonBrandClicks, WindowDays: 28, Target: 180, DueOn: "2027-01-31"})
	if err != nil || nb.Progress.Value != 280 {
		t.Fatalf("non-brand clicks progress = %+v, %v", nb.Progress, err)
	}
	if _, err := e.svc.UpdateGoal(e.ctx, imp.ID, gsc.GoalInput{Metric: gsc.MetricClicks, WindowDays: 28, Target: 5, DueOn: "2027-01-31"}); !errors.Is(err, core.ErrConflict) {
		t.Fatalf("updating a goal onto another's key: %v", err)
	}
	if err := e.svc.DeleteGoal(e.ctx, imp.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.DeleteGoal(e.ctx, imp.ID); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("deleting it twice: %v", err)
	}
	if goals, _ := e.svc.Goals(e.ctx); len(goals) != 2 {
		t.Fatalf("goals left = %d", len(goals))
	}
}

func TestSyncSchedule(t *testing.T) {
	e := newEnv(t)
	e.fake.SetRows(domainProperty, site(e.day(3), 1, 10))
	if err := e.svc.SyncIfDue(e.ctx); err != nil {
		t.Fatal(err)
	}
	if n := e.count(`SELECT count(*) FROM job_run`); n != 0 {
		t.Fatal("a sync ran with nothing connected")
	}
	if _, err := e.db.Exec(e.ctx, `INSERT INTO job_run (job, trigger, started_at) VALUES ('gsc_sync', 'scheduled', $1)`, e.now.Add(-3*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if !e.status().Syncing {
		t.Fatal("a running sync does not show as syncing")
	}
	if err := e.svc.SyncIfDue(e.ctx); err != nil {
		t.Fatal(err)
	}
	if e.status().Syncing || e.count(`SELECT count(*) FROM job_run WHERE status = 'failed'`) != 1 {
		t.Fatal("a sync a crash cut off still shows as running")
	}
	if _, err := e.db.Exec(e.ctx, `DELETE FROM job_run`); err != nil {
		t.Fatal(err)
	}
	e.connectServiceAccount(domainProperty)
	syncs := func() int64 { return e.count(`SELECT count(*) FROM job_run WHERE job = 'gsc_sync'`) }
	for range 2 {
		if err := e.svc.SyncIfDue(e.ctx); err != nil {
			t.Fatal(err)
		}
	}
	if syncs() != 1 {
		t.Fatalf("syncs right after connecting = %d, want 1", syncs())
	}
	starts := map[string]bool{}
	for _, q := range e.fake.Queries() {
		starts[q.StartDate] = true
	}
	if backfill := e.now.AddDate(0, -16, 0).Format(time.DateOnly); !starts[backfill] {
		t.Fatalf("the first sync's windows start on %v, want one on %s (16 months back)", starts, backfill)
	}

	e.now = e.now.Add(23 * time.Hour)
	_ = e.svc.SyncIfDue(e.ctx)
	if syncs() != 1 {
		t.Fatal("synced again within a day")
	}
	e.now = e.now.Add(2 * time.Hour)
	delete(e.fake.ServiceAccounts, saEmail)
	if err := e.svc.SyncIfDue(e.ctx); !errors.Is(err, core.ErrUpstream) {
		t.Fatalf("a sync Google refuses: %v", err)
	}
	if st := e.status(); !strings.Contains(st.LastSyncError, "Google refused") {
		t.Fatalf("last sync error = %q", st.LastSyncError)
	}
	e.now = e.now.Add(30 * time.Minute)
	_ = e.svc.SyncIfDue(e.ctx)
	if syncs() != 2 {
		t.Fatalf("syncs = %d; a failed sync retried within the hour", syncs())
	}

	e.connectServiceAccount(prefixProperty)
	if _, err := e.svc.SetProperty(e.ctx, gsc.PropertyInput{Property: prefixProperty, Backfill: new(false)}); err != nil {
		t.Fatal(err)
	}
	e.now = e.now.Add(31 * time.Minute)
	if err := e.svc.SyncIfDue(e.ctx); err != nil {
		t.Fatal(err)
	}
	last := e.fake.Queries()[len(e.fake.Queries())-1]
	if last.Site != prefixProperty || last.StartDate != e.day(27) {
		t.Fatalf("a new property without backfill pulled %s from %s, want %s from %s", last.Site, last.StartDate, prefixProperty, e.day(27))
	}
}

func TestSyncRecovers(t *testing.T) {
	e := newEnv(t)
	for n := 1; n <= 60; n++ {
		e.fake.SetRows(domainProperty, site(e.day(n), 1, 10))
	}
	long := gsctest.Row{Date: e.day(5), Query: "fernhollow docs", Page: prefixProperty + "docs?ref=" + strings.Repeat("x", 2500), Clicks: 1, Impressions: 2, Position: 3}
	e.fake.SetRows(domainProperty, site(e.day(5), 1, 10), long)
	if _, err := e.svc.ConnectServiceAccount(e.ctx, e.fake.ServiceAccountKey(t, saEmail)); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.SetProperty(e.ctx, gsc.PropertyInput{Property: domainProperty, Backfill: new(false)}); err != nil {
		t.Fatal(err)
	}
	e.sync()
	days := func() int64 { return e.count(`SELECT count(*) FROM gsc_site_daily`) }
	if days() != 27 {
		t.Fatalf("days without backfill = %d, want the 27 stored of the last 28", days())
	}
	if n := e.count(`SELECT count(*) FROM gsc_page_daily WHERE length(page) > 2000`); n != 0 {
		t.Fatal("a key too long for an index entry was stored")
	}

	// Backfill turned on fails after committing older chunks, then a
	// property pick waiting on the failure is not held back an hour.
	if _, err := e.svc.SetProperty(e.ctx, gsc.PropertyInput{Property: domainProperty}); err != nil {
		t.Fatal(err)
	}
	e.fake.FailQueriesFrom(e.day(20))
	if err := e.svc.SyncIfDue(e.ctx); !errors.Is(err, core.ErrUpstream) {
		t.Fatalf("a sync Google fails: %v", err)
	}
	if days() <= 27 {
		t.Fatal("the failed backfill committed no older chunk; the test does not cover a partial backfill")
	}
	e.fake.FailQueriesFrom("")
	if err := e.svc.SyncIfDue(e.ctx); err != nil {
		t.Fatal(err)
	}
	if n := e.count(`SELECT count(*) FROM job_run WHERE job = 'gsc_sync'`); n != 2 {
		t.Fatalf("syncs = %d; a failed sync retried within the hour", n)
	}
	if _, err := e.svc.SetProperty(e.ctx, gsc.PropertyInput{Property: domainProperty}); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.SyncIfDue(e.ctx); err != nil {
		t.Fatal(err)
	}
	if n := e.count(`SELECT count(*) FROM job_run WHERE job = 'gsc_sync'`); n != 3 {
		t.Fatalf("syncs = %d; picking the property again waited out the failure's backoff", n)
	}
	if days() != 60 {
		t.Fatalf("days after the backfill recovered = %d, want 60", days())
	}

	e.fake.FailQueriesFrom(e.day(0))
	e.now = e.now.Add(25 * time.Hour)
	_ = e.svc.SyncIfDue(e.ctx)
	e.fake.FailQueriesFrom("")
	if _, err := e.svc.SetProperty(e.ctx, gsc.PropertyInput{Property: prefixProperty}); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.SyncIfDue(e.ctx); err != nil {
		t.Fatal(err)
	}
	if n := e.count(`SELECT count(*) FROM job_run WHERE job = 'gsc_sync'`); n != 5 {
		t.Fatalf("syncs = %d; picking a property after a failure waited out the backoff", n)
	}

	e.now = e.now.Add(time.Hour)
	if _, err := e.svc.ConnectServiceAccount(e.ctx, e.fake.ServiceAccountKey(t, saEmail)); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.SyncIfDue(e.ctx); err != nil {
		t.Fatal(err)
	}
	if n := e.count(`SELECT count(*) FROM job_run WHERE job = 'gsc_sync'`); n != 6 {
		t.Fatalf("syncs = %d; a new credential was not tried at once", n)
	}
}

func TestDefaultBrandedTermsSkipOneLetterAliases(t *testing.T) {
	e := newEnv(t)
	v, err := e.cfg.Settings(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	v.Brand.Aliases = []string{"F", "Fern Hollow"}
	if v, err = e.cfg.PutSettings(e.ctx, v.Settings); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(v.BrandedTerms, []string{"fernhollow", "fern hollow"}) {
		t.Fatalf("default branded terms = %v", v.BrandedTerms)
	}
}

func TestInspectURL(t *testing.T) {
	e := newEnv(t)
	if _, err := e.svc.InspectURL(e.ctx, prefixProperty+"pricing"); !errors.Is(err, core.ErrInvalid) {
		t.Fatalf("inspecting while not connected: %v", err)
	}
	crawled := time.Date(2026, 11, 19, 14, 3, 0, 0, time.UTC)
	e.fake.SetInspection(prefixProperty+"pricing", gsctest.Inspection{Verdict: "PASS", CoverageState: "Submitted and indexed",
		LastCrawl: crawled, GoogleCanonical: prefixProperty + "pricing", UserCanonical: prefixProperty + "pricing/"})
	e.connectServiceAccount(domainProperty)
	in, err := e.svc.InspectURL(e.ctx, prefixProperty+"pricing")
	if err != nil {
		t.Fatal(err)
	}
	if in.CoverageState != "Submitted and indexed" || in.LastCrawl == nil || !in.LastCrawl.Equal(crawled) ||
		in.GoogleCanonical != prefixProperty+"pricing" || in.UserCanonical != prefixProperty+"pricing/" || in.Verdict != "PASS" {
		t.Fatalf("inspection = %+v", in)
	}
	unknown, err := e.svc.InspectURL(e.ctx, prefixProperty+"new")
	if err != nil || unknown.LastCrawl != nil || unknown.CoverageState != "URL is unknown to Google" {
		t.Fatalf("an uncrawled URL = %+v, %v", unknown, err)
	}
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
