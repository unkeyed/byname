package discovery

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/unkeyed/byname/internal/config"
	"github.com/unkeyed/byname/internal/core"
	"github.com/unkeyed/byname/internal/db"
	"github.com/unkeyed/byname/internal/engines"
	"github.com/unkeyed/byname/internal/money"
)

// lock keeps two replicas from reading the same answers at once.
var lock = db.WorkspaceLock("byname.discovery")

// retryAfter is how long a failed read waits before a pass tries it again.
const retryAfter = time.Hour

// unread selects the answers of finished checks that were never read for
// other brands: those from before reading was on, or whose check stopped
// between storing and reading them. Newest first, so recent lists fill
// first.
const unread = `FROM answer a JOIN run r ON r.id = a.run_id
	WHERE a.status = 'ok' AND r.status NOT IN ('queued', 'running')
	AND NOT EXISTS (SELECT 1 FROM brand_call c WHERE c.purpose = 'find' AND c.answer_id = a.id)`

// Backfill is reading past answers for other brands: how many are
// unread, what reading them costs, and the latest pass someone asked for.
type Backfill struct {
	Unread           int     `json:"unread" jsonschema:"answers of finished checks never read for other brands"`
	CostPerAnswerUSD float64 `json:"cost_per_answer_usd"`
	CostUSD          float64 `json:"cost_usd" jsonschema:"unread times cost_per_answer_usd: what reading them all is expected to cost"`
	Ready            bool    `json:"ready" jsonschema:"reading is on, a Treg token is stored and onboarding is finished, so a pass can start"`
	Latest           *Pass   `json:"latest" jsonschema:"the latest pass someone asked for, null before any"`
}

