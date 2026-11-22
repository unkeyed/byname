package logos_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/unkeyed/byname/internal/checks"
	"github.com/unkeyed/byname/internal/checks/checkstest"
	"github.com/unkeyed/byname/internal/config"
	"github.com/unkeyed/byname/internal/discovery"
	"github.com/unkeyed/byname/internal/engines"
	"github.com/unkeyed/byname/internal/engines/tregtest"
	"github.com/unkeyed/byname/internal/logos"
	"github.com/unkeyed/byname/internal/secretbox"
	"github.com/unkeyed/byname/internal/testdb"
)

var ctx = context.Background()

// png is a 1x1 image.
var png, _ = base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==")

type env struct {
	db     *pgxpool.Pool
	treg   *tregtest.Server
	config *config.Service
	found  *discovery.Service
	logos  *logos.Service
	images *httptest.Server
	now    time.Time
}

// setup seeds an instance, runs a check that finds Litware Cloud and
// Fabrikam Deploy, and points Context.dev's answers at a fake image host.
func setup(t *testing.T) *env {
	t.Helper()
	e := &env{db: testdb.New(t), treg: tregtest.New(t), now: time.Now()}
	e.images = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/logo.png" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(png)
	}))
	t.Cleanup(e.images.Close)
	e.config = config.New(e.db, config.Options{Secrets: secretbox.ForTest(), TregBaseURL: e.treg.URL})
	checkstest.Seed(t, e.config, e.treg, nil)
	e.found = discovery.New(e.db, e.config, discovery.Options{})
	svc := checks.New(e.db, e.config, checks.Options{Finder: e.found})
	if _, _, err := svc.RunNow(ctx); err != nil {
		t.Fatal(err)
	}
	if err := svc.Work(ctx); err != nil {
		t.Fatal(err)
	}
	e.logos = logos.New(e.db, e.config, logos.Options{Now: func() time.Time { return e.now }})
	e.answer(engines.BrandByDomain.ID, "logo.png")
	e.answer(engines.BrandByName.ID, "logo.png")
	e.treg.Reset()
	return e
}

// answer makes Context.dev answer tool with a record whose logos are a
// dark wordmark and the icon at path, which should be the one kept.
func (e *env) answer(tool, path string) {
	e.treg.SetBody(tool, fmt.Appendf(nil, `{"status":"ok","brand":{"domain":"found.example","title":"Found",
		"colors":[{"hex":"#543CFC","name":"Meteor Shower"}],
		"logos":[{"url":"%[1]s/wordmark.svg","mode":"dark","type":"logo","resolution":{"width":400,"height":100}},
			{"url":"%[1]s/%[2]s","mode":"light","type":"icon","resolution":{"width":256,"height":256}}]},
		"code":200,"key_metadata":{"credits_consumed":10}}`, e.images.URL, path))
}

