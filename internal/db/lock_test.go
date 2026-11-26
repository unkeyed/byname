package db_test

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/unkeyed/byname/internal/db"
	"github.com/unkeyed/byname/internal/testdb"
)

// Two entities never hold each other's locks, and the first entity's
// key is the one releases before entities took.
func TestEntityLocksAreTheirOwn(t *testing.T) {
	ctx := context.Background()
	pool := testdb.New(t)
	other := testdb.Entity(t, pool)
	key := db.EntityLock("byname.test")

	var first, released, second int32
	if err := pool.QueryRow(ctx, `SELECT `+key+`, hashtext('byname.test')`).Scan(&first, &released); err != nil {
		t.Fatal(err)
	}
	if err := other.QueryRow(ctx, `SELECT `+key).Scan(&second); err != nil {
		t.Fatal(err)
	}
	if first != released || second == first {
		t.Fatalf("keys: first entity %d, released %d, second entity %d; want the first two equal and the third apart", first, released, second)
	}

	held, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()
	if _, err := held.Exec(ctx, `SELECT pg_advisory_lock(`+key+`)`); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name string
		p    db.Pool
		want bool
	}{{"another connection in the first entity", pool, false}, {"the second entity", other, true}} {
		conn, err := c.p.Acquire(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var got bool
		if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(`+key+`)`).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != c.want {
			t.Errorf("%s took the lock the first entity holds: %v, want %v", c.name, got, c.want)
		}
		if got {
			_, _ = conn.Exec(ctx, `SELECT pg_advisory_unlock(`+key+`)`)
		}
		conn.Release()
	}
}

// TestOnlySharedWorkTakesAGlobalLock fails when code outside sign-in and
// migrations keys an advisory lock without db.EntityLock, which would
// make every entity wait on, or skip, the others' work.
func TestOnlySharedWorkTakesAGlobalLock(t *testing.T) {
	root := filepath.Join("..", "..")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == "node_modules" || d.Name() == ".git") {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		if strings.HasPrefix(rel, filepath.Join("internal", "auth")+string(filepath.Separator)) ||
			strings.HasPrefix(rel, filepath.Join("internal", "db")+string(filepath.Separator)) {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(src), "hashtext(") {
			t.Errorf("%s keys a lock with hashtext; use db.EntityLock, or move the shared work to internal/auth", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
