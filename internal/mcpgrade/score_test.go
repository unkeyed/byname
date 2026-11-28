package mcpgrade

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
)

const endpoint = "https://mcp.example.com/mcp"

func goodTool(name, title string) *rawTool {
	return &rawTool{
		name:        name,
		title:       title,
		description: "Searches the product documentation and returns the best matching pages with links.",
		hints:       map[string]bool{"read_only": true, "destructive": false, "idempotent": true, "open_world": false},
		input:       json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","description":"Words to search for"}},"required":["query"]}`),
		output:      json.RawMessage(`{"type":"object","properties":{"pages":{"type":"array"}}}`),
	}
}

// good is a server that passes every check that applies to it.
func good() *observation {
	return &observation{
		endpoint: endpoint,
		first:    httpSeen{status: 200, contentType: "application/json"},
		session: &sessionSeen{
			init: &mcp.InitializeResult{
				ProtocolVersion: "2026-07-28",
				Instructions:    "Use search_docs to find pages, then fetch_page to read one.",
				Capabilities:    &mcp.ServerCapabilities{Tools: &mcp.ToolCapabilities{}},
				ServerInfo: &mcp.Implementation{Name: "example", Title: "Example Docs", Description: "Searches Example's docs.",
					Version: "1.2.0", WebsiteURL: "https://example.com", Icons: []mcp.Icon{{Source: "https://example.com/icon.svg"}}},
			},
			negotiate:  120 * time.Millisecond,
			tools:      []*rawTool{goodTool("search_docs", "Search the documentation"), goodTool("fetch_page", "Read one documentation page")},
			badVersion: httpSeen{status: 400},
			unknownRPC: rpcSeen{code: -32601},
			origin:     httpSeen{status: 403},
		},
		html:     httpSeen{status: 200, contentType: "text/html; charset=utf-8"},
		manifest: manifestSeen{at: "https://mcp.example.com/.well-known/mcp.json", namesEndpoint: true},
		registry: registrySeen{listed: true},
	}
}

// signIn is a server that answers 401 with a complete OAuth discovery chain
// and was graded without a token.
func signIn() *observation {
	o := good()
	o.first = httpSeen{status: 401, contentType: "application/json"}
	o.session = nil
	o.auth = &authSeen{
		challenges: []oauthex.Challenge{{Scheme: "bearer", Params: map[string]string{"resource_metadata": "https://mcp.example.com/.well-known/oauth-protected-resource/mcp"}}},
		prmURL:     "https://mcp.example.com/.well-known/oauth-protected-resource/mcp",
		prm:        &oauthex.ProtectedResourceMetadata{Resource: endpoint, AuthorizationServers: []string{"https://auth.example.com"}},
		asURL:      "https://auth.example.com",
		as:         &oauthex.AuthServerMeta{Issuer: "https://auth.example.com", CodeChallengeMethodsSupported: []string{"S256"}, RegistrationEndpoint: "https://auth.example.com/register"},
	}
	return o
}

func checkIn(t *testing.T, r Report, id string) Check {
	t.Helper()
	for _, c := range r.Categories {
		for _, ch := range c.Checks {
			if ch.ID == id {
				return ch
			}
		}
	}
	t.Fatalf("no check %s in the report", id)
	return Check{}
}

