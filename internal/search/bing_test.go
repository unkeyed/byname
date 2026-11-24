package search_test

import (
	"errors"
	"fmt"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/unkeyed/byname/internal/core"
	"github.com/unkeyed/byname/internal/search"
	"github.com/unkeyed/byname/internal/search/searchtest"
	"github.com/unkeyed/byname/internal/secretbox"
)

// At e.now, Saturday Nov 21 2026, Bing's newest week is dated Friday
// Nov 20 and its newest day is Nov 20, both still counting. The newest
// final week ends Friday Nov 13, so every window ends there.
const (
	bingNewestWeek = "2026-11-20"
	bingAnchor     = "2026-11-13"
)

// friday is the Friday n weeks before Nov 20.
func friday(n int) string {
	return time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC).AddDate(0, 0, -7*n).Format(time.DateOnly)
}

// seedBing gives the Bing site 70 days of totals, 10 clicks and 100
// appearances a day, ending on Nov 20, and ten weekly top lists ending on
// Nov 20: a branded search with 20 clicks and a non-brand one with 30, so
// 20 of a week's 70 clicks are unlisted. The newest day and week, still
// counting, carry far more, which no window may count.
func (e *env) seedBing() {
	var days []searchtest.BingDay
	for n := 70; n >= 1; n-- {
		d := searchtest.BingDay{Date: e.day(n), Clicks: 10, Impressions: 100}
		if n == 1 {
			d.Clicks, d.Impressions = 900, 9000
		}
		days = append(days, d)
	}
	e.bing.SetTraffic(bingSite, days...)
	var queries, pages []searchtest.BingWeek
	for n := 9; n >= 0; n-- {
		brand, other := 20, 30
		if n == 0 {
			brand, other = 400, 500
		}
		queries = append(queries,
			searchtest.BingWeek{Date: friday(n), Key: "fernhollow pricing", Clicks: brand, Impressions: 200, Position: 2},
			searchtest.BingWeek{Date: friday(n), Key: "go api hosting", Clicks: other, Impressions: 300, Position: 7})
		pages = append(pages,
			searchtest.BingWeek{Date: friday(n), Key: bingSite + "pricing", Clicks: brand, Impressions: 200, Position: 2},
			searchtest.BingWeek{Date: friday(n), Key: bingSite + "docs", Clicks: other, Impressions: 300})
	}
	e.bing.SetQueries(bingSite, queries...)
	e.bing.SetPages(bingSite, pages...)
}

func (e *env) connectBing() {
	e.t.Helper()
	if _, err := e.svc.ConnectKey(e.ctx, search.Bing, bingKey); err != nil {
		e.t.Fatal(err)
	}
	if _, err := e.svc.SetSite(e.ctx, search.Bing, search.SiteInput{Site: bingSite}); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) bingStatus() search.Status {
	e.t.Helper()
	st, err := e.svc.Status(e.ctx, search.Bing)
	if err != nil {
		e.t.Fatal(err)
	}
	return st
}

