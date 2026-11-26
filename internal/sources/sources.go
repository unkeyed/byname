// Package sources fetches the pages AI answers cite and records whether
// each one names you and which other brands it names, for the Cited
// sources tab. Each newly cited URL is fetched once, and every page again
// once its last fetch is older than RefetchAfter.
//
// Fetching is polite: one request at a time per host, a few hosts at once,
// a pause between two requests to one host, timeouts, a body size cap,
// robots.txt honoured for the "byname" token, http(s) only, and no private
// or loopback addresses unless AllowPrivate is set for tests. A round runs
// under an advisory lock, so replicas never fetch the same pages together,
// and it writes job_run only when it has pages to fetch.
package sources

import (
	"context"
	"github.com/unkeyed/byname/internal/db"
	"log/slog"
	"net/url"
	"sort"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/unkeyed/byname/internal/config"
)

// Status is how a page's last fetch ended.
type Status string

const (
	StatusOK         Status = "ok"
	StatusFailed     Status = "failed"     // network error, timeout, HTTP error, too large or not a web page
	StatusDisallowed Status = "disallowed" // robots.txt says no
	StatusRefused    Status = "refused"    // not http(s), or a private or loopback address
)

// robotsToken is the product token robots.txt groups name Byname by.
const robotsToken = "byname"

var lock = db.WorkspaceLock("byname.sources.fetch")

type Options struct {
	// AllowPrivate lets fetches reach private and loopback addresses. Only
	// for tests and local development against a fake site.
	AllowPrivate bool
	// RefetchAfter is how old a page's last fetch gets before it is fetched
	// again. Default 7 days.
	RefetchAfter time.Duration
	// Timeout bounds each request, robots.txt included. Default 15s.
	Timeout time.Duration
	// MaxBody is the largest page read, in bytes. Default 2 MiB.
	MaxBody int64
	// Hosts is how many hosts are fetched from at once. Default 4.
	Hosts int
	// HostDelay is the pause between two requests to one host. Default 1s.
	HostDelay time.Duration
	// PerRound caps the pages one round fetches; the rest wait for the next
	// round. Default 200.
	PerRound int
	// RoundTime is how long a round keeps starting fetches. Default 45s,
	// so a round never holds up the scheduler's next tick for long.
	RoundTime time.Duration
	// UserAgent names Byname without contact details. Default "Byname".
	UserAgent string
	Now       func() time.Time
	Log       *slog.Logger
}

type Service struct {
	db     db.Pool
	config *config.Service
	fetch  *fetcher
	o      Options
}

func New(db db.Pool, cfg *config.Service, o Options) *Service {
	if o.RefetchAfter == 0 {
		o.RefetchAfter = 7 * 24 * time.Hour
	}
	if o.Timeout == 0 {
		o.Timeout = 15 * time.Second
	}
	if o.MaxBody == 0 {
		o.MaxBody = 2 << 20
	}
	if o.Hosts == 0 {
		o.Hosts = 4
	}
	if o.HostDelay == 0 {
		o.HostDelay = time.Second
	}
	if o.PerRound == 0 {
		o.PerRound = 200
	}
	if o.RoundTime == 0 {
		o.RoundTime = 45 * time.Second
	}
	if o.UserAgent == "" {
		o.UserAgent = "Byname"
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Log == nil {
		o.Log = slog.Default()
	}
	return &Service{db: db, config: cfg, o: o,
		fetch: &fetcher{client: newClient(o.AllowPrivate), agent: o.UserAgent, timeout: o.Timeout, maxBody: o.MaxBody}}
}

// Run is one round: it fetches the pages cited but never fetched, then
// those due a refresh, and returns how many it fetched. It returns at once
// when another process is fetching. Running it again straight after finds
// nothing due, so it is safe to call on every scheduler tick.
func (s *Service) Run(ctx context.Context) (int, error) {
	conn, err := s.db.Acquire(ctx)
	if err != nil {
		return 0, err
	}
	defer conn.Release()
	var locked bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(`+lock+`)`).Scan(&locked); err != nil || !locked {
		return 0, err
	}
	defer func() {
		if _, err := conn.Exec(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock(`+lock+`)`); err != nil {
			_ = conn.Conn().Close(context.WithoutCancel(ctx))
		}
	}()

	// Holding the lock, no other round is running: a round still marked
	// running was cut short by a crash or a shutdown.
	if _, err := s.db.Exec(ctx, `UPDATE job_run SET status = 'failed', finished_at = $1, error = 'interrupted before it finished'
		WHERE job = 'source_fetch' AND status = 'running'`, s.o.Now()); err != nil {
		return 0, err
	}
	due, err := s.due(ctx)
	if err != nil || len(due) == 0 {
		return 0, err
	}
	var job int64
	if err := s.db.QueryRow(ctx, `INSERT INTO job_run (job, trigger, status, started_at) VALUES ('source_fetch', 'scheduled', 'running', $1) RETURNING id`,
		s.o.Now()).Scan(&job); err != nil {
		return 0, err
	}
	n, err := s.fetchAll(ctx, due)
	status, msg := "completed", ""
	if err != nil {
		status, msg = "failed", err.Error()
	}
	if _, ferr := s.db.Exec(context.WithoutCancel(ctx), `UPDATE job_run SET status = $2, finished_at = $3, error = nullif($4, '') WHERE id = $1`,
		job, status, s.o.Now(), msg); ferr != nil && err == nil {
		err = ferr
	}
	return n, err
}

