package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/unkeyed/byname/internal/app"
	"github.com/unkeyed/byname/internal/auth"
	"github.com/unkeyed/byname/internal/core"
	"github.com/unkeyed/byname/internal/instance"
)

func listTools(t *testing.T) []*mcp.Tool {
	t.Helper()
	ctx := context.Background()
	srv := mcp.NewServer(&mcp.Implementation{Name: "byname", Version: "test"}, nil)
	addTools(srv, app.Services{Instance: instance.New("test", nil, notOnboarded{}, nil, nil)})
	addEntityTools(srv, nil)
	st, ct := mcp.NewInMemoryTransports()
	if _, err := srv.Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "test"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	tools, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	return tools.Tools
}

// TestEveryToolNamesItsScope fails when a tool does not say which one scope
// a caller needs, the MCP side of TestEveryRouteHasAnAccessClass.
func TestEveryToolNamesItsScope(t *testing.T) {
	tools := listTools(t)
	if len(tools) == 0 {
		t.Fatal("no tools registered")
	}
	for _, tool := range tools {
		scope, _ := tool.Meta[ScopeMeta].(string)
		if !slices.Contains(core.Scopes, core.Scope(scope)) {
			t.Errorf("%s: _meta[%q] = %q, want one of %v", tool.Name, ScopeMeta, scope, core.Scopes)
		}
	}
}

// An object schema with no properties that forbids additional ones accepts
// only {}. Inference produces that for a struct with only unexported
// fields, which marshals to something else, so every call that returns one
// would fail output validation.
func TestToolSchemasHaveNoEmptyObjects(t *testing.T) {
	for _, tool := range listTools(t) {
		for _, bad := range emptyObjects(tool.Name+".input", tool.InputSchema) {
			if bad == tool.Name+".input" {
				continue // a tool that takes no arguments
			}
			t.Errorf("%s accepts only {}", bad)
		}
		for _, bad := range emptyObjects(tool.Name+".output", tool.OutputSchema) {
			t.Errorf("%s accepts only {}", bad)
		}
	}
}

func emptyObjects(path string, v any) []string {
	var out []string
	switch v := v.(type) {
	case map[string]any:
		props, _ := v["properties"].(map[string]any)
		if v["type"] == "object" && len(props) == 0 && v["additionalProperties"] == false {
			out = append(out, path)
		}
		for k, child := range v {
			out = append(out, emptyObjects(path+"."+k, child)...)
		}
	case []any:
		for i, child := range v {
			out = append(out, emptyObjects(fmt.Sprintf("%s[%d]", path, i), child)...)
		}
	}
	return out
}

// tokens accepts a fixed set of access tokens, standing in for
// auth.OAuthServer, whose own tests drive the real OAuth flow into /mcp.
type tokens map[string]core.Actor

func (ts tokens) VerifyAccessToken(_ context.Context, token string) (core.Actor, time.Time, error) {
	a, ok := ts[token]
	if !ok {
		return core.Actor{}, time.Time{}, auth.ErrInvalidToken
	}
	return a, time.Now().Add(time.Hour), nil
}

func (tokens) ResourceMetadataURL(string) string {
	return "http://localhost/.well-known/oauth-protected-resource/mcp"
}

type bearer struct{ token string }

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	if b.token != "" {
		r.Header.Set("Authorization", "Bearer "+b.token)
	}
	return http.DefaultTransport.RoundTrip(r)
}

func connect(ctx context.Context, url, token string) (*mcp.ClientSession, error) {
	return mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil).Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint:             url,
		HTTPClient:           &http.Client{Transport: bearer{token}},
		DisableStandaloneSSE: true,
		MaxRetries:           -1,
	}, nil)
}

func TestStatusNeedsResultsRead(t *testing.T) {
	ctx := context.Background()
	client := core.Actor{Via: core.ViaOAuth, ID: "1", UserID: 1, Name: "Claude Code (via Ada)", Role: core.Admin}
	reader, writer := client, client
	reader.Scopes = []core.Scope{core.ResultsRead}
	writer.Scopes = []core.Scope{core.RunsWrite}
	srv := httptest.NewServer(Handler(app.Services{Instance: instance.New("v1.2.3", nil, notOnboarded{}, nil, nil)}, nil, tokens{"byn_at_reader": reader, "byn_at_writer": writer}, "test"))
	t.Cleanup(srv.Close)

	cs, err := connect(ctx, srv.URL, "byn_at_reader")
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "status"})
	if err != nil || res.IsError {
		t.Fatalf("status with results:read: %+v, %v", res, err)
	}
	raw, _ := json.Marshal(res.StructuredContent)
	var st instance.Status
	if err := json.Unmarshal(raw, &st); err != nil || st != (instance.Status{Version: "v1.2.3"}) {
		t.Fatalf("status = %s, want version v1.2.3 and onboarded false", raw)
	}

	cs2, err := connect(ctx, srv.URL, "byn_at_writer")
	if err != nil {
		t.Fatal(err)
	}
	defer cs2.Close()
	res, err = cs2.CallTool(ctx, &mcp.CallToolParams{Name: "status"})
	if err != nil {
		t.Fatal(err)
	}
	text := ""
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			text += tc.Text
		}
	}
	if !res.IsError || !strings.Contains(text, "results:read") {
		t.Fatalf("status without results:read: error %v, %q; want a tool error naming the scope", res.IsError, text)
	}

	for _, token := range []string{"", "byn_at_unknown"} {
		if _, err := connect(ctx, srv.URL, token); err == nil {
			t.Errorf("connect with token %q succeeded", token)
		}
	}
	r, err := http.Post(srv.URL, "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != http.StatusUnauthorized || !strings.Contains(r.Header.Get("WWW-Authenticate"), "resource_metadata=") {
		t.Fatalf("no token: %d %q, want 401 with the resource metadata challenge", r.StatusCode, r.Header.Get("WWW-Authenticate"))
	}
}

// notOnboarded answers only Onboarded; the status tool calls nothing else.
type notOnboarded struct{ instance.Config }

func (notOnboarded) Onboarded(context.Context) (bool, error) { return false, nil }
