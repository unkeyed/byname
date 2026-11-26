package db_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/unkeyed/byname/internal/db"
	"github.com/unkeyed/byname/internal/notify"
	"github.com/unkeyed/byname/internal/notify/notifytest"
	"github.com/unkeyed/byname/internal/results"
	"github.com/unkeyed/byname/internal/secretbox"
	"github.com/unkeyed/byname/internal/testdb"
)

// TestEntityBaselineMatchesPublic fails when a new entity's tables
// differ from the first entity's in any way a query could notice. A
// global migration that changes an entity table, instead of a file in
// migrations/entity, fails here.
func TestEntityBaselineMatchesPublic(t *testing.T) {
	ctx := context.Background()
	pool := testdb.New(t)
	w, err := db.CreateEntity(ctx, pool, "Second", "second")
	if err != nil {
		t.Fatal(err)
	}
	if w.Schema == db.Home {
		t.Fatalf("CreateEntity = %+v, want a schema of its own", w)
	}

	var public []string
	if err := pool.QueryRow(ctx, `SELECT array_agg(c.relname::text) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public' AND c.relkind IN ('r', 'p', 'v', 'm', 'f')`).Scan(&public); err != nil {
		t.Fatal(err)
	}
	for _, s := range db.SharedTables {
		if !slices.Contains(public, s) {
			t.Errorf("db.SharedTables names %s, which public does not have", s)
		}
	}
	owned := slices.DeleteFunc(slices.Clone(public), func(r string) bool { return slices.Contains(db.SharedTables, r) })

	qualified := regexp.MustCompile(`\b(public|entity_[0-9]+)\.([a-z_][a-z0-9_]*)`)
	normalize := func(schema string) func(string) string {
		return func(s string) string {
			return qualified.ReplaceAllStringFunc(s, func(m string) string {
				p := qualified.FindStringSubmatch(m)
				if p[1] == schema && (schema != "public" || slices.Contains(owned, p[2])) {
					return "S." + p[2]
				}
				return m
			})
		}
	}
	want := describe(t, pool, "public", owned, normalize("public"))
	got := describe(t, pool, w.Schema.String(), nil, normalize(w.Schema.String()))
	for _, l := range want {
		if !slices.Contains(got, l) {
			t.Errorf("public has, %s lacks: %s", w.Schema, l)
		}
	}
	for _, l := range got {
		if !slices.Contains(want, l) {
			t.Errorf("%s has, public lacks: %s", w.Schema, l)
		}
	}
}

