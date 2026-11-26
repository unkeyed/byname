package workspaces

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/unkeyed/byname/internal/app"
	"github.com/unkeyed/byname/internal/core"
	"github.com/unkeyed/byname/internal/db"
	"github.com/unkeyed/byname/internal/secretbox"
	"github.com/unkeyed/byname/internal/testdb"
)

var opts = Options{Version: "test", Secrets: secretbox.ForTest(), Log: slog.New(slog.DiscardHandler)}

// jobCounter stands in for each workspace's jobs, counting how many are
// running, per workspace.
type jobCounter struct {
	running map[int64]*atomic.Int32
}

func (c *jobCounter) build(w db.Workspace, p db.Pool) (app.Services, func(context.Context)) {
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
	t.Fatalf("workspace %d: jobs running %v, want %d", id, c.running[id], want)
}

func TestAWorkspaceAnotherReplicaMadeGoesLiveOnce(t *testing.T) {
	ctx := context.Background()
	pool := testdb.New(t)
	jobs := &jobCounter{running: map[int64]*atomic.Int32{}}
	r := &Registry{pool: pool, opts: opts, log: opts.Log, byID: map[int64]*live{}, build: jobs.build, now: time.Now}
	if err := r.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	first := r.Default()
	runCtx, stop := context.WithCancel(ctx)
	ran := make(chan struct{})
	go func() { r.Run(runCtx); close(ran) }()
	jobs.waitFor(t, first.Workspace.ID, 1)

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
		t.Fatalf("the workspace another replica made did not go live: %v", err)
	}
	for range 3 {
		if err := r.Refresh(ctx); err != nil {
			t.Fatal(err)
		}
	}
	jobs.waitFor(t, made.ID, 1)
	jobs.waitFor(t, first.Workspace.ID, 1)
	if len(jobs.running) != 2 {
		t.Fatalf("built %d workspaces, want each of the 2 built once", len(jobs.running))
	}

	// A slug this process has not seen goes live on the lookup itself.
	if _, err := other.Create(ctx, "Planetfall", "pf"); err != nil {
		t.Fatal(err)
	}
	pf, err := r.Lookup(ctx, "pf")
	if err != nil || pf.Workspace.Name != "Planetfall" {
		t.Fatalf("Lookup(pf) = %+v %v", pf.Workspace, err)
	}
	jobs.waitFor(t, pf.Workspace.ID, 1)
	if _, err := r.Lookup(ctx, "nope"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("Lookup(nope) = %v, want not found", err)
	}

	list, err := r.List(ctx)
	if err != nil || len(list) != 3 || !list[0].Default || list[0].ID != first.Workspace.ID || list[1].Default || list[2].Slug != "pf" {
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
		t.Fatalf("creating it again = %+v %v, want the same workspace", again, err)
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
	if e, err := r.Lookup(ctx, "unkey"); err != nil || e.Workspace.Name != "Unkey API" {
		t.Fatalf("Lookup after rename = %+v %v", e.Workspace, err)
	}
	if _, err := r.Rename(ctx, "nope", "x"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("renaming a missing workspace = %v, want not found", err)
	}
}

// A DATABASE_URL role that only reads and writes data cannot make a
// workspace; MIGRATION_DATABASE_URL's role makes it, and the read/write
// role then serves it.
func TestCreateThroughTheMigrationRole(t *testing.T) {
	ctx := context.Background()
	admin := testdb.New(t)
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
		// The role owns nothing, but its grants must go before it can.
		_, _ = admin.Exec(context.Background(), `REVOKE pg_read_all_data, pg_write_all_data FROM `+role)
	})

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
		t.Fatalf("the read/write role cannot read the new workspace's tables: %v", err)
	}
}

// Anyone can ask for a made-up slug, so a lookup this process cannot
// answer refreshes from the database at most once every missRefreshEvery.
// A workspace another replica made in that window goes live once it ends.
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
	if e, err := r.Lookup(ctx, "kyring"); err != nil || e.Workspace.ID != kyring.ID {
		t.Fatalf("Lookup(kyring) once the wait is over = %+v %v", e.Workspace, err)
	}
	// That refresh found what it looked for, so it holds nothing back.
	pf, err := other.Create(ctx, "Planetfall", "pf")
	if err != nil {
		t.Fatal(err)
	}
	if e, err := r.ByID(ctx, pf.ID); err != nil || e.Workspace.Slug != "pf" {
		t.Fatalf("ByID(pf) right after a refresh that found kyring = %+v %v", e.Workspace, err)
	}

	// Create answers from its own refresh, whatever a lookup just missed.
	if _, err := r.Lookup(ctx, "still-nope"); !errors.Is(err, core.ErrNotFound) {
		t.Fatal(err)
	}
	if made, err := r.Create(ctx, "Byname", ""); err != nil || made.Slug != "byname" {
		t.Fatalf("Create right after a miss = %+v %v", made, err)
	}
}
