package db

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Workspace is one brand an instance tracks, with everything that brand
// is set up with and every result it has. Its tables live in Schema.
type Workspace struct {
	ID        int64
	Slug      string
	Name      string
	Schema    Schema
	CreatedAt time.Time
}

// Schema is the Postgres schema a workspace's tables live in. The zero
// value, Public, is the first workspace's: the tables every release before
// workspaces created. Every later workspace has ws_<id>.
type Schema struct{ id int64 }

// Public is the first workspace's schema.
var Public Schema

func (s Schema) String() string {
	if s == Public {
		return "public"
	}
	return "ws_" + strconv.FormatInt(s.id, 10)
}

// searchPath finds s's tables first and the shared tables, which exist in
// public only, after them.
func (s Schema) searchPath() string {
	return pgx.Identifier{s.String()}.Sanitize() + ", public"
}

func parseSchema(name string) (Schema, error) {
	if name == "public" {
		return Public, nil
	}
	if rest, ok := strings.CutPrefix(name, "ws_"); ok {
		if id, err := strconv.ParseInt(rest, 10, 64); err == nil && id > 0 {
			return Schema{id}, nil
		}
	}
	return Public, fmt.Errorf("db: %q is not a workspace schema", name)
}

// SharedTables are the tables every workspace shares: the team and how it
// signs in, API keys, OAuth, the AI provider keys and the bookkeeping of
// migrations and workspaces. They exist in public only, and a workspace's
// queries reach them through its search_path. Every other table and view
// in public belongs to the first workspace, and every later workspace has
// its own copy.
var SharedTables = []string{
	"api_keys",
	"auth_challenges",
	"credential",
	"invites",
	"login_failures",
	"oauth_clients",
	"oauth_codes",
	"oauth_grants",
	"oauth_requests",
	"oauth_tokens",
	"passkeys",
	"recovery_codes",
	"request_counts",
	"schema_migrations",
	"sessions",
	"setup_tokens",
	"totp",
	"user_identities",
	"users",
	"workspace",
	"workspace_migration",
}

const workspaceColumns = `id, slug, name, schema_name, created_at`

func scanWorkspace(row pgx.Row) (Workspace, error) {
	var w Workspace
	var schema string
	if err := row.Scan(&w.ID, &w.Slug, &w.Name, &schema, &w.CreatedAt); err != nil {
		return Workspace{}, err
	}
	var err error
	w.Schema, err = parseSchema(schema)
	return w, err
}

// ErrSlugTaken is CreateWorkspace's answer when another workspace has the
// slug.
var ErrSlugTaken = errors.New("db: another workspace has this slug")

// CreateWorkspace adds a workspace, creates its schema and applies every
// workspace migration to it, all in one transaction under the migration
// lock. Calling it again with the same name and slug returns the same
// workspace, so a retry after any failure ends in the same state.
func CreateWorkspace(ctx context.Context, pool Pool, name, slug string) (Workspace, error) {
	files, err := workspaceFiles()
	if err != nil {
		return Workspace{}, err
	}
	var w Workspace
	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, migrateLock); err != nil {
			return err
		}
		w, err = scanWorkspace(tx.QueryRow(ctx, `SELECT `+workspaceColumns+` FROM workspace WHERE slug = $1`, slug))
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			w, err = scanWorkspace(tx.QueryRow(ctx, `INSERT INTO workspace (id, slug, name, schema_name) OVERRIDING SYSTEM VALUE
				SELECT id, $1, $2, 'ws_' || id FROM (SELECT nextval(pg_get_serial_sequence('workspace', 'id')) AS id) next
				RETURNING `+workspaceColumns, slug, name))
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `CREATE SCHEMA `+pgx.Identifier{w.Schema.String()}.Sanitize()); err != nil {
				return err
			}
		case err != nil:
			return err
		case w.Name != name:
			return ErrSlugTaken
		}
		for _, f := range files {
			if err := applyWorkspace(ctx, tx, w, f); err != nil {
				return fmt.Errorf("migrate workspace %s with %s: %w", w.Slug, f.name, err)
			}
		}
		return nil
	})
	if err != nil {
		return Workspace{}, err
	}
	return w, nil
}
