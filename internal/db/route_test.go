package db_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/unkeyed/byname/internal/db"
	"github.com/unkeyed/byname/internal/testdb"
)

// A pool bound to an entity keeps Lanes' routing: background work still
// waits for a Jobs connection, and every lane, the lock lane included,
// reaches the entity's tables, then the first entity's again once
// unbound.
func TestBoundLanesReachTheEntity(t *testing.T) {
	ctx := context.Background()
	base := testdb.New(t)
	w, err := db.CreateEntity(ctx, base, "Kyring", "kyring")
	if err != nil {
		t.Fatal(err)
	}
	open := func() *pgxpool.Pool {
		cfg := base.Config()
		cfg.MaxConns = 1
		p, err := pgxpool.NewWithConfig(ctx, cfg)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(p.Close)
		return p
	}
	lanes := db.Lanes{Web: open(), Jobs: open(), Locks: open()}
	kyring := db.In(lanes, w.Schema)
	bg := db.Background(ctx)

	held, err := lanes.Jobs.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	short, cancel := context.WithTimeout(bg, 200*time.Millisecond)
	defer cancel()
	if _, err := kyring.Exec(short, `SELECT 1`); err == nil {
		t.Fatal("bound background work got a connection while Jobs' only one was taken, want it to wait")
	}
	_ = held.Rollback(ctx)

	for name, c := range map[string]context.Context{"request": ctx, "background": bg} {
		var got string
		if err := kyring.QueryRow(c, `SELECT current_schema()`).Scan(&got); err != nil || got != w.Schema.String() {
			t.Errorf("a bound %s query ran in %q (%v), want %s", name, got, err, w.Schema)
		}
		if err := lanes.QueryRow(c, `SELECT current_schema()`).Scan(&got); err != nil || got != "public" {
			t.Errorf("an unbound %s query after a bound one ran in %q (%v), want public", name, got, err)
		}
	}
	conn, err := kyring.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var got string
	if err := conn.QueryRow(ctx, `SELECT current_schema()`).Scan(&got); err != nil || got != w.Schema.String() {
		t.Errorf("a bound lock connection is in %q (%v), want %s", got, err, w.Schema)
	}
	conn.Release()
}

func TestInRefusesAPoolThatDoesNotRoute(t *testing.T) {
	cfg := testdb.New(t).Config()
	cfg.PrepareConn = nil
	p, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	defer func() {
		if recover() == nil {
			t.Fatal("db.In bound a pool that would read the first entity's tables, want a panic")
		}
	}()
	db.In(p, db.Home)
}

func TestAMissingEntitySchemaFailsInsteadOfReadingPublic(t *testing.T) {
	ctx := context.Background()
	pool := testdb.New(t)
	w, err := db.CreateEntity(ctx, pool, "Planetfall", "planetfall")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `DROP SCHEMA `+w.Schema.String()+` CASCADE`); err != nil {
		t.Fatal(err)
	}
	var samples int
	if err := db.In(pool, w.Schema).QueryRow(ctx, `SELECT samples FROM instance_settings`).Scan(&samples); err == nil {
		t.Fatal("a query bound to a dropped entity read instance_settings, want an error")
	}
}

// A connection skips the server for a binding it served last. Once a
// deleted entity's schema is dropped, neither the binding Retire ended
// nor a new one for the same schema may reach the first entity's tables
// through such a connection.
func TestADroppedSchemaNeverRoutesToTheFirstEntity(t *testing.T) {
	ctx := context.Background()
	pool := testdb.New(t)
	w, err := db.CreateEntity(ctx, pool, "Planetfall", "planetfall")
	if err != nil {
		t.Fatal(err)
	}
	cfg := pool.Config()
	cfg.MaxConns = 1
	one, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(one.Close)
	in := db.In(one, w.Schema)
	var samples int
	if err := in.QueryRow(ctx, `SELECT samples FROM instance_settings`).Scan(&samples); err != nil {
		t.Fatal(err)
	}
	db.Retire(in)
	if _, err := pool.Exec(ctx, `DROP SCHEMA `+w.Schema.String()+` CASCADE`); err != nil {
		t.Fatal(err)
	}
	if err := in.QueryRow(ctx, `SELECT samples FROM instance_settings`).Scan(&samples); !errors.Is(err, db.ErrRetired) {
		t.Fatalf("a query through a retired binding = %v, want ErrRetired", err)
	}
	if err := db.In(one, w.Schema).QueryRow(ctx, `SELECT samples FROM instance_settings`).Scan(&samples); err == nil {
		t.Fatal("a new binding to the dropped schema read instance_settings on a connection that had its path, want an error")
	}
}
