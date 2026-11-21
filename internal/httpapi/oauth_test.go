package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"golang.org/x/oauth2"

	"github.com/unkeyed/byname/internal/auth"
	"github.com/unkeyed/byname/internal/core"
)

const loopbackRedirect = "http://127.0.0.1:33418/callback"

// newLocalServer serves Byname at http://localhost:<port> with that as its
// PUBLIC_URL, so the URLs an MCP client discovers are the ones it can reach.
func newLocalServer(t *testing.T) (anon, person client) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(nil)
	srv.Listener.Close()
	srv.Listener = ln
	origin := fmt.Sprintf("http://localhost:%d", ln.Addr().(*net.TCPAddr).Port)
	anon = serve(t, srv, origin)
	anon.base = origin
	return anon, anon.signup(map[string]string{"setup_token": anon.setupToken, "email": adminEmail})
}

// raw sends a request without following redirects and without the JSON
// conventions of client.do.
func (c client) raw(method, path string, body url.Values, header map[string]string) reply {
	c.t.Helper()
	var r io.Reader
	if body != nil {
		r = strings.NewReader(body.Encode())
	}
	req, err := http.NewRequest(method, c.base+path, r)
	if err != nil {
		c.t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := noFollow.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		c.t.Fatal(err)
	}
	return reply{code: res.StatusCode, header: res.Header, body: b}
}

func register(t *testing.T, c client, name string, redirects ...string) string {
	t.Helper()
	var reg struct {
		ClientID   string `json:"client_id"`
		AuthMethod string `json:"token_endpoint_auth_method"`
	}
	c.call("POST", "/oauth/register", map[string]any{"client_name": name, "redirect_uris": redirects}, http.StatusCreated, &reg)
	if reg.ClientID == "" || reg.AuthMethod != "none" {
		t.Fatalf("registered %+v, want a client_id and token_endpoint_auth_method none", reg)
	}
	return reg.ClientID
}

// pkce is one authorization attempt's verifier and its query.
type pkce struct {
	verifier, state string
	query           url.Values
}

func authorizeQuery(c client, clientID, redirect string) pkce {
	v := oauth2.GenerateVerifier()
	state := oauth2.GenerateVerifier()[:16]
	return pkce{v, state, url.Values{
		"response_type": {"code"}, "client_id": {clientID}, "redirect_uri": {redirect}, "state": {state},
		"code_challenge": {oauth2.S256ChallengeFromVerifier(v)}, "code_challenge_method": {"S256"}, "resource": {c.base + "/mcp"},
	}}
}

// consentHandle follows /oauth/authorize to the consent page the way a
// browser does and returns the request handle in its URL.
func consentHandle(t *testing.T, c client, authorizeURL string) string {
	t.Helper()
	u, err := url.Parse(authorizeURL)
	if err != nil {
		t.Fatal(err)
	}
	r := c.raw("GET", u.RequestURI(), nil, nil)
	loc, _ := url.Parse(r.header.Get("Location"))
	if r.code != http.StatusFound || loc == nil || loc.Path != "/oauth/consent" || loc.Query().Get("request") == "" {
		t.Fatalf("GET /oauth/authorize = %d to %q, want a redirect to the consent page", r.code, r.header.Get("Location"))
	}
	return loc.Query().Get("request")
}

// decide answers the consent page as person, approving with results:read
// unless scopes says otherwise, and returns the query of the redirect it
// sends the browser to.
func decide(t *testing.T, person client, handle string, approve bool, scopes ...core.Scope) url.Values {
	t.Helper()
	if len(scopes) == 0 {
		scopes = []core.Scope{core.ResultsRead}
	}
	var out struct{ Redirect string }
	person.call("POST", "/v1/oauth/requests/"+handle, map[string]any{"approve": approve, "scopes": scopes}, http.StatusOK, &out)
	u, err := url.Parse(out.Redirect)
	if err != nil {
		t.Fatal(err)
	}
	return u.Query()
}

type tokens struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
}

type oauthErrorBody struct {
	Error       string `json:"error"`
	Description string `json:"error_description"`
}

