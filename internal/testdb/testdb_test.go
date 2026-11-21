package testdb_test

import (
	"context"
	"testing"

	"github.com/unkeyed/byname/internal/testdb"
)

func TestDatabasesAreMigratedAndIsolated(t *testing.T) {
	ctx := context.Background()
	a, b := testdb.New(t), testdb.New(t)

	var migrations int
	if err := a.QueryRow(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&migrations); err != nil {
		t.Fatal(err)
	}
	if migrations == 0 {
		t.Fatal("fresh database has no migrations applied")
	}

	if _, err := a.Exec(ctx, `INSERT INTO users (email, role) VALUES ('only-in-a@example.com', 'admin')`); err != nil {
		t.Fatal(err)
	}
	var inB int
	if err := b.QueryRow(ctx, `SELECT count(*) FROM users WHERE email = 'only-in-a@example.com'`).Scan(&inB); err != nil {
		t.Fatal(err)
	}
	if inB != 0 {
		t.Fatal("a row written in one test database is visible in another")
	}
}
