package sources_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/unkeyed/byname/internal/checks/checkstest"
	"github.com/unkeyed/byname/internal/config"
	"github.com/unkeyed/byname/internal/engines/tregtest"
	"github.com/unkeyed/byname/internal/secretbox"
	"github.com/unkeyed/byname/internal/sources"
	"github.com/unkeyed/byname/internal/testdb"
)

var ctx = context.Background()

type env struct {
	db     *pgxpool.Pool
	config *config.Service
	now    time.Time
}

func setup(t *testing.T) *env {
	t.Helper()
	e := &env{db: testdb.New(t), now: time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)}
	treg := tregtest.New(t)
	e.config = config.New(e.db, config.Options{Secrets: secretbox.ForTest(), TregBaseURL: treg.URL})
	checkstest.Seed(t, e.config, treg, nil)
	return e
}

// service is a fetcher allowed to reach the httptest sites on 127.0.0.1,
// with no pause between requests to one host.
func (e *env) service(o sources.Options) *sources.Service {
	o.AllowPrivate = true
	if o.HostDelay == 0 {
		o.HostDelay = time.Nanosecond
	}
	o.Now = func() time.Time { return e.now }
	return sources.New(e.db, e.config, o)
}

// cite stores one finished run whose answer cites urls.
func (e *env) cite(t *testing.T, urls ...string) {
	t.Helper()
	var answer int64
	err := e.db.QueryRow(ctx, `WITH r AS (
			INSERT INTO run (question_set_id, trigger, status) SELECT id, 'run_now', 'completed' FROM question_set LIMIT 1 RETURNING id, question_set_id
		)
		INSERT INTO answer (run_id, question_id, surface_model_id, endpoint, fidelity, status, attempts, mentioned, cited)
		SELECT r.id, (SELECT id FROM question WHERE set_id = r.question_set_id ORDER BY ord LIMIT 1), (SELECT min(id) FROM surface_model),
			'test', 'app', 'ok', 1, false, false FROM r RETURNING id`).Scan(&answer)
	if err != nil {
		t.Fatal(err)
	}
	for i, u := range urls {
		if _, err := e.db.Exec(ctx, `INSERT INTO citation (answer_id, ord, url, domain, is_self) VALUES ($1, $2, $3, '', false)`, answer, i, u); err != nil {
			t.Fatal(err)
		}
	}
}

type row struct {
	status  string
	self    *bool
	brands  []string
	err     string
	fetched time.Time
	hash    *string
}

func (e *env) page(t *testing.T, url string) row {
	t.Helper()
	var r row
	var errText *string
	if err := e.db.QueryRow(ctx, `SELECT status, self_present, brands_present, error, fetched_at, content_hash FROM source_page WHERE url = $1`, url).
		Scan(&r.status, &r.self, &r.brands, &errText, &r.fetched, &r.hash); err != nil {
		t.Fatalf("source_page %s: %v", url, err)
	}
	if errText != nil {
		r.err = *errText
	}
	return r
}

func (e *env) count(t *testing.T, sql string) int {
	t.Helper()
	var n int
	if err := e.db.QueryRow(ctx, sql).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

type site map[string]func(w http.ResponseWriter, r *http.Request)

func serve(t *testing.T, s site) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h, ok := s[r.URL.Path]; ok {
			h(w, r)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func html(body string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, body)
	}
}