func describe(t *testing.T, pool *pgxpool.Pool, schema string, only []string, normalize func(string) string) []string {
	t.Helper()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SET LOCAL search_path = ''`); err != nil {
		t.Fatal(err)
	}
	rows, err := tx.Query(ctx, `
		WITH rel AS (
			SELECT c.oid, c.relname, c.relkind FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE n.nspname = $1 AND c.relkind IN ('r', 'p', 'v', 'm', 'f') AND ($2::text[] IS NULL OR c.relname = ANY ($2))
		)
		SELECT format('relation %s kind %s', relname, relkind) FROM rel
		UNION ALL
		SELECT format('column %s.%s #%s %s notnull=%s default=%s identity=%s generated=%s collation=%s sequence=%s',
			rel.relname, a.attname, row_number() OVER (PARTITION BY rel.oid ORDER BY a.attnum), format_type(a.atttypid, a.atttypmod),
			a.attnotnull, pg_get_expr(d.adbin, d.adrelid), a.attidentity, a.attgenerated, a.attcollation::regcollation,
			(SELECT format('%s,%s,%s,%s,%s,%s', s.seqtypid::regtype, s.seqstart, s.seqincrement, s.seqmin, s.seqmax, s.seqcycle)
				FROM pg_sequence s WHERE s.seqrelid = pg_get_serial_sequence(format('%I.%I', $1, rel.relname), a.attname)::regclass))
		FROM rel JOIN pg_attribute a ON a.attrelid = rel.oid AND a.attnum > 0 AND NOT a.attisdropped
		LEFT JOIN pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
		UNION ALL
		SELECT format('constraint %s.%s %s', rel.relname, c.conname, pg_get_constraintdef(c.oid))
		FROM rel JOIN pg_constraint c ON c.conrelid = rel.oid
		UNION ALL
		SELECT format('index %s', pg_get_indexdef(i.indexrelid))
		FROM rel JOIN pg_index i ON i.indrelid = rel.oid
		UNION ALL
		SELECT format('trigger %s', pg_get_triggerdef(tg.oid))
		FROM rel JOIN pg_trigger tg ON tg.tgrelid = rel.oid AND NOT tg.tgisinternal
		UNION ALL
		SELECT format('view %s %s', rel.relname, pg_get_viewdef(rel.oid))
		FROM rel WHERE rel.relkind IN ('v', 'm')`, schema, only)
	if err != nil {
		t.Fatal(err)
	}
	lines, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}

	rows, err = tx.Query(ctx, `SELECT c.relname, coalesce(array_agg(a.attname::text) FILTER (WHERE pg_get_expr(d.adbin, d.adrelid) = 'now()'), '{}')
		FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum > 0 AND NOT a.attisdropped
		LEFT JOIN pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
		WHERE n.nspname = $1 AND c.relkind IN ('r', 'p') AND ($2::text[] IS NULL OR c.relname = ANY ($2))
		GROUP BY c.relname`, schema, only)
	if err != nil {
		t.Fatal(err)
	}
	type table struct {
		name string
		now  []string
	}
	tables, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (table, error) {
		var tb table
		return tb, r.Scan(&tb.name, &tb.now)
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, tb := range tables {
		var data string
		if err := tx.QueryRow(ctx, `SELECT coalesce(jsonb_agg(to_jsonb(t) - $1::text[]
			ORDER BY to_jsonb(t)::text), '[]')::text FROM `+pgx.Identifier{schema, tb.name}.Sanitize()+` t`, tb.now).Scan(&data); err != nil {
			t.Fatal(err)
		}
		lines = append(lines, fmt.Sprintf("rows %s %s", tb.name, data))
	}
	for i, l := range lines {
		lines[i] = normalize(l)
	}
	slices.Sort(lines)
	return lines
}

// An instance upgraded from a release before entities is the first
// entity, named after its own brand, with its tables where they were.
func TestUpgradeMakesTheInstanceTheFirstEntity(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Through(t, "018_samples.sql")
	var runID int64
	if err := pool.QueryRow(ctx, `WITH b AS (INSERT INTO brand (name, kind) VALUES ('Northwind Cloud', 'self')),
		s AS (INSERT INTO question_set (version) VALUES (1) RETURNING id)
		INSERT INTO run (question_set_id, trigger, status, spend_usd) SELECT id, 'run_now', 'completed', 1.25 FROM s RETURNING id`).Scan(&runID); err != nil {
		t.Fatal(err)
	}

	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}

	var slug, name, schema string
	var baseline []string
	if err := pool.QueryRow(ctx, `SELECT slug, name, schema_name, ARRAY(SELECT name FROM entity_migration m WHERE m.entity_id = w.id)
		FROM entity w`).Scan(&slug, &name, &schema, &baseline); err != nil {
		t.Fatal(err)
	}
	if slug != "northwind-cloud" || name != "Northwind Cloud" || schema != "public" || !slices.Equal(baseline, []string{"000_baseline.sql"}) {
		t.Fatalf("entity = %s %q %s %v; want northwind-cloud, named after the brand, in public, with the baseline recorded", slug, name, schema, baseline)
	}
	var brand, status string
	var spend float64
	if err := pool.QueryRow(ctx, `SELECT (SELECT name FROM brand WHERE kind = 'self'), status, spend_usd FROM run WHERE id = $1`, runID).Scan(&brand, &status, &spend); err != nil {
		t.Fatal(err)
	}
	if brand != "Northwind Cloud" || status != "completed" || spend != 1.25 {
		t.Fatalf("after the upgrade: brand %q, run %s spending %v; want both as they were", brand, status, spend)
	}
	var schemas int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_namespace WHERE nspname LIKE 'entity\_%'`).Scan(&schemas); err != nil || schemas != 0 {
		t.Fatalf("%d entity schemas (%v), want none until someone adds an entity", schemas, err)
	}
}

