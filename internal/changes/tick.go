package changes

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/unkeyed/byname/internal/db"
)

// jobs is each job this package logs to job_run, with its insert. Each
// names its job as a SQL literal, which TestJobCheckCoversEveryJob reads.
var jobs = map[string]string{
	"site_changes":  `INSERT INTO job_run (job, trigger, started_at) VALUES ('site_changes', 'scheduled', $1) RETURNING id`,
	"recrawl_watch": `INSERT INTO job_run (job, trigger, started_at) VALUES ('recrawl_watch', 'scheduled', $1) RETURNING id`,
	"verdicts":      `INSERT INTO job_run (job, trigger, started_at) VALUES ('verdicts', 'scheduled', $1) RETURNING id`,
}

// locks keep two replicas from running the same work at once.
var (
	pollLock  = db.WorkspaceLock("byname.changes.poll")
	watchLock = db.WorkspaceLock("byname.changes.watch")
)

// pollEvery is the GitHub poll's period, fixed.
const pollEvery = time.Hour

// Tick is the scheduler's call each minute: the hourly GitHub poll when a
// repo is connected, then recrawl watch and verdicts once a Search Console
// sync has completed since they last ran. Each part holds its own advisory
// lock and returns at once when another process holds it.
func (s *Service) Tick(ctx context.Context) error {
	return errors.Join(s.pollIfDue(ctx), s.watchIfDue(ctx))
}

func (s *Service) pollIfDue(ctx context.Context) error {
	return s.locked(ctx, pollLock, []string{"site_changes"}, func() error {
		var due bool
		if err := s.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM repo_connection WHERE ciphertext IS NOT NULL
			AND (last_poll_at IS NULL OR last_poll_at <= $1::timestamptz - $2::interval))`, s.now(), pollEvery).Scan(&due); err != nil || !due {
			return err
		}
		return s.pollNow(ctx)
	})
}

// Poll polls the connected repo now, whether or not it is due.
func (s *Service) Poll(ctx context.Context) error {
	return s.locked(ctx, pollLock, []string{"site_changes"}, func() error { return s.pollNow(ctx) })
}

// pollNow runs one poll as a site_changes job, records how it went and
// the token expiry GitHub reported, and warns when the token is about to
// expire.
func (s *Service) pollNow(ctx context.Context) error {
	r, ok, err := s.connection(ctx)
	if err != nil || !ok {
		return err
	}
	return s.job(ctx, "site_changes", func() error {
		expires, err := s.poll(ctx, r)
		var msg *string
		if err != nil {
			msg = new(err.Error())
		}
		if _, uerr := s.db.Exec(ctx, `UPDATE repo_connection SET last_poll_at = $1, last_poll_error = $2,
				token_expires_at = CASE WHEN $3::timestamptz IS NOT NULL OR $2::text IS NULL THEN $3 ELSE token_expires_at END`,
			s.now(), msg, expires); uerr != nil {
			return errors.Join(err, uerr)
		}
		return errors.Join(err, s.warnExpiry(ctx, r, expires))
	})
}

func (s *Service) watchIfDue(ctx context.Context) error {
	return s.locked(ctx, watchLock, []string{"recrawl_watch", "verdicts"}, func() error {
		var due bool
		if err := s.db.QueryRow(ctx, `SELECT coalesce(
			(SELECT max(finished_at) FROM job_run WHERE job = 'search_sync' AND provider = 'google' AND status = 'completed')
				> coalesce((SELECT max(started_at) FROM job_run WHERE job = 'recrawl_watch'), '-infinity'), false)`).Scan(&due); err != nil || !due {
			return err
		}
		return s.watch(ctx)
	})
}

// Watch runs recrawl watch, then verdicts, now.
func (s *Service) Watch(ctx context.Context) error {
	return s.locked(ctx, watchLock, []string{"recrawl_watch", "verdicts"}, func() error { return s.watch(ctx) })
}

func (s *Service) watch(ctx context.Context) error {
	st, err := s.settings(ctx)
	if err != nil {
		return err
	}
	return errors.Join(
		s.job(ctx, "recrawl_watch", func() error { return s.recrawl(ctx, st) }),
		s.job(ctx, "verdicts", func() error { return s.verdicts(ctx, st) }),
	)
}

// job logs f as one run of job.
func (s *Service) job(ctx context.Context, job string, f func() error) error {
	var id int64
	if err := s.db.QueryRow(ctx, jobs[job], s.now()).Scan(&id); err != nil {
		return err
	}
	err := f()
	status, msg := "completed", (*string)(nil)
	if err != nil {
		status, msg = "failed", new(err.Error())
	}
	if _, ferr := s.db.Exec(context.WithoutCancel(ctx), `UPDATE job_run SET status = $2, error = $3, finished_at = $4 WHERE id = $1`,
		id, status, msg, s.now()); ferr != nil {
		return errors.Join(err, ferr)
	}
	if err != nil {
		return fmt.Errorf("%s: %w", job, err)
	}
	return nil
}

// locked runs f holding the session advisory lock key on a dedicated
// connection, so the lock dies with the process. With the lock held none
// of jobs runs anywhere, so one still marked running was cut off.
func (s *Service) locked(ctx context.Context, key string, names []string, f func() error) error {
	conn, err := s.db.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	var got bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(`+key+`)`).Scan(&got); err != nil || !got {
		return err
	}
	defer func() {
		if _, err := conn.Exec(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock(`+key+`)`); err != nil {
			_ = conn.Conn().Close(context.WithoutCancel(ctx))
		}
	}()
	if _, err := conn.Exec(ctx, `UPDATE job_run SET status = 'failed', finished_at = $1,
			error = 'the process stopped before this job finished; the next run starts over'
		WHERE job = ANY ($2) AND status = 'running'`, s.now(), names); err != nil {
		return err
	}
	return f()
}