func tokenRequest(t *testing.T, c client, form url.Values, want int) (tokens, oauthErrorBody) {
	t.Helper()
	r := c.raw("POST", "/oauth/token", form, nil)
	if r.code != want {
		t.Fatalf("POST /oauth/token %v = %d %s, want %d", form.Get("grant_type"), r.code, r.body, want)
	}
	var tok tokens
	var e oauthErrorBody
	_ = json.Unmarshal(r.body, &tok)
	_ = json.Unmarshal(r.body, &e)
	return tok, e
}

// connect runs the whole flow for a new client and returns its tokens.
func connect(t *testing.T, anon, person client, name string) (string, tokens) {
	t.Helper()
	id := register(t, anon, name, loopbackRedirect)
	p := authorizeQuery(anon, id, loopbackRedirect)
	got := decide(t, person, consentHandle(t, anon, "/oauth/authorize?"+p.query.Encode()), true)
	tok, _ := tokenRequest(t, anon, url.Values{
		"grant_type": {"authorization_code"}, "client_id": {id}, "code": {got.Get("code")},
		"redirect_uri": {loopbackRedirect}, "code_verifier": {p.verifier}, "resource": {anon.base + "/mcp"},
	}, http.StatusOK)
	return id, tok
}

func mcpSession(c client, token string) (*mcp.ClientSession, error) {
	return mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil).Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint: c.base + "/mcp", HTTPClient: &http.Client{Transport: bearer(token)}, DisableStandaloneSSE: true, MaxRetries: -1,
	}, nil)
}

// mcpStatus is the status of a bare tools/list with token.
func mcpStatus(c client, token string) reply {
	c.t.Helper()
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
	req, _ := http.NewRequest("POST", c.base+"/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", "2025-06-18")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return reply{code: res.StatusCode, header: res.Header, body: b}
}

func wantChallenge(t *testing.T, c client, r reply, invalid bool) {
	t.Helper()
	want := fmt.Sprintf(`Bearer resource_metadata="%s/.well-known/oauth-protected-resource/mcp"`, c.base)
	if invalid {
		want += `, error="invalid_token"`
	}
	if r.code != http.StatusUnauthorized || r.header.Get("WWW-Authenticate") != want {
		t.Fatalf("/mcp = %d with WWW-Authenticate %q, want 401 with %q", r.code, r.header.Get("WWW-Authenticate"), want)
	}
}

