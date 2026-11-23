// Package checks runs AI checks: it queues a run (on schedule or Run now),
// asks every question on every active surface and model pair through Treg,
// scores each answer and stores it with its cost, mentions and citations.
//
// A run moves queued -> running -> completed | capped | refused | failed.
// Only one run executes at a time across every process sharing the
// database. The process executing it holds a lease on the run row and
// renews it while it works, so a process that dies, hangs or is replaced
// stops renewing and the next one takes the run over once the lease
// passes. A resumed run skips every pair that already has an answer, so it
// never asks twice and never starts a second run.
package checks

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"github.com/unkeyed/byname/internal/db"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/unkeyed/byname/internal/config"
	"github.com/unkeyed/byname/internal/core"
)

type Status string

const (
	Queued    Status = "queued"
	Running   Status = "running"
	Completed Status = "completed"
	// Capped: the run or month spend cap stopped it queueing calls.
	Capped Status = "capped"
	// Refused: Treg refused a call on its own behalf, such as for an
	// insufficient balance, and the run stopped.
	Refused Status = "refused"
	Failed  Status = "failed"
)

type Trigger string

const (
	Scheduled Trigger = "scheduled"
	RunNow    Trigger = "run_now"
)

// Run is a queued or finished check, as Run now reports it.
type Run struct {
	ID            int64     `json:"id"`
	Status        Status    `json:"status"`
	Trigger       Trigger   `json:"trigger"`
	QuestionSetID int64     `json:"question_set_id"`
	QueuedAt      time.Time `json:"queued_at"`
}

type Service struct {
	db     db.Pool
	config *config.Service
	now    func() time.Time
	sleep  func(context.Context, time.Duration) error
	log    *slog.Logger
	wake   chan struct{}
	alerts Alerts
	finder Finder
	// writer lets one answer at a time be stored.
	writer sync.Mutex
	// owner names this process on the leases it takes, and lease is how long
	// one holds without a renewal.
	owner string
	lease time.Duration
}

// Finder reads each stored answer for the brands it names that nobody
// tracks (internal/discovery). Find records every outcome itself and
// never fails the check; its cost counts against the check's spend cap.
type Finder interface {
	Ready(ctx context.Context) (bool, error)
	Find(ctx context.Context, answerID, runID int64, text string) error
}

// Alerts hears about every finished run, so it can tell people
// (internal/notify). It must be idempotent: a run can be reported again.
type Alerts interface {
	RunFinished(ctx context.Context, runID int64) error
}

type Options struct {
	Now func() time.Time
	// Sleep waits out Treg's Retry-After. Tests replace it.
	Sleep func(context.Context, time.Duration) error
	Log   *slog.Logger
	// Alerts, when set, hears about each run as it finishes.
	Alerts Alerts
	// Finder, when set, reads each answer for other brands.
	Finder Finder
	// Lease is how long a claim on a run holds without a renewal, 90 seconds
	// unless set. Tests shorten it.
	Lease time.Duration
}

func New(db db.Pool, cfg *config.Service, o Options) *Service {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Sleep == nil {
		o.Sleep = sleep
	}
	if o.Log == nil {
		o.Log = slog.Default()
	}
	if o.Lease == 0 {
		o.Lease = 90 * time.Second
	}
	return &Service{db: db, config: cfg, now: o.Now, sleep: o.Sleep, log: o.Log, wake: make(chan struct{}, 1), alerts: o.Alerts, finder: o.Finder,
		owner: rand.Text(), lease: o.Lease}
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Wake fires after Run now queues a run, so this process's scheduler picks
// it up without waiting for its next tick.
func (s *Service) Wake() <-chan struct{} { return s.wake }

// Lock keys, each held for one short transaction. claimLock serializes
// creating runs, leaseLock taking a lease on one.
const (
	claimLock = `hashtext('byname.checks.claim')`
	leaseLock = `hashtext('byname.checks.lease')`
)

const runColumns = `id, status, trigger, question_set_id, queued_at`

func scanRun(row pgx.Row) (Run, error) {
	var r Run
	return r, row.Scan(&r.ID, &r.Status, &r.Trigger, &r.QuestionSetID, &r.QueuedAt)
}

// RunNow queues a check without moving the schedule. When a run is already
// queued or running it returns that run instead, with created false, so
// pressing Run now twice never starts two.
func (s *Service) RunNow(ctx context.Context) (Run, bool, error) {
	if err := s.config.Ready(ctx); err != nil {
		return Run{}, false, err
	}
	var run Run
	created := false
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(`+claimLock+`)`); err != nil {
			return err
		}
		var err error
		run, err = scanRun(tx.QueryRow(ctx, `SELECT `+runColumns+` FROM run WHERE status IN ('queued', 'running') ORDER BY id LIMIT 1`))
		if err != pgx.ErrNoRows {
			return err
		}
		created = true
		run, err = queue(ctx, tx, RunNow)
		return err
	})
	if created {
		select {
		case s.wake <- struct{}{}:
		default:
		}
	}
	return run, created, err
}

func queue(ctx context.Context, tx pgx.Tx, trigger Trigger) (Run, error) {
	run, err := scanRun(tx.QueryRow(ctx, `INSERT INTO run (question_set_id, trigger)
		SELECT id, $1 FROM question_set ORDER BY version DESC LIMIT 1 RETURNING `+runColumns, trigger))
	if err == pgx.ErrNoRows {
		return run, fmt.Errorf("%w: no question set; create one with PUT /v1/questions", core.ErrInvalid)
	}
	if err != nil {
		return run, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO job_run (job, trigger, run_id, status) VALUES ('ai_check', $1, $2, 'queued')`, trigger, run.ID)
	return run, err
}

