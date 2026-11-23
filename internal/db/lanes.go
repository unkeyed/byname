package db

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Pool is what the services query through. A *pgxpool.Pool is one, and so
// is Lanes, which keeps background work off the connections requests need.
// Acquire is only for holding a session advisory lock: the work done under
// the lock goes through the other methods.
type Pool interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Begin(ctx context.Context) (pgx.Tx, error)
	Acquire(ctx context.Context) (*pgxpool.Conn, error)
}

// Lanes sends each query to one of two pools by its context: work started
// under Background (checks, syncs, deliveries) to Jobs, everything else,
// which is requests, to Web. A check that fills Jobs never makes a page
// wait for a connection.
//
// Connections that hold a session lock come from Locks, never from the
// pools that do the work. A job holding a lock and waiting for a Jobs
// connection then never blocks another job holding a Jobs connection, so
// no number of jobs can wedge the pools, however small they are.
type Lanes struct {
	Web, Jobs, Locks *pgxpool.Pool
}

type backgroundKey struct{}

// Background marks ctx, and every context derived from it, as background
// work, so Lanes sends its queries to the Jobs pool.
func Background(ctx context.Context) context.Context {
	return context.WithValue(ctx, backgroundKey{}, true)
}

func (l Lanes) pick(ctx context.Context) *pgxpool.Pool {
	if bg, _ := ctx.Value(backgroundKey{}).(bool); bg {
		return l.Jobs
	}
	return l.Web
}

func (l Lanes) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return l.pick(ctx).Exec(ctx, sql, args...)
}

func (l Lanes) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return l.pick(ctx).Query(ctx, sql, args...)
}

func (l Lanes) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return l.pick(ctx).QueryRow(ctx, sql, args...)
}

func (l Lanes) Begin(ctx context.Context) (pgx.Tx, error) { return l.pick(ctx).Begin(ctx) }

func (l Lanes) Acquire(ctx context.Context) (*pgxpool.Conn, error) { return l.Locks.Acquire(ctx) }

// Close closes every pool that is open.
func (l Lanes) Close() {
	for _, p := range []*pgxpool.Pool{l.Web, l.Jobs, l.Locks} {
		if p != nil {
			p.Close()
		}
	}
}
