// Package socialtest serves the recorded social search replies to tests.
package socialtest

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Fixture is a recorded reply from internal/social/testdata.
func Fixture(t testing.TB, name string) []byte {
	t.Helper()
	_, here, _, _ := runtime.Caller(0)
	b, err := os.ReadFile(filepath.Join(filepath.Dir(here), "..", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Naming is a recorded reply whose posts all name Northwind Cloud. The
// recordings come from searches for other brands, and Byname keeps only
// posts that name the term, so tests about storing posts start each post's
// text with it, and give a Threads post without a caption one.
func Naming(t testing.TB, body []byte) []byte {
	t.Helper()
	d := json.NewDecoder(bytes.NewReader(body))
	d.UseNumber()
	var v any
	if err := d.Decode(&v); err != nil {
		t.Fatal(err)
	}
	var walk func(any)
	walk = func(v any) {
		switch v := v.(type) {
		case map[string]any:
			for k, x := range v {
				if s, ok := x.(string); ok && (k == "text" || k == "title") {
					v[k] = "Northwind Cloud: " + s
				} else if k == "caption" && x == nil {
					v[k] = map[string]any{"text": "Northwind Cloud"}
				} else {
					walk(x)
				}
			}
		case []any:
			for _, x := range v {
				walk(x)
			}
		}
	}
	walk(v)
	out, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
