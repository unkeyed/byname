package db

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Entity is one brand an instance tracks, with everything that brand
// is set up with and every result it has. Its tables live in Schema.
type Entity struct {
	ID        int64
	Slug      string
	Name      string
	Schema    Schema
	CreatedAt time.Time
}

// Schema is the Postgres schema an entity's tables live in. The zero
// value, Home, is the first entity's. Every later entity has entity_<id>.
type Schema struct{ id int64 }

// Home is the first entity's schema: the install's own, where every
// release before entities created the tables. It is whichever schema the
// role's own search_path resolves to, public unless DATABASE_URL or the
// role says otherwise, so it has no fixed name and String is empty.
var Home Schema

func (s Schema) String() string {
	if s == Home {
		return ""
	}
	return "entity_" + strconv.FormatInt(s.id, 10)
}

// setPath makes $1 the first schema on search_path, ahead of the role's
// own path, where the shared tables and functions such as page_key live.
// $2 is set_config's is_local. It answers the schema queries now use.
const setPath = `SELECT set_config('search_path', concat_ws(', ', $1::text, nullif(reset_val, '')), $2), current_schema()
	FROM pg_settings WHERE name = 'search_path'`

var entitySchema = regexp.MustCompile(`^entity_([0-9]+)$`)

func parseSchema(name string) Schema {
	if m := entitySchema.FindStringSubmatch(name); m != nil {
		if id, err := strconv.ParseInt(m[1], 10, 64); err == nil && id > 0 {
			return Schema{id}
		}
	}
	return Home
}

// SharedTables are the tables every entity shares: the team and how it
// signs in, API keys, OAuth, the AI provider keys and the bookkeeping of
// migrations and entities. They exist in the install's own schema only,
// and an entity's queries reach them through its search_path. Every
// other table and view there belongs to the first entity, and every
// later entity has its own copy.
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
	"entity",
	"entity_migration",
}

const entityColumns = `id, slug, name, schema_name, created_at`

func scanEntity(row pgx.Row) (Entity, error) {
	var w Entity
	var schema string
	if err := row.Scan(&w.ID, &w.Slug, &w.Name, &schema, &w.CreatedAt); err != nil {
		return Entity{}, err
	}
	w.Schema = parseSchema(schema)
	return w, nil
}

// ErrSlugTaken is CreateEntity's answer when another entity has the
// slug.
var ErrSlugTaken = errors.New("db: another entity has this slug")

// ErrSlugReserved is CreateEntity's answer for the slug default, which
// only the first entity ever has: a new instance's first entity
// starts there, and old links to /e/default mean it.
var ErrSlugReserved = errors.New("db: the slug default is the first entity's")

// ReservedSlug is the slug only the first entity ever has.
const ReservedSlug = "default"

// CreateEntity adds an entity, creates its schema and applies every
// entity migration to it, all in one transaction under the migration
// lock. Calling it again with the same name and slug returns the same
// entity, so a retry after any failure ends in the same state. The
// first entity is never made here, so its slug is always taken.
func CreateEntity(ctx context.Context, pool Pool, name, slug string) (Entity, error) {
	if slug == ReservedSlug {
		return Entity{}, ErrSlugReserved
	}
	files, err := entityFiles()
	if err != nil {
		return Entity{}, err
	}
	var w Entity
	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, migrateLock); err != nil {
			return err
		}
		w, err = scanEntity(tx.QueryRow(ctx, `SELECT `+entityColumns+` FROM entity WHERE slug = $1`, slug))
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			w, err = scanEntity(tx.QueryRow(ctx, `INSERT INTO entity (id, slug, name, schema_name) OVERRIDING SYSTEM VALUE
				SELECT id, $1, $2, 'entity_' || id FROM (SELECT nextval(pg_get_serial_sequence('entity', 'id')) AS id) next
				RETURNING `+entityColumns, slug, name))
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `CREATE SCHEMA `+pgx.Identifier{w.Schema.String()}.Sanitize()); err != nil {
				return err
			}
		case err != nil:
			return err
		case w.Name != name, w.Schema == Home:
			return ErrSlugTaken
		}
		for _, f := range files {
			if err := applyEntityLocked(ctx, tx, w, f); err != nil {
				return fmt.Errorf("migrate entity %s with %s: %w", w.Slug, f.name, err)
			}
		}
		return nil
	})
	if err != nil {
		return Entity{}, err
	}
	return w, nil
}

// SlugPattern is what an entity slug must match, as the entity
// table's CHECK says, with at most MaxSlugLen characters.
var SlugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

const MaxSlugLen = 40

var notSlug = regexp.MustCompile(`[^a-z0-9]+`)

// Slug is the slug an entity named name gets when none is chosen: its
// letters and digits in lower case, each run of anything else a hyphen,
// at most MaxSlugLen characters, or "default" when nothing is left. It is
// what 019_entities.sql gave the first entity.
func Slug(name string) string {
	s := strings.Trim(notSlug.ReplaceAllString(strings.ToLower(name), "-"), "-")
	s = strings.TrimRight(s[:min(len(s), MaxSlugLen)], "-")
	if s == "" {
		return "default"
	}
	return s
}

// Entities lists every entity, the first, which has the lowest id,
// first.
func Entities(ctx context.Context, pool Pool) ([]Entity, error) {
	rows, err := pool.Query(ctx, `SELECT `+entityColumns+` FROM entity ORDER BY id`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Entity, error) { return scanEntity(r) })
}

// ErrNoEntity is the answer about a slug no entity has.
var ErrNoEntity = errors.New("db: no entity has this slug")

// RenameEntity names the entity with slug name. Its slug stays.
func RenameEntity(ctx context.Context, pool Pool, slug, name string) (Entity, error) {
	w, err := scanEntity(pool.QueryRow(ctx, `UPDATE entity SET name = $2 WHERE slug = $1 RETURNING `+entityColumns, slug, name))
	if errors.Is(err, pgx.ErrNoRows) {
		return Entity{}, ErrNoEntity
	}
	return w, err
}

// CurrentEntity is the entity pool's queries are bound to with In,
// or the first entity when they are bound to none.
func CurrentEntity(ctx context.Context, pool Pool) (Entity, error) {
	return scanEntity(pool.QueryRow(ctx, `SELECT `+entityColumns+` FROM entity WHERE schema_name = current_schema()`))
}

// NameFirstEntity names the entity pool is bound to name, and gives
// it name's Slug, when it is the first entity and still has the name
// and slug a new instance starts with: Default and default. Any other
// entity keeps the name it was given. The slug stays default when
// another entity has name's.
func NameFirstEntity(ctx context.Context, pool Pool, name string) error {
	_, err := pool.Exec(ctx, `UPDATE entity w SET name = $1,
			slug = CASE WHEN EXISTS (SELECT 1 FROM entity o WHERE o.slug = $2) THEN w.slug ELSE $2 END
		WHERE w.schema_name = current_schema() AND w.schema_name !~ '^entity_[0-9]+$' AND w.slug = 'default' AND w.name = 'Default'`, name, Slug(name))
	return err
}
