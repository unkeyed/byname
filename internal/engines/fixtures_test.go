package engines

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite testdata/**/*.want.json from the parsers")

// A fixture is a provider response body in testdata/<family>/. <id>.json is
// the example Treg's catalog captured for that tool. <id>.<case>.synthetic.json
// is hand-made for an edge case no capture shows. Each body has a
// <name>.want.json next to it with the parsed Result.

func endpoints() []Endpoint {
	var out []Endpoint
	seen := map[string]bool{}
	for _, s := range All() {
		if !seen[s.Endpoint.ID] {
			seen[s.Endpoint.ID] = true
			out = append(out, s.Endpoint)
		}
	}
	return out
}

func fixtures(t *testing.T, e Endpoint) []string {
	t.Helper()
	var bodies []string
	captured := filepath.Join("testdata", e.Family, e.ID+".json")
	if _, err := os.Stat(captured); err == nil {
		bodies = append(bodies, captured)
	}
	synthetic, err := filepath.Glob(filepath.Join("testdata", e.Family, e.ID+".*.synthetic.json"))
	if err != nil {
		t.Fatal(err)
	}
	return append(bodies, synthetic...)
}

// TestEveryEndpointHasFixtures makes "pin a new endpoint" mean "pin it and
// add a response it can parse".
func TestEveryEndpointHasFixtures(t *testing.T) {
	for _, e := range endpoints() {
		if len(fixtures(t, e)) == 0 {
			t.Errorf("endpoint %s has no fixture: add testdata/%s/%s.json (the catalog's captured example) or %s.<case>.synthetic.json", e.ID, e.Family, e.ID, e.ID)
		}
		if e.TakesModel() {
			if _, err := os.Stat(filepath.Join("testdata", e.Family, e.Models+".json")); err != nil {
				t.Errorf("model list %s has no fixture: %v", e.Models, err)
			}
		}
	}
}

// TestEveryFixtureBelongsToAnEndpoint catches fixtures left behind after a
// pin changes.
func TestEveryFixtureBelongsToAnEndpoint(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "*", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		family, base := filepath.Base(filepath.Dir(f)), filepath.Base(f)
		owned := false
		for _, e := range endpoints() {
			if e.Family != family {
				continue
			}
			if strings.HasPrefix(base, e.ID+".") || (e.Models != "" && strings.HasPrefix(base, e.Models+".")) {
				owned = true
			}
		}
		if !owned {
			t.Errorf("%s matches no registered endpoint or model list in family %s", f, family)
		}
	}
}

type wantResult struct {
	Status        Status   `json:"status"`
	Error         string   `json:"error,omitempty"`
	Text          string   `json:"text"`
	ReportedModel string   `json:"reported_model,omitempty"`
	Citations     []string `json:"citations,omitempty"`
	FanOut        []string `json:"fan_out,omitempty"`
}

func asWant(r Result) wantResult {
	return wantResult{r.Status, r.Error, r.Text, r.ReportedModel, r.Citations, r.FanOut}
}

func TestFixturesParseToWant(t *testing.T) {
	for _, e := range endpoints() {
		for _, body := range fixtures(t, e) {
			t.Run(filepath.Base(body), func(t *testing.T) {
				raw, err := os.ReadFile(body)
				if err != nil {
					t.Fatal(err)
				}
				got := e.Decode(raw)
				if string(got.Raw) != string(raw) {
					t.Error("Raw is not the body as received")
				}
				wantPath := strings.TrimSuffix(body, ".json") + ".want.json"
				if *update {
					b, err := json.MarshalIndent(asWant(got), "", "  ")
					if err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(wantPath, append(b, '\n'), 0o644); err != nil {
						t.Fatal(err)
					}
					return
				}
				wantRaw, err := os.ReadFile(wantPath)
				if err != nil {
					t.Fatal(err)
				}
				var want wantResult
				if err := json.Unmarshal(wantRaw, &want); err != nil {
					t.Fatal(err)
				}
				if g := asWant(got); !reflect.DeepEqual(g, want) {
					t.Errorf("got  %+v\nwant %+v", g, want)
				}
			})
		}
	}
}
