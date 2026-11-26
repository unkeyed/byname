package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"testing"
	"time"

	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"

	"github.com/unkeyed/byname/internal/config"
	"github.com/unkeyed/byname/internal/db"
)

func twoWorkspaces(person client) {
	person.call("POST", "/v1/workspaces", map[string]string{"name": "Kyring"}, http.StatusCreated, nil)
	person.call("POST", "/w/kyring/v1/brands", map[string]any{"name": "Contoso", "kind": "competitor"}, http.StatusOK, nil)
	person.call("POST", "/v1/brands", map[string]any{"name": "Fabrikam", "kind": "competitor"}, http.StatusOK, nil)
}

func toolBrands(t *testing.T, cs *mcp.ClientSession) []string {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "list_brands"})
	if err != nil || res.IsError {
		t.Fatalf("list_brands = %+v, %v", res, err)
	}
	raw, _ := json.Marshal(res.StructuredContent)
	var out struct{ Brands []config.Brand }
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, b := range out.Brands {
		names = append(names, b.Name)
	}
	return names
}

// An MCP client given /w/kyring/mcp finds everything from the 401 alone,
// as Claude Code does, signs in for that URL, and reads Kyring's data.
func TestMCPClientConnectsToAWorkspace(t *testing.T) {
	anon, person := newLocalServer(t)
	twoWorkspaces(person)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	resource := anon.base + "/w/kyring/mcp"

	r := mcpStatusAt(anon, "/w/kyring/mcp", "")
	want := fmt.Sprintf(`Bearer resource_metadata="%s/.well-known/oauth-protected-resource/w/kyring/mcp"`, anon.base)
	if r.code != http.StatusUnauthorized || r.header.Get("WWW-Authenticate") != want {
		t.Fatalf("/w/kyring/mcp without a token = %d with WWW-Authenticate %q, want 401 with %q", r.code, r.header.Get("WWW-Authenticate"), want)
	}
	var prm oauthex.ProtectedResourceMetadata
	anon.call("GET", "/.well-known/oauth-protected-resource/w/kyring/mcp", nil, http.StatusOK, &prm)
	if prm.Resource != resource || !slices.Equal(prm.AuthorizationServers, []string{anon.base}) {
		t.Fatalf("Kyring's protected resource metadata = %+v, want resource %s on the instance's authorization server", prm, resource)
	}

	var named []string
	h, err := mcpauth.NewAuthorizationCodeHandler(&mcpauth.AuthorizationCodeHandlerConfig{
		DynamicClientRegistrationConfig: &mcpauth.DynamicClientRegistrationConfig{
			Metadata: &oauthex.ClientRegistrationMetadata{ClientName: "Go SDK client", RedirectURIs: []string{loopbackRedirect}},
		},
		AuthorizationCodeFetcher: func(_ context.Context, args *mcpauth.AuthorizationArgs) (*mcpauth.AuthorizationResult, error) {
			u, err := url.Parse(args.URL)
			if err != nil {
				return nil, err
			}
			named = append(named, u.Query().Get("resource"))
			got := decide(t, person, consentHandle(t, anon, args.URL), true)
			return &mcpauth.AuthorizationResult{Code: got.Get("code"), State: got.Get("state")}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "sdk", Version: "0"}, nil).Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint: resource, OAuthHandler: h, DisableStandaloneSSE: true, MaxRetries: -1,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	if !slices.Equal(named, []string{resource}) {
		t.Fatalf("the client authorized for %v, want %s once", named, resource)
	}
	if list, err := cs.ListTools(ctx, nil); err != nil || len(list.Tools) == 0 {
		t.Fatalf("tools/list = %v, %v", list, err)
	}
	if got := toolBrands(t, cs); !slices.Equal(got, []string{"Contoso"}) {
		t.Fatalf("list_brands at /w/kyring/mcp = %v, want Kyring's Contoso only", got)
	}
}

// One sign-in covers every workspace: a token for any of the instance's
// MCP servers works at each, and only those servers are a resource.
func TestOneMCPSignInCoversEveryWorkspace(t *testing.T) {
	anon, person := newLocalServer(t)
	twoWorkspaces(person)

	_, forDefault := connectTo(t, anon, person, "Claude Code", anon.base+"/mcp")
	_, forKyring := connectTo(t, anon, person, "Cursor", anon.base+"/w/kyring/mcp")
	for _, c := range []struct {
		name, token, path string
		want              []string
	}{
		{"a /mcp token at /w/kyring/mcp", forDefault.AccessToken, "/w/kyring/mcp", []string{"Contoso"}},
		{"a /w/kyring/mcp token at /mcp", forKyring.AccessToken, "/mcp", []string{"Fabrikam"}},
		{"a /w/kyring/mcp token at /w/default/mcp", forKyring.AccessToken, "/w/default/mcp", []string{"Fabrikam"}},
	} {
		cs, err := mcpSessionAt(anon, c.path, c.token)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got := toolBrands(t, cs); !slices.Equal(got, c.want) {
			t.Errorf("%s: list_brands = %v, want %v", c.name, got, c.want)
		}
		cs.Close()
	}

	id := register(t, anon, "Claude Code", loopbackRedirect)
	p := authorizeQuery(anon, id, loopbackRedirect)
	code := decide(t, person, consentHandle(t, anon, "/oauth/authorize?"+p.query.Encode()), true).Get("code")
	for _, bad := range []string{
		anon.base + "/w/nope/mcp",
		anon.base + "/w/kyring/v1",
		anon.base + "/w/Kyring/mcp",
		"https://other.example/mcp",
		"https://other.example/w/kyring/mcp",
	} {
		q := authorizeQuery(anon, id, loopbackRedirect).query
		q.Set("resource", bad)
		if loc := anon.raw("GET", "/oauth/authorize?"+q.Encode(), nil, nil).header.Get("Location"); loc != "/oauth/consent?error=invalid_target" {
			t.Errorf("authorize for %s went to %q, want invalid_target", bad, loc)
		}
		_, e := tokenRequest(t, anon, url.Values{
			"grant_type": {"authorization_code"}, "client_id": {id}, "code": {code},
			"redirect_uri": {loopbackRedirect}, "code_verifier": {p.verifier}, "resource": {bad},
		}, http.StatusBadRequest)
		if e.Error != "invalid_target" {
			t.Errorf("token for %s = %+v, want invalid_target", bad, e)
		}
	}
}

// Someone without credentials cannot tell a workspace's slug from a
// made-up one, and cannot make the server look for either. Only a
// signed-in caller, or an MCP client with a token, brings a workspace
// another replica made live here.
func TestAnonymousCallersCannotTellWhichSlugsExist(t *testing.T) {
	anon, person := newLocalServer(t)
	if _, err := db.CreateWorkspace(context.Background(), anon.db, "Kyring", "kyring"); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/v1/brands", "/v1/nothing-here", "/auth/options"} {
		real, made := anon.do("GET", "/w/default"+path, nil), anon.do("GET", "/w/nope"+path, nil)
		if real.code != made.code || !bytes.Equal(real.body, made.body) {
			t.Errorf("GET %s under a real slug = %d %s, under a made-up one %d %s; want the same", path, real.code, real.body, made.code, made.body)
		}
	}
	if r := anon.do("GET", "/w/kyring/v1/brands", nil); r.code != http.StatusUnauthorized {
		t.Fatalf("an anonymous GET under a slug this replica has not seen = %d, want 401", r.code)
	}
	if r := anon.do("GET", "/.well-known/oauth-protected-resource/w/kyring/mcp", nil); r.code != http.StatusNotFound {
		t.Fatalf("anonymous metadata for a slug this replica has not seen = %d, want 404 without looking", r.code)
	}
	if _, ok := anon.reg.Live("kyring"); ok {
		t.Fatal("an anonymous request brought kyring live, want only signed-in callers to look for new workspaces")
	}

	_, token := connectTo(t, anon, person, "Cursor", anon.base+"/w/kyring/mcp")
	cs, err := mcpSessionAt(anon, "/w/kyring/mcp", token.AccessToken)
	if err != nil {
		t.Fatalf("an MCP client with a token at a workspace another replica made: %v", err)
	}
	if got := toolBrands(t, cs); len(got) != 0 {
		t.Errorf("Kyring's brands = %v, want none", got)
	}
	cs.Close()
	anon.call("GET", "/.well-known/oauth-protected-resource/w/kyring/mcp", nil, http.StatusOK, nil)

	if _, err := db.CreateWorkspace(context.Background(), anon.db, "Planetfall", "planetfall"); err != nil {
		t.Fatal(err)
	}
	person.call("GET", "/w/planetfall/v1/brands", nil, http.StatusOK, nil)
}
