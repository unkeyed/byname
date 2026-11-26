package entities

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/unkeyed/byname/internal/app"
	"github.com/unkeyed/byname/internal/core"
	"github.com/unkeyed/byname/internal/db"
	"github.com/unkeyed/byname/internal/secretbox"
	"github.com/unkeyed/byname/internal/testdb"
)

var opts = Options{Version: "test", Secrets: secretbox.ForTest(), Log: slog.New(slog.DiscardHandler)}

type jobCounter struct {
	running map[int64]*atomic.Int32
}

func (c *jobCounter) build(w db.Entity, p db.Pool) (app.Services, func(context.Context)) {
	svc, _ := Build(w, p, opts)
	n := &atomic.Int32{}
	c.running[w.ID] = n
	return svc, func(ctx context.Context) {
		n.Add(1)
		<-ctx.Done()
		n.Add(-1)
	}
}

func (c *jobCounter) waitFor(t *testing.T, id int64, want int32) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if n := c.running[id]; n != nil && n.Load() == want {
			return
		}
	}
	t.Fatalf("entity %d: jobs running %v, want %d", id, c.running[id], want)
}

func TestAnEntityAnotherReplicaMadeGoesLiveOnce(t *testing.T) {
	ctx := context.Background()
	pool := testdb.New(t)
	jobs := &jobCounter{running: map[int64]*atomic.Int32{}}
	r := &Registry{pool: pool, opts: opts, log: opts.Log, byID: map[int64]*entry{}, build: jobs.build, now: time.Now}
	if err := r.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	first := r.Default()
	runCtx, stop := context.WithCancel(ctx)
	ran := make(chan struct{})
	go func() { r.Run(runCtx); close(ran) }()
	jobs.waitFor(t, first.Entity.ID, 1)

	other, err := Open(ctx, pool, opts)
	if err != nil {
		t.Fatal(err)
	}
	made, err := other.Create(ctx, "Kyring", "")
	if err != nil {
		t.Fatal(err)
	}
	if made.Slug != "kyring" || made.Default || made.Onboarded {
		t.Fatalf("Create = %+v, want kyring, not the default, not onboarded", made)
	}
	if _, err := r.ByID(ctx, made.ID); err != nil {
		t.Fatalf("the entity another replica made did not go live: %v", err)
	}
	for range 3 {
		if err := r.Refresh(ctx); err != nil {
			t.Fatal(err)
		}
	}
	jobs.waitFor(t, made.ID, 1)
	jobs.waitFor(t, first.Entity.ID, 1)
	if len(jobs.running) != 2 {
		t.Fatalf("built %d entities, want each of the 2 built once", len(jobs.running))
	}

	if _, err := other.Create(ctx, "Planetfall", "pf"); err != nil {
		t.Fatal(err)
	}
	pf, err := r.Lookup(ctx, "pf")
	if err != nil || pf.Entity.Name != "Planetfall" {
		t.Fatalf("Lookup(pf) = %+v %v", pf.Entity, err)
	}
	jobs.waitFor(t, pf.Entity.ID, 1)
	if _, err := r.Lookup(ctx, "nope"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("Lookup(nope) = %v, want not found", err)
	}

	list, err := r.List(ctx)
	if err != nil || len(list) != 3 || !list[0].Default || list[0].ID != first.Entity.ID || list[1].Default || list[2].Slug != "pf" {
		t.Fatalf("List = %+v %v, want the first as default, then kyring and pf", list, err)
	}

	stop()
	select {
	case <-ran:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after its context ended")
	}
	for id := range jobs.running {
		jobs.waitFor(t, id, 0)
	}
}

func TestCreateAndRename(t *testing.T) {
	ctx := context.Background()
	r, err := Open(ctx, testdb.New(t), opts)
	if err != nil {
		t.Fatal(err)
	}
	a, err := r.Create(ctx, "  Unkey  ", "")
	if err != nil || a.Name != "Unkey" || a.Slug != "unkey" {
		t.Fatalf("Create = %+v %v", a, err)
	}
	again, err := r.Create(ctx, "Unkey", "unkey")
	if err != nil || again.ID != a.ID {
		t.Fatalf("creating it again = %+v %v, want the same entity", again, err)
	}
	for _, c := range []struct {
		name, slug string
		want       error
	}{
		{"Unkey Two", "unkey", core.ErrConflict},
		{"", "", core.ErrInvalid},
		{"Bad slug", "Bad Slug", core.ErrInvalid},
		{"Trailing", "trailing-", core.ErrInvalid},
	} {
		if _, err := r.Create(ctx, c.name, c.slug); !errors.Is(err, c.want) {
			t.Errorf("Create(%q, %q) = %v, want %v", c.name, c.slug, err, c.want)
		}
	}
	renamed, err := r.Rename(ctx, "unkey", "Unkey API")
	if err != nil || renamed.Name != "Unkey API" || renamed.Slug != "unkey" {
		t.Fatalf("Rename = %+v %v", renamed, err)
	}
	if e, err := r.Lookup(ctx, "unkey"); err != nil || e.Entity.Name != "Unkey API" {
		t.Fatalf("Lookup after rename = %+v %v", e.Entity, err)
	}
	if _, err := r.Rename(ctx, "nope", "x"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("renaming a missing entity = %v, want not found", err)
	}
}

