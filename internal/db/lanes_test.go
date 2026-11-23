package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/unkeyed/byname/internal/db"
	"github.com/unkeyed/byname/internal/testdb"
)

func TestBackgroundWorkCannotTakeRequestConnections(t *testing.T) {
	ctx := context.Background()
	base := testdb.New(t).Config()
	open := func() *pgxpool.Pool {
		cfg := base.Copy()
		cfg.MaxConns = 1
		p, err := pgxpool.NewWithConfig(ctx, cfg)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(p.Close)
		return p
	}
	lanes := db.Lanes{Web: open(), Jobs: open(), Locks: open()}

	bg := db.Background(ctx)
	held, err := lanes.Begin(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Rollback(ctx)

	short, cancel := context.WithTimeout(bg, 200*time.Millisecond)
	defer cancel()
	if _, err := lanes.Exec(short, `SELECT 1`); err == nil {
		t.Fatal("a second background query got a connection, want it to wait for the full jobs pool")
	}

	req, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var one int
	if err := lanes.QueryRow(req, `SELECT 1`).Scan(&one); err != nil || one != 1 {
		t.Fatalf("a request query while background work holds every jobs connection: %v, want an answer", err)
	}
}

func TestJobsHoldingLocksStillGetConnectionsForTheirWork(t *testing.T) {
	ctx := db.Background(context.Background())
	base := testdb.New(t).Config()
	open := func(n int32) *pgxpool.Pool {
		cfg := base.Copy()
		cfg.MaxConns = n
		p, err := pgxpool.NewWithConfig(context.Background(), cfg)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(p.Close)
		return p
	}
	lanes := db.Lanes{Web: open(1), Jobs: open(1), Locks: open(1)}

	// Two jobs each take a lock and then query, as the scheduler's jobs do.
	// With locks held on work connections, each would wait for the other's.
	done := make(chan error, 2)
	for _, key := range []string{"a", "b"} {
		go func() {
			conn, err := lanes.Acquire(ctx)
			if err != nil {
				done <- err
				return
			}
			defer conn.Release()
			if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock(hashtext($1))`, key); err != nil {
				done <- err
				return
			}
			short, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			_, err = lanes.Exec(short, `SELECT pg_sleep(0.1)`)
			done <- err
		}()
	}
	for range 2 {
		if err := <-done; err != nil {
			t.Fatalf("a job holding a lock could not run its query: %v", err)
		}
	}
}
