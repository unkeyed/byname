package db_test

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/unkeyed/byname/internal/core"
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
