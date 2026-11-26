package workspaces

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/unkeyed/byname/internal/app"
	"github.com/unkeyed/byname/internal/core"
	"github.com/unkeyed/byname/internal/db"
)

// Entity is one live workspace: its row and its services, bound to its
// tables.
type Entity struct {
	Workspace db.Workspace
	Services  app.Services
}

// Summary is a workspace as GET /v1/workspaces lists it.
type Summary struct {
	ID        int64  `json:"id"`
	Slug      string `json:"slug" jsonschema:"the workspace's name in paths: /w/<slug>/v1/..."`
	Name      string `json:"name"`
	Onboarded bool   `json:"onboarded" jsonschema:"whether its onboarding is finished; until then nothing runs on schedule"`
	Default   bool   `json:"default" jsonschema:"whether paths without /w/<slug> mean this workspace: the first one made"`
}

type live struct {
	Entity
	run     func(context.Context)
	started bool
}

// Registry is every workspace this process serves. Each is built once,
// and its jobs start once, when Run is running.
type Registry struct {
	pool  db.Pool
	opts  Options
	log   *slog.Logger
	build func(db.Workspace, db.Pool) (app.Services, func(context.Context))

	refreshMu sync.Mutex

	mu      sync.RWMutex
	byID    map[int64]*live
	runCtx  context.Context
	stopped bool
	running sync.WaitGroup

	now      func() time.Time
	missMu   sync.Mutex
	missedAt time.Time
}

// Open builds every workspace in the database on pool, which must not be
// bound to one. Nothing runs in the background until Run.
func Open(ctx context.Context, pool db.Pool, o Options) (*Registry, error) {
	r := &Registry{pool: pool, opts: o, log: o.Log, byID: map[int64]*live{}, now: time.Now}
	if r.log == nil {
		r.log = slog.Default()
	}
	r.build = func(w db.Workspace, p db.Pool) (app.Services, func(context.Context)) { return Build(w, p, o) }
	if err := r.Refresh(ctx); err != nil {
		return nil, err
	}
	return r, nil
}

// Run starts every workspace's jobs, and each new workspace's as it goes
// live, then looks for workspaces another replica made every minute.
// When ctx ends it returns once every job has stopped.
func (r *Registry) Run(ctx context.Context) {
	r.mu.Lock()
	r.runCtx = ctx
	for _, l := range r.byID {
		r.startLocked(l)
	}
	r.mu.Unlock()
	t := time.NewTicker(refreshEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			r.mu.Lock()
			r.stopped = true
			r.mu.Unlock()
			r.running.Wait()
			return
		case <-t.C:
			if err := r.Refresh(ctx); err != nil && ctx.Err() == nil {
				r.log.Warn("bringing new workspaces live failed; retrying in a minute", "err", err)
			}
		}
	}
}

func (r *Registry) startLocked(l *live) {
	if l.started || r.runCtx == nil || r.stopped {
		return
	}
	l.started = true
	r.running.Go(func() { l.run(r.runCtx) })
}

// Refresh brings every workspace row live that is not yet, and picks up
// new names and slugs. Calling it again changes nothing, so a workspace
// another replica made goes live here on the next call.
func (r *Registry) Refresh(ctx context.Context) error {
	r.refreshMu.Lock()
	defer r.refreshMu.Unlock()
	all, err := db.Workspaces(ctx, r.pool)
	if err != nil {
		return err
	}
	var errs []error
	for _, w := range all {
		r.mu.Lock()
		l, ok := r.byID[w.ID]
		if ok {
			l.Workspace = w
		}
		r.mu.Unlock()
		if ok {
			continue
		}
		svc, run := r.build(w, db.In(r.pool, w.Schema))
		if err := prepare(ctx, w, svc, r.log); err != nil {
			errs = append(errs, err)
			continue
		}
		r.mu.Lock()
		l = &live{Entity: Entity{Workspace: w, Services: svc}, run: run}
		r.byID[w.ID] = l
		r.startLocked(l)
		r.mu.Unlock()
	}
	return errors.Join(errs...)
}

