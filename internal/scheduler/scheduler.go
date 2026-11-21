// Package scheduler runs Byname's jobs inside the one process: every
// minute it refreshes the model lists when a day has passed, claims the
// AI check when it is due, and executes queued runs. It holds no state
// that matters across restarts: the schedule, the claim and the run all
// live in Postgres, so a restart, a redeploy or a second replica never
// starts a check twice.
package scheduler

import (
	"context"
	"log/slog"
	"time"

	"github.com/unkeyed/byname/internal/checks"
	"github.com/unkeyed/byname/internal/config"
)

type Scheduler struct {
	checks    *checks.Service
	config    *config.Service
	search    SearchSync
	log       *slog.Logger
	ticks     <-chan time.Time
	now       func() time.Time
	lastPrune time.Time
}

type Options struct {
	// Ticks replaces the one-minute ticker, for tests.
	Ticks <-chan time.Time
	Now   func() time.Time
	Log   *slog.Logger
	// Search is the daily Search Console sync, nil when not wired.
	Search SearchSync
}

// SearchSync is internal/gsc's daily sync: it does nothing until it is due
// and holds its own advisory lock.
type SearchSync interface {
	SyncIfDue(ctx context.Context) error
}

func New(c *checks.Service, cfg *config.Service, o Options) *Scheduler {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Log == nil {
		o.Log = slog.Default()
	}
	return &Scheduler{checks: c, config: cfg, search: o.Search, log: o.Log, ticks: o.Ticks, now: o.Now}
}

// Run ticks once at start, which resumes a run a previous process left
// running and catches up a check missed while nothing was running, then on
// every tick and whenever Run now queues a run, until ctx ends. A run in
// progress when ctx ends stays running for the next start to resume.
func (s *Scheduler) Run(ctx context.Context) {
	ticks := s.ticks
	if ticks == nil {
		t := time.NewTicker(time.Minute)
		defer t.Stop()
		ticks = t.C
	}
	for {
		s.Tick(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticks:
		case <-s.checks.Wake():
		}
	}
}

// Tick is one round of every job.
func (s *Scheduler) Tick(ctx context.Context) {
	if err := s.config.RefreshModelsIfDue(ctx); err != nil && ctx.Err() == nil {
		s.log.Warn("model list refresh failed", "err", err)
	}
	if now := s.now(); now.Sub(s.lastPrune) >= 24*time.Hour {
		n, err := s.checks.PruneRaw(ctx)
		if err == nil {
			s.lastPrune = now
		}
		if err != nil && ctx.Err() == nil {
			s.log.Warn("raw response pruning failed", "err", err)
		} else if n > 0 {
			s.log.Info("pruned raw responses past their retention", "answers", n)
		}
	}
	// Search Console: the daily sync.
	if s.search != nil {
		if err := s.search.SyncIfDue(ctx); err != nil && ctx.Err() == nil {
			s.log.Warn("Search Console sync failed", "err", err)
		}
	}
	if id, err := s.checks.ClaimDue(ctx); err != nil && ctx.Err() == nil {
		s.log.Error("claiming the scheduled check failed", "err", err)
	} else if id != 0 {
		s.log.Info("scheduled check queued", "run", id)
	}
	if err := s.checks.Work(ctx); err != nil && ctx.Err() == nil {
		s.log.Error("running checks failed", "err", err)
	}
}
