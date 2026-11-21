// Package providertest fakes every provider's own API, for tests that run a
// surface on the scraper, provider or gateway route instead of Treg. One
// server serves each provider under a path prefix named for its credential
// kind ("/openai", "/dataforseo", ...), so BaseURL(kind) is what goes in
// engines.Conn.BaseURL, and Conn(kind) is a ready Conn.
//
// Each provider checks its key where the real one reads it and expects
// Secret(kind):
//
//   - OpenAI, Perplexity, cloro and the gateway: Authorization: Bearer providertest-key
//   - Anthropic: x-api-key: providertest-key, with an anthropic-version header
//   - Google: x-goog-api-key: providertest-key
//   - SerpApi: the api_key query parameter, providertest-key
//   - DataForSEO: HTTP basic auth, so the secret is
//     providertest-login:providertest-password
//
// A wrong key gets that provider's own error body and status. Answers and
// model lists come from internal/engines/testdata: the pinned endpoints'
// fixtures that tregtest serves for the scraping providers, and the
// synthetic fixtures for the model APIs. Every answer names Northwind.
package providertest

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/unkeyed/byname/internal/engines"
	"github.com/unkeyed/byname/internal/engines/tregtest"
)

const (
	key                = "providertest-key"
	dataforseoLogin    = "providertest-login"
	dataforseoPassword = "providertest-password"
)

// Secret is the key the fake accepts for kind.
func Secret(kind engines.CredentialKind) string {
	if kind == engines.CredentialDataForSEO {
		return dataforseoLogin + ":" + dataforseoPassword
	}
	return key
}

// Call is one request the fake received.
type Call struct {
	Kind engines.CredentialKind
	// Path is below the provider's prefix, such as /v1/responses.
	Path       string
	Method     string
	Query      url.Values
	Header     http.Header
	Body       []byte
	Authorized bool
}

type response struct {
	status int
	body   []byte
}

type Server struct {
	*httptest.Server

	mu    sync.Mutex
	calls []Call
	// routes maps kind, method and path to the response.
	routes map[string]response
	denied map[engines.CredentialKind][]byte
}

// providers is each provider's auth check, its routes as method and path to
// a fixture under internal/engines/testdata, and its reply to a wrong key.
var providers = map[engines.CredentialKind]struct {
	authorized   func(*http.Request) bool
	routes       map[string]string
	deniedStatus int
	denied       string
}{
	engines.CredentialOpenAI: {
		authorized: bearer,
		routes: map[string]string{
			"POST /v1/responses": "openai/openai.responses.search.synthetic.json",
			"GET /v1/models":     "openai/openai.models.synthetic.json",
		},
		deniedStatus: http.StatusUnauthorized,
		denied:       "openai/openai.responses.error.synthetic.json",
	},
	engines.CredentialAnthropic: {
		authorized: func(r *http.Request) bool {
			return r.Header.Get("x-api-key") == key && r.Header.Get("anthropic-version") != ""
		},
		routes: map[string]string{
			"POST /v1/messages": "anthropic/anthropic.messages.search.synthetic.json",
			"GET /v1/models":    "anthropic/anthropic.models.synthetic.json",
		},
		deniedStatus: http.StatusUnauthorized,
		denied:       "anthropic/anthropic.messages.error.synthetic.json",
	},
	engines.CredentialGoogle: {
		authorized: func(r *http.Request) bool { return r.Header.Get("x-goog-api-key") == key },
		routes: map[string]string{
			"POST /v1beta/models/*:generateContent": "google/google.generate-content.grounded.synthetic.json",
			"GET /v1beta/models":                    "google/google.models.synthetic.json",
		},
		deniedStatus: http.StatusBadRequest,
		denied:       "google/google.generate-content.error.synthetic.json",
	},
	engines.CredentialPerplexity: {
		authorized: bearer,
		routes: map[string]string{
			"POST /chat/completions": "perplexity/perplexity.chat-completions.sonar.synthetic.json",
		},
		deniedStatus: http.StatusUnauthorized,
		denied:       `inline:{"error":{"message":"Invalid API key provided.","type":"invalid_api_key","code":401}}`,
	},
	engines.CredentialGateway: {
		authorized: bearer,
		routes: map[string]string{
			"POST /chat/completions": "gateway/gateway.chat-completions.annotations.synthetic.json",
			"GET /models":            "gateway/gateway.models.synthetic.json",
		},
		deniedStatus: http.StatusUnauthorized,
		denied:       "gateway/gateway.chat-completions.error.synthetic.json",
	},
	engines.CredentialDataForSEO: {
		authorized: func(r *http.Request) bool {
			login, password, ok := r.BasicAuth()
			return ok && login == dataforseoLogin && password == dataforseoPassword
		},
		routes:       nativeRoutes("dataforseo"),
		deniedStatus: http.StatusUnauthorized,
		denied:       `inline:{"version":"0.1.20260101","status_code":40100,"status_message":"You are not authorized to access this resource.","time":"0 sec.","cost":0,"tasks_count":0,"tasks_error":0,"tasks":null}`,
	},
	engines.CredentialCloro: {
		authorized:   bearer,
		routes:       nativeRoutes("cloro"),
		deniedStatus: http.StatusUnauthorized,
		denied:       `inline:{"success":false,"error":{"code":"UNAUTHORIZED","message":"Invalid or missing API key","timestamp":"2026-11-21T00:00:00Z"}}`,
	},
	engines.CredentialSerpApi: {
		authorized: func(r *http.Request) bool { return r.URL.Query().Get("api_key") == key },
		routes: map[string]string{
			"GET /search.json?engine=bing_copilot":  tregtest.Fixtures["serpapi.x.bing-copilot"],
			"GET /search.json?engine=brave_ai_mode": tregtest.Fixtures["serpapi.x.brave-ai-mode"],
			"GET /account.json":                     `inline:{"account_email":"team@northwind.example","plan_searches_left":250,"total_searches_left":250}`,
		},
		deniedStatus: http.StatusUnauthorized,
		denied:       `inline:{"error":"Invalid API key. Your API key should be here: https://serpapi.com/manage-api-key"}`,
	},
}