// Default is the workspace paths without /w/<slug> mean: the first one,
// which has the lowest id.
func (r *Registry) Default() Entity {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.byID[r.defaultID()].Entity
}

func (r *Registry) defaultID() int64 {
	ids := make([]int64, 0, len(r.byID))
	for id := range r.byID {
		ids = append(ids, id)
	}
	return slices.Min(ids)
}

// Lookup is the workspace with slug. A slug it does not know yet sends it
// to the database, for a workspace made or renamed elsewhere, at most
// once every missRefreshEvery.
func (r *Registry) Lookup(ctx context.Context, slug string) (Entity, error) {
	e, ok, err := r.find(ctx, func(w db.Workspace) bool { return w.Slug == slug })
	if err == nil && !ok {
		err = fmt.Errorf("%w: no workspace %q; GET /v1/workspaces lists them", core.ErrNotFound, slug)
	}
	return e, err
}

// ByID is the workspace with id, refreshing as Lookup does when it is not
// live.
func (r *Registry) ByID(ctx context.Context, id int64) (Entity, error) {
	e, ok, err := r.find(ctx, func(w db.Workspace) bool { return w.ID == id })
	if err == nil && !ok {
		err = fmt.Errorf("%w: no workspace %d", core.ErrNotFound, id)
	}
	return e, err
}

const missRefreshEvery = 3 * time.Second

func (r *Registry) find(ctx context.Context, match func(db.Workspace) bool) (Entity, bool, error) {
	inMemory := func() (Entity, bool) {
		r.mu.RLock()
		defer r.mu.RUnlock()
		for _, l := range r.byID {
			if match(l.Workspace) {
				return l.Entity, true
			}
		}
		return Entity{}, false
	}
	if e, ok := inMemory(); ok {
		return e, true, nil
	}
	r.missMu.Lock()
	if r.now().Before(r.missedAt.Add(missRefreshEvery)) {
		r.missMu.Unlock()
		return Entity{}, false, nil
	}
	r.missedAt = r.now()
	r.missMu.Unlock()
	if err := r.Refresh(ctx); err != nil {
		return Entity{}, false, err
	}
	e, ok := inMemory()
	if ok {
		r.missMu.Lock()
		r.missedAt = time.Time{}
		r.missMu.Unlock()
	}
	return e, ok, nil
}

