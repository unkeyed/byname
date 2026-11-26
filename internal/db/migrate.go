// Package db applies the embedded SQL migrations at startup and routes
// each query to its workspace's tables.
package db

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql migrations/workspace/*.sql
var migrations embed.FS

const migrateLock = `SELECT pg_advisory_xact_lock(hashtext('byname.migrate'))`

// Migrate applies every migrations/NNN_*.sql not yet recorded in
// schema_migrations, in name order, then every migrations/workspace file
// to each workspace that has not recorded it in workspace_migration. Each
// file runs in its own transaction holding the migrate lock, so replicas
// booting together apply each file exactly once and the losers wait, then
// find nothing to do. Files are never edited after release; a change is a
// new file.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	if err := MigrateThrough(ctx, pool, ""); err != nil {
		return err
	}
	return migrateWorkspaces(ctx, pool)
}

// MigrateThrough applies the global track only, stopping after the file
// named last, such as 016_social_filters.sql, so a test can build a
// database as an earlier release left it. An empty last applies every
// global file.
func MigrateThrough(ctx context.Context, pool *pgxpool.Pool, last string) error {
	names, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)
	if last != "" {
		i := slices.Index(names, "migrations/"+last)
		if i < 0 {
			return fmt.Errorf("no migration %s", last)
		}
		names = names[:i+1]
	}
	for _, name := range names {
		sql, err := migrations.ReadFile(name)
		if err != nil {
			return err
		}
		if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			return apply(ctx, tx, name, string(sql))
		}); err != nil {
			return fmt.Errorf("migrate %s: %w", name, err)
		}
	}
	return nil
}

func apply(ctx context.Context, tx pgx.Tx, name, sql string) error {
	if _, err := tx.Exec(ctx, migrateLock); err != nil {
		return err
	}
	// CREATE TABLE IF NOT EXISTS needs CREATE on the schema even when the
	// table exists, so a role that only reads and writes data could not
	// start on an up-to-date database.
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT to_regclass('schema_migrations') IS NOT NULL`).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		if _, err := tx.Exec(ctx, `CREATE TABLE schema_migrations (
			name       text PRIMARY KEY,
			applied_at timestamptz NOT NULL DEFAULT now()
		)`); err != nil {
			return err
		}
	}
	var applied bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE name = $1)`, name).Scan(&applied); err != nil {
		return err
	}
	if applied {
		return nil
	}
	if _, err := tx.Exec(ctx, sql); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO schema_migrations (name) VALUES ($1)`, name)
	return err
}

type workspaceFile struct{ name, sql string }

func workspaceFiles() ([]workspaceFile, error) {
	names, err := fs.Glob(migrations, "migrations/workspace/*.sql")
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	files := make([]workspaceFile, len(names))
	for i, name := range names {
		sql, err := migrations.ReadFile(name)
		if err != nil {
			return nil, err
		}
		files[i] = workspaceFile{path.Base(name), string(sql)}
	}
	return files, nil
}

func migrateWorkspaces(ctx context.Context, pool Pool) error {
	all, err := Workspaces(ctx, pool)
	if err != nil {
		return err
	}
	for _, w := range all {
		if err := MigrateWorkspace(ctx, pool, w); err != nil {
			return err
		}
	}
	return nil
}

// MigrateWorkspace applies every migrations/workspace file w has not
// recorded, each in its own transaction holding the migrate lock, as
// Migrate does at start. A workspace another replica's older binary made
// lacks the files only this binary has.
func MigrateWorkspace(ctx context.Context, pool Pool, w Workspace) error {
	missing, err := missingWorkspaceFiles(ctx, pool, w.ID)
	if err != nil {
		return err
	}
	for _, f := range missing {
		if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, migrateLock); err != nil {
				return err
			}
			return applyWorkspaceLocked(ctx, tx, w, f)
		}); err != nil {
			return fmt.Errorf("migrate workspace %s with %s: %w", w.Slug, f.name, err)
		}
	}
	return nil
}

// WorkspaceMigrated reports whether the workspace with id has recorded
// every migrations/workspace file this binary has.
func WorkspaceMigrated(ctx context.Context, pool Pool, id int64) (bool, error) {
	missing, err := missingWorkspaceFiles(ctx, pool, id)
	return len(missing) == 0, err
}

func missingWorkspaceFiles(ctx context.Context, pool Pool, id int64) ([]workspaceFile, error) {
	files, err := workspaceFiles()
	if err != nil {
		return nil, err
	}
	var applied []string
	if err := pool.QueryRow(ctx, `SELECT ARRAY(SELECT name FROM workspace_migration WHERE workspace_id = $1)`, id).Scan(&applied); err != nil {
		return nil, err
	}
	return slices.DeleteFunc(files, func(f workspaceFile) bool { return slices.Contains(applied, f.name) }), nil
}

func applyWorkspaceLocked(ctx context.Context, tx pgx.Tx, w Workspace, f workspaceFile) error {
	var applied bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM workspace_migration WHERE workspace_id = $1 AND name = $2)`,
		w.ID, f.name).Scan(&applied); err != nil {
		return err
	}
	if applied {
		return nil
	}
	if w.Schema != Home {
		if _, err := tx.Exec(ctx, setPath, pgx.Identifier{w.Schema.String()}.Sanitize(), true); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, f.sql); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO workspace_migration (workspace_id, name) VALUES ($1, $2)`, w.ID, f.name)
	return err
}