// TestOAuthConnectsAnMCPClient walks an MCP client through discovery,
// registration, consent, PKCE, tool calls as the person, refresh rotation,
// and refresh reuse, which ends the connection.
func TestOAuthConnectsAnMCPClient(t *testing.T) {
	anon, person := newLocalServer(t)
	ctx := context.Background()

	wantChallenge(t, anon, mcpStatus(anon, ""), false)
	var prm oauthex.ProtectedResourceMetadata
	anon.call("GET", "/.well-known/oauth-protected-resource/mcp", nil, http.StatusOK, &prm)
	if prm.Resource != anon.base+"/mcp" || len(prm.AuthorizationServers) != 1 || prm.AuthorizationServers[0] != anon.base ||
		!slices.Equal(prm.ScopesSupported, []string{"results:read", "runs:write", "config:write"}) {
		t.Fatalf("protected resource metadata = %+v", prm)
	}
	var asm struct {
		Issuer, AuthorizationEndpoint, TokenEndpoint, RegistrationEndpoint string
		Methods                                                            []string `json:"code_challenge_methods_supported"`
		Scopes                                                             []string `json:"scopes_supported"`
	}
	r := anon.call("GET", "/.well-known/oauth-authorization-server", nil, http.StatusOK, &asm)
	if asm.Issuer != anon.base || len(asm.Methods) != 1 || asm.Methods[0] != "S256" || r.header.Get("Access-Control-Allow-Origin") != "*" ||
		!slices.Equal(asm.Scopes, prm.ScopesSupported) {
		t.Fatalf("authorization server metadata = %s", r.body)
	}

	id := register(t, anon, "Claude Code", loopbackRedirect)
	// A native app listens on whatever port is free when it signs in.
	redirect := "http://127.0.0.1:40001/callback"
	p := authorizeQuery(anon, id, redirect)
	handle := consentHandle(t, anon, "/oauth/authorize?"+p.query.Encode())
	if got := anon.do("GET", "/v1/oauth/requests/"+handle, nil); got.code != http.StatusUnauthorized {
		t.Fatalf("consent request without a session = %d, want 401 so the page signs in first", got.code)
	}
	var consent auth.ConsentRequest
	person.call("GET", "/v1/oauth/requests/"+handle, nil, http.StatusOK, &consent)
	if consent.ClientName != "Claude Code" || consent.RedirectURI != redirect || !slices.Equal(consent.Scopes, core.Scopes) {
		t.Fatalf("consent page shows %+v, want every scope offered to a client that named none", consent)
	}
	got := decide(t, person, handle, true)
	if got.Get("state") != p.state || got.Get("code") == "" {
		t.Fatalf("approval redirected with %v, want a code and state %q", got, p.state)
	}
	if again := person.do("POST", "/v1/oauth/requests/"+handle, map[string]any{"approve": true, "scopes": []string{"results:read"}}); again.code != http.StatusNotFound {
		t.Fatalf("approving the same request twice = %d, want 404", again.code)
	}

	exchange := url.Values{"grant_type": {"authorization_code"}, "client_id": {id}, "code": {got.Get("code")}, "redirect_uri": {redirect}}
	exchange.Set("code_verifier", oauth2.GenerateVerifier())
	if _, e := tokenRequest(t, anon, exchange, http.StatusBadRequest); e.Error != "invalid_grant" {
		t.Fatalf("a wrong code_verifier got %+v, want invalid_grant", e)
	}
	exchange.Set("code_verifier", p.verifier)
	tok, _ := tokenRequest(t, anon, exchange, http.StatusOK)
	if tok.TokenType != "Bearer" || tok.ExpiresIn != 3600 || !strings.HasPrefix(tok.AccessToken, "byn_at_") || !strings.HasPrefix(tok.RefreshToken, "byn_rt_") || tok.Scope != "results:read" {
		t.Fatalf("tokens = %+v", tok)
	}

	cs, err := mcpSession(anon, tok.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	if list, err := cs.ListTools(ctx, nil); err != nil || len(list.Tools) == 0 {
		t.Fatalf("tools/list = %v, %v", list, err)
	}
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "status"})
	if err != nil || res.IsError {
		t.Fatalf("status = %+v, %v", res, err)
	}
	var apps []auth.App
	person.call("GET", "/v1/apps", nil, http.StatusOK, &apps)
	if len(apps) != 1 || apps[0].Name != "Claude Code" || apps[0].ApprovedBy != adminEmail || apps[0].LastUsedAt == nil ||
		!slices.Equal(apps[0].Scopes, []core.Scope{core.ResultsRead}) {
		t.Fatalf("connected apps = %+v", apps)
	}

	refresh := url.Values{"grant_type": {"refresh_token"}, "client_id": {id}, "refresh_token": {tok.RefreshToken}}
	rotated, _ := tokenRequest(t, anon, refresh, http.StatusOK)
	if rotated.RefreshToken == tok.RefreshToken || rotated.AccessToken == tok.AccessToken {
		t.Fatal("refresh did not rotate the tokens")
	}
	if r := mcpStatus(anon, rotated.AccessToken); r.code != http.StatusOK {
		t.Fatalf("/mcp with the refreshed token = %d %s", r.code, r.body)
	}
	if _, e := tokenRequest(t, anon, refresh, http.StatusBadRequest); e.Error != "invalid_grant" {
		t.Fatalf("reusing a refresh token got %+v, want invalid_grant", e)
	}
	wantChallenge(t, anon, mcpStatus(anon, rotated.AccessToken), true)
	person.call("GET", "/v1/apps", nil, http.StatusOK, &apps)
	if len(apps) != 0 {
		t.Fatalf("after refresh reuse the connection is still listed: %+v", apps)
	}
}