// Pass is one job reading past answers, as Check history lists it.
type Pass struct {
	JobID      int64      `json:"job_id"`
	Status     string     `json:"status" jsonschema:"queued, running, completed, capped (the monthly spend cap stopped it), refused (Treg refused, such as for an empty balance) or failed"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
	Read       int        `json:"read" jsonschema:"answers it read"`
	SpentUSD   float64    `json:"spent_usd"`
	Error      string     `json:"error"`
}

func (s *Service) Backfill(ctx context.Context) (Backfill, error) {
	var out Backfill
	var err error
	if out.Ready, err = s.Ready(ctx); err != nil {
		return out, err
	}
	if out.CostPerAnswerUSD, err = config.FindCostPerAnswer(ctx, s.db); err != nil {
		return out, err
	}
	if err := s.db.QueryRow(ctx, `SELECT count(*) `+unread).Scan(&out.Unread); err != nil {
		return out, err
	}
	out.CostUSD = out.CostPerAnswerUSD * float64(out.Unread)
	var p Pass
	err = s.db.QueryRow(ctx, `SELECT j.id, j.status, j.started_at, j.finished_at, coalesce(j.error, ''),
			(SELECT count(*) FROM brand_call c WHERE c.job_run_id = j.id AND c.status = 'ok'),
			coalesce((SELECT sum(cost_usd) FROM brand_call c WHERE c.job_run_id = j.id), 0)::float8
		FROM job_run j WHERE j.job = 'other_brands' AND j.trigger = 'run_now' ORDER BY j.id DESC LIMIT 1`).
		Scan(&p.JobID, &p.Status, &p.StartedAt, &p.FinishedAt, &p.Error, &p.Read, &p.SpentUSD)
	switch {
	case err == nil:
		out.Latest = &p
	case !errors.Is(err, pgx.ErrNoRows):
		return out, err
	}
	return out, nil
}

// StartBackfill queues a pass over every unread answer, which the
// scheduler starts within a minute. A pass already queued or running is
// returned as it is, so asking twice never reads twice.
func (s *Service) StartBackfill(ctx context.Context) (Backfill, error) {
	b, err := s.Backfill(ctx)
	switch {
	case err != nil:
		return b, err
	case !b.Ready:
		return b, fmt.Errorf("%w: reading answers for other brands needs it turned on, a Treg token and a finished onboarding", core.ErrInvalid)
	case b.Latest != nil && (b.Latest.Status == "queued" || b.Latest.Status == "running"):
		return b, nil
	case b.Unread == 0:
		return b, fmt.Errorf("%w: every answer has been read already", core.ErrInvalid)
	}
	if _, err := s.db.Exec(ctx, `INSERT INTO job_run (job, trigger, status, started_at) VALUES ('other_brands', 'run_now', 'queued', $1)`, s.now()); err != nil {
		return b, err
	}
	return s.Backfill(ctx)
}

// Tick is the scheduler's call each minute: it runs a pass someone asked
// for, then reads again the answers whose read failed. It holds an
// advisory lock and returns at once when another process holds it.
func (s *Service) Tick(ctx context.Context) error {
	unlock, ok, err := db.TryLock(ctx, s.db, lock)
	if err != nil || !ok {
		return err
	}
	defer unlock()
	// With the lock held no pass runs anywhere, so a scheduled one still
	// marked running was cut off. A pass someone asked for carries on.
	if _, err := s.db.Exec(ctx, `UPDATE job_run SET status = 'failed', finished_at = $1,
			error = 'the process stopped before this pass finished; the answers it read stay read'
		WHERE job = 'other_brands' AND trigger = 'scheduled' AND status = 'running'`, s.now()); err != nil {
		return err
	}
	treg, ok, err := s.ready(ctx)
	if err != nil {
		return err
	}
	if !ok {
		_, err := s.db.Exec(ctx, `UPDATE job_run SET status = 'failed', finished_at = $1,
				error = 'reading answers for other brands was turned off, or the Treg token removed, before this pass ran'
			WHERE job = 'other_brands' AND trigger = 'run_now' AND status IN ('queued', 'running')`, s.now())
		return err
	}
	var job int64
	err = s.db.QueryRow(ctx, `SELECT id FROM job_run WHERE job = 'other_brands' AND trigger = 'run_now' AND status IN ('queued', 'running') ORDER BY id LIMIT 1`).Scan(&job)
	switch {
	case err == nil:
		if err := s.pass(ctx, treg, job, `SELECT a.id, a.text `+unread+` ORDER BY a.id DESC LIMIT 20`); err != nil {
			return err
		}
	case !errors.Is(err, pgx.ErrNoRows):
		return err
	}
	return s.retry(ctx, treg)
}

// retryable selects the answers no read has stored brands for, whose
// failed reads number fewer than maxAttempts, the latest call over
// retryAfter ago ($1 is that moment). A read Treg refused cost nothing and
// is no attempt, so an empty balance never uses one up.
var retryable = fmt.Sprintf(`SELECT a.id, a.text FROM answer a
	WHERE a.status = 'ok' AND a.id IN (SELECT answer_id FROM brand_call WHERE purpose = 'find' AND answer_id IS NOT NULL
		GROUP BY answer_id HAVING bool_and(status IN ('failed', 'refused')) AND count(*) FILTER (WHERE status = 'failed') < %d AND max(called_at) < $1)
	ORDER BY a.id DESC LIMIT 20`, maxAttempts)

// retry reads again the answers whose reads failed, as a scheduled
// other_brands job, logged only when there is one to read.
func (s *Service) retry(ctx context.Context, treg engines.Treg) error {
	var due bool
	if err := s.db.QueryRow(ctx, `SELECT EXISTS (`+retryable+`)`, s.now().Add(-retryAfter)).Scan(&due); err != nil || !due {
		return err
	}
	var job int64
	if err := s.db.QueryRow(ctx, `INSERT INTO job_run (job, trigger, status, started_at) VALUES ('other_brands', 'scheduled', 'running', $1) RETURNING id`, s.now()).Scan(&job); err != nil {
		return err
	}
	return s.pass(ctx, treg, job, retryable, s.now().Add(-retryAfter))
}

// passStop is why a pass ended early.
type passStop struct{ status, msg string }

func (e *passStop) Error() string { return e.msg }

// pass reads the answers query selects, one at a time so the monthly
// spend cap is never overshot, batch by batch until none is left, the cap
// is reached or Treg refuses a call, and records how job ended.
func (s *Service) pass(ctx context.Context, treg engines.Treg, job int64, query string, args ...any) error {
	if _, err := s.db.Exec(ctx, `UPDATE job_run SET status = 'running', started_at = $2 WHERE id = $1 AND status = 'queued'`, job, s.now()); err != nil {
		return err
	}
	err := s.batches(ctx, treg, job, query, args...)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	status, msg := "completed", (*string)(nil)
	var stop *passStop
	switch {
	case errors.As(err, &stop):
		status, msg = stop.status, &stop.msg
	case err != nil:
		status, msg = "failed", new(err.Error())
	}
	if _, ferr := s.db.Exec(context.WithoutCancel(ctx), `UPDATE job_run SET status = $2, error = $3, finished_at = $4 WHERE id = $1`,
		job, status, msg, s.now()); ferr != nil {
		return errors.Join(err, ferr)
	}
	if status == "failed" {
		return err
	}
	return nil
}

func (s *Service) batches(ctx context.Context, treg engines.Treg, job int64, query string, args ...any) error {
	for {
		rows, err := s.db.Query(ctx, query, args...)
		if err != nil {
			return err
		}
		reads, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (read, error) {
			var v read
			return v, r.Scan(&v.answer, &v.text)
		})
		if err != nil || len(reads) == 0 {
			return err
		}
		for _, r := range reads {
			if err := s.underCap(ctx); err != nil {
				return err
			}
			refused, err := s.find(ctx, treg, r, call{job: job})
			if err != nil {
				return err
			}
			if refused != nil {
				return &passStop{status: "refused", msg: refused.Error()}
			}
		}
	}
}

// underCap returns a passStop once this month's spend has reached the
// monthly spend cap, which checks, social searches and other brands share.
func (s *Service) underCap(ctx context.Context) error {
	st, err := s.config.Settings(ctx)
	if err != nil || st.SpendCapMonthUSD == nil {
		return err
	}
	loc := st.Schedule().Location
	local := s.now().In(loc)
	start := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, loc)
	var spent float64
	if err := s.db.QueryRow(ctx, `SELECT coalesce(sum(usd), 0)::float8 FROM spend WHERE at >= $1`, start).Scan(&spent); err != nil {
		return err
	}
	if spent >= *st.SpendCapMonthUSD {
		return &passStop{status: "capped", msg: fmt.Sprintf("stopped reading answers: this month has spent %s; the monthly spend cap is %s", money.Format(spent), money.Format(*st.SpendCapMonthUSD))}
	}
	return nil
}
