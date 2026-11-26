package db_test

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/unkeyed/byname/internal/core"
	"github.com/unkeyed/byname/internal/db"
	"github.com/unkeyed/byname/internal/engines"
	"github.com/unkeyed/byname/internal/panel"
	"github.com/unkeyed/byname/internal/testdb"
)

// TestRoleChecksMatchCore fails when a role CHECK in a migration lists other
// roles than core.Roles.
func TestRoleChecksMatchCore(t *testing.T) {
	want := make([]string, len(core.Roles))
	for i, r := range core.Roles {
		want[i] = string(r)
	}
	slices.Sort(want)
	check := regexp.MustCompile(`role IN \(([^)]*)\)`)
	files, err := filepath.Glob("migrations/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, f := range files {
		sql, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range check.FindAllStringSubmatch(string(sql), -1) {
			found++
			var got []string
			for _, v := range strings.Split(m[1], ",") {
				got = append(got, strings.Trim(strings.TrimSpace(v), "'"))
			}
			slices.Sort(got)
			if !slices.Equal(got, want) {
				t.Errorf("%s: %s lists %v, core.Roles is %v", f, m[0], got, want)
			}
		}
	}
	if found == 0 {
		t.Fatal("no role CHECK found in the migrations")
	}
}

func checkValues(t *testing.T, pool *pgxpool.Pool, schema, table, column string) [][]string {
	t.Helper()
	rows, err := pool.Query(context.Background(), `SELECT pg_get_constraintdef(c.oid) FROM pg_constraint c
		JOIN pg_class r ON r.oid = c.conrelid JOIN pg_namespace n ON n.oid = r.relnamespace
		JOIN pg_attribute a ON a.attrelid = r.oid AND a.attnum = c.conkey[1]
		WHERE c.contype = 'c' AND cardinality(c.conkey) = 1 AND n.nspname = $1 AND r.relname = $2 AND a.attname = $3`, schema, table, column)
	if err != nil {
		t.Fatal(err)
	}
	defs, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	var out [][]string
	for _, def := range defs {
		if vals := quoted(def); len(vals) > 0 {
			out = append(out, vals)
		}
	}
	if len(out) == 0 {
		t.Errorf("%s.%s has no CHECK listing the values of %s", schema, table, column)
	}
	return out
}

var literal = regexp.MustCompile(`'([^']*)'`)

func quoted(def string) []string {
	var vals []string
	for _, m := range literal.FindAllStringSubmatch(def, -1) {
		vals = append(vals, m[1])
	}
	slices.Sort(vals)
	return vals
}

func migratedSchemas(t *testing.T) (*pgxpool.Pool, []string) {
	t.Helper()
	pool := testdb.New(t)
	w, err := db.CreateEntity(context.Background(), pool, "Second", "second")
	if err != nil {
		t.Fatal(err)
	}
	return pool, []string{"public", w.Schema.String()}
}

func sorted(vals []string) []string {
	vals = slices.Clone(vals)
	slices.Sort(vals)
	return vals
}

// TestPanelChecksMatchEngines fails when the surface, fidelity, match mode
// or brand kind CHECKs allow other values than the Go code that writes
// them.
func TestPanelChecksMatchEngines(t *testing.T) {
	pool, schemas := migratedSchemas(t)
	var surfaces []string
	for _, s := range engines.All() {
		surfaces = append(surfaces, string(s.Surface))
	}
	fidelities := []string{string(engines.FidelityApp), string(engines.FidelityModelMatch), string(engines.FidelityApproximation)}
	for _, c := range []struct {
		table, column string
		want          []string
	}{
		{"surface", "key", surfaces},
		{"surface_model", "fidelity", fidelities},
		{"answer", "fidelity", fidelities},
		{"instance_settings", "match_mode", []string{string(panel.MatchWholeWord), string(panel.MatchSelfAnywhere)}},
		{"brand", "kind", kindStrings(append(slices.Clone(panel.TrackedKinds), panel.KindUntracked, panel.KindHidden))},
		{"surface", "route", routeStrings()},
	} {
		for _, schema := range schemas {
			for _, got := range checkValues(t, pool, schema, c.table, c.column) {
				if want := sorted(c.want); !slices.Equal(got, want) {
					t.Errorf("%s.%s CHECK (%s IN ...) allows %v, the Go code has %v", schema, c.table, c.column, got, want)
				}
			}
		}
	}
}

func routeStrings() []string {
	var out []string
	for _, r := range engines.Routes {
		out = append(out, string(r))
	}
	return out
}

func kindStrings(kinds []panel.BrandKind) []string {
	out := make([]string, len(kinds))
	for i, k := range kinds {
		out[i] = string(k)
	}
	return out
}

func TestTrackedBrandMatchesPanel(t *testing.T) {
	pool, schemas := migratedSchemas(t)
	for _, schema := range schemas {
		var def string
		if err := pool.QueryRow(context.Background(), `SELECT pg_get_viewdef(to_regclass($1))`, schema+".tracked_brand").Scan(&def); err != nil {
			t.Fatal(err)
		}
		if got, want := quoted(def), sorted(kindStrings(panel.TrackedKinds)); !slices.Equal(got, want) {
			t.Errorf("%s.tracked_brand holds kinds %v, panel.TrackedKinds is %v", schema, got, want)
		}
	}
}

func TestCredentialKindsMatchEngines(t *testing.T) {
	pool := testdb.New(t)
	var kinds []string
	for _, k := range engines.CredentialKinds {
		kinds = append(kinds, string(k))
	}
	for _, got := range checkValues(t, pool, "public", "credential", "kind") {
		if want := sorted(kinds); !slices.Equal(got, want) {
			t.Errorf("credential CHECK (kind IN ...) allows %v, internal/engines has %v", got, want)
		}
	}
}