// TestOAuthConnectionsEnd checks every way a connection stops working:
// RFC 7009 revocation, Revoke in Settings, a reused code, and removing the
// person it acts for.
func TestOAuthConnectionsEnd(t *testing.T) {
	anon, person := newLocalServer(t)

	_, tok := connect(t, anon, person, "Cursor")
	if r := anon.raw("POST", "/oauth/revoke", url.Values{"token": {tok.RefreshToken}}, nil); r.code != http.StatusOK {
		t.Fatalf("revoke = %d %s", r.code, r.body)
	}
	wantChallenge(t, anon, mcpStatus(anon, tok.AccessToken), true)
	if r := anon.raw("POST", "/oauth/revoke", url.Values{"token": {"never-issued"}}, nil); r.code != http.StatusOK {
		t.Fatalf("revoking an unknown token = %d, want 200 (RFC 7009 section 2.2)", r.code)
	}

	_, tok = connect(t, anon, person, "Cursor")
	var apps []auth.App
	person.call("GET", "/v1/apps", nil, http.StatusOK, &apps)
	if len(apps) != 1 {
		t.Fatalf("connected apps = %+v, want the new one", apps)
	}
	person.call("DELETE", "/v1/apps/"+apps[0].ID, nil, http.StatusNoContent, nil)
	wantChallenge(t, anon, mcpStatus(anon, tok.AccessToken), true)
	if _, e := tokenRequest(t, anon, url.Values{"grant_type": {"refresh_token"}, "client_id": {"x"}, "refresh_token": {tok.RefreshToken}}, http.StatusBadRequest); e.Error != "invalid_grant" {
		t.Fatalf("refreshing a revoked connection got %+v", e)
	}

	id := register(t, anon, "Claude Code", loopbackRedirect)
	p := authorizeQuery(anon, id, loopbackRedirect)
	got := decide(t, person, consentHandle(t, anon, "/oauth/authorize?"+p.query.Encode()), true)
	exchange := url.Values{"grant_type": {"authorization_code"}, "client_id": {id}, "code": {got.Get("code")}, "redirect_uri": {loopbackRedirect}, "code_verifier": {p.verifier}}
	tok, _ = tokenRequest(t, anon, exchange, http.StatusOK)
	if _, e := tokenRequest(t, anon, exchange, http.StatusBadRequest); e.Error != "invalid_grant" {
		t.Fatalf("a second exchange of one code got %+v, want invalid_grant", e)
	}
	wantChallenge(t, anon, mcpStatus(anon, tok.AccessToken), true)

	var invite struct{ Token string }
	person.call("POST", "/v1/invites", map[string]string{"email": "grace@example.com", "role": "viewer"}, http.StatusCreated, &invite)
	teammate := anon.signup(map[string]string{"invite_token": invite.Token, "email": "grace@example.com"})
	_, tok = connect(t, anon, teammate, "Claude Code")
	if r := mcpStatus(anon, tok.AccessToken); r.code != http.StatusOK {
		t.Fatalf("teammate's token = %d before removal", r.code)
	}
	var users []auth.User
	person.call("GET", "/v1/users", nil, http.StatusOK, &users)
	for _, u := range users {
		if u.Email == "grace@example.com" {
			person.call("DELETE", fmt.Sprintf("/v1/users/%d", u.ID), nil, http.StatusNoContent, nil)
		}
	}
	wantChallenge(t, anon, mcpStatus(anon, tok.AccessToken), true)
	if _, e := tokenRequest(t, anon, url.Values{"grant_type": {"refresh_token"}, "client_id": {"x"}, "refresh_token": {tok.RefreshToken}}, http.StatusBadRequest); e.Error != "invalid_grant" {
		t.Fatalf("a removed teammate's refresh token got %+v", e)
	}
}

func TestMCPRefusesAPIKeys(t *testing.T) {
	anon, person := newLocalServer(t)
	var key struct{ Key string }
	person.call("POST", "/v1/keys", map[string]any{"name": "old agent", "scopes": []string{"results:read"}}, http.StatusCreated, &key)
	r := mcpStatus(anon, key.Key)
	wantChallenge(t, anon, r, true)
	if !strings.Contains(string(r.body), "API keys do not work on /mcp") {
		t.Fatalf("an API key on /mcp says %q, want it to explain the switch to sign-in", r.body)
	}
	agent := anon
	agent.key = key.Key
	agent.call("GET", "/v1/status", nil, http.StatusOK, nil)
}

