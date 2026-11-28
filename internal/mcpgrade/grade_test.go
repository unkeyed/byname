package mcpgrade

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type searchIn struct {
	Query string `json:"query" jsonschema:"words to search the documentation for"`
	Limit int    `json:"limit,omitempty" jsonschema:"how many pages to return, 10 when unset"`
}

type searchOut struct {
	Pages []string `json:"pages" jsonschema:"links to the matching pages, best first"`
}

func registryServer(t *testing.T, mode, endpoint string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v0/servers" || r.URL.Query().Get("search") == "" {
			http.NotFound(w, r)
			return
		}
		switch mode {
		case "down":
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
		case "listed":
			fmt.Fprintf(w, `{"servers":[{"server":{"name":"example.northwind/docs","remotes":[{"type":"streamable-http","url":%q}]}}]}`, endpoint+"/")
		default:
			fmt.Fprint(w, `{"servers":[{"server":{"name":"example.other/docs","remotes":[{"type":"streamable-http","url":"https://other.example/mcp"}]}}],"metadata":{"count":1}}`)
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func wellDocumented(t *testing.T) *httptest.Server {
	t.Helper()
	srv := mcp.NewServer(&mcp.Implementation{
		Name: "northwind-docs", Title: "Northwind Docs", Version: "1.4.0",
		Description: "Searches and reads Northwind's product documentation.",
		WebsiteURL:  "https://northwind.example",
		Icons:       []mcp.Icon{{Source: "https://northwind.example/icon.svg", MIMEType: "image/svg+xml"}},
	}, &mcp.ServerOptions{Instructions: "Use search_docs to find pages about a Northwind product, then cite the links it returns."})
	mcp.AddTool(srv, &mcp.Tool{
		Name: "search_docs", Title: "Search the documentation",
		Description: "Searches Northwind's product documentation and returns links to the best matching pages, best first.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, DestructiveHint: new(false), OpenWorldHint: new(false)},
	}, func(context.Context, *mcp.CallToolRequest, searchIn) (*mcp.CallToolResult, searchOut, error) {
		t.Error("the grade called a tool")
		return nil, searchOut{}, nil
	})
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, &mcp.StreamableHTTPOptions{Stateless: true})

	var ts *httptest.Server
	mux := http.NewServeMux()
	mux.Handle("/mcp", http.NewCrossOriginProtection().Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.Contains(r.Header.Get("Accept"), "text/html") {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, "<h1>Connect Northwind Docs</h1>")
			return
		}
		handler.ServeHTTP(w, r)
	})))
	mux.HandleFunc("/.well-known/mcp.json", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"name":"Northwind Docs","endpoint":%q}`, ts.URL+"/mcp")
	})
	ts = httptest.NewTLSServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

type fake struct {
	toolsJSON    string
	sse          bool
	endless      bool
	checkVersion bool
	signIn       bool
	token        string
	prm          bool
	s256         bool

	mu   sync.Mutex
	auth map[string][]string
	url  string
}

func (f *fake) start(t *testing.T) string {
	t.Helper()
	f.auth = map[string][]string{}
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", f.serveMCP)
	mux.HandleFunc("/.well-known/oauth-protected-resource/mcp", func(w http.ResponseWriter, r *http.Request) {
		f.saw(r)
		if !f.prm {
			http.NotFound(w, r)
			return
		}
		fmt.Fprintf(w, `{"resource":%q,"authorization_servers":[%q]}`, f.url+"/mcp", f.url)
	})
	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
		f.saw(r)
		methods := `["plain"]`
		if f.s256 {
			methods = `["S256"]`
		}
		fmt.Fprintf(w, `{"issuer":%q,"authorization_endpoint":"%[1]s/authorize","token_endpoint":"%[1]s/token","registration_endpoint":"%[1]s/register","response_types_supported":["code"],"code_challenge_methods_supported":%s}`, f.url, methods)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		f.saw(r)
		http.NotFound(w, r)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	f.url = srv.URL
	return srv.URL + "/mcp"
}

func (f *fake) saw(r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.auth[r.URL.Path] = append(f.auth[r.URL.Path], r.Header.Get("Authorization"))
}

func (f *fake) serveMCP(w http.ResponseWriter, r *http.Request) {
	f.saw(r)
	if f.signIn && r.Header.Get("Authorization") != "Bearer "+f.token {
		w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="fake", resource_metadata="%s/.well-known/oauth-protected-resource/mcp"`, f.url))
		http.Error(w, "sign in", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	if v := r.Header.Get("MCP-Protocol-Version"); f.checkVersion && v != "" && !slices.Contains([]string{"2026-07-28", "2025-11-25", "2025-06-18"}, v) {
		http.Error(w, "unsupported protocol version", http.StatusBadRequest)
		return
	}
	var req struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		http.Error(w, "bad JSON", http.StatusBadRequest)
		return
	}
	if req.ID == nil {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	var body string
	switch req.Method {
	case "initialize":
		body = `{"protocolVersion":"2025-06-18","capabilities":{"tools":{}},"serverInfo":{"name":"fake","version":"0.1.0"}}`
	case "tools/list":
		if f.endless {
			w.Header().Set("Content-Type", "text/event-stream")
			for r.Context().Err() == nil {
				if _, err := fmt.Fprint(w, ": still working\n\n"); err != nil {
					return
				}
			}
			return
		}
		body = `{"tools":` + f.toolsJSON + `}`
	case "ping":
		body = `{}`
	}
	msg := fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"result":%s}`, req.ID, body)
	if body == "" {
		msg = fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"error":{"code":-32601,"message":"method not found"}}`, req.ID)
	}
	if f.sse {
		var line bytes.Buffer
		if err := json.Compact(&line, []byte(msg)); err != nil {
			panic(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "event: message\ndata: %s\n\n", line.String())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, msg)
}

