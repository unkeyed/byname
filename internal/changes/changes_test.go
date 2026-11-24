package changes_test

import (
	"context"
	"encoding/json"
	"net/url"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/unkeyed/byname/internal/changes"
	"github.com/unkeyed/byname/internal/changes/githubtest"
	"github.com/unkeyed/byname/internal/config"
	"github.com/unkeyed/byname/internal/notify"
	"github.com/unkeyed/byname/internal/results"
	"github.com/unkeyed/byname/internal/search"
	"github.com/unkeyed/byname/internal/search/searchtest"
	"github.com/unkeyed/byname/internal/secretbox"
	"github.com/unkeyed/byname/internal/testdb"
)

const (
	property = "sc-domain:northwind.example"
	site     = "https://www.northwind.example/"
	token    = "github_pat_11NORTHWIND0readonly"
)

type env struct {
	t      *testing.T
	ctx    context.Context
	db     *pgxpool.Pool
	svc    *changes.Service
	search *search.Service
	google *searchtest.Google
	gh     *githubtest.Server
	now    time.Time
}

// newEnv is a Northwind instance in zone, with Search Console connected to
// a fake Google and a fake GitHub serving northwind/site.
func newEnv(t *testing.T, zone string) *env {
	t.Helper()
	pool := testdb.New(t)
	e := &env{t: t, ctx: context.Background(), db: pool, now: time.Date(2026, 11, 21, 17, 0, 0, 0, time.UTC),
		google: searchtest.NewGoogle(t), gh: githubtest.New(t, "northwind", "site", token)}
	cfg := config.New(pool, config.Options{Secrets: secretbox.ForTest()})
	if _, err := cfg.PutSettings(e.ctx, config.Settings{
		Brand:  config.SelfBrand{Name: "Northwind", Domains: []string{"northwind.example"}},
		Market: "US", Language: "en", TimeZone: zone, CheckEveryDays: 3, CheckTime: "06:00",
		SpendCapRunUSD: 5, Concurrency: 8, Retries: 1, TimeoutS: 180,
	}); err != nil {
		t.Fatal(err)
	}
	now := func() time.Time { return e.now }
	u, _ := url.Parse("http://localhost:8080")
	e.search = search.New(pool, search.Options{Secrets: secretbox.ForTest(), BaseURLs: map[search.Provider]string{search.Google: e.google.URL}, PublicURL: u, Terms: cfg, Now: now})
	e.google.AddSite(property, "siteOwner")
	if _, err := e.search.ConnectKey(e.ctx, search.Google, e.google.ServiceAccountKey(t, "byname@northwind.iam.gserviceaccount.com")); err != nil {
		t.Fatal(err)
	}
	if _, err := e.search.SetSite(e.ctx, search.Google, search.SiteInput{Site: property}); err != nil {
		t.Fatal(err)
	}
	alerts := notify.New(pool, results.New(pool), notify.Options{Now: now})
	e.svc = changes.New(pool, e.search, changes.Options{Secrets: secretbox.ForTest(), Alerts: alerts, GitHubBaseURL: e.gh.URL, Now: now})
	return e
}