func categoryIn(t *testing.T, r Report, id string) Category {
	t.Helper()
	for _, c := range r.Categories {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("no category %s in the report", id)
	return Category{}
}

func scoreOf(c Category) string {
	if c.Score == nil {
		return "null"
	}
	return fmt.Sprint(*c.Score)
}

func TestCatalog(t *testing.T) {
	known := map[string]bool{}
	for _, c := range categories {
		known[c.id] = true
	}
	ids := map[string]bool{}
	perCategory := map[string]int{}
	for _, c := range checks {
		if ids[c.id] {
			t.Errorf("check %s is listed twice", c.id)
		}
		ids[c.id] = true
		if _, name, ok := strings.Cut(c.id, "."); !ok || name == "" || !known[c.category()] {
			t.Errorf("check %s is not <category>.<name> with a known category", c.id)
		}
		if c.fix == "" || c.title == "" {
			t.Errorf("check %s needs a title and a fix", c.id)
		}
		if c.weight <= 0 {
			t.Errorf("check %s has weight %d", c.id, c.weight)
		}
		perCategory[c.category()]++
	}
	for _, c := range categories {
		if perCategory[c.id] == 0 {
			t.Errorf("category %s has no checks", c.id)
		}
	}
}

func TestGoodServerScoresFull(t *testing.T) {
	r := score(good())
	for _, c := range r.Categories {
		for _, ch := range c.Checks {
			if ch.Status != Pass && ch.Status != Skipped {
				t.Errorf("%s is %s: %s", ch.ID, ch.Status, ch.Finding)
			}
			if ch.Status == Pass && ch.Fix != "" {
				t.Errorf("%s passed but carries a fix", ch.ID)
			}
		}
	}
	if r.Score != 100 || r.Grade != "A+" {
		t.Errorf("score %d %s, want 100 A+", r.Score, r.Grade)
	}
	if c := categoryIn(t, r, "authorization"); c.Score != nil || c.Note != "The server did not ask for sign-in" {
		t.Errorf("authorization without a 401 should be dropped with a note, got %s %q", scoreOf(c), c.Note)
	}
	if r.Server.Name != "example" || r.Server.ProtocolVersion != "2026-07-28" || r.Server.Transport != "streamable_http" || *r.Server.NegotiateMS != 120 {
		t.Errorf("server info %+v", r.Server)
	}
	if len(r.Tools) != 2 || r.Tools[0].Score != 100 || r.Tools[0].Key == "" || r.Tools[0].Key == r.Tools[1].Key {
		t.Errorf("tools %+v", r.Tools)
	}
}

func TestSignInWithoutToken(t *testing.T) {
	r := score(signIn())
	want := map[string]string{
		"connection": "100", "authorization": "100", "metadata": "0",
		"definitions": "0", "documentation": "0", "discoverability": "100",
	}
	for id, s := range want {
		if c := categoryIn(t, r, id); scoreOf(c) != s {
			t.Errorf("%s scored %s, want %s", id, scoreOf(c), s)
		}
	}
	if c := categoryIn(t, r, "metadata"); c.Note != "Needs a token to grade" {
		t.Errorf("a required category that needs a token should say so, got %q", c.Note)
	}
	if ch := checkIn(t, r, "connection.negotiate"); ch.Status != Skipped || ch.Finding != "Needs a token to grade" {
		t.Errorf("negotiate %s %q", ch.Status, ch.Finding)
	}
	if r.Score != 50 || r.Grade != "D" {
		t.Errorf("overall %d %s, want the mean of six categories, 50 D", r.Score, r.Grade)
	}
}

func TestOptionalCategoryCountsWhenAnyCheckIsAssessed(t *testing.T) {
	o := good()
	o.registry = registrySeen{err: errors.New("dial tcp: timeout")}
	o.manifest = manifestSeen{}
	o.html = httpSeen{status: 404, contentType: "text/plain"}
	r := score(o)
	if c := categoryIn(t, r, "discoverability"); scoreOf(c) != "0" {
		t.Errorf("discoverability with two failures and one skip should score 0, got %s", scoreOf(c))
	}
	if r.Score != 80 {
		t.Errorf("overall %d, want (100*4 + 0)/5 = 80", r.Score)
	}
}

func TestChecks(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(o *observation)
		check  string
		want   Status
		frac   float64
	}{
		{"plain http", func(o *observation) { o.endpoint = "http://mcp.example.com/mcp" }, "connection.https", Fail, 0},
		{"legacy sse", func(o *observation) {
			o.session, o.first, o.legacySSE = nil, httpSeen{status: 405}, true
		}, "connection.transport", Partial, 0.5},
		{"nothing speaks", func(o *observation) {
			o.session, o.first, o.sessionErr = nil, httpSeen{status: 404, contentType: "text/html"}, errors.New("404")
		}, "connection.transport", Fail, 0},
		{"no session", func(o *observation) {
			o.session, o.sessionErr = nil, errors.New("connection refused")
		}, "connection.negotiate", Fail, 0},
		{"2025-11-25", func(o *observation) { o.session.init.ProtocolVersion = "2025-11-25" }, "connection.version", Partial, 0.75},
		{"2025-06-18", func(o *observation) { o.session.init.ProtocolVersion = "2025-06-18" }, "connection.version", Partial, 0.5},
		{"2024-11-05", func(o *observation) { o.session.init.ProtocolVersion = "2024-11-05" }, "connection.version", Fail, 0},
		{"no session skips version", func(o *observation) { o.session = nil }, "connection.version", Skipped, 0},
		{"accepts a bad version", func(o *observation) { o.session.badVersion = httpSeen{status: 200} }, "connection.bad_version", Fail, 0},
		{"wrong error code", func(o *observation) { o.session.unknownRPC = rpcSeen{code: -32600} }, "connection.unknown_method", Fail, 0},
		{"accepts a foreign origin", func(o *observation) { o.session.origin = httpSeen{status: 200} }, "connection.origin", Fail, 0},
		{"refuses origin with 400", func(o *observation) { o.session.origin = httpSeen{status: 400} }, "connection.origin", Pass, 1},

		{"no 401 skips authorization", func(o *observation) {}, "authorization.pkce", Skipped, 0},
		{"bearer without resource_metadata", func(o *observation) {
			*o = *signIn()
			o.auth.challenges = []oauthex.Challenge{{Scheme: "bearer", Params: map[string]string{"realm": "x"}}}
		}, "authorization.www_authenticate", Partial, 0.5},
		{"no bearer challenge", func(o *observation) {
			*o = *signIn()
			o.auth.challenges = nil
		}, "authorization.www_authenticate", Fail, 0},
		{"resource is another URL", func(o *observation) {
			*o = *signIn()
			o.auth.prm.Resource = "https://mcp.example.com"
		}, "authorization.resource_metadata", Partial, 0.5},
		{"resource with a trailing slash", func(o *observation) {
			*o = *signIn()
			o.auth.prm.Resource = endpoint + "/"
		}, "authorization.resource_metadata", Pass, 1},
		{"no resource metadata", func(o *observation) {
			*o = *signIn()
			o.auth.prm, o.auth.prmErr, o.auth.as, o.auth.asURL = nil, errors.New("404"), nil, ""
		}, "authorization.as_metadata", Fail, 0},
		{"plain PKCE only", func(o *observation) {
			*o = *signIn()
			o.auth.as.CodeChallengeMethodsSupported = []string{"plain"}
		}, "authorization.pkce", Fail, 0},
		{"no registration", func(o *observation) {
			*o = *signIn()
			o.auth.as.RegistrationEndpoint = ""
		}, "authorization.registration", Fail, 0},
		{"client ID metadata documents", func(o *observation) {
			*o = *signIn()
			o.auth.as.RegistrationEndpoint, o.auth.as.ClientIDMetadataDocumentSupported = "", true
		}, "authorization.registration", Pass, 1},

		{"no description", func(o *observation) { o.session.init.ServerInfo.Description = "" }, "metadata.description", Fail, 0},
		{"website not a URL", func(o *observation) { o.session.init.ServerInfo.WebsiteURL = "example.com" }, "metadata.website", Fail, 0},
		{"small icon", func(o *observation) {
			o.session.init.ServerInfo.Icons = []mcp.Icon{{Source: "https://example.com/i.png", Sizes: []string{"32x32"}}}
		}, "metadata.icon", Partial, 0.5},
		{"64px icon", func(o *observation) {
			o.session.init.ServerInfo.Icons = []mcp.Icon{{Source: "https://example.com/i.png", Sizes: []string{"64x64"}}}
		}, "metadata.icon", Pass, 1},
		{"no serverInfo", func(o *observation) { o.session.init.ServerInfo = nil }, "metadata.name", Fail, 0},
		{"no instructions", func(o *observation) { o.session.init.Instructions = " " }, "metadata.instructions", Fail, 0},
		{"no capabilities", func(o *observation) { o.session.init.Capabilities = &mcp.ServerCapabilities{} }, "metadata.capabilities", Fail, 0},

		{"tools without the capability", func(o *observation) { o.session.init.Capabilities = &mcp.ServerCapabilities{} }, "definitions.listed", Partial, 0.5},
		{"no tools", func(o *observation) { o.session.tools = nil }, "definitions.listed", Fail, 0},
		{"no tools skips tool checks", func(o *observation) { o.session.tools = nil }, "documentation.description", Skipped, 0},
		{"one bad name of two", func(o *observation) { o.session.tools[1].name = "fetch page!" }, "definitions.name_chars", Partial, 0.5},
		{"duplicate names", func(o *observation) { o.session.tools[1].name = "search_docs" }, "definitions.name_unique", Fail, 0},
		{"long name", func(o *observation) { o.session.tools[0].name = strings.Repeat("a", 65) }, "definitions.name_64", Partial, 0.5},
		{"no input schema", func(o *observation) { o.session.tools[0].input = nil }, "definitions.input_schema", Partial, 0.5},
		{"input schema not an object", func(o *observation) {
			o.session.tools[0].input = json.RawMessage(`{"type":"string"}`)
		}, "definitions.input_schema", Partial, 0.5},
		{"x-mcp-header on an object", func(o *observation) {
			o.session.tools[0].input = json.RawMessage(`{"type":"object","properties":{"q":{"type":"object","x-mcp-header":"Q"}}}`)
		}, "definitions.input_schema", Partial, 0.5},
		{"x-mcp-header used twice", func(o *observation) {
			o.session.tools[0].input = json.RawMessage(`{"type":"object","properties":{"a":{"type":"string","x-mcp-header":"Q"},"b":{"type":"object","properties":{"c":{"type":"string","x-mcp-header":"q"}}}}}`)
		}, "definitions.input_schema", Partial, 0.5},
		{"no output schemas", func(o *observation) { o.session.tools[0].output, o.session.tools[1].output = nil, nil }, "definitions.output_schema", Skipped, 0},
		{"output schema an array", func(o *observation) { o.session.tools[0].output = json.RawMessage(`{"type":"array"}`) }, "definitions.output_schema", Partial, 0.5},
		{"missing hints", func(o *observation) { delete(o.session.tools[0].hints, "read_only") }, "definitions.annotations", Partial, 0.5},
		{"title from annotations", func(o *observation) {
			o.session.tools[0].title, o.session.tools[0].annotTitle = "", "Search the documentation"
		}, "definitions.titles", Pass, 1},
		{"long description", func(o *observation) { o.session.tools[0].description = strings.Repeat("a", 2049) }, "definitions.desc_2kb", Partial, 0.5},
		{"zero-width space", func(o *observation) { o.session.tools[0].description += "​" }, "definitions.hidden_chars", Partial, 0.5},
		{"bidi override in a schema", func(o *observation) {
			o.session.tools[1].input = json.RawMessage(`{"type":"object","properties":{"q":{"type":"string","description":"‮evil"}}}`)
		}, "definitions.hidden_chars", Partial, 0.5},
		{"41 tools", func(o *observation) {
			for i := range 39 {
				o.session.tools = append(o.session.tools, goodTool(fmt.Sprintf("t%d", i), "A tool"))
			}
		}, "definitions.tool_budget", Partial, 0.5},
		{"81 tools", func(o *observation) {
			for i := range 79 {
				o.session.tools = append(o.session.tools, goodTool(fmt.Sprintf("t%d", i), "A tool"))
			}
		}, "definitions.tool_budget", Fail, 0},
		{"one declared list fails", func(o *observation) {
			o.session.lists = map[string]error{"resources/list": nil, "prompts/list": errors.New("-32601")}
		}, "definitions.declared_lists", Partial, 0.5},

		{"title restates the name", func(o *observation) { o.session.tools[0].title = "Search Docs" }, "documentation.title", Partial, 0.5},
		{"short description", func(o *observation) {
			o.session.tools[0].description, o.session.tools[1].description = "Searches docs.", "Reads a page."
		}, "documentation.description", Partial, 0.5},
		{"description restates the title", func(o *observation) {
			o.session.tools[0].description, o.session.tools[1].description = "Search the documentation", "Read one documentation page."
		}, "documentation.description", Fail, 0},
		{"half the inputs undescribed", func(o *observation) {
			for _, tl := range o.session.tools {
				tl.input = json.RawMessage(`{"type":"object","properties":{"q":{"type":"string","description":"Query"},"n":{"type":"integer"}}}`)
			}
		}, "documentation.inputs", Partial, 0.5},
		{"enum counts as a type", func(o *observation) {
			o.session.tools[0].input = json.RawMessage(`{"type":"object","properties":{"mode":{"enum":["a","b"],"description":"Mode"}}}`)
		}, "documentation.inputs", Pass, 1},
		{"no inputs", func(o *observation) { o.session.tools[0].input = json.RawMessage(`{"type":"object"}`) }, "documentation.inputs", Pass, 1},
		{"no hints at all", func(o *observation) {
			o.session.tools[0].hints, o.session.tools[1].hints = nil, map[string]bool{"read_only": true}
		}, "documentation.annotations", Partial, 0.125},
		{"no output", func(o *observation) { o.session.tools[0].output = nil }, "documentation.output", Partial, 0.5},

		{"manifest elsewhere", func(o *observation) { o.manifest.namesEndpoint = false }, "discoverability.manifest", Partial, 0.5},
		{"no manifest", func(o *observation) { o.manifest = manifestSeen{} }, "discoverability.manifest", Fail, 0},
		{"no setup page", func(o *observation) { o.html = httpSeen{status: 405, contentType: "text/plain"} }, "discoverability.setup_page", Fail, 0},
		{"registry unreachable", func(o *observation) { o.registry = registrySeen{err: errors.New("503")} }, "discoverability.registry", Skipped, 0},
		{"not in the registry", func(o *observation) { o.registry = registrySeen{} }, "discoverability.registry", Fail, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := good()
			tc.mutate(o)
			ch := checkIn(t, score(o), tc.check)
			if ch.Status != tc.want || ch.Fraction != tc.frac {
				t.Errorf("%s is %s %.3f (%s), want %s %.3f", tc.check, ch.Status, ch.Fraction, ch.Finding, tc.want, tc.frac)
			}
			if (ch.Status == Fail || ch.Status == Partial) && ch.Fix == "" {
				t.Errorf("%s fell short without a fix", tc.check)
			}
		})
	}
}

