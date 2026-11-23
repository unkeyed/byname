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
	lanes := db.Lanes{Web: open(), Jobs: open()}

	bg := db.Background(ctx)
	held, err := lanes.Acquire(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()

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