func TestANewInstanceStartsWithADefaultEntity(t *testing.T) {
	pool := testdb.New(t)
	var slug, name, schema string
	if err := pool.QueryRow(context.Background(), `SELECT slug, name, schema_name FROM entity`).Scan(&slug, &name, &schema); err != nil {
		t.Fatal(err)
	}
	if slug != "default" || name != "Default" || schema != "public" {
		t.Fatalf("entity = %s %q %s, want default in public", slug, name, schema)
	}
}

func TestCreateEntityIsIdempotent(t *testing.T) {
	ctx := context.Background()
	pool := testdb.New(t)

	var wg sync.WaitGroup
	got := make(chan db.Entity, 3)
	for range 3 {
		wg.Go(func() {
			w, err := db.CreateEntity(ctx, pool, "Kyring", "kyring")
			if err != nil {
				t.Error(err)
			}
			got <- w
		})
	}
	wg.Wait()
	close(got)
	var first db.Entity
	for w := range got {
		if first.ID == 0 {
			first = w
		}
		if w != first {
			t.Fatalf("concurrent creates returned %+v and %+v, want one entity", first, w)
		}
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM entity`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("%d entities (%v), want the first and Kyring", n, err)
	}
	if _, err := db.CreateEntity(ctx, pool, "Kyring Labs", "kyring"); !errors.Is(err, db.ErrSlugTaken) {
		t.Fatalf("another name on a taken slug: %v, want ErrSlugTaken", err)
	}
}

// No entity but the first ever has the slug default, and only the
// first is renamed from it, so a slug never moves from one entity to
// another and a replica's stale slug can only name the right entity.
func TestOnlyTheFirstEntityIsEverDefault(t *testing.T) {
	ctx := context.Background()
	pool := testdb.New(t)
	for _, name := range []string{"Default", "Kyring"} {
		if _, err := db.CreateEntity(ctx, pool, name, "default"); !errors.Is(err, db.ErrSlugReserved) {
			t.Fatalf("CreateEntity(%q, default) = %v, want ErrSlugReserved", name, err)
		}
	}
	if err := db.NameFirstEntity(ctx, pool, "Unkey"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateEntity(ctx, pool, "Default", "default"); !errors.Is(err, db.ErrSlugReserved) {
		t.Fatalf("CreateEntity(Default, default) once the first is renamed = %v, want ErrSlugReserved", err)
	}
	if _, err := db.CreateEntity(ctx, pool, "Unkey", "unkey"); !errors.Is(err, db.ErrSlugTaken) {
		t.Fatalf("CreateEntity with the first entity's name and slug = %v, want ErrSlugTaken", err)
	}

	w, err := db.CreateEntity(ctx, pool, "Kyring", "kyring")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE entity SET name = 'Default', slug = 'default' WHERE id = $1`, w.ID); err != nil {
		t.Fatal(err)
	}
	kyring := db.In(pool, w.Schema)
	if err := db.NameFirstEntity(ctx, kyring, "Planetfall"); err != nil {
		t.Fatal(err)
	}
	if got, err := db.CurrentEntity(ctx, kyring); err != nil || got.Slug != "default" || got.Name != "Default" {
		t.Fatalf("a later entity at default after NameFirstEntity = %+v %v, want it left as it was", got, err)
	}
}