func TestBingConnectsWithAnAPIKey(t *testing.T) {
	e := newEnv(t)
	_, err := e.svc.ConnectKey(e.ctx, search.Bing, "a-key-bing-never-issued")
	if !errors.Is(err, core.ErrUpstream) || !strings.Contains(err.Error(), "refused the API key") || strings.Contains(err.Error(), "a-key-bing-never-issued") {
		t.Fatalf("a key Bing refuses: %v; want ErrUpstream saying so, without the key", err)
	}
	if e.bingStatus().Connected {
		t.Fatal("a refused key left a connection")
	}
	st, err := e.svc.ConnectKey(e.ctx, search.Bing, "  "+bingKey+"\n")
	if err != nil {
		t.Fatal(err)
	}
	if !st.Connected || st.Method != search.MethodAPIKey || st.Account != "" || st.Site != "" || st.StepDays != 7 {
		t.Fatalf("status after connecting = %+v", st)
	}
	var sealed string
	if err := e.db.QueryRow(e.ctx, `SELECT ciphertext FROM search_connection WHERE provider = 'bing'`).Scan(&sealed); err != nil {
		t.Fatal(err)
	}
	if key, err := secretbox.ForTest().Open(sealed, "search:bing:api_key"); err != nil || key != bingKey || strings.Contains(sealed, bingKey) {
		t.Fatalf("stored credential opens to %q, %v; want the key, sealed", key, err)
	}
	if strings.Contains(fmt.Sprintf("%+v", st), bingKey) {
		t.Fatal("the status carries the API key")
	}
	if google := e.status(); google.Connected {
		t.Fatal("connecting Bing connected Google")
	}

	sites, err := e.svc.Sites(e.ctx, search.Bing)
	if err != nil {
		t.Fatal(err)
	}
	if len(sites) != 2 || sites[0].URL != bingSite || !sites[0].Suggested || sites[1].Suggested || sites[0].Kind != "" {
		t.Fatalf("sites = %+v; want the two verified ones, the brand's domain suggested", sites)
	}
	if _, err := e.svc.SetSite(e.ctx, search.Bing, search.SiteInput{Site: "https://staging.fernhollow.dev/"}); !errors.Is(err, core.ErrInvalid) {
		t.Fatalf("a site Bing has not verified: %v", err)
	}
	if _, err := e.svc.SetSite(e.ctx, search.Bing, search.SiteInput{Site: "https://fernhollow.dev/"}); !errors.Is(err, core.ErrInvalid) {
		t.Fatalf("a site named unlike Bing names it: %v", err)
	}
	st, err = e.svc.SetSite(e.ctx, search.Bing, search.SiteInput{Site: bingSite, Backfill: new(true)})
	if err != nil || st.Site != bingSite || st.SiteKind != "" || st.Backfill {
		t.Fatalf("pick the site: %+v, %v; want it picked, with no backfill, which Bing has no use for", st, err)
	}
	if err := e.svc.Disconnect(e.ctx, search.Bing); err != nil || e.bingStatus().Connected {
		t.Fatalf("disconnect: %v", err)
	}
	if _, err := e.svc.Status(e.ctx, "yahoo"); !errors.Is(err, core.ErrInvalid) {
		t.Fatalf("an unknown provider: %v", err)
	}
}