const hints = `"annotations":{"readOnlyHint":true,"destructiveHint":false,"idempotentHint":true,"openWorldHint":false}`

func grade(t *testing.T, target Target) Report {
	t.Helper()
	if target.Client == nil {
		target.Client = http.DefaultClient
	}
	if target.RegistryURL == "" {
		target.RegistryURL = registryServer(t, "unlisted", target.URL)
	}
	r, err := Grade(t.Context(), target)
	if err != nil {
		t.Fatalf("Grade: %v", err)
	}
	return r
}

func wantStatus(t *testing.T, r Report, id string, want Status) Check {
	t.Helper()
	ch := checkIn(t, r, id)
	if ch.Status != want {
		t.Errorf("%s is %s (%s), want %s", id, ch.Status, ch.Finding, want)
	}
	return ch
}

func TestGradeWellDocumentedServer(t *testing.T) {
	ts := wellDocumented(t)
	endpoint := ts.URL + "/mcp"
	r := grade(t, Target{URL: endpoint, Client: ts.Client(), RegistryURL: registryServer(t, "listed", endpoint)})
	for _, c := range r.Categories {
		for _, ch := range c.Checks {
			if ch.Status != Pass && ch.Status != Skipped {
				t.Errorf("%s is %s: %s", ch.ID, ch.Status, ch.Finding)
			}
		}
	}
	if r.Score != 100 || r.Grade != "A+" {
		t.Errorf("score %d %s, want 100 A+", r.Score, r.Grade)
	}
	if r.Server.ProtocolVersion != "2026-07-28" || r.Server.Name != "northwind-docs" || r.Server.TokenUsed {
		t.Errorf("server %+v", r.Server)
	}
	if len(r.Tools) != 1 || r.Tools[0].Score != 100 || len(r.Tools[0].Params) != 2 || !r.Tools[0].HasOutput {
		t.Errorf("tools %+v", r.Tools)
	}
}

