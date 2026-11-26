package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type schemaKey struct{}

const pathKey = "byname.schema"

// RouteSchemas points each connection a pool built from cfg hands out at
// the tables of the entity its query is bound to with In, or at the
// first entity's when it is bound to none, as sign-in is. Code that
// queries through such a pool must never SET search_path outside a
// transaction; SET LOCAL is fine.
func RouteSchemas(cfg *pgxpool.Config) {
	cfg.PrepareConn = func(ctx context.Context, conn *pgx.Conn) (bool, error) {
		want, _ := ctx.Value(schemaKey{}).(Schema)
		data := conn.PgConn().CustomData()
		if have, _ := data[pathKey].(Schema); have == want {
			return true, nil
		}
		if want == Home {
			if _, err := conn.Exec(ctx, `RESET search_path`); err != nil {
				return false, err
			}
			delete(data, pathKey)
			return true, nil
		}
		// A schema that is missing, or that the role cannot use, would
		// leave the install's own schema first on the path and hand out
		// the first entity's rows, so that fails here instead.
		var first string
		if err := conn.QueryRow(ctx, setPath, pgx.Identifier{want.String()}.Sanitize(), false).Scan(nil, &first); err != nil {
			return false, err
		}
		if first != want.String() {
			return false, fmt.Errorf("db: entity schema %s is missing or not usable", want)
		}
		data[pathKey] = want
		return true, nil
	}
}

// In is p with every query, transaction and lock connection bound to
// schema's entity. p's pools must come from a config RouteSchemas
// prepared; In panics on a pool that was not, since its queries would
// silently read the first entity's tables.
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

// KeyScope keeps keys an entity sends to a service every entity
// shares, such as Treg's idempotency keys, apart from another entity's
// same key: "" for the first entity, whose keys predate entities and
// must not change, and "entity_<id>/" for a pool In bound to any other.
func KeyScope(p Pool) string {
	if b, ok := p.(bound); ok && b.schema != Home {
		return b.schema.String() + "/"
	}
	return ""
}