func TestBingSync(t *testing.T) {
	e := newEnv(t)
	e.seedBing()
	e.connectBing()
	if err := e.svc.Sync(e.ctx, search.Bing); err != nil {
		t.Fatal(err)
	}
	var methods []string
	for _, c := range e.bing.Calls() {
		if c.Method != "GetUserSites" {
			methods = append(methods, c.Method)
			if c.SiteURL != bingSite {
				t.Errorf("%s asked for site %q, want %q", c.Method, c.SiteURL, bingSite)
			}
		}
	}
	if !slices.Equal(methods, []string{"GetQueryStats", "GetPageStats", "GetRankAndTrafficStats"}) {
		t.Fatalf("a sync called %v, want the three stats methods", methods)
	}

	st := e.bingStatus()
	if st.LatestFinal != bingAnchor || !slices.Equal(st.PartialDays, []string{e.day(1)}) || st.FirstDay != e.day(70) || st.LastSyncAt == nil {
		t.Fatalf("status: latest final %s, partial %v, first %s; want %s, the newest day, and %s", st.LatestFinal, st.PartialDays, st.FirstDay, bingAnchor, e.day(70))
	}
	var first, last string
	var final bool
	if err := e.db.QueryRow(e.ctx, `SELECT first_day::text, last_day::text, is_final FROM search_query_stats
		WHERE provider = 'bing' AND query = 'go api hosting' AND last_day = $1`, bingNewestWeek).Scan(&first, &last, &final); err != nil {
		t.Fatal(err)
	}
	if first != "2026-11-14" || last != bingNewestWeek || final {
		t.Fatalf("the newest week's row covers %s to %s, final %v; want Nov 14 to Nov 20, still partial", first, last, final)
	}
	if n := e.count(`SELECT count(*) FROM search_query_stats WHERE provider = 'bing' AND is_final`); n != 18 {
		t.Fatalf("final weekly search rows = %d, want 2 for each of the 9 older weeks", n)
	}

	sum, err := e.svc.Summary(e.ctx, search.Bing)
	if err != nil {
		t.Fatal(err)
	}
	c, p := sum.Current, sum.Previous
	if c == nil || c.From != "2026-10-17" || c.To != bingAnchor || c.Days != 28 || c.DaysWithData != 28 {
		t.Fatalf("headline window = %+v; want the 4 weeks Oct 17 to Nov 13", c)
	}
	if c.Clicks != 280 || c.Impressions != 2800 || c.Position != 0 || c.BrandClicks != 80 || c.NonBrandClicks != 120 || c.UnlistedClicks != 80 {
		t.Fatalf("headline = %+v; want 280 clicks, 80 brand, 120 non-brand, 80 unlisted and no position", c.Totals)
	}
	if p == nil || p.From != "2026-09-19" || p.To != "2026-10-16" || p.Clicks != 280 {
		t.Fatalf("previous window = %+v; want Sep 19 to Oct 16", p)
	}

	top, err := e.svc.TopQueries(e.ctx, search.Bing, search.Top{})
	if err != nil {
		t.Fatal(err)
	}
	if top.From != "2026-10-17" || top.To != bingAnchor || len(top.Queries) != 2 || top.Queries[0].Query != "go api hosting" || top.Queries[0].Clicks != 120 || top.Queries[0].Position != 7 {
		t.Fatalf("top searches = %+v", top)
	}
	short, err := e.svc.TopQueries(e.ctx, search.Bing, search.Top{Days: 10, NonBrand: true})
	if err != nil || short.From != "2026-11-07" || short.To != bingAnchor || len(short.Queries) != 1 || short.Queries[0].Clicks != 30 {
		t.Fatalf("10 days of non-brand searches = %+v, %v; want the one whole week Nov 7 to Nov 13", short, err)
	}
	pages, err := e.svc.TopPages(e.ctx, search.Bing, search.Top{})
	if err != nil || len(pages.Pages) != 2 || pages.Pages[0].Page != bingSite+"docs" || pages.Pages[0].Clicks != 120 || pages.Pages[0].Position != 0 {
		t.Fatalf("top pages = %+v, %v; want docs first, with no position since Bing sent -1", pages, err)
	}

	daily, err := e.svc.Daily(e.ctx, search.Bing, search.Range{})
	if err != nil {
		t.Fatal(err)
	}
	newest := daily.Days[len(daily.Days)-1]
	if len(daily.Days) != 70 || daily.LatestFinal != e.day(2) || newest.Date != e.day(1) || newest.Final || newest.Position != 0 {
		t.Fatalf("daily: %d days, latest final %s, newest %+v", len(daily.Days), daily.LatestFinal, newest)
	}

	split, err := e.svc.Brand(e.ctx, search.Bing, search.Range{})
	if err != nil {
		t.Fatal(err)
	}
	var ends []string
	for _, b := range split.Points {
		ends = append(ends, b.Date)
		if b != (search.BrandPoint{Date: b.Date, Clicks: 280, BrandClicks: 80, NonBrandClicks: 120, UnlistedClicks: 80}) {
			t.Fatalf("brand point %+v; want 4 whole weeks", b)
		}
	}
	if !slices.Equal(ends, []string{"2026-10-09", "2026-10-16", "2026-10-23", "2026-10-30", "2026-11-06", bingAnchor}) {
		t.Fatalf("brand points end on %v; want each Friday whose 4 weeks are all stored", ends)
	}

	g, err := e.svc.UpsertGoal(e.ctx, search.GoalInput{Provider: search.Bing, Metric: search.MetricClicks, WindowDays: 90, Target: 600, DueOn: "2027-03-31"})
	if err != nil {
		t.Fatal(err)
	}
	if pr := g.Progress; pr == nil || pr.From != "2026-08-15" || pr.To != bingAnchor || pr.Value != 630 || pr.DaysWithData != 63 || !pr.Reached {
		t.Fatalf("90-day Bing goal = %+v; want 13 whole weeks, Aug 15 to Nov 13", g.Progress)
	}
	if google, err := e.svc.UpsertGoal(e.ctx, search.GoalInput{Provider: search.Google, Metric: search.MetricClicks, WindowDays: 90, Target: 600, DueOn: "2027-03-31"}); err != nil || google.ID == g.ID || google.Progress != nil {
		t.Fatalf("the same goal on Google = %+v, %v; want its own goal with no data yet", google, err)
	}
}

