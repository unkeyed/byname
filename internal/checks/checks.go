// Package checks runs AI checks: it queues a run (on schedule or Run now),
// asks every question on every active surface and model pair through Treg,
// scores each answer and stores it with its cost, mentions and citations.
//
// A run moves queued -> running -> completed | capped | refused | failed.
// Only one run executes at a time across every process sharing the
// database, under a session advisory lock held on its own connection, so a
// process that dies mid-run frees the lock with its connection and the next
// one resumes the run. A resumed run skips every pair that already has an
// answer, so it never asks twice and never starts a second run.
package checks

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

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
	db     *pgxpool.Pool
	config *config.Service
	now    func() time.Time
	sleep  func(context.Context, time.Duration) error
	log    *slog.Logger
	wake   chan struct{}
	alerts Alerts
	finder Finder
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
}

func New(db *pgxpool.Pool, cfg *config.Service, o Options) *Service {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Sleep == nil {
		o.Sleep = sleep
	}
	if o.Log == nil {
		o.Log = slog.Default()
	}
	return &Service{db: db, config: cfg, now: o.Now, sleep: o.Sleep, log: o.Log, wake: make(chan struct{}, 1), alerts: o.Alerts, finder: o.Finder}
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

// Lock keys. claimLock serializes creating runs; runLock is held for as
// long as a process executes runs.
const (
	claimLock = `hashtext('byname.checks.claim')`
	runLock   = `hashtext('byname.checks.run')`
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
// is already executing runs. The lock lives on a dedicated connection, so
// if this process dies the lock goes with the connection.
func (s *Service) Work(ctx context.Context) error {
	conn, err := s.db.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	var locked bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(`+runLock+`)`).Scan(&locked); err != nil || !locked {
		return err
	}
	defer func() {
		// A pooled connection that kept the lock would block every other
		// process forever, so one that cannot unlock is closed instead.
		if _, err := conn.Exec(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock(`+runLock+`)`); err != nil {
			_ = conn.Conn().Close(context.WithoutCancel(ctx))
		}
	}()
	for {
		var id int64
		err := s.db.QueryRow(ctx, `SELECT id FROM run WHERE status IN ('queued', 'running')
			ORDER BY status = 'running' DESC, id LIMIT 1`).Scan(&id)
		if err == pgx.ErrNoRows {
			return nil
		}
		if err != nil {
			return err
		}
		if err := s.RunCheck(ctx, id); err != nil {
			return err
		}
	}
}

// PruneRaw drops raw responses older than raw_retention_days, when set.
func (s *Service) PruneRaw(ctx context.Context) (int64, error) {
	tag, err := s.db.Exec(ctx, `UPDATE answer SET raw = NULL WHERE raw IS NOT NULL
		AND answered_at < $1::timestamptz - make_interval(days => (SELECT raw_retention_days FROM instance_settings))`, s.now())
	return tag.RowsAffected(), err
}