// Migrate applies the entity track to an entity that lacks a file.
// Losing the baseline's record and tables stands in for an entity made
// before a later entity migration.
func TestMigrateCatchesEveryEntityUp(t *testing.T) {
	ctx := context.Background()
	pool := testdb.New(t)
	w, err := db.CreateEntity(ctx, pool, "Planetfall", "planetfall")
	if err != nil {
		t.Fatal(err)
	}
	schema := pgx.Identifier{w.Schema.String()}.Sanitize()
	if _, err := pool.Exec(ctx, `DROP SCHEMA `+schema+` CASCADE; CREATE SCHEMA `+schema+`;
		DELETE FROM entity_migration WHERE entity_id = `+fmt.Sprint(w.ID)); err != nil {
		t.Fatal(err)
	}

	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var samples int
	if err := pool.QueryRow(ctx, `SELECT samples FROM `+schema+`.instance_settings`).Scan(&samples); err != nil || samples != 1 {
		t.Fatalf("%s.instance_settings after Migrate: samples %d, %v; want the baseline's row", w.Schema, samples, err)
	}
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate again: %v, want nothing left to do", err)
	}
}

func TestSlugMatchesTheMigration(t *testing.T) {
	ctx := context.Background()
	pool := testdb.New(t)
	for _, name := range []string{"Unkey", "Planet Fall", "  --Kyring & Co.--  ", "Ünïcode", "日本", "", "a" + strings.Repeat("-b", 30)} {
		var want string
		if err := pool.QueryRow(ctx, `SELECT coalesce(nullif(btrim(left(btrim(regexp_replace(lower($1), '[^a-z0-9]+', '-', 'g'), '-'), 40), '-'), ''), 'default')`,
			name).Scan(&want); err != nil {
			t.Fatal(err)
		}
		if got := db.Slug(name); got != want || !db.SlugPattern.MatchString(got) {
			t.Errorf("Slug(%q) = %q, the migration gives %q", name, got, want)
		}
	}
}

func TestEntitiesAreListedRenamedAndKnowThemselves(t *testing.T) {
	ctx := context.Background()
	pool := testdb.New(t)
	second, err := db.CreateEntity(ctx, pool, "Kyring", "kyring")
	if err != nil {
		t.Fatal(err)
	}
	all, err := db.Entities(ctx, pool)
	if err != nil || len(all) != 2 || all[0].Schema != db.Home || all[1].ID != second.ID {
		t.Fatalf("Entities = %+v %v, want the first then kyring", all, err)
	}
	for _, c := range []struct {
		pool db.Pool
		want int64
	}{{pool, all[0].ID}, {db.In(pool, second.Schema), second.ID}} {
		if w, err := db.CurrentEntity(ctx, c.pool); err != nil || w.ID != c.want {
			t.Errorf("CurrentEntity = %+v %v, want id %d", w, err, c.want)
		}
	}
	w, err := db.RenameEntity(ctx, pool, "kyring", "Kyring CRM")
	if err != nil || w.Name != "Kyring CRM" || w.Slug != "kyring" {
		t.Fatalf("RenameEntity = %+v %v, want the new name and the same slug", w, err)
	}
	if _, err := db.RenameEntity(ctx, pool, "nope", "x"); !errors.Is(err, db.ErrNoEntity) {
		t.Fatalf("renaming a missing entity: %v, want ErrNoEntity", err)
	}
}