// nativeRoutes serves each pinned endpoint of family at its native path
// with the fixture tregtest serves for it, and its model list likewise.
func nativeRoutes(family string) map[string]string {
	routes := map[string]string{}
	for _, s := range engines.All() {
		e := s.Endpoint
		if e.Family != family {
			continue
		}
		routes[e.Method+" "+e.Native] = tregtest.Fixtures[e.ID]
		if e.NativeModels != "" {
			routes["GET "+e.NativeModels] = tregtest.Fixtures[e.Models]
		}
	}
	return routes
}

func bearer(r *http.Request) bool { return r.Header.Get("Authorization") == "Bearer "+key }

// New starts the fake, which closes when the test ends.
func New(t testing.TB) *Server {
	t.Helper()
	s := &Server{routes: map[string]response{}, denied: map[engines.CredentialKind][]byte{}}
	for kind, p := range providers {
		b, err := load(p.denied)
		if err != nil {
			t.Fatalf("providertest: %s: %v", kind, err)
		}
		s.denied[kind] = b
		for route, file := range p.routes {
			b, err := load(file)
			if err != nil {
				t.Fatalf("providertest: %s %s: %v", kind, route, err)
			}
			s.routes[string(kind)+" "+route] = response{http.StatusOK, b}
		}
	}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.Close)
	return s
}

func load(src string) ([]byte, error) {
	if body, ok := strings.CutPrefix(src, "inline:"); ok {
		return []byte(body), nil
	}
	return os.ReadFile(filepath.Join(testdata(), strings.TrimPrefix(src, "")))
}

func testdata() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "testdata")
}

// BaseURL is kind's Conn.BaseURL on this server.
func (s *Server) BaseURL(kind engines.CredentialKind) string { return s.URL + "/" + string(kind) }

// Conn is a Conn that calls kind on this server with the key it accepts.
func (s *Server) Conn(kind engines.CredentialKind) engines.Conn {
	return engines.Conn{Secret: Secret(kind), BaseURL: s.BaseURL(kind), Client: s.Client()}
}

// Respond replaces what kind answers to method and path, such as
// Respond(engines.CredentialOpenAI, "POST", "/v1/responses", 500, body).
// A Google generateContent path is matched as /v1beta/models/*:generateContent
// and a SerpApi search as /search.json?engine=<engine>.
func (s *Server) Respond(kind engines.CredentialKind, method, path string, status int, body []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.routes[string(kind)+" "+method+" "+path] = response{status, body}
}

// Calls returns every request received so far, in order.
func (s *Server) Calls() []Call {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Call(nil), s.calls...)
}

// Reset forgets the recorded calls.
func (s *Server) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = nil
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	prefix, rest, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
	kind := engines.CredentialKind(prefix)
	p, known := providers[kind]
	body, _ := io.ReadAll(r.Body)
	c := Call{Kind: kind, Path: "/" + rest, Method: r.Method, Query: r.URL.Query(), Header: r.Header.Clone(), Body: body}
	c.Authorized = known && p.authorized(r)
	s.mu.Lock()
	s.calls = append(s.calls, c)
	resp, found := s.routes[string(kind)+" "+routeKey(kind, r.Method, c.Path, c.Query)]
	s.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	switch {
	case !known || !found:
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprintf(w, `{"error":{"message":"providertest: no route for %s %s %s"}}`, kind, r.Method, c.Path)
	case !c.Authorized:
		b, err := load(p.denied)
		if err != nil && strings.HasPrefix(p.denied, "") {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err != nil || !strings.HasPrefix(p.denied, "") {
			b = []byte(p.denied)
		}
		w.WriteHeader(p.deniedStatus)
		_, _ = w.Write(b)
	default:
		w.WriteHeader(resp.status)
		_, _ = w.Write(resp.body)
	}
}

// routeKey is the route a request matches: its method and path, with
// Google's model and SerpApi's engine folded in the way providers lists
// them.
func routeKey(kind engines.CredentialKind, method, path string, q url.Values) string {
	switch kind {
	case engines.CredentialGoogle:
		if m, ok := strings.CutPrefix(path, "/v1beta/models/"); ok && strings.HasSuffix(m, ":generateContent") {
			path = "/v1beta/models/*:generateContent"
		}
	case engines.CredentialSerpApi:
		if path == "/search.json" {
			path += "?engine=" + q.Get("engine")
		}
	}
	return method + " " + path
}
