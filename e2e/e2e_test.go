//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"

	"github.com/unkeyed/byname/internal/authtest"
)

var (
	baseURL    = strings.TrimRight(envOr("BASE_URL", "http://localhost:8080"), "/")
	publicURL  = strings.TrimRight(envOr("E2E_PUBLIC_URL", baseURL), "/")
	setupToken = os.Getenv("E2E_SETUP_TOKEN")
	adminEmail = envOr("E2E_ADMIN_EMAIL", "e2e-admin@byname-e2e.test")
)

// passkeyFile keeps the suite's passkey between runs, since the setup link
// that created it works once.
func passkeyFile() string {
	if f := os.Getenv("E2E_PASSKEY_FILE"); f != "" {
		return f
	}
	name := strings.NewReplacer(":", "_", "/", "_").Replace(strings.TrimPrefix(strings.TrimPrefix(baseURL, "http://"), "https://"))
	return filepath.Join(os.TempDir(), "byname-e2e-passkey-"+name+".json")
}

func TestHealthz(t *testing.T) {
	res := do(t, request{method: "GET", path: "/healthz"})
	res.expect(t, http.StatusOK)
}

func TestOpenAPIIsPublic(t *testing.T) {
	res := do(t, request{method: "GET", path: "/v1/openapi.yaml"})
	res.expect(t, http.StatusOK)
	if !bytes.HasPrefix(res.body, []byte("openapi: 3.1")) {
		t.Fatalf("GET /v1/openapi.yaml does not start with an OpenAPI 3.1 header: %.40q", res.body)
	}
}

func TestSetupLinkMadeAnAdmin(t *testing.T) {
	session := bootstrap(t)
	var me struct {
		Via    string   `json:"via"`
		Role   string   `json:"role"`
		Scopes []string `json:"scopes"`
	}
	do(t, request{method: "GET", path: "/v1/me", session: session}).decodeAs(t, http.StatusOK, &me)
	if me.Via != "session" || me.Role != "admin" || !slices.Equal(me.Scopes, []string{"results:read", "runs:write", "config:write"}) {
		t.Fatalf("GET /v1/me = %+v, want the setup link's admin with every scope", me)
	}
}

// A key reaches GET /v1/status only with results:read, never an admin
// route, and can always revoke itself.
func TestKeysCarryScopes(t *testing.T) {
	session := bootstrap(t)
	reader := createKey(t, session, "results:read")
	runner := createKey(t, session, "runs:write")

	var st struct {
		Version   string `json:"version"`
		Onboarded *bool  `json:"onboarded"`
	}
	do(t, request{method: "GET", path: "/v1/status", key: reader}).decodeAs(t, http.StatusOK, &st)
	if st.Version == "" || st.Onboarded == nil || *st.Onboarded {
		t.Fatalf("GET /v1/status = %+v, want a version and onboarded false", st)
	}
	do(t, request{method: "GET", path: "/v1/status", key: runner}).expect(t, http.StatusForbidden)
	do(t, request{method: "GET", path: "/v1/keys", key: reader}).expect(t, http.StatusForbidden)
	do(t, request{method: "POST", path: "/v1/keys", key: reader, body: map[string]any{"name": "minted", "scopes": []string{"results:read"}}}).expect(t, http.StatusForbidden)

	do(t, request{method: "DELETE", path: "/v1/keys/current", key: runner}).expect(t, http.StatusNoContent)
	do(t, request{method: "GET", path: "/v1/status", key: runner}).expect(t, http.StatusUnauthorized)
	do(t, request{method: "DELETE", path: "/v1/keys/current", key: reader}).expect(t, http.StatusNoContent)
}

func TestMCPStatus(t *testing.T) {
	session := bootstrap(t)
	key := createKey(t, session, "results:read")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if _, err := connectMCP(ctx, bearer{""}); err == nil {
		t.Fatal("MCP accepted a session without a token")
	}
	if _, err := connectMCP(ctx, bearer{key}); err == nil {
		t.Fatal("MCP accepted an API key")
	}

	cs, err := connectMCPWithOAuth(ctx, session)
	if err != nil {
		t.Fatalf("connect to %s/mcp: %v", baseURL, err)
	}
	defer cs.Close()
	tools, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	var names []string
	for _, tool := range tools.Tools {
		names = append(names, tool.Name)
	}
	if !slices.Contains(names, "status") {
		t.Fatalf("tools/list = %v, missing status", names)
	}
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "status"})
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if res.IsError {
		t.Fatalf("status returned a tool error: %s", toolText(res))
	}
}

func TestPasswordAloneIsRefused(t *testing.T) {
	bootstrap(t)
	res := do(t, request{method: "POST", path: "/auth/password", body: map[string]string{"email": adminEmail, "password": "a-guessed-password"}})
	for _, c := range res.Cookies() {
		if c.Name == "byname_session" && c.Value != "" {
			t.Fatal("POST /auth/password set a session cookie")
		}
	}
	res.expect(t, http.StatusNotFound, http.StatusUnauthorized, http.StatusTooManyRequests)
}

