package db_test

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/unkeyed/byname/internal/core"
	"github.com/unkeyed/byname/internal/engines"
	"github.com/unkeyed/byname/internal/panel"
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

// TestPanelChecksMatchEngines fails when 003_panel.sql's surface, fidelity
// or match mode CHECKs list other values than the Go code that writes them.
func TestPanelChecksMatchEngines(t *testing.T) {
	sql, err := os.ReadFile("migrations/003_panel.sql")
	if err != nil {
		t.Fatal(err)
	}
	var surfaces []string
	for _, s := range engines.All() {
		surfaces = append(surfaces, string(s.Surface))
	}
	for column, want := range map[string][]string{
		"key":        surfaces,
		"fidelity":   {string(engines.FidelityApp), string(engines.FidelityModelMatch), string(engines.FidelityApproximation)},
		"match_mode": {string(panel.MatchWholeWord), string(panel.MatchSelfAnywhere)},
		"kind":       {string(panel.KindSelf), string(panel.KindCompetitor), string(panel.KindTechnology)},
	} {
		check := regexp.MustCompile(`CHECK \(` + column + ` IN \(([^)]*)\)`)
		found := check.FindAllStringSubmatch(string(sql), -1)
		if len(found) == 0 {
			t.Errorf("no CHECK (%s IN (...)) in 003_panel.sql", column)
		}
		slices.Sort(want)
		for _, m := range found {
			var got []string
			for _, v := range strings.Split(m[1], ",") {
				got = append(got, strings.Trim(strings.TrimSpace(v), "'"))
			}
			slices.Sort(got)
			if !slices.Equal(got, want) {
				t.Errorf("CHECK (%s IN ...) lists %v, the Go code has %v", column, got, want)
			}
		}
	}
}