// ClaimDue queues the scheduled check if it is due and checks are not
// paused, and sets the next one. It returns the queued run's id, or 0.
// Under concurrent callers exactly one claims a due check: the claim takes
// an advisory lock and then rechecks next_check_at under a row lock. The
// next check is computed from now, so a process that was down for several
// intervals catches up with one check, not one per missed interval.
func (s *Service) ClaimDue(ctx context.Context) (int64, error) {
	now := s.now()
	settings, err := s.config.Settings(ctx)
	if err != nil {
		return 0, err
	}
	var runID int64
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var locked, paused bool
		var next *time.Time
		if err := tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(`+claimLock+`)`).Scan(&locked); err != nil || !locked {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT checks_paused, next_check_at FROM instance_settings FOR UPDATE`).Scan(&paused, &next); err != nil {
			return err
		}
		if paused || next == nil || next.After(now) {
			return nil
		}
		following := config.NextCheck(now, &now, settings.Schedule())
		if _, err := tx.Exec(ctx, `UPDATE instance_settings SET next_check_at = $1, last_check_at = $2`, following, now); err != nil {
			return err
		}
		skip := func(why string) error {
			_, err := tx.Exec(ctx, `INSERT INTO job_run (job, trigger, status, started_at, finished_at, error)
				VALUES ('ai_check', 'scheduled', 'skipped', $1, $1, $2)`, now, why)
			return err
		}
		var open bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM run WHERE status IN ('queued', 'running'))`).Scan(&open); err != nil {
			return err
		}
		if open {
			return skip("a check was already queued or running")
		}
		if err := s.config.Ready(ctx); err != nil {
			if errors.Is(err, core.ErrInvalid) {
				return skip(err.Error())
			}
			return err
		}
		run, err := queue(ctx, tx, Scheduled)
		runID = run.ID
		return err
	})
	return runID, err
}

// Work executes open runs, oldest first and a running one before any
// queued one, until none is left. It returns at once when another process
// holds a live lease on a run.
func (s *Service) Work(ctx context.Context) error {
	for {
		id, err := s.claim(ctx)
		if err != nil || id == 0 {
			return err
		}
		if err := s.runLeased(ctx, id); err != nil {
			return err
		}
	}
}

// claim takes the lease on the next open run, or returns 0 when there is
// none or another process's lease on one has not passed. Lease times are
// the database's clock, so processes with skewed clocks agree.
func (s *Service) claim(ctx context.Context) (int64, error) {
	var id int64
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(`+leaseLock+`)`); err != nil {
			return err
		}
		err := tx.QueryRow(ctx, `UPDATE run SET lease_owner = $1, lease_until = now() + make_interval(secs => $2)
			WHERE id = (SELECT id FROM run WHERE status IN ('queued', 'running') ORDER BY status = 'running' DESC, id LIMIT 1)
				AND NOT EXISTS (SELECT 1 FROM run WHERE status IN ('queued', 'running') AND lease_until > now() AND lease_owner <> $1)
			RETURNING id`, s.owner, s.lease.Seconds()).Scan(&id)
		if err == pgx.ErrNoRows {
			return nil
		}
		return err
	})
	return id, err
}

// runLeased executes a run while renewing its lease. When a renewal finds
// the lease taken, or none succeeds for a whole lease, the run is stopped
// where it is, for its new owner to resume. The lease is given back when
// the run ends or this process stops, so the next one need not wait it out.
func (s *Service) runLeased(ctx context.Context, id int64) error {
	work, stop := context.WithCancelCause(ctx)
	defer stop(nil)
	renewed := make(chan struct{})
	go func() {
		defer close(renewed)
		last := time.Now()
		t := time.NewTicker(s.lease / 3)
		defer t.Stop()
		for {
			select {
			case <-work.Done():
				return
			case <-t.C:
			}
			tag, err := s.db.Exec(work, `UPDATE run SET lease_until = now() + make_interval(secs => $3)
				WHERE id = $1 AND lease_owner = $2`, id, s.owner, s.lease.Seconds())
			switch {
			case err == nil && tag.RowsAffected() == 0:
				stop(fmt.Errorf("another process took over check #%d", id))
				return
			case err == nil:
				last = time.Now()
			case time.Since(last) >= s.lease:
				stop(fmt.Errorf("could not renew the lease on check #%d for %s: %w", id, s.lease, err))
				return
			}
		}
	}()
	err := s.RunCheck(work, id)
	stop(nil)
	<-renewed
	if _, rerr := s.db.Exec(context.WithoutCancel(ctx), `UPDATE run SET lease_owner = NULL, lease_until = NULL
		WHERE id = $1 AND lease_owner = $2`, id, s.owner); rerr != nil && err == nil {
		s.log.Warn("give back the lease on a check", "run", id, "err", rerr)
	}
	if cause := context.Cause(work); err != nil && cause != nil && !errors.Is(cause, context.Canceled) {
		return cause
	}
	return err
}

// PruneRaw drops raw responses older than raw_retention_days, when set.
func (s *Service) PruneRaw(ctx context.Context) (int64, error) {
	tag, err := s.db.Exec(ctx, `UPDATE answer SET raw = NULL WHERE raw IS NOT NULL
		AND answered_at < $1::timestamptz - make_interval(days => (SELECT raw_retention_days FROM instance_settings))`, s.now())
	return tag.RowsAffected(), err
}
