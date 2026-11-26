// Package db applies the embedded SQL migrations at startup and routes
// each query to its entity's tables.
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

//go:embed migrations/*.sql migrations/entity/*.sql
var migrations embed.FS

const migrateLock = `SELECT pg_advisory_xact_lock(hashtext('byname.migrate'))`

// Migrate applies every migrations/NNN_*.sql not yet recorded in
// schema_migrations, in name order, then every migrations/entity file
// to each entity that has not recorded it in entity_migration. Each
// file runs in its own transaction holding the migrate lock, so replicas
// booting together apply each file exactly once and the losers wait, then
// find nothing to do. Files are never edited after release; a change is a
// new file.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	if err := MigrateThrough(ctx, pool, ""); err != nil {
		return err
	}
	return migrateEntities(ctx, pool)
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

type entityFile struct{ name, sql string }

func entityFiles() ([]entityFile, error) {
	names, err := fs.Glob(migrations, "migrations/entity/*.sql")
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	files := make([]entityFile, len(names))
	for i, name := range names {
		sql, err := migrations.ReadFile(name)
		if err != nil {
			return nil, err
		}
		files[i] = entityFile{path.Base(name), string(sql)}
	}
	return files, nil
}

func migrateEntities(ctx context.Context, pool Pool) error {
	all, err := Entities(ctx, pool)
	if err != nil {
		return err
	}
	for _, w := range all {
		if err := MigrateEntity(ctx, pool, w); err != nil {
			return err
		}
	}
	return nil
}

// MigrateEntity applies every migrations/entity file w has not
// recorded, each in its own transaction holding the migrate lock, as
// Migrate does at start. An entity another replica's older binary made
// lacks the files only this binary has.
func MigrateEntity(ctx context.Context, pool Pool, w Entity) error {
	missing, err := missingEntityFiles(ctx, pool, w.ID)
	if err != nil {
		return err
	}
	for _, f := range missing {
		if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, migrateLock); err != nil {
				return err
			}
			return applyEntityLocked(ctx, tx, w, f)
		}); err != nil {
			return fmt.Errorf("migrate entity %s with %s: %w", w.Slug, f.name, err)
		}
	}
	return nil
}

// EntityMigrated reports whether the entity with id has recorded
// every migrations/entity file this binary has.
func EntityMigrated(ctx context.Context, pool Pool, id int64) (bool, error) {
	missing, err := missingEntityFiles(ctx, pool, id)
	return len(missing) == 0, err
}

func missingEntityFiles(ctx context.Context, pool Pool, id int64) ([]entityFile, error) {
	files, err := entityFiles()
	if err != nil {
		return nil, err
	}
	var applied []string
	if err := pool.QueryRow(ctx, `SELECT ARRAY(SELECT name FROM entity_migration WHERE entity_id = $1)`, id).Scan(&applied); err != nil {
		return nil, err
	}
	return slices.DeleteFunc(files, func(f entityFile) bool { return slices.Contains(applied, f.name) }), nil
}

func applyEntityLocked(ctx context.Context, tx pgx.Tx, w Entity, f entityFile) error {
	var applied bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM entity_migration WHERE entity_id = $1 AND name = $2)`,
		w.ID, f.name).Scan(&applied); err != nil {
		return err
	}
	if applied {
		return nil
	}
	if w.Schema != Home {
		// A schema dropped since w was read would leave the install's own
		// first on the path, and the file would change the first entity's
		// tables instead.
		var first string
		if err := tx.QueryRow(ctx, setPath, pgx.Identifier{w.Schema.String()}.Sanitize(), true).Scan(nil, &first); err != nil {
			return err
		}
		if first != w.Schema.String() {
			return fmt.Errorf("db: entity schema %s is missing or not usable", w.Schema)
		}
	}
	if _, err := tx.Exec(ctx, f.sql); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO entity_migration (entity_id, name) VALUES ($1, $2)`, w.ID, f.name)
	return err
}