// List is every workspace, the default first, with whether each has
// finished onboarding.
func (r *Registry) List(ctx context.Context) ([]Summary, error) {
	if err := r.Refresh(ctx); err != nil {
		return nil, err
	}
	r.mu.RLock()
	all := make([]Entity, 0, len(r.byID))
	for _, l := range r.byID {
		all = append(all, l.Entity)
	}
	first := r.defaultID()
	r.mu.RUnlock()
	slices.SortFunc(all, func(a, b Entity) int { return int(a.Workspace.ID - b.Workspace.ID) })
	out := make([]Summary, 0, len(all))
	for _, e := range all {
		s, err := summarize(ctx, e, first)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

func summarize(ctx context.Context, e Entity, first int64) (Summary, error) {
	done, err := e.Services.Config.Onboarded(ctx)
	w := e.Workspace
	return Summary{ID: w.ID, Slug: w.Slug, Name: w.Name, Onboarded: done, Default: w.ID == first}, err
}

func (r *Registry) summary(ctx context.Context, id int64) (Summary, error) {
	e, err := r.ByID(ctx, id)
	if err != nil {
		return Summary{}, err
	}
	r.mu.RLock()
	first := r.defaultID()
	r.mu.RUnlock()
	return summarize(ctx, e, first)
}

const maxNameLen = 80

func parseName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if n := utf8.RuneCountInString(name); n < 1 || n > maxNameLen {
		return "", fmt.Errorf("%w: name must be 1 to %d characters, got %d", core.ErrInvalid, maxNameLen, n)
	}
	return name, nil
}

// Create makes a workspace named name at slug, or at name's slug when
// slug is empty, and brings it live with onboarding not started. Creating
// it again with the same name and slug returns the same workspace.
func (r *Registry) Create(ctx context.Context, name, slug string) (Summary, error) {
	name, err := parseName(name)
	if err != nil {
		return Summary{}, err
	}
	derived := slug == ""
	if derived {
		slug = db.Slug(name)
	}
	if len(slug) > db.MaxSlugLen || !db.SlugPattern.MatchString(slug) {
		return Summary{}, fmt.Errorf("%w: slug must be lower-case letters and digits joined by single hyphens, at most %d characters, got %q", core.ErrInvalid, db.MaxSlugLen, slug)
	}
	w, err := r.createSchema(ctx, name, slug)
	if errors.Is(err, db.ErrSlugReserved) {
		if derived {
			return Summary{}, fmt.Errorf("%w: %q has no letters or digits to make a slug from; choose a slug", core.ErrInvalid, name)
		}
		return Summary{}, fmt.Errorf("%w: the slug %s is kept for the first workspace; choose another", core.ErrInvalid, db.ReservedSlug)
	}
	if errors.Is(err, db.ErrSlugTaken) {
		return Summary{}, fmt.Errorf("%w: another workspace has the slug %q; choose another", core.ErrConflict, slug)
	}
	if err != nil {
		return Summary{}, err
	}
	if err := r.Refresh(ctx); err != nil {
		return Summary{}, err
	}
	return r.summary(ctx, w.ID)
}

func (r *Registry) createSchema(ctx context.Context, name, slug string) (db.Workspace, error) {
	if r.opts.MigrateDB == nil {
		w, err := db.CreateWorkspace(ctx, r.pool, name, slug)
		if pgErr := (*pgconn.PgError)(nil); errors.As(err, &pgErr) && pgErr.Code == "42501" {
			return w, fmt.Errorf("%w: DATABASE_URL's role cannot create a workspace's tables; set MIGRATION_DATABASE_URL to a role that can", err)
		}
		return w, err
	}
	cfg := r.opts.MigrateDB.Copy()
	cfg.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return db.Workspace{}, fmt.Errorf("connect to MIGRATION_DATABASE_URL: %w", err)
	}
	defer pool.Close()
	w, err := db.CreateWorkspace(ctx, pool, name, slug)
	if err != nil && !errors.Is(err, db.ErrSlugTaken) {
		return w, fmt.Errorf("create the workspace's tables at MIGRATION_DATABASE_URL: %w", err)
	}
	return w, err
}

// Rename gives the workspace with slug a new name. Its slug stays.
func (r *Registry) Rename(ctx context.Context, slug, name string) (Summary, error) {
	name, err := parseName(name)
	if err != nil {
		return Summary{}, err
	}
	w, err := db.RenameWorkspace(ctx, r.pool, slug, name)
	if errors.Is(err, db.ErrNoWorkspace) {
		return Summary{}, fmt.Errorf("%w: no workspace %q; GET /v1/workspaces lists them", core.ErrNotFound, slug)
	}
	if err != nil {
		return Summary{}, err
	}
	if err := r.Refresh(ctx); err != nil {
		return Summary{}, err
	}
	return r.summary(ctx, w.ID)
}

type entityKey struct{}

// NewContext is ctx carrying e, the workspace a request is for.
func NewContext(ctx context.Context, e Entity) context.Context {
	return context.WithValue(ctx, entityKey{}, e)
}

// FromContext is the workspace NewContext put on ctx, if any.
func FromContext(ctx context.Context) (Entity, bool) {
	e, ok := ctx.Value(entityKey{}).(Entity)
	return e, ok
}