func TestSetupLinkIsSpent(t *testing.T) {
	bootstrap(t)
	if setupToken == "" {
		t.Skip("E2E_SETUP_TOKEN is not set")
	}
	do(t, request{method: "GET", path: "/auth/setup/" + setupToken}).expect(t, http.StatusNotFound)
	do(t, request{method: "POST", path: "/auth/signup/passkey/begin",
		body: map[string]string{"setup_token": setupToken, "email": "intruder@byname-e2e.test"}}).expect(t, http.StatusNotFound)
}

var (
	setupOnce sync.Once
	session   string
	setupErr  error
)

// bootstrap signs in once per run and returns the admin's session.
func bootstrap(t *testing.T) string {
	t.Helper()
	setupOnce.Do(func() { session, setupErr = signIn() })
	if setupErr != nil {
		t.Fatalf("bootstrap: %v", setupErr)
	}
	return session
}

func createKey(t *testing.T, session string, scopes ...string) string {
	t.Helper()
	var key struct {
		Key string `json:"key"`
	}
	do(t, request{method: "POST", path: "/v1/keys", session: session, body: map[string]any{"name": "e2e " + unique(), "scopes": scopes}}).decodeAs(t, http.StatusCreated, &key)
	if !strings.HasPrefix(key.Key, "byn_") {
		t.Fatalf("POST /v1/keys returned key %q, want byn_...", key.Key)
	}
	return key.Key
}

type ceremony struct {
	Handle  string          `json:"handle"`
	Options json.RawMessage `json:"options"`
}

func signIn() (string, error) {
	if raw, err := os.ReadFile(passkeyFile()); err == nil {
		var pk authtest.Passkey
		if err := json.Unmarshal(raw, &pk); err != nil {
			return "", fmt.Errorf("read %s: %w", passkeyFile(), err)
		}
		session, err := passkeySignIn(&pk)
		if err == nil || setupToken == "" {
			return session, err
		}
	}
	if setupToken == "" {
		return "", fmt.Errorf("no saved passkey at %s and no E2E_SETUP_TOKEN: set it to the token of the setup link Byname logged at start", passkeyFile())
	}
	var begun ceremony
	if err := call(request{method: "POST", path: "/auth/signup/passkey/begin",
		body: map[string]string{"setup_token": setupToken, "email": adminEmail, "name": "E2E"}}, &begun); err != nil {
		return "", err
	}
	pk, cred, err := authtest.NewPasskey(publicURL, begun.Options)
	if err != nil {
		return "", err
	}
	res, err := send(request{method: "POST", path: "/auth/signup/passkey/finish", body: map[string]any{"handle": begun.Handle, "credential": cred}})
	if err != nil {
		return "", err
	}
	if err := res.check(http.StatusOK); err != nil {
		return "", err
	}
	if err := savePasskey(pk); err != nil {
		return "", err
	}
	return sessionCookie(res)
}

func passkeySignIn(pk *authtest.Passkey) (string, error) {
	var begun ceremony
	if err := call(request{method: "POST", path: "/auth/passkey/begin"}, &begun); err != nil {
		return "", err
	}
	cred, err := pk.Sign(begun.Options)
	if err != nil {
		return "", err
	}
	res, err := send(request{method: "POST", path: "/auth/passkey/finish", body: map[string]any{"handle": begun.Handle, "credential": cred}})
	if err != nil {
		return "", err
	}
	if err := res.check(http.StatusOK); err != nil {
		return "", err
	}
	if err := savePasskey(pk); err != nil {
		return "", err
	}
	return sessionCookie(res)
}

func savePasskey(pk *authtest.Passkey) error {
	raw, err := json.Marshal(pk)
	if err != nil {
		return err
	}
	return os.WriteFile(passkeyFile(), raw, 0o600)
}

func sessionCookie(res response) (string, error) {
	for _, c := range res.Cookies() {
		if c.Name == "byname_session" && c.Value != "" {
			return c.Value, nil
		}
	}
	return "", fmt.Errorf("%s %s set no byname_session cookie", res.req.method, res.req.path)
}

func call(r request, out any) error {
	res, err := send(r)
	if err != nil {
		return err
	}
	if err := res.check(http.StatusOK); err != nil {
		return err
	}
	return json.Unmarshal(res.body, out)
}

func connectMCP(ctx context.Context, rt http.RoundTripper) (*mcp.ClientSession, error) {
	return mcp.NewClient(&mcp.Implementation{Name: "byname-e2e", Version: "0"}, nil).Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint:             baseURL + "/mcp",
		HTTPClient:           &http.Client{Transport: rt, Timeout: 30 * time.Second},
		DisableStandaloneSSE: true,
		MaxRetries:           -1,
	}, nil)
}