// The first entity takes its brand's name once, and no other entity
// ever does.
func TestNameFirstEntity(t *testing.T) {
	ctx := context.Background()
	pool := testdb.New(t)
	taken, err := db.CreateEntity(ctx, pool, "Planetfall", "planetfall")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.NameFirstEntity(ctx, db.In(pool, taken.Schema), "Something else"); err != nil {
		t.Fatal(err)
	}
	if err := db.NameFirstEntity(ctx, pool, "Planetfall"); err != nil {
		t.Fatal(err)
	}
	if err := db.NameFirstEntity(ctx, pool, "Unkey"); err != nil {
		t.Fatal(err)
	}
	all, err := db.Entities(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	if all[0].Name != "Planetfall" || all[0].Slug != "default" {
		t.Errorf("first entity = %+v, want named Planetfall, keeping default as the slug planetfall is taken", all[0])
	}
	if all[1].Name != "Planetfall" || all[1].Slug != "planetfall" {
		t.Errorf("second entity = %+v, want it untouched", all[1])
	}

	fresh := testdb.New(t)
	if err := db.NameFirstEntity(ctx, fresh, "Unkey Inc."); err != nil {
		t.Fatal(err)
	}
	if w, err := db.CurrentEntity(ctx, fresh); err != nil || w.Name != "Unkey Inc." || w.Slug != "unkey-inc" {
		t.Fatalf("first entity = %+v %v, want Unkey Inc. at unkey-inc", w, err)
	}
}

// An install whose tables are not in public, because DATABASE_URL sets
// search_path or the role has a schema of its own, upgrades with its
// tables where they are: the first entity is that schema, its locks
// keep the keys earlier releases took, and a new entity reaches the
// shared tables and functions there.
func TestAnInstallOutsidePublicUpgrades(t *testing.T) {
	ctx := context.Background()
	pool := testdb.ThroughIn(t, "018_samples.sql", "app")
	if _, err := pool.Exec(ctx, `INSERT INTO brand (name, kind) VALUES ('Northwind Cloud', 'self')`); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}

	first, err := db.CurrentEntity(ctx, pool)
	if err != nil || first.Slug != "northwind-cloud" {
		t.Fatalf("CurrentEntity = %+v, %v; want the install as northwind-cloud", first, err)
	}

	alerts := notify.New(pool, results.New(pool), notify.Options{Secrets: secretbox.ForTest(), PublicURL: &url.URL{Scheme: "https", Host: "byname.example"}})
	rec, srv := notifytest.NewReceiver(t)
	if _, err := alerts.CreateChannel(ctx, notify.ChannelInput{Name: "hook", Kind: notify.Webhook,
		Settings: json.RawMessage(`{"url":"` + srv.URL + `"}`), Events: []notify.Kind{notify.CheckFailed}}); err != nil {
		t.Fatal(err)
	}
	if err := alerts.Emit(ctx, notify.Event{Subject: "run/1", Payload: notify.CheckFailedPayload{RunID: 1, Message: "x"}}); err != nil {
		t.Fatal(err)
	}
	if n, err := alerts.DeliverDue(ctx); err != nil || n != 1 || len(rec.Requests()) != 1 {
		t.Fatalf("DeliverDue = %d, %v, %d requests; want the alert delivered", n, err, len(rec.Requests()))
	}

	var sameKey bool
	if err := pool.QueryRow(ctx, `SELECT `+db.EntityLock("byname.test")+` = hashtext('byname.test')`).Scan(&sameKey); err != nil || !sameKey {
		t.Fatalf("the first entity's lock key is hashtext(name): %v, %v; want true, as releases before entities took it", sameKey, err)
	}

	w, err := db.CreateEntity(ctx, pool, "Kyring", "kyring")
	if err != nil {
		t.Fatal(err)
	}
	kyring := db.In(pool, w.Schema)
	var key string
	var users, brands int
	if err := kyring.QueryRow(ctx, `SELECT page_key('https://www.Example.com/a/'), (SELECT count(*) FROM users), (SELECT count(*) FROM brand)`).
		Scan(&key, &users, &brands); err != nil || key != "example.com/a" || brands != 0 {
		t.Fatalf("Kyring: page_key %q, %d users, %d brands, %v; want app's page_key and users and its own empty brand table", key, users, brands, err)
	}
	if got, err := db.CurrentEntity(ctx, kyring); err != nil || got.ID != w.ID {
		t.Fatalf("CurrentEntity in Kyring = %+v, %v; want Kyring", got, err)
	}
}