// The consent page offers the scopes a client asked for, the person picks
// from them within their role, the grant keeps the choice, and /mcp holds
// the connection to it.
func TestOAuthScopes(t *testing.T) {
	anon, person := newLocalServer(t)
	ctx := context.Background()
	id := register(t, anon, "Cursor", loopbackRedirect)
	p := authorizeQuery(anon, id, loopbackRedirect)
	p.query.Set("scope", "openid runs:write mcp")
	handle := consentHandle(t, anon, "/oauth/authorize?"+p.query.Encode())
	var consent auth.ConsentRequest
	person.call("GET", "/v1/oauth/requests/"+handle, nil, http.StatusOK, &consent)
	if !slices.Equal(consent.Scopes, []core.Scope{core.RunsWrite}) {
		t.Fatalf("consent offers %v, want only runs:write, the one Byname scope the client named", consent.Scopes)
	}
	for _, scopes := range [][]string{{}, {"results:read"}, {"everything"}} {
		if r := person.do("POST", "/v1/oauth/requests/"+handle, map[string]any{"approve": true, "scopes": scopes}); r.code != http.StatusBadRequest {
			t.Fatalf("approving with %v = %d %s, want 400", scopes, r.code, r.body)
		}
	}
	got := decide(t, person, handle, true, core.RunsWrite)
	tok, _ := tokenRequest(t, anon, url.Values{
		"grant_type": {"authorization_code"}, "client_id": {id}, "code": {got.Get("code")},
		"redirect_uri": {loopbackRedirect}, "code_verifier": {p.verifier},
	}, http.StatusOK)
	if tok.Scope != "runs:write" {
		t.Fatalf("token scope %q, want runs:write", tok.Scope)
	}
	cs, err := mcpSession(anon, tok.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "status"})
	if err != nil || !res.IsError {
		t.Fatalf("status over a runs:write connection = %+v, %v; want a tool error", res, err)
	}

	var invite struct{ Token string }
	person.call("POST", "/v1/invites", map[string]string{"email": "vi@example.com", "role": "viewer"}, http.StatusCreated, &invite)
	viewer := anon.signup(map[string]string{"invite_token": invite.Token})
	q := authorizeQuery(anon, id, loopbackRedirect)
	handle = consentHandle(t, anon, "/oauth/authorize?"+q.query.Encode())
	r := viewer.do("POST", "/v1/oauth/requests/"+handle, map[string]any{"approve": true, "scopes": []string{"results:read", "config:write"}})
	if r.code != http.StatusForbidden || !strings.Contains(string(r.body), "config:write") {
		t.Fatalf("viewer approving config:write = %d %s, want 403 naming the scope", r.code, r.body)
	}
	decide(t, viewer, handle, true)
}

func TestOAuthRegistrationRedirectURIs(t *testing.T) {
	anon, _ := newLocalServer(t)
	for _, uri := range []string{
		"https://app.example.com/oauth/callback",
		"http://127.0.0.1:33418/callback",
		"http://localhost:6274/oauth/callback",
		"http://[::1]:8080/cb",
		"cursor://anysphere.cursor-retrieval/oauth/user-byname/callback",
		"com.example.app:/oauth2redirect",
	} {
		if r := anon.do("POST", "/oauth/register", map[string]any{"redirect_uris": []string{uri}}); r.code != http.StatusCreated {
			t.Errorf("registering %s = %d %s, want 201", uri, r.code, r.body)
		}
	}
	for _, uri := range []string{
		"javascript:alert(1)",
		"JavaScript://example.com/%0aalert(1)",
		"data:text/html,<script>alert(1)</script>",
		"file:///etc/passwd",
		"http://evil.example/callback",
		"http://127.0.0.1.evil.example/callback",
		"https://user@app.example.com/callback",
		"https://app.example.com/callback#frag",
		"/relative/callback",
	} {
		r := anon.do("POST", "/oauth/register", map[string]any{"redirect_uris": []string{uri}})
		var e oauthErrorBody
		_ = json.Unmarshal(r.body, &e)
		if r.code != http.StatusBadRequest || e.Error != "invalid_redirect_uri" {
			t.Errorf("registering %s = %d %s, want 400 invalid_redirect_uri", uri, r.code, r.body)
		}
	}
	if r := anon.do("POST", "/oauth/register", map[string]any{"client_name": "Claude\nCode", "redirect_uris": []string{loopbackRedirect}}); r.code != http.StatusBadRequest {
		t.Errorf("a client_name with a newline = %d, want 400", r.code)
	}
}