func TestGradeMisbehavingServer(t *testing.T) {
	f := &fake{toolsJSON: `[
		{"name":"search docs!","description":"Search.","inputSchema":{"type":"object","properties":{"q":{"type":"string"}}}},
		{"name":"lookup","title":"Look up a record","description":"Looks up one customer record by its ID and returns every field on it.",
		 "inputSchema":{"type":"object","properties":{"id":{"type":"object","description":"Record ID","x-mcp-header":"Record-Id"}}},` + hints + `}
	]`}
	endpoint := f.start(t)
	r := grade(t, Target{URL: endpoint})

	if len(r.Tools) != 2 || r.Tools[1].Name != "lookup" {
		t.Fatalf("tools %+v, want both, including the lookup the SDK drops", r.Tools)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "t", Version: "1"}, nil).Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: endpoint, DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	if res, err := cs.ListTools(t.Context(), nil); err != nil || len(res.Tools) != 1 {
		t.Errorf("the go-sdk should drop lookup from tools/list, which is why the grade reads the wire; got %v, %v", res, err)
	}

	wantStatus(t, r, "connection.https", Fail)
	wantStatus(t, r, "connection.bad_version", Fail)
	wantStatus(t, r, "connection.unknown_method", Pass)
	wantStatus(t, r, "connection.origin", Fail)
	if ch := wantStatus(t, r, "connection.version", Partial); ch.Fraction != 0.5 {
		t.Errorf("2025-06-18 should earn half, got %v", ch.Fraction)
	}
	wantStatus(t, r, "authorization.www_authenticate", Skipped)
	wantStatus(t, r, "metadata.name", Pass)
	wantStatus(t, r, "metadata.description", Fail)
	if ch := wantStatus(t, r, "definitions.name_chars", Partial); !strings.Contains(ch.Finding, "search docs!") {
		t.Errorf("name_chars should name the bad tool: %s", ch.Finding)
	}
	if ch := wantStatus(t, r, "definitions.input_schema", Partial); !strings.Contains(ch.Finding, "lookup (x-mcp-header on id") {
		t.Errorf("input_schema should say why lookup is dropped: %s", ch.Finding)
	}
	wantStatus(t, r, "definitions.annotations", Partial)
	wantStatus(t, r, "definitions.titles", Partial)
	wantStatus(t, r, "documentation.inputs", Partial)
	wantStatus(t, r, "discoverability.registry", Fail)
	if r.Grade == "A+" || r.Grade == "A" {
		t.Errorf("a misbehaving server graded %s (%d)", r.Grade, r.Score)
	}
}

func TestGradeChecksProtocolVersionHeader(t *testing.T) {
	f := &fake{toolsJSON: `[]`, checkVersion: true}
	r := grade(t, Target{URL: f.start(t)})
	wantStatus(t, r, "connection.bad_version", Pass)
	wantStatus(t, r, "definitions.listed", Fail)
}

func TestGradeSignIn(t *testing.T) {
	cases := []struct {
		name      string
		prm, s256 bool
		want      map[string]Status
	}{
		{"complete", true, true, map[string]Status{
			"authorization.www_authenticate": Pass, "authorization.resource_metadata": Pass,
			"authorization.as_metadata": Pass, "authorization.pkce": Pass, "authorization.registration": Pass,
		}},
		{"no S256", true, false, map[string]Status{"authorization.as_metadata": Pass, "authorization.pkce": Fail}},
		{"no resource metadata", false, true, map[string]Status{
			"authorization.www_authenticate": Pass, "authorization.resource_metadata": Fail, "authorization.pkce": Fail,
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &fake{toolsJSON: `[]`, signIn: true, token: "secret", prm: tc.prm, s256: tc.s256}
			r := grade(t, Target{URL: f.start(t)})
			for id, want := range tc.want {
				wantStatus(t, r, id, want)
			}
			if c := categoryIn(t, r, "metadata"); scoreOf(c) != "0" || c.Note != "Needs a token to grade" {
				t.Errorf("metadata without a token %s %q", scoreOf(c), c.Note)
			}
			wantStatus(t, r, "connection.transport", Pass)
			wantStatus(t, r, "connection.negotiate", Skipped)
		})
	}
}

func TestGradeWithToken(t *testing.T) {
	f := &fake{toolsJSON: `[{"name":"lookup","description":"Looks up one customer record.","inputSchema":{"type":"object"}}]`,
		signIn: true, token: "secret", prm: true, s256: true}
	r := grade(t, Target{URL: f.start(t), Token: "secret"})
	if !r.Server.TokenUsed || len(r.Tools) != 1 {
		t.Errorf("a token should open the session: %+v, %d tools", r.Server, len(r.Tools))
	}
	wantStatus(t, r, "authorization.pkce", Pass)
	wantStatus(t, r, "metadata.name", Pass)
}

