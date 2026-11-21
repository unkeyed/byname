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
// is hand-made from the provider's documented shape, for an API or an edge
// case no capture shows. Each answer body has a <name>.want.json next to it
// with the parsed Result.

// fixtureOwner is anything whose responses have fixtures: a pinned endpoint,
// which the Treg and scraper routes call, or a direct provider's or the
// gateway's API.
type fixtureOwner struct {
	id, family string
	decode     func(raw []byte) Result
	// models is the model list's fixture id, or empty when there is no list
	// to read.
	models      string
	parseModels func(raw []byte) ([]Model, error)
}

func fixtureOwners(t *testing.T) []fixtureOwner {
	t.Helper()
	var out []fixtureOwner
	seen := map[string]bool{}
	for _, s := range All() {
		e := s.Endpoint
		if !seen[e.ID] {
			seen[e.ID] = true
			out = append(out, fixtureOwner{id: e.ID, family: e.Family, decode: e.Decode, models: e.Models, parseModels: parseDataForSEOModels})
		}
	}
	for _, api := range directAPIs(t) {
		o := fixtureOwner{id: api.id, family: api.family, decode: func(raw []byte) Result {
			r := api.parse(raw)
			r.Raw = raw
			return r
		}}
		if api.models.path != "" {
			o.models, o.parseModels = api.models.id, api.models.parse
		}
		out = append(out, o)
	}
	return out
}

// directAPIs returns the API behind every provider and gateway row of the
// registry, by binding each to a surface it serves.
func directAPIs(t *testing.T) []providerAPI {
	t.Helper()
	var out []providerAPI
	seen := map[string]bool{}
	for _, a := range adapters {
		if a.Route != RouteProvider && a.Route != RouteGateway {
			continue
		}
		bound := false
		for _, s := range All() {
			if !a.serves(s) {
				continue
			}
			api := a.new(RouteSpec{Spec: s, Route: a.Route, Adapter: a}, Conn{}).(apiAdapter).api
			if a.Route == RouteProvider && api.id != a.Endpoint {
				t.Errorf("%s: the API's fixture id %q is not the endpoint %q stored on its answers", a.Title, api.id, a.Endpoint)
			}
			if !seen[api.id] {
				seen[api.id] = true
				out = append(out, api)
			}
			bound = true
			break
		}
		if !bound {
			t.Errorf("%s serves no surface", a.Title)
		}
	}
	return out
}

// fixtureFiles returns the bodies for id in family: the captured one, then
// the synthetic ones.
func fixtureFiles(t *testing.T, family, id string) []string {
	t.Helper()
	var bodies []string
	for _, pattern := range []string{id + ".json", id + ".synthetic.json", id + ".*.synthetic.json"} {
		m, err := filepath.Glob(filepath.Join("testdata", family, pattern))
		if err != nil {
			t.Fatal(err)
		}
		bodies = append(bodies, m...)
	}
	return bodies
}

// TestEveryEndpointHasFixtures makes "pin a new endpoint" or "add a
// provider" mean "and add a response it can parse".
func TestEveryEndpointHasFixtures(t *testing.T) {
	for _, o := range fixtureOwners(t) {
		if len(fixtureFiles(t, o.family, o.id)) == 0 {
			t.Errorf("%s has no fixture: add testdata/%s/%s.json (a captured response) or %s.<case>.synthetic.json", o.id, o.family, o.id, o.id)
		}
		if o.models == "" {
			continue
		}
		lists := fixtureFiles(t, o.family, o.models)
		if len(lists) == 0 {
			t.Errorf("model list %s has no fixture in testdata/%s", o.models, o.family)
		}
		for _, f := range lists {
			raw, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			if models, err := o.parseModels(raw); err != nil || len(models) == 0 {
				t.Errorf("%s: %d models, %v", f, len(models), err)
			}
		}
	}
}

// TestEveryFixtureBelongsToAnEndpoint catches fixtures left behind after a
// pin or a provider changes.
func TestEveryFixtureBelongsToAnEndpoint(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "*", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	owners := fixtureOwners(t)
	for _, f := range files {
		family, base := filepath.Base(filepath.Dir(f)), filepath.Base(f)
		owned := false
		for _, o := range owners {
			if o.family != family {
				continue
			}
			if strings.HasPrefix(base, o.id+".") || (o.models != "" && strings.HasPrefix(base, o.models+".")) {
				owned = true
			}
		}
		if !owned {
			t.Errorf("%s matches no registered endpoint, API or model list in family %s", f, family)
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
	for _, o := range fixtureOwners(t) {
		for _, body := range fixtureFiles(t, o.family, o.id) {
			t.Run(filepath.Base(body), func(t *testing.T) {
				raw, err := os.ReadFile(body)
				if err != nil {
					t.Fatal(err)
				}
				got := o.decode(raw)
				if string(got.Raw) != string(raw) {
					t.Error("Raw is not the body as received")
				}
				if got.CostUSD != nil {
					t.Errorf("a parser reported cost %v; cost comes only from the route", *got.CostUSD)
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