// TestOAuthAuthorizeRefusals checks that a refused authorization request is
// explained on Byname's own page and never redirected, so Byname cannot be
// used as an open redirector, and that declining goes back to the client.
func TestOAuthAuthorizeRefusals(t *testing.T) {
	anon, person := newLocalServer(t)
	id := register(t, anon, "Claude Code", loopbackRedirect)

	wantPage := func(q url.Values, code string) {
		t.Helper()
		r := anon.raw("GET", "/oauth/authorize?"+q.Encode(), nil, nil)
		if loc := r.header.Get("Location"); r.code != http.StatusFound || loc != "/oauth/consent?error="+code {
			t.Errorf("authorize %v = %d to %q, want the consent page with error %s", q, r.code, loc, code)
		}
	}
	q := authorizeQuery(anon, "no-such-client", loopbackRedirect).query
	wantPage(q, "invalid_client")
	q = authorizeQuery(anon, id, "http://127.0.0.1:33418/elsewhere").query
	wantPage(q, "invalid_redirect_uri")
	q = authorizeQuery(anon, id, "https://evil.example/callback").query
	wantPage(q, "invalid_redirect_uri")

	q = authorizeQuery(anon, id, loopbackRedirect).query
	q.Del("code_challenge")
	wantPage(q, "invalid_request")
	q = authorizeQuery(anon, id, loopbackRedirect).query
	q.Set("code_challenge_method", "plain")
	wantPage(q, "invalid_request")
	q = authorizeQuery(anon, id, loopbackRedirect).query
	q.Set("response_type", "token")
	wantPage(q, "unsupported_response_type")
	q = authorizeQuery(anon, id, loopbackRedirect).query
	q.Set("resource", "https://other.example/mcp")
	wantPage(q, "invalid_target")

	p := authorizeQuery(anon, id, loopbackRedirect)
	got := decide(t, person, consentHandle(t, anon, "/oauth/authorize?"+p.query.Encode()), false)
	if got.Get("error") != "access_denied" || got.Get("state") != p.state || got.Has("code") {
		t.Fatalf("declining redirected with %v, want error=access_denied and the state", got)
	}
}

// TestSDKClientConnectsWithOAuth connects the Go MCP SDK's own OAuth client,
// which finds everything from the 401 alone, as Claude Code does.
func TestSDKClientConnectsWithOAuth(t *testing.T) {
	anon, person := newLocalServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	h, err := mcpauth.NewAuthorizationCodeHandler(&mcpauth.AuthorizationCodeHandlerConfig{
		DynamicClientRegistrationConfig: &mcpauth.DynamicClientRegistrationConfig{
			Metadata: &oauthex.ClientRegistrationMetadata{ClientName: "Go SDK client", RedirectURIs: []string{loopbackRedirect}},
		},
		AuthorizationCodeFetcher: func(_ context.Context, args *mcpauth.AuthorizationArgs) (*mcpauth.AuthorizationResult, error) {
			got := decide(t, person, consentHandle(t, anon, args.URL), true)
			return &mcpauth.AuthorizationResult{Code: got.Get("code"), State: got.Get("state")}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "sdk", Version: "0"}, nil).Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint: anon.base + "/mcp", OAuthHandler: h, DisableStandaloneSSE: true, MaxRetries: -1,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "status"})
	if err != nil || res.IsError {
		t.Fatalf("status = %+v, %v", res, err)
	}
}

type bearer string

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(b)))
	return http.DefaultTransport.RoundTrip(r)
}

// mcpExclusions are routes an API key can call with no MCP tool, and why.
var mcpExclusions = map[string]string{
	"DELETE /v1/keys/current": "revokes the calling API key; MCP clients sign in with OAuth and are disconnected under Settings > Apps",
}