func TestGradeRegistry(t *testing.T) {
	for mode, want := range map[string]Status{"listed": Pass, "unlisted": Fail, "down": Skipped} {
		t.Run(mode, func(t *testing.T) {
			f := &fake{toolsJSON: `[]`}
			endpoint := f.start(t)
			r := grade(t, Target{URL: endpoint, RegistryURL: registryServer(t, mode, endpoint)})
			wantStatus(t, r, "discoverability.registry", want)
		})
	}
}

func TestGradeUnreachableServer(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	endpoint := srv.URL + "/mcp"
	srv.Close()
	r := grade(t, Target{URL: endpoint})
	wantStatus(t, r, "connection.transport", Fail)
	wantStatus(t, r, "connection.negotiate", Fail)
	if c := categoryIn(t, r, "metadata"); scoreOf(c) != "0" || c.Note != "No session" {
		t.Errorf("metadata of a server that is down %s %q", scoreOf(c), c.Note)
	}
	if r.Grade != "F" {
		t.Errorf("a server that is down graded %s (%d)", r.Grade, r.Score)
	}
}

func TestGradeErrorsOnlyWhenContextEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := Grade(ctx, Target{URL: "https://mcp.example.com/mcp", Client: http.DefaultClient}); err == nil {
		t.Error("Grade with a cancelled context should return its error")
	}
}

func TestGradeSendsTokenOnlyToEndpoint(t *testing.T) {
	f := &fake{toolsJSON: `[]`, signIn: true, token: "secret", prm: true, s256: true}
	endpoint := f.start(t)
	registry := newRecorder(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, `{"servers":[]}`) })
	grade(t, Target{URL: endpoint, Token: "secret", RegistryURL: registry.URL})

	f.mu.Lock()
	defer f.mu.Unlock()
	for path, seen := range f.auth {
		for _, a := range seen {
			if path != "/mcp" && a != "" {
				t.Errorf("%s saw the token", path)
			}
		}
	}
	if seen := f.auth["/mcp"]; !slices.Contains(seen, "") || !slices.Contains(seen, "Bearer secret") {
		t.Errorf("the endpoint saw %q, want the sign-in probe bare and the session with the token", seen)
	}
	if len(f.auth["/.well-known/oauth-protected-resource/mcp"]) == 0 || len(f.auth["/.well-known/oauth-authorization-server"]) == 0 {
		t.Errorf("the sign-in metadata was never fetched: %v", f.auth)
	}
	for _, a := range registry.saw("/v0/servers") {
		if a != "" {
			t.Error("the registry saw the token")
		}
	}
}

func TestGradeSurvivesEndlessToolsList(t *testing.T) {
	f := &fake{toolsJSON: `[]`, endless: true}
	start := time.Now()
	r := grade(t, Target{URL: f.start(t)})
	if took := time.Since(start); took > 10*time.Second {
		t.Errorf("the grade took %s", took)
	}
	if ch := wantStatus(t, r, "definitions.listed", Fail); !strings.Contains(ch.Finding, "2 MiB") {
		t.Errorf("listed should say the answer was cut: %s", ch.Finding)
	}
	wantStatus(t, r, "metadata.name", Pass)
}

func TestToolsListOverEventStream(t *testing.T) {
	f := &fake{sse: true, toolsJSON: `[{"name":"lookup","title":"Look up a record","description":"Looks up one customer record by ID.",
		"inputSchema":{"type":"object"},` + hints + `}]`}
	r := grade(t, Target{URL: f.start(t)})
	if len(r.Tools) != 1 || r.Tools[0].Name != "lookup" || strings.Join(r.Tools[0].Hints, ",") != "read_only,idempotent" {
		t.Errorf("tools %+v", r.Tools)
	}
	wantStatus(t, r, "definitions.annotations", Pass)
	wantStatus(t, r, "connection.unknown_method", Pass)
}