func TestBingResync(t *testing.T) {
	e := newEnv(t)
	e.seedBing()
	e.connectBing()
	if err := e.svc.Sync(e.ctx, search.Bing); err != nil {
		t.Fatal(err)
	}

	// Later that day Bing revises its newest week, where the branded
	// search fell out of the top list, and its oldest week and day have
	// aged out of what it keeps.
	e.bing.SetQueries(bingSite,
		searchtest.BingWeek{Date: friday(8), Key: "go api hosting", Clicks: 30, Impressions: 300, Position: 7},
		searchtest.BingWeek{Date: friday(1), Key: "go api hosting", Clicks: 31, Impressions: 300, Position: 7},
		searchtest.BingWeek{Date: friday(0), Key: "go api hosting", Clicks: 450, Impressions: 3000, Position: 6})
	e.bing.SetTraffic(bingSite, searchtest.BingDay{Date: e.day(69), Clicks: 10, Impressions: 100}, searchtest.BingDay{Date: e.day(1), Clicks: 950, Impressions: 9100})
	if err := e.svc.Sync(e.ctx, search.Bing); err != nil {
		t.Fatal(err)
	}
	if n := e.count(`SELECT count(*) FROM search_query_stats WHERE provider = 'bing' AND last_day = $1`, bingNewestWeek); n != 1 {
		t.Fatalf("searches in the newest week = %d; want the branded one Bing dropped gone", n)
	}
	if n := e.count(`SELECT clicks FROM search_query_stats WHERE provider = 'bing' AND last_day = $1`, bingNewestWeek); n != 450 {
		t.Fatalf("the newest week's clicks = %d, want Bing's revision", n)
	}
	if n := e.count(`SELECT clicks FROM search_query_stats WHERE provider = 'bing' AND last_day = $1 AND query = 'go api hosting'`, friday(1)); n != 31 {
		t.Fatalf("a final week Bing sent again = %d clicks, want its new count", n)
	}
	if n := e.count(`SELECT count(*) FROM search_query_stats WHERE provider = 'bing' AND last_day = $1`, friday(9)); n != 2 {
		t.Fatal("a final week that aged out of Bing was deleted")
	}
	if n := e.count(`SELECT count(*) FROM search_site_daily WHERE provider = 'bing'`); n != 70 {
		t.Fatalf("days stored = %d; want all 70, the ones Bing no longer sends kept", n)
	}

	// A week later the Nov 20 week and day are final, and the windows move
	// to that week.
	e.now = e.now.AddDate(0, 0, 7)
	e.bing.SetQueries(bingSite,
		searchtest.BingWeek{Date: friday(1), Key: "go api hosting", Clicks: 31, Impressions: 300, Position: 7},
		searchtest.BingWeek{Date: friday(0), Key: "go api hosting", Clicks: 460, Impressions: 3000, Position: 6},
		searchtest.BingWeek{Date: "2026-11-27", Key: "go api hosting", Clicks: 5, Impressions: 30, Position: 6})
	var days []searchtest.BingDay
	for d := time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC); !d.After(time.Date(2026, 11, 27, 0, 0, 0, 0, time.UTC)); d = d.AddDate(0, 0, 1) {
		days = append(days, searchtest.BingDay{Date: d.Format(time.DateOnly), Clicks: 10, Impressions: 100})
	}
	e.bing.SetTraffic(bingSite, days...)
	if err := e.svc.SyncIfDue(e.ctx); err != nil {
		t.Fatal(err)
	}
	if st := e.bingStatus(); st.LatestFinal != bingNewestWeek || !slices.Equal(st.PartialDays, []string{"2026-11-27"}) {
		t.Fatalf("a week on: latest final %s partial %v; want Nov 20 and Nov 27", st.LatestFinal, st.PartialDays)
	}
}