func (e *env) count(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := e.db.QueryRow(ctx, sql, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (e *env) limit(t *testing.T, n int, on bool) {
	t.Helper()
	if _, err := e.found.PutSettings(ctx, discovery.SettingsInput{Logos: &on, LogoMonthLimit: &n}); err != nil {
		t.Fatal(err)
	}
}

func (e *env) brandID(t *testing.T, name string) int64 {
	t.Helper()
	var id int64
	if err := e.db.QueryRow(ctx, `SELECT id FROM brand WHERE name = $1`, name).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// Each brand is looked up once, yours and your competitors first, by
// domain when it has one and by name otherwise, at most the monthly limit
// of new brands, and every lookup's cost is counted as spend.
func TestEachBrandIsLookedUpOnceWithinTheMonthlyLimit(t *testing.T) {
	e := setup(t)
	e.limit(t, 3, true)
	if err := e.logos.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if got := e.count(t, `SELECT count(*) FROM brand_logo`); got != 3 {
		t.Fatalf("logos = %d, want the limit of 3", got)
	}
	for _, name := range []string{"Northwind", "Contoso", "Litware Cloud"} {
		if e.count(t, `SELECT count(*) FROM brand_logo WHERE brand_id = $1`, e.brandID(t, name)) != 1 {
			t.Errorf("%s has no logo; yours, your competitor and the domain-known found brand come first", name)
		}
	}
	img, err := e.logos.Image(ctx, e.brandID(t, "Northwind"))
	if err != nil || img.MIME != "image/png" || !bytes.Equal(img.Bytes, png) {
		t.Fatalf("image = %s %d bytes, %v; want the stored icon", img.MIME, len(img.Bytes), err)
	}
	list, _ := e.logos.Logos(ctx)
	if len(list) != 3 || list[0].Color != "#543cfc" || list[0].Version == 0 {
		t.Errorf("logos = %+v, want three with the colour lowercased", list)
	}
	for _, c := range e.treg.Calls() {
		if c.Tool != engines.BrandByDomain.ID {
			t.Errorf("called %s; every brand so far has a domain", c.Tool)
		}
	}
	var spent float64
	if err := e.db.QueryRow(ctx, `SELECT sum(usd)::float8 FROM spend WHERE source = 'brands' AND run_id IS NULL`).Scan(&spent); err != nil || spent != 0.03 {
		t.Errorf("lookup spend = %v, %v; want 3 lookups at the fake's $0.01", spent, err)
	}
	st, _ := e.found.Settings(ctx)
	if st.Logos.ThisMonth != 3 || st.Logos.MonthLimit != 3 || st.Logos.CostPerBrandUSD != 0.01 || st.Logos.MonthMaxUSD != 0.03 {
		t.Errorf("settings = %+v, want 3 of 3 used at $0.01 each", st.Logos)
	}

	e.treg.Reset()
	if err := e.logos.Tick(ctx); err != nil || len(e.treg.Calls()) != 0 {
		t.Fatalf("a second tick made %d calls, %v; want none past the monthly limit", len(e.treg.Calls()), err)
	}
	e.limit(t, 10, true)
	if err := e.logos.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	calls := e.treg.Calls()
	if len(calls) != 1 || calls[0].Tool != engines.BrandByName.ID {
		t.Fatalf("calls = %+v, want one lookup by name for Fabrikam Deploy, which has no domain", calls)
	}
	var domains []string
	if err := e.db.QueryRow(ctx, `SELECT domains FROM brand WHERE name = 'Fabrikam Deploy'`).Scan(&domains); err != nil || len(domains) != 1 || domains[0] != "found.example" {
		t.Errorf("Fabrikam Deploy's domains = %v, %v; want the domain Context.dev found", domains, err)
	}
	e.treg.Reset()
	if err := e.logos.Tick(ctx); err != nil || len(e.treg.Calls()) != 0 {
		t.Errorf("a tick with every brand looked up made %d calls, %v", len(e.treg.Calls()), err)
	}
	if got := e.count(t, `SELECT count(*) FROM job_run WHERE job = 'brand_logos'`); got != 2 {
		t.Errorf("brand_logos jobs = %d, want one per tick that looked something up", got)
	}
}

// A brand Context.dev has nothing for is never asked about again; a lookup
// whose image would not download is tried again a day later.
func TestMissesAreFinalAndFailuresRetryNextDay(t *testing.T) {
	e := setup(t)
	e.treg.SetBody(engines.BrandByDomain.ID, []byte(`{"status":"error","message":"not found","code":404}`))
	e.answer(engines.BrandByName.ID, "missing.png")
	if err := e.logos.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if got := e.count(t, `SELECT count(*) FROM brand_call WHERE status = 'not_found'`); got != 3 {
		t.Errorf("misses = %d, want the 3 brands with a domain", got)
	}
	if got := e.count(t, `SELECT count(*) FROM brand_call WHERE status = 'failed' AND error LIKE '%HTTP 404%'`); got != 1 {
		t.Errorf("failed lookups = %d, want Fabrikam Deploy's, whose image is missing", got)
	}
	st, _ := e.found.Settings(ctx)
	if st.Logos.ThisMonth != 0 {
		t.Errorf("this month = %d, want 0: misses and failures are not new brands", st.Logos.ThisMonth)
	}
	e.answer(engines.BrandByDomain.ID, "logo.png")
	e.answer(engines.BrandByName.ID, "logo.png")
	e.treg.Reset()
	if err := e.logos.Tick(ctx); err != nil || len(e.treg.Calls()) != 0 {
		t.Fatalf("the next tick made %d calls, %v; want none within a day", len(e.treg.Calls()), err)
	}
	e.now = e.now.Add(25 * time.Hour)
	if err := e.logos.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if calls := e.treg.Calls(); len(calls) != 1 || calls[0].Tool != engines.BrandByName.ID {
		t.Errorf("a day later: %+v, want only the failed brand tried again", calls)
	}
}

func TestNothingIsLookedUpWhileOffOrOverTheMonthlyCap(t *testing.T) {
	e := setup(t)
	e.limit(t, 100, false)
	if err := e.logos.Tick(ctx); err != nil || len(e.treg.Calls()) != 0 {
		t.Fatalf("with logos off: %d calls, %v", len(e.treg.Calls()), err)
	}
	e.limit(t, 100, true)
	if _, err := e.db.Exec(ctx, `UPDATE instance_settings SET spend_cap_month_usd = (SELECT sum(usd) FROM spend) + 0.015`); err != nil {
		t.Fatal(err)
	}
	if err := e.logos.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	var status, msg string
	if err := e.db.QueryRow(ctx, `SELECT status, error FROM job_run WHERE job = 'brand_logos'`).Scan(&status, &msg); err != nil {
		t.Fatal(err)
	}
	if got := len(e.treg.Calls()); got != 2 || status != "capped" {
		t.Errorf("%d lookups, job %s (%s); want capped after 2 lookups of $0.01 crossed the $0.015 left", got, status, msg)
	}
}
