package entities

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

// Live is one entity this process serves: its row and its services,
// bound to its tables.
type Live struct {
	Entity   db.Entity
	Services app.Services
}

// Summary is an entity as GET /v1/entities lists it.
type Summary struct {
	ID        int64  `json:"id"`
	Slug      string `json:"slug" jsonschema:"the entity's name in paths: /e/<slug>/v1/..."`
	Name      string `json:"name"`
	Onboarded bool   `json:"onboarded" jsonschema:"whether its onboarding is finished; until then nothing runs on schedule"`
	Default   bool   `json:"default" jsonschema:"whether paths without /e/<slug> mean this entity: the first one made"`
}

type entry struct {
	Live
	pool db.Pool
	run  func(context.Context)
	// stop and done are set once its jobs start: stop ends them, and done
	// closes when they have.
	stop context.CancelFunc
	done chan struct{}
}

// Registry is every entity this process serves. Each is built once,
// and its jobs start once, when Run is running. A deleted entity stops
// being served, and its jobs stop, at the next Refresh.
type Registry struct {
	pool  db.Pool
	opts  Options
	log   *slog.Logger
	build func(db.Entity, db.Pool) (app.Services, func(context.Context))

	refreshMu sync.Mutex

	mu       sync.RWMutex
	byID     map[int64]*entry
	runCtx   context.Context
	stopped  bool
	running  sync.WaitGroup
	onRemove []func(id int64)

	// grace is how long a deleted entity's schema stays: long enough
	// that every replica has refreshed and stopped serving it.
	grace time.Duration

	now      func() time.Time
	missMu   sync.Mutex
	missedAt time.Time
}

// Open builds every entity in the database on pool, which must not be
// bound to one. Nothing runs in the background until Run.
func Open(ctx context.Context, pool db.Pool, o Options) (*Registry, error) {
	r := &Registry{pool: pool, opts: o, log: o.Log, byID: map[int64]*entry{}, now: time.Now, grace: deleteGrace}
	if r.log == nil {
		r.log = slog.Default()
	}
	r.build = func(w db.Entity, p db.Pool) (app.Services, func(context.Context)) { return Build(w, p, o) }
	if err := r.Refresh(ctx); err != nil {
		return nil, err
	}
	return r, nil
}

// Run starts every entity's jobs, and each new entity's as it goes
// live, then every minute looks for entities another replica made or
// deleted, and drops the schemas of entities deleted long enough ago.
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
				r.log.Warn("bringing new entities live failed; retrying in a minute", "err", err)
			}
			if err := r.Sweep(ctx); err != nil && ctx.Err() == nil {
				r.log.Warn("dropping deleted entities failed; retrying in a minute", "err", err)
			}
		}
	}
}

func (r *Registry) startLocked(l *entry) {
	if l.stop != nil || r.runCtx == nil || r.stopped {
		return
	}
	ctx, stop := context.WithCancel(r.runCtx)
	l.stop, l.done = stop, make(chan struct{})
	r.running.Go(func() {
		defer close(l.done)
		l.run(ctx)
	})
}

// OnRemove has f called with the id of each entity this process stops
// serving, once its jobs have stopped, so a cache of anything built for
// it can let go.
func (r *Registry) OnRemove(f func(id int64)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.onRemove = append(r.onRemove, f)
}

// remove stops serving the entities with ids: Lookup and ByID no longer
// find them, their jobs stop and are waited for, and any query still
// running through their services fails from then on instead of reaching
// a schema that is about to go.
func (r *Registry) remove(ids ...int64) {
	r.mu.Lock()
	var gone []*entry
	for _, id := range ids {
		if l, ok := r.byID[id]; ok {
			delete(r.byID, id)
			gone = append(gone, l)
		}
	}
	hooks := slices.Clone(r.onRemove)
	r.mu.Unlock()
	for _, l := range gone {
		if l.stop != nil {
			l.stop()
			<-l.done
		}
		db.Retire(l.pool)
		for _, f := range hooks {
			f(l.Entity.ID)
		}
	}
}