func TestBingDatesWithAnOffset(t *testing.T) {
	for _, offset := range []string{"+0530", "-0700"} {
		t.Run(offset, func(t *testing.T) {
			e := newEnv(t)
			e.bing.DateOffset = offset
			e.seedBing()
			e.connectBing()
			if err := e.svc.Sync(e.ctx, search.Bing); err != nil {
				t.Fatal(err)
			}
			if st := e.bingStatus(); st.FirstDay != e.day(70) || st.LatestFinal != bingAnchor || !slices.Equal(st.PartialDays, []string{e.day(1)}) {
				t.Fatalf("with dates at %s: first %s, latest final %s, partial %v; want the days Bing named", offset, st.FirstDay, st.LatestFinal, st.PartialDays)
			}
		})
	}
}

func TestBingRefusals(t *testing.T) {
	e := newEnv(t)
	e.seedBing()
	e.connectBing()
	for code, says := range map[int]string{4: "throttling this account", 5: "throttling requests for this site", 17: "throttling this server", 14: "may not read this site", 3: "refused the API key", 99: "tries again in an hour"} {
		e.bing.Refuse("GetPageStats", code)
		err := e.svc.Sync(e.ctx, search.Bing)
		if !errors.Is(err, core.ErrUpstream) || !strings.Contains(err.Error(), says) {
			t.Fatalf("Bing error %d: %v; want ErrUpstream saying %q", code, err, says)
		}
		if st := e.bingStatus(); !strings.Contains(st.LastSyncError, says) || strings.Contains(st.LastSyncError, bingKey) {
			t.Fatalf("Bing error %d: last sync error %q", code, st.LastSyncError)
		}
	}
	if n := e.count(`SELECT count(*) FROM job_run WHERE job = 'search_sync' AND provider = 'bing' AND status = 'failed'`); n != 6 {
		t.Fatalf("failed Bing syncs = %d, want 6", n)
	}

	// Bing's address answering nothing must not leak the key, which rides
	// in the URL.
	gone := httptest.NewServer(nil)
	gone.Close()
	offline := search.New(e.db, search.Options{Secrets: secretbox.ForTest(), BaseURLs: map[search.Provider]string{search.Bing: gone.URL}, Terms: e.cfg, Now: func() time.Time { return e.now }})
	_, err := offline.ConnectKey(e.ctx, search.Bing, bingKey)
	if !errors.Is(err, core.ErrUpstream) || strings.Contains(err.Error(), bingKey) || strings.Contains(err.Error(), "apikey") {
		t.Fatalf("Bing unreachable: %v; want ErrUpstream without the key", err)
	}
}

func TestOneProviderFailingNeverHoldsUpAnother(t *testing.T) {
	e := newEnv(t)
	e.seedBing()
	for n := 1; n <= 30; n++ {
		e.fake.SetRows(domainProperty, site(e.day(n), 5, 50))
	}
	e.connectServiceAccount(domainProperty)
	e.connectBing()
	e.bing.Refuse("GetQueryStats", 4)
	if err := e.svc.SyncIfDue(e.ctx); !errors.Is(err, core.ErrUpstream) || !strings.Contains(err.Error(), "Bing") {
		t.Fatalf("a round where Bing throttles: %v", err)
	}
	jobs := func(provider, status string) int64 {
		return e.count(`SELECT count(*) FROM job_run WHERE job = 'search_sync' AND provider = $1 AND status = $2`, provider, status)
	}
	if jobs("google", "completed") != 1 || jobs("bing", "failed") != 1 || e.status().FirstDay == "" {
		t.Fatal("Google did not sync in the round Bing failed")
	}
	if !strings.Contains(e.bingStatus().LastSyncError, "throttling") || e.status().LastSyncError != "" {
		t.Fatal("each provider does not keep its own last error")
	}
	e.bing.Refuse("GetQueryStats", 0)
	e.now = e.now.Add(30 * time.Minute)
	if err := e.svc.SyncIfDue(e.ctx); err != nil {
		t.Fatal(err)
	}
	if jobs("bing", "failed")+jobs("bing", "completed") != 1 {
		t.Fatal("Bing retried within the hour")
	}
	e.now = e.now.Add(31 * time.Minute)
	if err := e.svc.SyncIfDue(e.ctx); err != nil {
		t.Fatal(err)
	}
	if jobs("bing", "completed") != 1 || jobs("google", "completed") != 1 {
		t.Fatal("after the hour Bing did not sync again, or Google synced before its day was up")
	}
}