func TestBrandsOnPage(t *testing.T) {
	e := setup(t)
	var agents []string
	var mu sync.Mutex
	srv := serve(t, site{
		"/both": func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			agents = append(agents, r.UserAgent())
			mu.Unlock()
			html(`<html><head><title>Gateways compared</title></head><body><p>Northwind&nbsp;Cloud and <b>Contoso</b> both work.</p></body></html>`)(w, r)
		},
		"/competitor-only": html(`<body><p>Use Contoso Gateway.</p><script>var x = "Northwind";</script><!-- Northwind --><a href="https://northwind.example">docs</a></body>`),
		"/lowercase":       html(`<body>northwind and contoso in lower case are not names.</body>`),
	})
	e.cite(t, srv.URL+"/both", srv.URL+"/competitor-only", srv.URL+"/lowercase")

	n, err := e.service(sources.Options{UserAgent: "Byname/test"}).Run(ctx)
	if err != nil || n != 3 {
		t.Fatalf("Run = %d, %v; want 3 pages fetched", n, err)
	}
	both := e.page(t, srv.URL+"/both")
	if both.status != "ok" || both.self == nil || !*both.self || !slices.Equal(both.brands, []string{"Contoso"}) || both.hash == nil {
		t.Errorf("/both = %+v; want ok, you present, Contoso named, a content hash", both)
	}
	comp := e.page(t, srv.URL+"/competitor-only")
	if comp.status != "ok" || comp.self == nil || *comp.self || !slices.Equal(comp.brands, []string{"Contoso"}) {
		t.Errorf("/competitor-only = %+v; want you absent (a script, a comment and a link are not text) and Contoso named", comp)
	}
	lower := e.page(t, srv.URL+"/lowercase")
	if lower.self == nil || *lower.self || len(lower.brands) != 0 {
		t.Errorf("/lowercase = %+v; want nothing matched under whole-word, case-sensitive matching", lower)
	}
	if len(agents) != 1 || agents[0] != "Byname/test" {
		t.Errorf("user agents = %v, want Byname/test", agents)
	}
	if got := e.count(t, `SELECT count(*) FROM job_run WHERE job = 'source_fetch' AND status = 'completed' AND finished_at IS NOT NULL`); got != 1 {
		t.Errorf("completed source_fetch jobs = %d, want 1", got)
	}
}

func TestSelfAnywhereMatchesLikeAnswers(t *testing.T) {
	e := setup(t)
	s := checkstest.Settings()
	s.MatchMode = "self_anywhere"
	if _, err := e.config.PutSettings(ctx, s); err != nil {
		t.Fatal(err)
	}
	srv := serve(t, site{"/p": html(`<p>try NORTHWIND today</p>`)})
	e.cite(t, srv.URL+"/p")
	if _, err := e.service(sources.Options{}).Run(ctx); err != nil {
		t.Fatal(err)
	}
	if p := e.page(t, srv.URL+"/p"); p.self == nil || !*p.self {
		t.Errorf("page = %+v; want you present, as an answer's text would be under self_anywhere", p)
	}
}

func TestLimits(t *testing.T) {
	e := setup(t)
	srv := serve(t, site{
		"/robots.txt": func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprint(w, "User-agent: *\nDisallow: /\n\nUser-agent: byname\nDisallow: /private\nAllow: /private/ok\n")
		},
		"/private/page": html(`<p>Northwind</p>`),
		"/private/ok":   html(`<p>Northwind</p>`),
		"/big":          html(strings.Repeat("Northwind ", 400)),
		"/slow": func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-time.After(2 * time.Second):
			case <-r.Context().Done():
			}
			html(`<p>Northwind</p>`)(w, r)
		},
		"/paper.pdf": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/pdf")
			fmt.Fprint(w, "%PDF Northwind")
		},
	})
	e.cite(t, srv.URL+"/private/page", srv.URL+"/private/ok", srv.URL+"/big", srv.URL+"/slow", srv.URL+"/paper.pdf", srv.URL+"/gone", "ftp://files.example/x")

	if _, err := e.service(sources.Options{MaxBody: 1024, Timeout: 300 * time.Millisecond}).Run(ctx); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]struct{ status, err string }{
		"/private/page": {"disallowed", "robots.txt"},
		"/private/ok":   {"ok", ""},
		"/big":          {"failed", "larger than 1 KiB"},
		"/slow":         {"failed", "timed out"},
		"/paper.pdf":    {"failed", "application/pdf"},
		"/gone":         {"failed", "HTTP 404"},
	} {
		p := e.page(t, srv.URL+path)
		if p.status != want.status || !strings.Contains(p.err, want.err) {
			t.Errorf("%s = %s %q, want %s with %q", path, p.status, p.err, want.status, want.err)
		}
		if p.status != "ok" && p.self != nil {
			t.Errorf("%s: self_present = %v, want null when not read", path, *p.self)
		}
	}
	if p := e.page(t, "ftp://files.example/x"); p.status != "refused" {
		t.Errorf("ftp URL = %+v, want refused", p)
	}
}

func TestPrivateAddressesRefusedByDefault(t *testing.T) {
	e := setup(t)
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		fmt.Fprint(w, "Northwind")
	}))
	t.Cleanup(srv.Close)
	e.cite(t, srv.URL+"/page", strings.Replace(srv.URL, "127.0.0.1", "localhost", 1)+"/page")

	strict := sources.New(e.db, e.config, sources.Options{Now: func() time.Time { return e.now }})
	if _, err := strict.Run(ctx); err != nil {
		t.Fatal(err)
	}
	for _, u := range []string{srv.URL + "/page", strings.Replace(srv.URL, "127.0.0.1", "localhost", 1) + "/page"} {
		if p := e.page(t, u); p.status != "refused" || !strings.Contains(p.err, "private, loopback") {
			t.Errorf("%s = %+v, want refused by the address guard", u, p)
		}
	}
	if hits.Load() != 0 {
		t.Errorf("the loopback site was hit %d times, want none", hits.Load())
	}
}