// due lists the URLs to fetch this round: never fetched first, then the
// stalest.
func (s *Service) due(ctx context.Context) ([]string, error) {
	rows, err := s.db.Query(ctx, `SELECT url FROM (
			SELECT DISTINCT c.url, NULL::timestamptz AS fetched_at FROM citation c
			WHERE NOT EXISTS (SELECT 1 FROM source_page p WHERE p.url = c.url)
			UNION ALL
			SELECT url, fetched_at FROM source_page WHERE fetched_at < $1
		) d ORDER BY fetched_at NULLS FIRST, url LIMIT $2`, s.o.Now().Add(-s.o.RefetchAfter), s.o.PerRound)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// fetchAll fetches due, one host per worker so a host sees one request at
// a time, and stores each page as it lands.
func (s *Service) fetchAll(ctx context.Context, due []string) (int, error) {
	settings, err := s.config.Settings(ctx)
	if err != nil {
		return 0, err
	}
	brands, err := s.config.PanelBrands(ctx)
	if err != nil {
		return 0, err
	}
	f := *s.fetch
	f.brands, f.mode = brands, settings.MatchMode

	type host struct {
		origin *url.URL
		urls   []*url.URL
	}
	byHost := map[string]*host{}
	var refused []page
	for _, raw := range due {
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			refused = append(refused, page{url: raw, domain: "", status: StatusRefused, brands: []string{}, err: "not an http(s) URL"})
			continue
		}
		key := u.Scheme + "://" + u.Host
		if byHost[key] == nil {
			byHost[key] = &host{origin: &url.URL{Scheme: u.Scheme, Host: u.Host}}
		}
		byHost[key].urls = append(byHost[key].urls, u)
	}
	hosts := make([]*host, 0, len(byHost))
	for _, h := range byHost {
		hosts = append(hosts, h)
	}
	sort.Slice(hosts, func(i, j int) bool { return hosts[i].origin.String() < hosts[j].origin.String() })

	var mu sync.Mutex
	var firstErr error
	fetched := 0
	store := func(p page) {
		err := s.store(ctx, p)
		mu.Lock()
		defer mu.Unlock()
		if err != nil && firstErr == nil {
			firstErr = err
		}
		if err == nil {
			fetched++
		}
	}
	for _, p := range refused {
		store(p)
	}

	deadline := s.o.Now().Add(s.o.RoundTime)
	queue := make(chan *host)
	var wg sync.WaitGroup
	for range min(s.o.Hosts, len(hosts)) {
		wg.Go(func() {
			for h := range queue {
				rules, err := f.robots(ctx, h.origin)
				for i, u := range h.urls {
					if ctx.Err() != nil || s.o.Now().After(deadline) {
						break
					}
					if i > 0 && pause(ctx, s.o.HostDelay) != nil {
						break
					}
					var p page
					if err != nil {
						p = failure(page{url: u.String(), domain: hostOf(u), brands: []string{}}, err)
						if p.status == StatusFailed {
							p.err = "robots.txt could not be read: " + p.err
						}
					} else {
						p = f.fetch(ctx, u, rules)
					}
					if ctx.Err() != nil {
						break
					}
					store(p)
				}
			}
		})
	}
	for _, h := range hosts {
		select {
		case queue <- h:
		case <-ctx.Done():
		}
	}
	close(queue)
	wg.Wait()
	if firstErr == nil {
		firstErr = ctx.Err()
	}
	return fetched, firstErr
}

func (s *Service) store(ctx context.Context, p page) error {
	_, err := s.db.Exec(ctx, `INSERT INTO source_page (url, domain, status, fetched_at, http_status, content_hash, self_present, brands_present, error)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, nullif($9, ''))
		ON CONFLICT (url) DO UPDATE SET domain = $2, status = $3, fetched_at = $4, http_status = $5, content_hash = $6,
			self_present = $7, brands_present = $8, error = nullif($9, '')`,
		p.url, p.domain, p.status, s.o.Now(), p.httpStatus, p.hash, p.self, p.brands, p.err)
	return err
}

func pause(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
