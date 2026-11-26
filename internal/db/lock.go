package db

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// EntityLock is the SQL for the advisory lock key named name in the
// entity the connection is bound to, so work in one entity never
// waits on or skips the same work in another. The first entity, the
// one whose schema is not entity_<id>, keeps hashtext(name), the key releases
// before entities took, so the old and new copies running side by side
// during a deploy still keep each other out. name is a constant such as
// byname.checks.claim.
func EntityLock(name string) string {
	return `hashtext(CASE WHEN current_schema() ~ '^entity_[0-9]+$' THEN '` + name + `:' || current_schema() ELSE '` + name + `' END)`
}

// lockConnWait is how long TryLock waits for a lock connection. Every
// entity's jobs share the few the Locks pool has, so one entity's
// long sync can hold one for minutes.
var lockConnWait = 3 * time.Second

// TryLock takes the session advisory lock key, an EntityLock, on a
// connection of its own, so the lock dies with the process, and returns
// unlock. ok is false when another process holds the lock, or when no lock
// connection frees up within a few seconds: a background job then skips
// this round and runs again on its next tick.
func TryLock(ctx context.Context, pool Pool, key string) (unlock func(), ok bool, err error) {
	wait, cancel := context.WithTimeout(ctx, lockConnWait)
	conn, err := pool.Acquire(wait)
	busy := wait.Err() != nil && ctx.Err() == nil
	cancel()
	if err != nil && busy {
		slog.DebugContext(ctx, "every lock connection is busy; skipping this round", "lock", key)
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return hold(ctx, conn, `SELECT pg_try_advisory_lock(`+key+`)`, key)
}

// Lock is TryLock for a request that must wait for the lock: it waits as
// long as ctx allows for a connection and then for the lock.
func Lock(ctx context.Context, pool Pool, key string) (unlock func(), err error) {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	unlock, _, err = hold(ctx, conn, `SELECT pg_advisory_lock(`+key+`) IS NOT NULL`, key)
	return unlock, err
}

func hold(ctx context.Context, conn *pgxpool.Conn, take, key string) (func(), bool, error) {
	var got bool
	if err := conn.QueryRow(ctx, take).Scan(&got); err != nil || !got {
		conn.Release()
		return nil, false, err
	}
	return func() {
		if _, err := conn.Exec(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock(`+key+`)`); err != nil {
			_ = conn.Conn().Close(context.WithoutCancel(ctx))
		}
		conn.Release()
	}, true, nil
}
