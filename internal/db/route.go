package db

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type schemaKey struct{}

// pathKey is where a connection remembers which workspace's search_path it
// has, so switching costs a round trip and staying costs none.
const pathKey = "byname.schema"

// RouteSchemas makes every connection cfg's pools hand out reach the
// tables of the workspace its query is bound to with In, and the first
// workspace's when it is bound to none, as sign-in and every query before
// workspaces are. Code that queries through these pools must never SET
// search_path outside a transaction; SET LOCAL is fine.
func RouteSchemas(cfg *pgxpool.Config) {
	cfg.PrepareConn = func(ctx context.Context, conn *pgx.Conn) (bool, error) {
		want, _ := ctx.Value(schemaKey{}).(Schema)
		data := conn.PgConn().CustomData()
		if have, _ := data[pathKey].(Schema); have == want {
			return true, nil
		}
		// The first workspace's path is the connection's own, so an
		// instance with one workspace never sets it.
		sql := `RESET search_path`
		if want != Public {
			sql = `SET search_path = ` + want.searchPath()
		}
		if _, err := conn.Exec(ctx, sql); err != nil {
			return false, err
		}
		data[pathKey] = want
		return true, nil
	}
}

// In is p with every query, transaction and lock connection bound to
// schema's workspace. p's pools must come from a config RouteSchemas
// prepared; In panics on a pool that was not, since its queries would
// silently read the first workspace's tables.
func In(p Pool, schema Schema) Pool {
	if b, ok := p.(bound); ok {
		p = b.p
	}
	var pools []*pgxpool.Pool
	switch q := p.(type) {
	case *pgxpool.Pool:
		pools = []*pgxpool.Pool{q}
	case Lanes:
		pools = []*pgxpool.Pool{q.Web, q.Jobs, q.Locks}
	}
	for _, q := range pools {
		if q.Config().PrepareConn == nil {
			panic("db.In: the pool's config did not go through db.RouteSchemas")
		}
	}
	return bound{p, schema}
}

type bound struct {
	p      Pool
	schema Schema
}

func (b bound) bind(ctx context.Context) context.Context {
	return context.WithValue(ctx, schemaKey{}, b.schema)
}

func (b bound) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return b.p.Exec(b.bind(ctx), sql, args...)
}

func (b bound) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return b.p.Query(b.bind(ctx), sql, args...)
}

func (b bound) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return b.p.QueryRow(b.bind(ctx), sql, args...)
}

func (b bound) Begin(ctx context.Context) (pgx.Tx, error) { return b.p.Begin(b.bind(ctx)) }

func (b bound) Acquire(ctx context.Context) (*pgxpool.Conn, error) { return b.p.Acquire(b.bind(ctx)) }