// Refresh brings every entity row live that is not yet, picks up new
// names and slugs, and stops serving every entity that was deleted.
// Calling it again changes nothing, so an entity another replica made
// goes live here on the next call, and one it deleted stops. An entity goes
// live only once it has every entity migration this binary has: one
// lacking a file is caught up first, and while that fails it is logged
// and not served, and the next call tries again.
func (r *Registry) Refresh(ctx context.Context) error {
	r.refreshMu.Lock()
	defer r.refreshMu.Unlock()
	all, err := db.Entities(ctx, r.pool)
	if err != nil {
		return err
	}
	r.mu.RLock()
	var gone []int64
	for id := range r.byID {
		if !slices.ContainsFunc(all, func(w db.Entity) bool { return w.ID == id }) {
			gone = append(gone, id)
		}
	}
	r.mu.RUnlock()
	r.remove(gone...)
	var errs []error
	for _, w := range all {
		r.mu.Lock()
		l, ok := r.byID[w.ID]
		if ok {
			l.Entity = w
		}
		r.mu.Unlock()
		if ok {
			continue
		}
		if err := r.catchUp(ctx, w); err != nil {
			r.log.Warn("not serving an entity until it has every entity migration; retrying on the next refresh", "entity", w.Slug, "err", err)
			continue
		}
		pool := db.In(r.pool, w.Schema)
		svc, run := r.build(w, pool)
		if err := prepare(ctx, w, svc, r.log); err != nil {
			errs = append(errs, err)
			continue
		}
		r.mu.Lock()
		l = &entry{Live: Live{Entity: w, Services: svc}, pool: pool, run: run}
		r.byID[w.ID] = l
		r.startLocked(l)
		r.mu.Unlock()
	}
	return errors.Join(errs...)
}

// Default is the entity paths without /e/<slug> mean: the first one,
// which has the lowest id.
func (r *Registry) Default() Live {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.byID[r.defaultID()].Live
}

func (r *Registry) defaultID() int64 {
	ids := make([]int64, 0, len(r.byID))
	for id := range r.byID {
		ids = append(ids, id)
	}
	return slices.Min(ids)
}

// Lookup is the entity with slug. A slug it does not know yet sends it
// to the database, for an entity made or renamed elsewhere, at most
// once every missRefreshEvery.
func (r *Registry) Lookup(ctx context.Context, slug string) (Live, error) {
	e, ok, err := r.find(ctx, func(w db.Entity) bool { return w.Slug == slug })
	if err == nil && !ok {
		err = fmt.Errorf("%w: no entity %q; GET /v1/entities lists them", core.ErrNotFound, slug)
	}
	return e, err
}

// Live is the entity with slug if this process serves it already,
// without asking the database.
func (r *Registry) Live(slug string) (Live, bool) {
	return r.live(func(w db.Entity) bool { return w.Slug == slug })
}

func (r *Registry) live(match func(db.Entity) bool) (Live, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, l := range r.byID {
		if match(l.Entity) {
			return l.Live, true
		}
	}
	return Live{}, false
}

// ByID is the entity with id, refreshing as Lookup does when it is not
// live.
func (r *Registry) ByID(ctx context.Context, id int64) (Live, error) {
	e, ok, err := r.find(ctx, func(w db.Entity) bool { return w.ID == id })
	if err == nil && !ok {
		err = fmt.Errorf("%w: no entity %d", core.ErrNotFound, id)
	}
	return e, err
}

const missRefreshEvery = 3 * time.Second

func (r *Registry) find(ctx context.Context, match func(db.Entity) bool) (Live, bool, error) {
	if e, ok := r.live(match); ok {
		return e, true, nil
	}
	r.missMu.Lock()
	if r.now().Before(r.missedAt.Add(missRefreshEvery)) {
		r.missMu.Unlock()
		return Live{}, false, nil
	}
	r.missedAt = r.now()
	r.missMu.Unlock()
	if err := r.Refresh(ctx); err != nil {
		return Live{}, false, err
	}
	e, ok := r.live(match)
	if ok {
		r.missMu.Lock()
		r.missedAt = time.Time{}
		r.missMu.Unlock()
	}
	return e, ok, nil
}

// List is every entity, the default first, with whether each has
// finished onboarding.
func (r *Registry) List(ctx context.Context) ([]Summary, error) {
	if err := r.Refresh(ctx); err != nil {
		return nil, err
	}
	r.mu.RLock()
	all := make([]Live, 0, len(r.byID))
	for _, l := range r.byID {
		all = append(all, l.Live)
	}
	first := r.defaultID()
	r.mu.RUnlock()
	slices.SortFunc(all, func(a, b Live) int { return int(a.Entity.ID - b.Entity.ID) })
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

func summarize(ctx context.Context, e Live, first int64) (Summary, error) {
	done, err := e.Services.Config.Onboarded(ctx)
	w := e.Entity
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

// Create makes an entity named name at slug, or at name's slug when
// slug is empty, and brings it live with onboarding not started. Creating
// it again with the same name and slug returns the same entity.
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
		return Summary{}, fmt.Errorf("%w: the slug %s is kept for the first entity; choose another", core.ErrInvalid, db.ReservedSlug)
	}
	if errors.Is(err, db.ErrSlugTaken) {
		return Summary{}, fmt.Errorf("%w: another entity has the slug %q; choose another", core.ErrConflict, slug)
	}
	if err != nil {
		return Summary{}, err
	}
	if err := r.Refresh(ctx); err != nil {
		return Summary{}, err
	}
	return r.summary(ctx, w.ID)
}