// connectMCPWithOAuth connects the way Claude Code does: the SDK's OAuth
// client finds Byname's authorization server from the 401, registers, and
// opens the authorization URL, where the signed-in person approves.
func connectMCPWithOAuth(ctx context.Context, session string) (*mcp.ClientSession, error) {
	const redirect = "http://127.0.0.1:33418/callback"
	h, err := mcpauth.NewAuthorizationCodeHandler(&mcpauth.AuthorizationCodeHandlerConfig{
		DynamicClientRegistrationConfig: &mcpauth.DynamicClientRegistrationConfig{
			Metadata: &oauthex.ClientRegistrationMetadata{ClientName: "Byname e2e", RedirectURIs: []string{redirect}},
		},
		AuthorizationCodeFetcher: func(_ context.Context, args *mcpauth.AuthorizationArgs) (*mcpauth.AuthorizationResult, error) {
			return approve(args.URL, session)
		},
	})
	if err != nil {
		return nil, err
	}
	return mcp.NewClient(&mcp.Implementation{Name: "byname-e2e", Version: "0"}, nil).Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint:             baseURL + "/mcp",
		HTTPClient:           &http.Client{Timeout: 30 * time.Second},
		OAuthHandler:         h,
		DisableStandaloneSSE: true,
		MaxRetries:           -1,
	}, nil)
}

// approve does what the browser and the person do with an authorization
// URL: follow it to the consent page and approve as session.
func approve(authorizeURL, session string) (*mcpauth.AuthorizationResult, error) {
	noFollow := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := noFollow.Get(authorizeURL)
	if err != nil {
		return nil, err
	}
	res.Body.Close()
	consent, err := url.Parse(res.Header.Get("Location"))
	if err != nil || consent.Query().Get("request") == "" {
		return nil, fmt.Errorf("GET %s = %d to %q, want the consent page", authorizeURL, res.StatusCode, res.Header.Get("Location"))
	}
	var out struct{ Redirect string }
	if err := call(request{method: "POST", path: "/v1/oauth/requests/" + consent.Query().Get("request"), session: session,
		body: map[string]any{"approve": true, "scopes": []string{"results:read"}}}, &out); err != nil {
		return nil, err
	}
	back, err := url.Parse(out.Redirect)
	if err != nil {
		return nil, err
	}
	return &mcpauth.AuthorizationResult{Code: back.Query().Get("code"), State: back.Query().Get("state")}, nil
}

type bearer struct{ key string }

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	if b.key != "" {
		r = r.Clone(r.Context())
		r.Header.Set("Authorization", "Bearer "+b.key)
	}
	return http.DefaultTransport.RoundTrip(r)
}

func toolText(res *mcp.CallToolResult) string {
	var parts []string
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			parts = append(parts, tc.Text)
		}
	}
	return strings.Join(parts, " ")
}

type request struct {
	method, path string
	key          string // sent as a bearer token
	session      string // sent as the byname_session cookie
	header       http.Header
	body         any // []byte is sent as is; anything else as JSON
}

type response struct {
	*http.Response
	req  request
	body []byte
}

var httpClient = &http.Client{Timeout: 30 * time.Second}

func do(t *testing.T, r request) response {
	t.Helper()
	res, err := send(r)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func send(r request) (response, error) {
	var body io.Reader
	switch b := r.body.(type) {
	case nil:
	case []byte:
		body = bytes.NewReader(b)
	default:
		j, err := json.Marshal(b)
		if err != nil {
			return response{}, err
		}
		body = bytes.NewReader(j)
	}
	req, err := http.NewRequest(r.method, baseURL+r.path, body)
	if err != nil {
		return response{}, err
	}
	for k, v := range r.header {
		req.Header[k] = v
	}
	if r.body != nil && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if r.key != "" {
		req.Header.Set("Authorization", "Bearer "+r.key)
	}
	if r.session != "" {
		req.AddCookie(&http.Cookie{Name: "byname_session", Value: r.session})
	}
	res, err := httpClient.Do(req)
	if err != nil {
		return response{}, fmt.Errorf("%s %s: %w", r.method, r.path, err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		return response{}, fmt.Errorf("%s %s: read body: %w", r.method, r.path, err)
	}
	return response{Response: res, req: r, body: b}, nil
}

func (r response) check(codes ...int) error {
	if slices.Contains(codes, r.StatusCode) {
		return nil
	}
	return fmt.Errorf("%s %s: got %d %s, want %v", r.req.method, r.req.path, r.StatusCode, bytes.TrimSpace(r.body), codes)
}

func (r response) expect(t *testing.T, codes ...int) {
	t.Helper()
	if err := r.check(codes...); err != nil {
		t.Fatal(err)
	}
}

func (r response) decodeAs(t *testing.T, code int, v any) {
	t.Helper()
	r.expect(t, code)
	if err := json.Unmarshal(r.body, v); err != nil {
		t.Fatalf("%s %s: decode %s: %v", r.req.method, r.req.path, r.body, err)
	}
}

func unique() string {
	return strconv.FormatInt(time.Now().UnixNano(), 36) + hex.EncodeToString(randomBytes(3))
}

func randomBytes(n int) []byte {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return b
}

func envOr(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}