// A DATABASE_URL role that only reads and writes data cannot make an
// entity; MIGRATION_DATABASE_URL's role makes it, and the read/write
// role then serves it.
func TestCreateThroughTheMigrationRole(t *testing.T) {
	ctx := context.Background()
	admin := testdb.New(t)
	rw := readWriteRole(t, admin)

	r, err := Open(ctx, rw, opts)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Create(ctx, "Kyring", ""); err == nil || !strings.Contains(err.Error(), "MIGRATION_DATABASE_URL") {
		t.Fatalf("Create as a read/write role = %v, want an error naming MIGRATION_DATABASE_URL", err)
	}
	withMigrate := opts
	withMigrate.MigrateDB = admin.Config()
	r, err = Open(ctx, rw, withMigrate)
	if err != nil {
		t.Fatal(err)
	}
	made, err := r.Create(ctx, "Kyring", "")
	if err != nil || made.Slug != "kyring" || made.Onboarded {
		t.Fatalf("Create through MIGRATION_DATABASE_URL = %+v %v", made, err)
	}
	e, err := r.Lookup(ctx, "kyring")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Services.Config.Brands(ctx); err != nil {
		t.Fatalf("the read/write role cannot read the new entity's tables: %v", err)
	}
}

// An entity goes live only with every entity migration this binary
// has, as one another replica's older binary made would lack a newer
// file. The registry applies what is missing through the role Create
// uses, and until that works the entity is not served at all. Losing an
// entity_migration row stands in for the missing file.
func TestAnEntityGoesLiveOnlyWithEveryEntityMigration(t *testing.T) {
	ctx := context.Background()
	admin := testdb.New(t)
	rw := readWriteRole(t, admin)
	w, err := db.CreateEntity(ctx, admin, "Kyring", "kyring")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `DELETE FROM entity_migration WHERE entity_id = $1`, w.ID); err != nil {
		t.Fatal(err)
	}

	r, err := Open(ctx, rw, opts)
	if err != nil {
		t.Fatalf("Open with an entity it cannot catch up = %v, want the others served", err)
	}
	if _, err := r.ByID(ctx, w.ID); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("an entity lacking a migration the read/write role cannot apply: ByID = %v, want not found", err)
	}
	if _, err := r.Lookup(ctx, "default"); err != nil {
		t.Fatalf("the first entity: %v, want it served", err)
	}

	schema := pgx.Identifier{w.Schema.String()}.Sanitize()
	if _, err := admin.Exec(ctx, `DROP SCHEMA `+schema+` CASCADE; CREATE SCHEMA `+schema); err != nil {
		t.Fatal(err)
	}
	if err := r.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ByID(ctx, w.ID); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("after another refresh as the read/write role: ByID = %v, want still not found", err)
	}

	withMigrate := opts
	withMigrate.MigrateDB = admin.Config()
	r, err = Open(ctx, rw, withMigrate)
	if err != nil {
		t.Fatal(err)
	}
	e, err := r.ByID(ctx, w.ID)
	if err != nil {
		t.Fatalf("with MIGRATION_DATABASE_URL: ByID = %v, want the entity caught up and live", err)
	}
	if _, err := e.Services.Config.Brands(ctx); err != nil {
		t.Fatalf("the caught-up entity's tables: %v", err)
	}
}

func readWriteRole(t *testing.T, admin *pgxpool.Pool) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	role := fmt.Sprintf("byname_rw_%d", time.Now().UnixNano())
	for _, sql := range []string{
		`CREATE ROLE ` + role + ` LOGIN PASSWORD 'rw'`,
		`GRANT pg_read_all_data, pg_write_all_data TO ` + role,
	} {
		if _, err := admin.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { _, _ = admin.Exec(context.Background(), `DROP ROLE IF EXISTS `+role) })
	cfg := admin.Config().Copy()
	cfg.ConnConfig.User, cfg.ConnConfig.Password = role, "rw"
	db.RouteSchemas(cfg)
	rw, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rw.Close)
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), `REVOKE pg_read_all_data, pg_write_all_data FROM `+role)
	})
	return rw
}

// Anyone can ask for a made-up slug, so a lookup this process cannot
// answer refreshes from the database at most once every missRefreshEvery.
// An entity another replica made in that window goes live once it ends.
func TestUnknownSlugsRefreshAtMostOnceAWhile(t *testing.T) {
	ctx := context.Background()
	pool := testdb.New(t)
	r, err := Open(ctx, pool, opts)
	if err != nil {
		t.Fatal(err)
	}
	clock := time.Now()
	r.now = func() time.Time { return clock }
	other, err := Open(ctx, pool, opts)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := r.Lookup(ctx, "nope"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("Lookup(nope) = %v, want not found", err)
	}
	kyring, err := other.Create(ctx, "Kyring", "")
	if err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(missRefreshEvery - time.Millisecond)
	if _, err := r.Lookup(ctx, "kyring"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("Lookup(kyring) within %v of a refresh that found nothing = %v, want not found without asking the database", missRefreshEvery, err)
	}
	if _, err := r.ByID(ctx, kyring.ID); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("ByID(kyring) within %v of a refresh that found nothing = %v, want not found", missRefreshEvery, err)
	}

	clock = clock.Add(time.Millisecond)
	if e, err := r.Lookup(ctx, "kyring"); err != nil || e.Entity.ID != kyring.ID {
		t.Fatalf("Lookup(kyring) once the wait is over = %+v %v", e.Entity, err)
	}
	pf, err := other.Create(ctx, "Planetfall", "pf")
	if err != nil {
		t.Fatal(err)
	}
	if e, err := r.ByID(ctx, pf.ID); err != nil || e.Entity.Slug != "pf" {
		t.Fatalf("ByID(pf) right after a refresh that found kyring = %+v %v", e.Entity, err)
	}

	if _, err := r.Lookup(ctx, "still-nope"); !errors.Is(err, core.ErrNotFound) {
		t.Fatal(err)
	}
	if made, err := r.Create(ctx, "Byname", ""); err != nil || made.Slug != "byname" {
		t.Fatalf("Create right after a miss = %+v %v", made, err)
	}
}