func (r *Registry) createSchema(ctx context.Context, name, slug string) (w db.Entity, err error) {
	err = r.asMigrator(ctx, func(pool db.Pool) error {
		w, err = db.CreateEntity(ctx, pool, name, slug)
		return err
	})
	return w, err
}

func (r *Registry) catchUp(ctx context.Context, w db.Entity) error {
	done, err := db.EntityMigrated(ctx, r.pool, w.ID)
	if err != nil || done {
		return err
	}
	return r.asMigrator(ctx, func(pool db.Pool) error { return db.MigrateEntity(ctx, pool, w) })
}

// asMigrator runs f on a pool of the role that creates and drops an
// entity's tables: MIGRATION_DATABASE_URL's when it is set, else
// DATABASE_URL's.
// An error that is not the caller's says which role failed.
func (r *Registry) asMigrator(ctx context.Context, f func(db.Pool) error) error {
	if r.opts.MigrateDB == nil {
		err := f(r.pool)
		if pgErr := (*pgconn.PgError)(nil); errors.As(err, &pgErr) && pgErr.Code == "42501" {
			return fmt.Errorf("%w: DATABASE_URL's role cannot create or drop an entity's tables; set MIGRATION_DATABASE_URL to a role that can", err)
		}
		return err
	}
	cfg := r.opts.MigrateDB.Copy()
	cfg.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return fmt.Errorf("connect to MIGRATION_DATABASE_URL: %w", err)
	}
	defer pool.Close()
	err = f(pool)
	if err != nil && !errors.Is(err, db.ErrSlugTaken) && !errors.Is(err, db.ErrSlugReserved) {
		return fmt.Errorf("change the entity's tables at MIGRATION_DATABASE_URL: %w", err)
	}
	return err
}

// Rename gives the entity with slug a new name. Its slug stays.
func (r *Registry) Rename(ctx context.Context, slug, name string) (Summary, error) {
	name, err := parseName(name)
	if err != nil {
		return Summary{}, err
	}
	w, err := db.RenameEntity(ctx, r.pool, slug, name)
	if errors.Is(err, db.ErrNoEntity) {
		return Summary{}, fmt.Errorf("%w: no entity %q; GET /v1/entities lists them", core.ErrNotFound, slug)
	}
	if err != nil {
		return Summary{}, err
	}
	if err := r.Refresh(ctx); err != nil {
		return Summary{}, err
	}
	return r.summary(ctx, w.ID)
}

// Delete marks the entity with slug deleted and stops serving it here at
// once. Other replicas stop at their next Refresh, and Sweep drops its
// tables once deleteGrace has passed. The first entity is never deleted.
func (r *Registry) Delete(ctx context.Context, slug string) error {
	r.refreshMu.Lock()
	defer r.refreshMu.Unlock()
	w, err := db.DeleteEntity(ctx, r.pool, slug)
	switch {
	case errors.Is(err, db.ErrNoEntity):
		return fmt.Errorf("%w: no entity %q; GET /v1/entities lists them", core.ErrNotFound, slug)
	case errors.Is(err, db.ErrFirstEntity):
		return fmt.Errorf("%w: %s is the first entity, which /v1 and /mcp without /e/<slug> mean, and its tables share a schema with the team, keys and sign-in, so it cannot be deleted", core.ErrConflict, slug)
	case err != nil:
		return err
	}
	r.remove(w.ID)
	return nil
}

// deleteGrace is how long Sweep leaves a deleted entity's tables, well
// past refreshEvery, so that by then no replica serves it any more.
const deleteGrace = 5 * time.Minute

// Sweep drops the schema and row of every entity deleted at least
// r.grace ago. Every replica runs it; the migration lock lets one drop
// each entity and the rest find nothing left.
func (r *Registry) Sweep(ctx context.Context) error {
	due, err := db.DeletedEntities(ctx, r.pool, r.grace)
	if err != nil || len(due) == 0 {
		return err
	}
	for _, w := range due {
		r.remove(w.ID)
	}
	return r.asMigrator(ctx, func(pool db.Pool) error {
		var errs []error
		for _, w := range due {
			dropped, err := db.DropEntity(ctx, pool, w)
			if err != nil {
				errs = append(errs, fmt.Errorf("drop entity %s (%s): %w", w.Slug, w.Schema, err))
			}
			if dropped {
				r.log.Info("dropped a deleted entity's tables", "entity", w.Slug, "schema", w.Schema.String())
			}
		}
		return errors.Join(errs...)
	})
}

type entityKey struct{}

// NewContext is ctx carrying e, the entity a request is for.
func NewContext(ctx context.Context, e Live) context.Context {
	return context.WithValue(ctx, entityKey{}, e)
}

// FromContext is the entity NewContext put on ctx, if any.
func FromContext(ctx context.Context) (Live, bool) {
	e, ok := ctx.Value(entityKey{}).(Live)
	return e, ok
}