func TestToolCheckFindingNamesOffenders(t *testing.T) {
	o := good()
	o.session.tools[1].description = ""
	ch := checkIn(t, score(o), "definitions.descriptions")
	if ch.Finding != "1 of 2 tools fall short: fetch_page (No description)" {
		t.Errorf("finding %q", ch.Finding)
	}
}

func TestToolScore(t *testing.T) {
	o := good()
	tl := o.session.tools[0]
	tl.output = nil
	tl.input = json.RawMessage(`{"type":"object","properties":{"q":{"type":"string","description":"Query"},"n":{"type":"integer"}}}`)
	r := score(o)
	if got := r.Tools[0].Score; got != 75 {
		t.Errorf("tool score %d, want 15 + 35 + 30/2 + 10 + 0 = 75", got)
	}
	if got := r.Tools[0].Params; len(got) != 2 || got[0].Name != "n" || got[0].Type != "integer" || got[1].Description != "Query" {
		t.Errorf("params %+v", got)
	}
	if got := r.Tools[0].Hints; strings.Join(got, ",") != "read_only,idempotent" {
		t.Errorf("hints %v, want the ones declared true", got)
	}
	marks := map[string]Status{}
	for _, m := range r.Tools[0].Marks {
		marks[m.Check] = m.Status
	}
	if marks["documentation.output"] != Fail || marks["documentation.inputs"] != Partial || marks["definitions.output_schema"] != Skipped {
		t.Errorf("marks %v", marks)
	}
}

func TestLetter(t *testing.T) {
	for score, want := range map[int]Letter{100: "A+", 95: "A+", 94: "A", 85: "A", 84: "B", 70: "B", 69: "C", 55: "C", 54: "D", 40: "D", 39: "F", 0: "F"} {
		if got := letter(score); got != want {
			t.Errorf("letter(%d) = %s, want %s", score, got, want)
		}
	}
}