func TestOneRequestAtATimePerHost(t *testing.T) {
	e := setup(t)
	type counter struct{ now, peak atomic.Int32 }
	var global counter
	var urls []string
	var sites []*counter
	for range 3 {
		c := &counter{}
		sites = append(sites, c)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for _, k := range []*counter{c, &global} {
				n := k.now.Add(1)
				for p := k.peak.Load(); n > p && !k.peak.CompareAndSwap(p, n); p = k.peak.Load() {
				}
			}
			time.Sleep(40 * time.Millisecond)
			c.now.Add(-1)
			global.now.Add(-1)
			if r.URL.Path == "/robots.txt" {
				http.NotFound(w, r)
				return
			}
			html(`<p>Contoso</p>`)(w, r)
		}))
		t.Cleanup(srv.Close)
		for i := range 4 {
			urls = append(urls, fmt.Sprintf("%s/p%d", srv.URL, i))
		}
	}
	e.cite(t, urls...)
	n, err := e.service(sources.Options{Hosts: 2}).Run(ctx)
	if err != nil || n != 12 {
		t.Fatalf("Run = %d, %v; want 12", n, err)
	}
	for i, c := range sites {
		if c.peak.Load() != 1 {
			t.Errorf("site %d saw %d requests at once, want 1", i, c.peak.Load())
		}
	}
	if p := global.peak.Load(); p > 2 {
		t.Errorf("%d requests at once across sites, want at most Hosts = 2", p)
	}
}

func TestRefetchIsIdempotent(t *testing.T) {
	e := setup(t)
	body := `<p>Contoso</p>`
	var mu sync.Mutex
	var hits int
	srv := serve(t, site{"/p": func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		hits++
		html(body)(w, r)
	}})
	e.cite(t, srv.URL+"/p")
	e.cite(t, srv.URL+"/p")
	svc := e.service(sources.Options{RefetchAfter: 7 * 24 * time.Hour})

	if n, err := svc.Run(ctx); err != nil || n != 1 {
		t.Fatalf("first round = %d, %v; want the URL cited twice fetched once", n, err)
	}
	first := e.page(t, srv.URL+"/p")
	e.now = e.now.Add(6 * 24 * time.Hour)
	if n, err := svc.Run(ctx); err != nil || n != 0 {
		t.Fatalf("second round = %d, %v; want nothing due", n, err)
	}
	if jobs := e.count(t, `SELECT count(*) FROM job_run WHERE job = 'source_fetch'`); jobs != 1 {
		t.Errorf("source_fetch jobs = %d, want 1: a round with nothing due logs nothing", jobs)
	}

	mu.Lock()
	body = `<p>Contoso and Northwind</p>`
	mu.Unlock()
	e.now = e.now.Add(2 * 24 * time.Hour)
	if n, err := svc.Run(ctx); err != nil || n != 1 {
		t.Fatalf("refresh round = %d, %v; want the stale page fetched again", n, err)
	}
	again := e.page(t, srv.URL+"/p")
	if !again.fetched.After(first.fetched) || again.self == nil || !*again.self || *again.hash == *first.hash {
		t.Errorf("after refresh = %+v, before %+v; want a later fetch that now finds you, with a new hash", again, first)
	}
	if rows := e.count(t, `SELECT count(*) FROM source_page`); rows != 1 || hits != 2 {
		t.Errorf("source_page rows = %d, page hits = %d; want one row and two fetches", rows, hits)
	}
}

func TestInterruptedRoundIsClosed(t *testing.T) {
	e := setup(t)
	if _, err := e.db.Exec(ctx, `INSERT INTO job_run (job, trigger, status) VALUES ('source_fetch', 'scheduled', 'running')`); err != nil {
		t.Fatal(err)
	}
	if _, err := e.service(sources.Options{}).Run(ctx); err != nil {
		t.Fatal(err)
	}
	if n := e.count(t, `SELECT count(*) FROM job_run WHERE job = 'source_fetch' AND status = 'running'`); n != 0 {
		t.Errorf("%d source_fetch jobs still running, want the interrupted one marked failed", n)
	}
}
