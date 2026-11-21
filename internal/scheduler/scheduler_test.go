package scheduler_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/unkeyed/byname/internal/checks"
	"github.com/unkeyed/byname/internal/checks/checkstest"
	"github.com/unkeyed/byname/internal/config"
	"github.com/unkeyed/byname/internal/engines/tregtest"
	"github.com/unkeyed/byname/internal/scheduler"
	"github.com/unkeyed/byname/internal/secretbox"
	"github.com/unkeyed/byname/internal/sources"
	"github.com/unkeyed/byname/internal/testdb"
)

var now = time.Date(2026, 11, 21, 9, 0, 0, 0, time.UTC)

type env struct {
	db   *pgxpool.Pool
	treg *tregtest.Server
}

func setup(t *testing.T) *env {
	t.Helper()
	e := &env{db: testdb.New(t), treg: tregtest.New(t)}
	checkstest.Seed(t, e.process(nil).cfg, nil)
	return e
}

type process struct {
	cfg    *config.Service
	checks *checks.Service
	sched  *scheduler.Scheduler
}

// process is one Byname process on the shared database.
func (e *env) process(ticks <-chan time.Time) process {
	clock := func() time.Time { return now }
	cfg := config.New(e.db, config.Options{Secrets: secretbox.ForTest(), TregBaseURL: e.treg.URL, Now: clock})
	c := checks.New(e.db, cfg, checks.Options{Now: clock})
	return process{cfg, c, scheduler.New(c, cfg, scheduler.Options{Ticks: ticks, Now: clock})}
}

func (e *env) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := e.db.Exec(context.Background(), sql, args...); err != nil {
		t.Fatal(err)
	}
}

func (e *env) count(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := e.db.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestConcurrentSchedulersStartOneRun(t *testing.T) {
	e := setup(t)
	e.exec(t, `UPDATE instance_settings SET next_check_at = $1`, now.Add(-time.Minute))
	var wg sync.WaitGroup
	for range 2 {
		p := e.process(nil)
		wg.Go(func() {
			for range 3 {
				p.sched.Tick(context.Background())
			}
		})
	}
	wg.Wait()
	if n := e.count(t, `SELECT count(*) FROM run`); n != 1 {
		t.Fatalf("%d runs, want exactly 1 from two schedulers ticking together", n)
	}
	if n := e.count(t, `SELECT count(*) FROM run WHERE status = 'completed' AND trigger = 'scheduled'`); n != 1 {
		t.Fatal("the scheduled run did not complete")
	}
	if n := e.count(t, `SELECT count(*) FROM answer`); n != 12 || e.treg.Answers() != 12 {
		t.Fatalf("%d answers from %d calls, want 12 of each", n, e.treg.Answers())
	}
	if n := e.count(t, `SELECT count(*) FROM job_run WHERE job = 'ai_check'`); n != 1 {
		t.Fatalf("%d ai_check job rows, want 1", n)
	}
}

func TestMissedChecksCatchUpOnce(t *testing.T) {
	e := setup(t)
	e.exec(t, `UPDATE instance_settings SET last_check_at = $1, next_check_at = $2`, now.AddDate(0, 0, -12), now.AddDate(0, 0, -9))
	p := e.process(nil)
	for range 3 {
		p.sched.Tick(context.Background())
	}
	if n := e.count(t, `SELECT count(*) FROM run`); n != 1 {
		t.Fatalf("%d runs after a 12-day outage, want 1", n)
	}
	s, err := p.cfg.Settings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 11, 24, 6, 0, 0, 0, time.UTC); s.NextCheckAt == nil || !s.NextCheckAt.Equal(want) || !s.LastCheckAt.Equal(now) {
		t.Fatalf("next %v last %v, want next %v three days after the catch-up", s.NextCheckAt, s.LastCheckAt, want)
	}
}

func TestPausedChecksDoNotRun(t *testing.T) {
	e := setup(t)
	e.exec(t, `UPDATE instance_settings SET next_check_at = $1, checks_paused = true`, now.Add(-time.Minute))
	e.process(nil).sched.Tick(context.Background())
	if n := e.count(t, `SELECT count(*) FROM run`); n != 0 {
		t.Fatalf("%d runs while paused, want 0", n)
	}
}

func TestRunNowRunsOnTheSchedulerWithoutMovingTheSchedule(t *testing.T) {
	e := setup(t)
	next := now.Add(48 * time.Hour)
	e.exec(t, `UPDATE instance_settings SET next_check_at = $1`, next)
	p := e.process(make(chan time.Time))
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { p.sched.Run(ctx); close(done) }()
	defer func() { stop(); <-done }()

	run, created, err := p.checks.RunNow(context.Background())
	if err != nil || !created {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for e.count(t, `SELECT count(*) FROM run WHERE id = $1 AND status = 'completed'`, run.ID) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the run now run did not complete on the scheduler")
		}
		time.Sleep(20 * time.Millisecond)
	}
	s, _ := p.cfg.Settings(context.Background())
	if !s.NextCheckAt.Equal(next) || s.LastCheckAt != nil {
		t.Fatalf("next %v last %v, want the schedule untouched by run now", s.NextCheckAt, s.LastCheckAt)
	}
}

func TestStartResumesARunLeftRunning(t *testing.T) {
	e := setup(t)
	first := e.process(nil)
	run, _, err := first.checks.RunNow(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	e.exec(t, `UPDATE run SET status = 'running', started_at = $1 WHERE id = $2`, now, run.ID)

	p := e.process(make(chan time.Time))
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { p.sched.Run(ctx); close(done) }()
	defer func() { stop(); <-done }()
	deadline := time.Now().Add(10 * time.Second)
	for e.count(t, `SELECT count(*) FROM run WHERE id = $1 AND status = 'completed'`, run.ID) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("start did not resume the running run")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if n := e.count(t, `SELECT count(*) FROM run`); n != 1 {
		t.Fatalf("%d runs, want the one resumed", n)
	}
}

func TestTickFetchesWhatTheCheckCited(t *testing.T) {
	e := setup(t)
	p := e.process(nil)
	clock := func() time.Time { return now }
	src := sources.New(e.db, p.cfg, sources.Options{Now: clock, Timeout: time.Second, HostDelay: time.Nanosecond})
	sched := scheduler.New(p.checks, p.cfg, scheduler.Options{Now: clock, Sources: src})
	if _, _, err := p.checks.RunNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	sched.Tick(context.Background())
	cited := e.count(t, `SELECT count(DISTINCT url) FROM citation`)
	if got := e.count(t, `SELECT count(*) FROM source_page`); cited == 0 || got != cited {
		t.Fatalf("source_page has %d rows, want one per cited URL (%d)", got, cited)
	}
	if n := e.count(t, `SELECT count(*) FROM job_run WHERE job = 'source_fetch' AND status = 'completed'`); n != 1 {
		t.Errorf("completed source_fetch jobs = %d, want 1", n)
	}
}
