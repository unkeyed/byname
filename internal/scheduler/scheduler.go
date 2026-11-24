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
	"sync"
	"sync/atomic"
	"time"

	"github.com/unkeyed/byname/internal/checks"
	"github.com/unkeyed/byname/internal/config"
	"github.com/unkeyed/byname/internal/sources"
)

type Scheduler struct {
	checks    *checks.Service
	config    *config.Service
	search    SearchSync
	searching sync.WaitGroup
	busy      atomic.Bool
	log       *slog.Logger
	ticks     <-chan time.Time
	now       func() time.Time
	lastPrune time.Time
	sources   *sources.Service
	changes   SiteChanges
	social    Social
	socialing sync.WaitGroup
	fetching  atomic.Bool
	brands    []Ticker
	branding  sync.WaitGroup
	finding   atomic.Bool
}

type Options struct {
	// Ticks replaces the one-minute ticker, for tests.
	Ticks <-chan time.Time
	Now   func() time.Time
	Log   *slog.Logger
	// Sources fetches the pages answers cite; nil skips it.
	Sources *sources.Service
	// Search is the daily search engine sync, nil when not wired.
	Search SearchSync
	// Changes is site changes, nil when not wired.
	Changes SiteChanges
	// Social is social mentions, nil when not wired.
	Social Social
	// OtherBrands reads past answers for other brands and looks up
	// logos, in order; empty when not wired.
	OtherBrands []Ticker
}

// Ticker is a job that decides itself what is due on each tick and holds
// its own advisory lock: internal/discovery and internal/logos.
type Ticker interface {
	Tick(ctx context.Context) error
}

// SearchSync is internal/search's daily sync: it does nothing until a
// provider is due and holds its own advisory lock.
type SearchSync interface {
	SyncIfDue(ctx context.Context) error
}

// SiteChanges is internal/changes: the hourly GitHub poll, and recrawl
// watch and verdicts once a Google sync has completed. It decides
// itself what is due and holds its own advisory locks.
type SiteChanges interface {
	Tick(ctx context.Context) error
}

// Social is internal/social: the fetch every few hours and the digest
// when a period ends. It decides itself what is due and holds its own
// advisory lock.
type Social interface {
	Tick(ctx context.Context) error
}

func New(c *checks.Service, cfg *config.Service, o Options) *Scheduler {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Log == nil {
		o.Log = slog.Default()
	}
	return &Scheduler{checks: c, config: cfg, log: o.Log, ticks: o.Ticks, now: o.Now, sources: o.Sources, search: o.Search, changes: o.Changes, social: o.Social, brands: o.OtherBrands}
}

// Run ticks once at start, which resumes a run a previous process left
// running and catches up a check missed while nothing was running, then on
// every tick and whenever Run now queues a run, until ctx ends. A run in
// progress when ctx ends stays running for the next start to resume, and
// Run returns once a search sync in flight has stopped.
func (s *Scheduler) Run(ctx context.Context) {
	defer s.searching.Wait()
	defer s.socialing.Wait()
	defer s.branding.Wait()
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
	// Search engines: the daily sync, then site changes, which follow it,
	// on their own goroutine so a long backfill never holds up checks.
	if (s.search != nil || s.changes != nil) && s.busy.CompareAndSwap(false, true) {
		s.searching.Go(func() {
			defer s.busy.Store(false)
			if s.search != nil {
				if err := s.search.SyncIfDue(ctx); err != nil && ctx.Err() == nil {
					s.log.Warn("search sync failed", "err", err)
				}
			}
			if s.changes != nil {
				if err := s.changes.Tick(ctx); err != nil && ctx.Err() == nil {
					s.log.Warn("site changes failed", "err", err)
				}
			}
		})
	}
	// Social mentions: a fetch calls slow search APIs, so it too runs on
	// its own goroutine.
	if s.social != nil && s.fetching.CompareAndSwap(false, true) {
		s.socialing.Go(func() {
			defer s.fetching.Store(false)
			if err := s.social.Tick(ctx); err != nil && ctx.Err() == nil {
				s.log.Warn("social mentions failed", "err", err)
			}
		})
	}
	// Other brands: reading past answers and looking up logos call slow
	// paid tools, so they too run on their own goroutine.
	if len(s.brands) > 0 && s.finding.CompareAndSwap(false, true) {
		s.branding.Go(func() {
			defer s.finding.Store(false)
			for _, t := range s.brands {
				if err := t.Tick(ctx); err != nil && ctx.Err() == nil {
					s.log.Warn("other brands failed", "err", err)
				}
			}
		})
	}
	if id, err := s.checks.ClaimDue(ctx); err != nil && ctx.Err() == nil {
		s.log.Error("claiming the scheduled check failed", "err", err)
	} else if id != 0 {
		s.log.Info("scheduled check queued", "run", id)
	}
	if err := s.checks.Work(ctx); err != nil && ctx.Err() == nil {
		s.log.Error("running checks failed", "err", err)
	}
	// Cited sources: after the checks above, fetch the pages they newly
	// cited and refresh stale ones.
	if s.sources != nil {
		if n, err := s.sources.Run(ctx); err != nil && ctx.Err() == nil {
			s.log.Warn("fetching cited sources failed", "err", err)
		} else if n > 0 {
			s.log.Info("fetched cited sources", "pages", n)
		}
	}
}