// mcpTools maps each route an API key can call to the MCP tool that does
// the same, so the two surfaces cannot drift apart unnoticed.
var mcpTools = map[string]string{
	"GET /v1/status": "status",

	"GET /v1/settings":              "get_settings",
	"PUT /v1/settings":              "update_settings",
	"GET /v1/brands":                "list_brands",
	"POST /v1/brands":               "upsert_brand",
	"PUT /v1/brands/{id}":           "update_brand",
	"DELETE /v1/brands/{id}":        "delete_brand",
	"GET /v1/questions":             "get_questions",
	"PUT /v1/questions":             "update_questions",
	"GET /v1/question-sets":         "list_question_sets",
	"GET /v1/question-sets/{id}":    "get_question_set",
	"GET /v1/surfaces":              "list_surfaces",
	"PUT /v1/surfaces/{key}":        "update_surface",
	"GET /v1/surfaces/{key}/models": "list_surface_models",
	// Onboarding, pair tests and the cost estimate.
	"GET /v1/onboarding":              "get_onboarding",
	"PUT /v1/onboarding/steps/{step}": "save_onboarding_step",
	"POST /v1/onboarding/finish":      "finish_onboarding",
	"POST /v1/pairs/{id}/test":        "test_pair",
	"GET /v1/estimate":                "estimate_cost",

	"POST /v1/runs":          "run_now",
	"POST /v1/checks/pause":  "pause_checks",
	"POST /v1/checks/resume": "resume_checks",

	"GET /v1/runs":                   "list_runs",
	"GET /v1/runs/{id}":              "get_run",
	"GET /v1/runs/{id}/answers":      "list_run_answers",
	"GET /v1/runs/{id}/questions":    "run_questions",
	"GET /v1/answers/{id}":           "get_answer",
	"GET /v1/compare":                "compare_runs",
	"GET /v1/questions/{id}/history": "question_history",
	"GET /v1/metrics/rates":          "mention_rates",
	"GET /v1/metrics/share":          "share_of_mentions",
	"GET /v1/metrics/summary":        "results_summary",
	"GET /v1/metrics/competitors":    "competitors",
	"GET /v1/gaps":                   "gaps",
	"GET /v1/jobs":                   "list_jobs",

	"GET /v1/prompts":                "prompts",
	"GET /v1/sources":                "cited_sources",
	"GET /v1/metrics/engine":         "engine_view",
	"GET /v1/alerts":                 "list_alerts",
	"POST /v1/alerts/read":           "mark_alerts_read",
	"GET /v1/search/summary":         "search_summary",
	"GET /v1/search/daily":           "search_daily",
	"GET /v1/search/brand":           "search_brand_split",
	"GET /v1/search/pages":           "top_pages",
	"GET /v1/search/queries":         "top_queries",
	"GET /v1/goals":                  "list_goals",
	"POST /v1/goals":                 "upsert_goal",
	"PUT /v1/goals/{id}":             "update_goal",
	"DELETE /v1/goals/{id}":          "delete_goal",
	"PUT /v1/settings/branded-terms": "update_branded_terms",
	"GET /v1/gsc":                    "search_console_status",
}

// TestEveryAPIKeyRouteHasAnMCPTool fails when a route an API key can call
// has neither an MCP tool nor a stated reason to lack one, names a tool the
// MCP server does not have, or needs another scope than its tool.
func TestEveryAPIKeyRouteHasAnMCPTool(t *testing.T) {
	anon, person := newLocalServer(t)
	_, tok := connect(t, anon, person, "parity check")
	cs, err := mcpSession(anon, tok.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	list, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	toolScope := map[string]string{}
	for _, tool := range list.Tools {
		toolScope[tool.Name], _ = tool.Meta["byname.dev/scope"].(string)
	}
	for _, rt := range routes {
		if rt.access == public || rt.access == session || rt.access == admin {
			continue
		}
		tool, mapped := mcpTools[rt.pattern]
		_, excluded := mcpExclusions[rt.pattern]
		switch {
		case !mapped && !excluded:
			t.Errorf("%s: an API key can call it but it has no MCP tool; add one, or a reason to mcpExclusions", rt.pattern)
		case mapped && toolScope[tool] == "":
			t.Errorf("%s maps to %s, which the MCP server does not have", rt.pattern, tool)
		case mapped && toolScope[tool] != string(rt.access):
			t.Errorf("%s needs %s but its tool %s needs %s", rt.pattern, rt.access, tool, toolScope[tool])
		}
	}
}

func TestWrongMethodIs405WithAllow(t *testing.T) {
	c := newServer(t)
	_, agent := login(t, c)
	for _, tc := range []struct{ method, path, allow string }{
		{"POST", "/v1/status", "GET"},
		{"PUT", "/v1/keys", "GET, POST"},
		{"PUT", "/v1/users/1", "PATCH, DELETE"},
	} {
		r := agent.do(tc.method, tc.path, nil)
		if r.code != http.StatusMethodNotAllowed || r.header.Get("Allow") != tc.allow {
			t.Errorf("%s %s = %d Allow %q, want 405 Allow %q", tc.method, tc.path, r.code, r.header.Get("Allow"), tc.allow)
		}
	}
}