func (e *env) count(sql string, args ...any) int {
	e.t.Helper()
	var n int
	if err := e.db.QueryRow(e.ctx, sql, args...).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

func (e *env) list() []changes.Change {
	e.t.Helper()
	list, err := e.svc.List(e.ctx)
	if err != nil {
		e.t.Fatal(err)
	}
	return list
}

func (e *env) connect() {
	e.t.Helper()
	if _, err := e.svc.ConnectRepo(e.ctx, changes.RepoInput{Repo: "northwind/site", Token: token}); err != nil {
		e.t.Fatal(err)
	}
	if _, err := e.svc.SetRules(e.ctx, []changes.Rule{
		{Glob: "content/blog/*.mdx", URL: site + "blog/{1}"},
		{Glob: "app/(marketing)/**/page.tsx", URL: site + "{1}"},
	}); err != nil {
		e.t.Fatal(err)
	}
}

func ago(now time.Time, d time.Duration) *time.Time { t := now.Add(-d); return &t }

func TestPollStoresOneChangePerPullAndPageAndRepollsWithoutDuplicates(t *testing.T) {
	e := newEnv(t, "UTC")
	e.connect()
	e.gh.AddPull(githubtest.Pull{Number: 41, Title: "Rewrite pricing", MergedAt: ago(e.now, 50*time.Hour), Updated: e.now.Add(-49 * time.Hour), Files: []githubtest.File{
		{Name: "app/(marketing)/pricing/page.tsx", Status: "modified"},
		{Name: "app/(marketing)/pricing/table.tsx", Status: "modified"},
		{Name: "content/blog/launch-week.mdx", Status: "added"},
		{Name: "package.json", Status: "modified"},
	}})
	e.gh.AddPull(githubtest.Pull{Number: 42, Title: "Abandoned idea", Updated: e.now.Add(-30 * time.Hour), Files: []githubtest.File{{Name: "content/blog/idea.mdx", Status: "added"}}})
	e.gh.AddPull(githubtest.Pull{Number: 7, Title: "Too old for the first poll", MergedAt: ago(e.now, 40*24*time.Hour), Updated: e.now.Add(-40 * 24 * time.Hour),
		Files: []githubtest.File{{Name: "content/blog/old.mdx", Status: "modified"}}})

	if err := e.svc.Tick(e.ctx); err != nil {
		t.Fatal(err)
	}
	got := map[string]changes.Kind{}
	for _, c := range e.list() {
		if c.Source != changes.FromGitHub || c.PRNumber != 41 || c.Repo != "northwind/site" || c.Stage != changes.AwaitingCrawl {
			t.Errorf("change %+v", c)
		}
		got[c.URL] = c.Kind
	}
	want := map[string]changes.Kind{site + "pricing": changes.Edit, site + "blog/launch-week": changes.NewPage}
	if len(got) != len(want) || got[site+"pricing"] != want[site+"pricing"] || got[site+"blog/launch-week"] != want[site+"blog/launch-week"] {
		t.Fatalf("changes = %v, want %v", got, want)
	}

	// Within the hour nothing polls; after it, a PR updated again (a late
	// comment) is read again and still stored once per page.
	e.now = e.now.Add(30 * time.Minute)
	calls := len(e.gh.Calls())
	if err := e.svc.Tick(e.ctx); err != nil || len(e.gh.Calls()) != calls {
		t.Fatalf("a tick within the hour called GitHub: %v", err)
	}
	e.now = e.now.Add(time.Hour)
	e.gh.AddPull(githubtest.Pull{Number: 41, Title: "Rewrite pricing page", MergedAt: ago(e.now, 51*time.Hour+30*time.Minute), Updated: e.now.Add(-time.Minute), Files: []githubtest.File{
		{Name: "app/(marketing)/pricing/page.tsx", Status: "modified"},
		{Name: "content/blog/launch-week.mdx", Status: "added"},
	}})
	e.gh.AddPull(githubtest.Pull{Number: 43, Title: "Docs home", MergedAt: ago(e.now, 2*time.Hour), Updated: e.now.Add(-2 * time.Hour),
		Files: []githubtest.File{{Name: "app/(marketing)/page.tsx", Status: "modified"}}})
	if err := e.svc.Tick(e.ctx); err != nil {
		t.Fatal(err)
	}
	if n := e.count(`SELECT count(*) FROM change`); n != 3 {
		t.Fatalf("after a re-poll there are %d changes, want 3", n)
	}
	if n := e.count(`SELECT count(*) FROM change WHERE pr_title = 'Rewrite pricing page'`); n != 2 {
		t.Fatalf("the re-poll updated %d changes' titles, want 2", n)
	}
	if n := e.count(`SELECT count(*) FROM job_run WHERE job = 'site_changes' AND status = 'completed'`); n != 2 {
		t.Fatalf("%d completed site_changes jobs, want 2", n)
	}
}

func TestRulesAddedAfterAPollFindRecentMerges(t *testing.T) {
	e := newEnv(t, "UTC")
	if _, err := e.svc.ConnectRepo(e.ctx, changes.RepoInput{Repo: "northwind/site", Token: token}); err != nil {
		t.Fatal(err)
	}
	e.gh.AddPull(githubtest.Pull{Number: 3, Title: "Launch post", MergedAt: ago(e.now, 5*24*time.Hour), Updated: e.now.Add(-5 * 24 * time.Hour),
		Files: []githubtest.File{{Name: "content/blog/launch.mdx", Status: "added"}}})
	if err := e.svc.Tick(e.ctx); err != nil {
		t.Fatal(err)
	}
	if n := e.count(`SELECT count(*) FROM change`); n != 0 {
		t.Fatalf("%d changes without path rules, want 0", n)
	}
	if _, err := e.svc.SetRules(e.ctx, []changes.Rule{{Glob: "content/blog/*.mdx", URL: site + "blog/{1}"}}); err != nil {
		t.Fatal(err)
	}
	e.now = e.now.Add(time.Minute)
	if err := e.svc.Tick(e.ctx); err != nil {
		t.Fatal(err)
	}
	if list := e.list(); len(list) != 1 || list[0].URL != site+"blog/launch" {
		t.Fatalf("after adding a rule the next tick found %+v, want the launch post", list)
	}
}

func TestTokenExpiringFiresOnce(t *testing.T) {
	e := newEnv(t, "UTC")
	far := e.now.Add(60 * 24 * time.Hour)
	e.gh.SetExpiry(&far)
	e.connect()
	st, err := e.svc.RepoStatus(e.ctx)
	if err != nil || st.TokenExpiresAt == nil || !st.TokenExpiresAt.Equal(far) || st.ExpiringSoon {
		t.Fatalf("status %+v, %v; want the expiry GitHub reported, not soon", st, err)
	}
	if err := e.svc.Poll(e.ctx); err != nil {
		t.Fatal(err)
	}
	if n := e.count(`SELECT count(*) FROM alert WHERE kind = 'token_expiring'`); n != 0 {
		t.Fatalf("%d token_expiring alerts 60 days out, want 0", n)
	}
	soon := e.now.Add(10 * 24 * time.Hour)
	e.gh.SetExpiry(&soon)
	for range 3 {
		e.now = e.now.Add(2 * time.Hour)
		if err := e.svc.Tick(e.ctx); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.svc.TestRepo(e.ctx); err != nil {
		t.Fatal(err)
	}
	if n := e.count(`SELECT count(*) FROM alert WHERE kind = 'token_expiring'`); n != 1 {
		t.Fatalf("%d token_expiring alerts after four checks within 14 days, want 1", n)
	}
	if st, _ := e.svc.RepoStatus(e.ctx); !st.ExpiringSoon {
		t.Fatalf("status %+v, want expiring soon", st)
	}
}

// seedChange stores a manual change to page, merged at.
func (e *env) seedChange(page string, at time.Time) int64 {
	e.t.Helper()
	d, err := e.svc.LogChange(e.ctx, changes.ManualChange{URL: page, Kind: changes.Edit, MergedAt: at.Format(time.RFC3339)})
	if err != nil {
		e.t.Fatal(err)
	}
	return d.ID
}

func (e *env) syncCompleted() {
	e.t.Helper()
	if _, err := e.db.Exec(e.ctx, `INSERT INTO job_run (job, provider, trigger, status, started_at, finished_at) VALUES ('search_sync', 'google', 'scheduled', 'completed', $1, $1)`, e.now.Add(-time.Second)); err != nil {
		e.t.Fatal(err)
	}
}

func TestRecrawlSetsTheDatesInTheInstanceTimeZone(t *testing.T) {
	e := newEnv(t, "America/New_York")
	pricing, blog, docs := site+"pricing", site+"blog/launch-week", site+"docs"
	merged := time.Date(2026, 11, 18, 15, 0, 0, 0, time.UTC)
	e.seedChange(pricing, merged)
	e.seedChange(blog, merged.Add(time.Hour))
	e.seedChange(docs, merged.Add(2*time.Hour))
	// 03:30 UTC on Nov 20 is 22:30 on Nov 19 in New York.
	e.google.SetInspection(pricing, searchtest.Inspection{Verdict: "PASS", CoverageState: "Submitted and indexed", LastCrawl: time.Date(2026, 11, 20, 3, 30, 0, 0, time.UTC)})
	// Crawled before the merge: not the crawl that counts.
	e.google.SetInspection(blog, searchtest.Inspection{Verdict: "PASS", CoverageState: "Submitted and indexed", LastCrawl: merged.Add(-24 * time.Hour)})
	e.google.SetInspection(docs, searchtest.Inspection{Verdict: "PASS", CoverageState: "Submitted and indexed", LastCrawl: merged.Add(48 * time.Hour)})

	if err := e.svc.Tick(e.ctx); err != nil {
		t.Fatal(err)
	}
	if n := e.count(`SELECT count(*) FROM url_inspection`); n != 0 {
		t.Fatalf("recrawl watch ran %d inspections before any Search Console sync completed", n)
	}
	cap2 := changes.New(e.db, e.search, changes.Options{Secrets: secretbox.ForTest(), Now: func() time.Time { return e.now }, InspectionsPerDay: 2})
	e.syncCompleted()
	if err := cap2.Tick(e.ctx); err != nil {
		t.Fatal(err)
	}
	if n := e.count(`SELECT count(*) FROM url_inspection`); n != 2 {
		t.Fatalf("%d inspections with a daily cap of 2", n)
	}
	byURL := map[string]changes.Change{}
	for _, c := range e.list() {
		byURL[c.URL] = c
	}
	p := byURL[pricing]
	if p.FirstCrawlAt == nil || !p.FirstCrawlAt.Equal(time.Date(2026, 11, 20, 3, 30, 0, 0, time.UTC)) || *p.InterimOn != "2026-12-06" || *p.VerdictOn != "2026-12-20" || p.Stage != changes.AwaitingInterim {
		t.Fatalf("pricing = %+v, want crawled Nov 19 in New York, interim Dec 6 and verdict Dec 20", p)
	}
	if b := byURL[blog]; b.FirstCrawlAt != nil || b.Stage != changes.AwaitingCrawl {
		t.Fatalf("blog = %+v, want no first crawl: Google's last crawl was before the merge", b)
	}
	if byURL[docs].FirstCrawlAt != nil {
		t.Fatal("docs was inspected past the daily cap")
	}

	// The next day's sync frees the quota, and the next tick inspects what is left.
	e.now = e.now.Add(24 * time.Hour)
	e.syncCompleted()
	if err := cap2.Tick(e.ctx); err != nil {
		t.Fatal(err)
	}
	d, err := e.svc.Get(e.ctx, byURL[docs].ID)
	if err != nil || d.FirstCrawlAt == nil || *d.InterimOn != "2026-12-07" || len(d.Inspections) != 1 || d.Timeline[len(d.Timeline)-1].Kind != "verdict" {
		t.Fatalf("docs = %+v, %v", d, err)
	}
}

// seedDays stores Search Console numbers for page on each day from
// today-from to today-to, final through today-3.
func (e *env) seedDays(page string, from, to int, clicks, impressions int, position float64) {
	e.t.Helper()
	for n := from; n >= to; n-- {
		day := e.now.AddDate(0, 0, -n).Format(time.DateOnly)
		if _, err := e.db.Exec(e.ctx, `INSERT INTO search_page_stats (provider, site, first_day, last_day, page, clicks, impressions, position, is_final) VALUES ('google', $1, $2, $2, $3, $4, $5, $6, $7)`,
			property, day, page, clicks, impressions, position, n >= 3); err != nil {
			e.t.Fatal(err)
		}
	}
}

func TestVerdictsAgainstTheControlsStillHeld(t *testing.T) {
	e := newEnv(t, "UTC")
	page, steady, released := site+"pricing", site+"docs", site+"about"
	for n := 80; n >= 0; n-- {
		if _, err := e.db.Exec(e.ctx, `INSERT INTO search_site_daily (provider, site, day, clicks, impressions, position, is_final) VALUES ('google', $1, $2, 100, 10000, 8, $3)`,
			property, e.now.AddDate(0, 0, -n).Format(time.DateOnly), n >= 3); err != nil {
			t.Fatal(err)
		}
	}
	// Merged 42 days ago, crawled 40 days ago: the interim and final reads are both due.
	merged, crawled := e.now.AddDate(0, 0, -42), e.now.AddDate(0, 0, -40)
	id := e.seedChange(page, merged)
	e.google.SetInspection(page, searchtest.Inspection{Verdict: "PASS", CoverageState: "Submitted and indexed", LastCrawl: crawled})
	// Before the merge the page sits at 12; after the crawl it climbs to 6
	// and triples its impressions. Partial days at the end would sink it.
	e.seedDays(page, 80, 42, 2, 100, 12)
	e.seedDays(page, 41, 3, 12, 300, 6)
	e.seedDays(page, 2, 0, 0, 5000, 40)
	// The steady control holds still. The released one's hold ended
	// yesterday; it took off after the crawl and would cancel the gain.
	e.seedDays(steady, 80, 0, 10, 1000, 8)
	e.seedDays(released, 80, 41, 10, 1000, 8)
	e.seedDays(released, 40, 0, 50, 3000, 2)
	for url, hold := range map[string]string{steady: "2028-03-15", released: e.now.AddDate(0, 0, -1).Format(time.DateOnly)} {
		if _, err := e.svc.UpsertControlPage(e.ctx, changes.ControlInput{URL: url, HoldUntil: hold}); err != nil {
			t.Fatal(err)
		}
	}
	controls, err := e.svc.ControlPages(e.ctx)
	if err != nil || len(controls) != 2 || controls[0].URL != released || controls[0].Active || !controls[1].Active {
		t.Fatalf("controls = %+v, %v; want about inactive and docs active", controls, err)
	}

	e.syncCompleted()
	if err := e.svc.Tick(e.ctx); err != nil {
		t.Fatal(err)
	}
	e.now = e.now.Add(24 * time.Hour)
	e.syncCompleted()
	if err := e.svc.Tick(e.ctx); err != nil {
		t.Fatal(err)
	}
	d, err := e.svc.Get(e.ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if d.Stage != changes.Decided || d.Verdict == nil || *d.Verdict != changes.Improved || d.Interim == nil || d.Interim.Outcome != changes.Improved {
		t.Fatalf("change = %+v, want an improved interim read and an improved verdict", d.Change)
	}
	f := d.Final
	if f.After.Days != 31 || f.Before.Days != 31 || len(f.Controls) != 1 || f.Controls[0] != steady || f.Deltas.Position != -6 || f.Deltas.ImpressionsRatio != 3 {
		t.Fatalf("final read = %+v, want 31 days a side against docs only, position -6 and impressions ×3", f)
	}
	if d.Interim.After.Days != 17 || d.Before == nil || d.Before.Impressions != 3100 || d.Before.Position != 12 {
		t.Fatalf("interim %+v, before %+v; want a 17-day interim and 31 days at 100 impressions before", d.Interim, d.Before)
	}
	var payload []byte
	if n := e.count(`SELECT count(*) FROM alert WHERE kind = 'change_verdict'`); n != 1 {
		t.Fatalf("%d change_verdict alerts after two passes, want 1", n)
	}
	if err := e.db.QueryRow(e.ctx, `SELECT payload FROM alert WHERE kind = 'change_verdict'`).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	var p notify.ChangeVerdictPayload
	if err := json.Unmarshal(payload, &p); err != nil || p.ChangeID != id || p.Verdict != "improved" {
		t.Fatalf("change_verdict payload %s", payload)
	}
	if n := e.count(`SELECT count(*) FROM job_run WHERE job IN ('recrawl_watch', 'verdicts') AND status = 'completed'`); n != 4 {
		t.Fatalf("%d completed recrawl_watch and verdicts jobs, want 4", n)
	}
}

func TestOnlyManualChangesAreDeleted(t *testing.T) {
	e := newEnv(t, "UTC")
	e.connect()
	e.gh.AddPull(githubtest.Pull{Number: 5, Title: "Blog", MergedAt: ago(e.now, time.Hour), Updated: e.now.Add(-time.Hour), Files: []githubtest.File{{Name: "content/blog/a.mdx", Status: "added"}}})
	if err := e.svc.Poll(e.ctx); err != nil {
		t.Fatal(err)
	}
	manual := e.seedChange(site+"contact", e.now.Add(-time.Hour))
	for _, c := range e.list() {
		err := e.svc.DeleteChange(e.ctx, c.ID)
		if (c.ID == manual) != (err == nil) {
			t.Errorf("deleting %s change %d: %v", c.Source, c.ID, err)
		}
	}
}
