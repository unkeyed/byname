// Package tregtest is a fake Treg for tests outside internal/engines. It
// answers /call/<tool id> with the captured or synthetic fixture for that
// tool from internal/engines/testdata, sets X-Treg-Cost-Micro the way Treg
// does, and replays a repeated Idempotency-Key the way Treg does.
package tregtest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// Fixtures is the body served per tool id, as a file under
// internal/engines/testdata. Synthetic fixtures are chosen where the
// captured one names no brand, so answers have something to score.
var Fixtures = map[string]string{
	"dataforseo.x.ai-optimization-chat-gpt-llm-scraper-live-advanced": "dataforseo/dataforseo.x.ai-optimization-chat-gpt-llm-scraper-live-advanced.json",
	"dataforseo.x.ai-optimization-claude-llm-responses-live":          "dataforseo/dataforseo.x.ai-optimization-claude-llm-responses-live.forced-search.synthetic.json",
	"dataforseo.x.ai-optimization-chat-gpt-llm-responses-live":        "dataforseo/dataforseo.x.ai-optimization-chat-gpt-llm-responses-live.reasoning.synthetic.json",
	"dataforseo.x.ai-optimization-gemini-llm-responses-live":          "dataforseo/dataforseo.x.ai-optimization-gemini-llm-responses-live.reasoning-redirects.synthetic.json",
	"dataforseo.x.ai-optimization-perplexity-llm-responses-live":      "dataforseo/dataforseo.x.ai-optimization-perplexity-llm-responses-live.json",
	"dataforseo.x.ai-optimization-claude-llm-responses-models":        "dataforseo/dataforseo.x.ai-optimization-claude-llm-responses-models.json",
	"dataforseo.x.ai-optimization-chat-gpt-llm-responses-models":      "dataforseo/dataforseo.x.ai-optimization-chat-gpt-llm-responses-models.json",
	"dataforseo.x.ai-optimization-gemini-llm-responses-models":        "dataforseo/dataforseo.x.ai-optimization-gemini-llm-responses-models.json",
	"dataforseo.x.ai-optimization-perplexity-llm-responses-models":    "dataforseo/dataforseo.x.ai-optimization-perplexity-llm-responses-models.json",
	"cloro.ai-search.gemini.scrape":                                   "cloro/cloro.ai-search.gemini.scrape.json",
	"cloro.google.serp.ai_mode":                                       "cloro/cloro.google.serp.ai_mode.json",
	"cloro.google.serp.organic":                                       "cloro/cloro.google.serp.organic.aioverview.synthetic.json",
	"cloro.bing.serp.ai_mode":                                         "cloro/cloro.bing.serp.ai_mode.json",
	"serpapi.x.bing-copilot":                                          "serpapi/serpapi.x.bing-copilot.json",
	"serpapi.x.brave-ai-mode":                                         "serpapi/serpapi.x.brave-ai-mode.json",
}

// DefaultCostMicro is what every answer call costs unless SetCost says
// otherwise. Model lists are free, as on Treg.
const DefaultCostMicro = 10_000

// Call is one request the fake received.
type Call struct {
	Tool           string
	Token          string
	IdempotencyKey string
	Replay         bool
}

// Refusal makes the fake answer as Treg refusing on its own behalf.
type Refusal struct {
	Status int
	Code   string
}

type Server struct {
	*httptest.Server

	mu     sync.Mutex
	calls  []Call
	cost   map[string]*int64
	paid   map[string]int64
	bodies map[string][]byte
	// Before runs for each call before it is answered. A non-nil Refusal
	// refuses it.
	before func(Call) *Refusal
}

// New starts a fake Treg that closes when the test ends.
func New(t testing.TB) *Server {
	t.Helper()
	s := &Server{cost: map[string]*int64{}, paid: map[string]int64{}, bodies: map[string][]byte{}}
	for tool, file := range Fixtures {
		b, err := os.ReadFile(filepath.Join(testdata(), file))
		if err != nil {
			t.Fatalf("tregtest: %v", err)
		}
		s.bodies[tool] = b
	}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.Close)
	return s
}

func testdata() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "testdata")
}

// SetCost sets one tool's X-Treg-Cost-Micro. A nil cost sends no header,
// as for a call that ran on the team's own key.
func (s *Server) SetCost(tool string, micro *int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if micro == nil {
		s.cost[tool] = new(int64(-1))
		return
	}
	s.cost[tool] = micro
}

// SetBody replaces the body one tool answers with.
func (s *Server) SetBody(tool string, body []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bodies[tool] = body
}

// Before installs a hook that runs before each call is answered.
func (s *Server) Before(f func(Call) *Refusal) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.before = f
}

// Calls returns every call received so far, in order.
func (s *Server) Calls() []Call {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Call(nil), s.calls...)
}

// Reset forgets the calls received so far, so a test can count only the
// calls it makes after setting up.
func (s *Server) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = nil
}

// Answers counts the calls that were not model lists or replays.
func (s *Server) Answers() int {
	n := 0
	for _, c := range s.Calls() {
		if !strings.HasSuffix(c.Tool, "-models") && !c.Replay {
			n++
		}
	}
	return n
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	tool, ok := strings.CutPrefix(r.URL.Path, "/call/")
	c := Call{Tool: tool, Token: r.Header.Get("X-Treg-Token"), IdempotencyKey: r.Header.Get("Idempotency-Key")}
	s.mu.Lock()
	body, known := s.bodies[tool]
	before := s.before
	price, priced := s.cost[tool]
	original, replay := s.paid[c.IdempotencyKey]
	c.Replay = replay && c.IdempotencyKey != ""
	s.calls = append(s.calls, c)
	s.mu.Unlock()

	if !ok || !known {
		refuse(w, http.StatusNotFound, "unknown_tool")
		return
	}
	if c.Token == "" {
		refuse(w, http.StatusUnauthorized, "missing_token")
		return
	}
	if before != nil {
		if rf := before(c); rf != nil {
			refuse(w, rf.Status, rf.Code)
			return
		}
	}
	cost := int64(DefaultCostMicro)
	switch {
	case priced:
		cost = *price
	case strings.HasSuffix(tool, "-models"):
		cost = 0
	}
	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("X-Treg-Call-Id", fmt.Sprintf("call_%d", len(s.Calls())))
	switch {
	case c.Replay:
		h.Set("X-Treg-Idempotent-Replay", "true")
		h.Set("X-Treg-Cost-Micro", "0")
		if original >= 0 {
			h.Set("X-Treg-Original-Cost-Micro", fmt.Sprint(original))
		}
	case cost >= 0:
		h.Set("X-Treg-Cost-Micro", fmt.Sprint(cost))
	}
	if c.IdempotencyKey != "" && !c.Replay {
		s.mu.Lock()
		s.paid[c.IdempotencyKey] = cost
		s.mu.Unlock()
	}
	_, _ = w.Write(body)
}

func refuse(w http.ResponseWriter, status int, code string) {
	w.Header().Set("X-Treg-Error", "1")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code, "message": "refused by the fake Treg"})
}
