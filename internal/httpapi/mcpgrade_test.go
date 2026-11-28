package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/unkeyed/byname/internal/core"
	"github.com/unkeyed/byname/internal/engines/tregtest"
)

const mcpToken = "mcp_secret_northwind_tokenvalue"

func fakeMCPRegistry(t *testing.T) string {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, `{"servers":[]}`) }))
	t.Cleanup(srv.Close)
	return srv.URL
}

func northwindMCP(t *testing.T) string {
	t.Helper()
	srv := mcp.NewServer(&mcp.Implementation{Name: "northwind", Title: "Northwind", Version: "1.0.0"}, nil)
	mcp.AddTool(srv, &mcp.Tool{Name: "search", Description: "Searches Northwind's documentation and returns the best pages."},
		func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, struct{}, error) {
			t.Error("the grade called a tool")
			return nil, struct{}{}, nil
		})
	h := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, &mcp.StreamableHTTPOptions{Stateless: true})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+mcpToken {
			w.Header().Set("WWW-Authenticate", `Bearer realm="northwind"`)
			http.Error(w, "sign in", http.StatusUnauthorized)
			return
		}
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(ts.Close)
	return ts.URL + "/mcp"
}

type mcpServerView struct {
	URL      string `json:"url"`
	TokenSet bool   `json:"token_set"`
	Latest   *struct {
		ID      int64  `json:"id"`
		URL     string `json:"url"`
		Score   int    `json:"score"`
		Grade   string `json:"grade"`
		Partial string `json:"partial"`
		Server  struct {
			TokenUsed bool `json:"token_used"`
		} `json:"server"`
		Categories []struct {
			ID     string            `json:"id"`
			Checks []json.RawMessage `json:"checks"`
		} `json:"categories"`
		Tools []json.RawMessage `json:"tools"`
	} `json:"latest"`
	History []struct {
		ID    int64 `json:"id"`
		Score int   `json:"score"`
	} `json:"history"`
	Review struct {
		Status        string   `json:"status"`
		Error         string   `json:"error"`
		HasToken      bool     `json:"has_token"`
		Reviewed      int      `json:"reviewed"`
		Tools         int      `json:"tools"`
		Score         *int     `json:"score"`
		CostUSD       *float64 `json:"cost_usd"`
		EstimateUSD   float64  `json:"estimate_usd"`
		EstimateTools int      `json:"estimate_tools"`
	} `json:"review"`
}

func (v mcpServerView) toolReview(t *testing.T, i int) *struct {
	Score int `json:"score"`
	Title struct {
		Score  int    `json:"score"`
		Reason string `json:"reason"`
	} `json:"title"`
} {
	t.Helper()
	var tool struct {
		Review *struct {
			Score int `json:"score"`
			Title struct {
				Score  int    `json:"score"`
				Reason string `json:"reason"`
			} `json:"title"`
		} `json:"review"`
	}
	if err := json.Unmarshal(v.Latest.Tools[i], &tool); err != nil {
		t.Fatal(err)
	}
	return tool.Review
}

const reviewReply = `{"title":{"score":20,"reason":"Add a title."},"description":{"score":80,"reason":"Clear."},"inputs":{"score":100,"reason":"No inputs."}}`

func TestMCPServerGradeOverHTTP(t *testing.T) {
	c := newServer(t)
	person, agent := login(t, c)
	reader := c.withKey(t, person, core.ResultsRead)
	join := func(email string, role core.Role) client {
		var inv struct{ Token string }
		person.call("POST", "/v1/invites", map[string]string{"email": email, "role": string(role)}, http.StatusCreated, &inv)
		return c.signup(map[string]string{"invite_token": inv.Token, "name": email})
	}
	editor, viewer := join("ed@example.com", core.Editor), join("vi@example.com", core.Viewer)
	var bodies []string
	keep := func(r reply) reply { bodies = append(bodies, string(r.body)); return r }
	endpoint := northwindMCP(t)

	var v mcpServerView
	r := keep(viewer.call("GET", "/v1/mcp-server", nil, http.StatusOK, &v))
	if v.URL != "" || v.TokenSet || v.Latest != nil || v.History == nil || !strings.Contains(string(r.body), `"history":[]`) {
		t.Fatalf("before a URL: %s", r.body)
	}
	agent.call("POST", "/v1/mcp-server/grade", nil, http.StatusBadRequest, nil)
	viewer.call("PUT", "/v1/mcp-server", map[string]string{"url": endpoint}, http.StatusForbidden, nil)
	reader.call("PUT", "/v1/mcp-server", map[string]string{"url": endpoint}, http.StatusForbidden, nil)
	agent.call("PUT", "/v1/mcp-server", map[string]string{"url": "http://mcp.northwind.example/mcp"}, http.StatusBadRequest, nil)
	keep(agent.call("PUT", "/v1/mcp-server", map[string]string{"url": endpoint}, http.StatusOK, &v))
	if v.URL != endpoint {
		t.Fatalf("url = %q, want %q", v.URL, endpoint)
	}

	for _, as := range []client{agent, editor, viewer} {
		if r := as.do("PUT", "/v1/mcp-server/token", map[string]string{"token": mcpToken}); r.code != http.StatusForbidden {
			t.Errorf("PUT /v1/mcp-server/token by a non-admin = %d %s, want 403", r.code, r.body)
		}
	}
	reader.call("POST", "/v1/mcp-server/grade", nil, http.StatusForbidden, nil)
	viewer.call("POST", "/v1/mcp-server/grade", nil, http.StatusForbidden, nil)

	keep(agent.call("POST", "/v1/mcp-server/grade", nil, http.StatusOK, &v))
	if v.Latest == nil || v.Latest.Partial != "needs_token" || v.Latest.Server.TokenUsed || v.Latest.URL != endpoint || len(v.Latest.Categories) != 6 {
		t.Fatalf("a grade without the token = %+v", v.Latest)
	}
	partial := v.Latest.ID

	keep(person.call("PUT", "/v1/mcp-server/token", map[string]string{"token": mcpToken}, http.StatusOK, &v))
	if !v.TokenSet {
		t.Fatal("token_set is false after an admin set it")
	}
	keep(editor.call("POST", "/v1/mcp-server/grade", nil, http.StatusOK, &v))
	if v.Latest.Partial != "" || !v.Latest.Server.TokenUsed || len(v.Latest.Tools) != 1 {
		t.Fatalf("a grade with the token = %+v", v.Latest)
	}
	if len(v.History) != 2 || v.History[0].ID != v.Latest.ID || v.History[1].ID != partial || v.History[0].Score <= v.History[1].Score {
		t.Fatalf("history = %+v, want the full grade above the partial one", v.History)
	}

	if _, err := c.db.Exec(context.Background(), `INSERT INTO mcp_grade (url, score, grade, rules, report)
		SELECT 'https://old.example/mcp', 10, 'F', '2026-11', '{}' FROM generate_series(1, 55)`); err != nil {
		t.Fatal(err)
	}
	keep(agent.call("POST", "/v1/mcp-server/grade", nil, http.StatusOK, &v))
	if len(v.History) != 50 || v.History[0].ID != v.Latest.ID || v.History[1].Score != 10 {
		t.Fatalf("after 58 grades, history has %d starting %+v, want the newest 50", len(v.History), v.History[:2])
	}
	var stored int
	if err := c.db.QueryRow(context.Background(), `SELECT count(*) FROM mcp_grade`).Scan(&stored); err != nil || stored != 50 {
		t.Fatalf("%d grades stored, %v; want the newest 50", stored, err)
	}

	keep(reader.call("GET", "/v1/mcp-server", nil, http.StatusOK, nil))
	u, _ := url.Parse(endpoint)
	keep(agent.call("PUT", "/v1/mcp-server", map[string]string{"url": "http://localhost:" + u.Port() + "/mcp"}, http.StatusOK, &v))
	if v.TokenSet {
		t.Error("moving the URL to another host kept the token")
	}
	keep(agent.call("PUT", "/v1/mcp-server", map[string]string{"url": ""}, http.StatusOK, &v))
	if v.URL != "" || v.Latest == nil {
		t.Errorf("removing the server = %+v, want no URL and the grades kept", v)
	}
	for _, b := range bodies {
		if strings.Contains(b, mcpToken) {
			t.Fatalf("an API response carries the MCP server token: %s", b)
		}
	}
}

// A paid review of tool docs: runs:write queues it once a Treg token is
// stored, the scheduler runs it, and its spend shows in Check history and
// the month's costs.
func TestMCPToolReviewOverHTTP(t *testing.T) {
	c := newServer(t)
	person, agent := login(t, c)
	reader := c.withKey(t, person, core.ResultsRead)
	c.treg.Reply(func(tregtest.Call) []byte { return tregtest.LLMBody(reviewReply) })
	agent.call("POST", "/v1/mcp-server/review", nil, http.StatusBadRequest, nil)
	agent.call("PUT", "/v1/mcp-server", map[string]string{"url": northwindMCP(t)}, http.StatusOK, nil)
	person.call("PUT", "/v1/mcp-server/token", map[string]string{"token": mcpToken}, http.StatusOK, nil)
	var v mcpServerView
	r := agent.call("POST", "/v1/mcp-server/grade", nil, http.StatusOK, &v)
	if v.Review.Status != "none" || v.Review.HasToken || v.Review.Tools != 1 || v.Review.EstimateTools != 1 || !strings.Contains(string(r.body), `"review":null`) {
		t.Fatalf("before a review: %+v in %s", v.Review, r.body)
	}
	if r := agent.do("POST", "/v1/mcp-server/review", nil); r.code != http.StatusBadRequest || !strings.Contains(string(r.body), "Treg token") {
		t.Fatalf("a review without a Treg token = %d %s", r.code, r.body)
	}
	person.call("PUT", "/v1/credentials/treg", map[string]string{"secret": tregSecret}, http.StatusOK, nil)
	reader.call("POST", "/v1/mcp-server/review", nil, http.StatusForbidden, nil)
	agent.call("POST", "/v1/mcp-server/review", nil, http.StatusOK, &v)
	if v.Review.Status != "queued" || !v.Review.HasToken {
		t.Fatalf("POST /v1/mcp-server/review = %+v", v.Review)
	}
	if err := c.svc.MCPGrade.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	reader.call("GET", "/v1/mcp-server", nil, http.StatusOK, &v)
	tr := v.toolReview(t, 0)
	if v.Review.Status != "completed" || v.Review.Reviewed != 1 || v.Review.Score == nil || *v.Review.Score != 76 || tr == nil || tr.Score != 76 || tr.Title.Reason != "Add a title." {
		t.Fatalf("after the review: %+v, tool review %+v", v.Review, tr)
	}
	var jobs []struct {
		Job      string   `json:"job"`
		SpendUSD *float64 `json:"spend_usd"`
	}
	reader.call("GET", "/v1/jobs", nil, http.StatusOK, &jobs)
	if len(jobs) == 0 || jobs[0].Job != "mcp_review" || jobs[0].SpendUSD == nil || *jobs[0].SpendUSD != *v.Review.CostUSD {
		t.Errorf("Check history = %+v, want the review with what it spent, %v", jobs, *v.Review.CostUSD)
	}
	var costs struct {
		Month struct {
			MCPReviewUSD float64 `json:"mcp_review_usd"`
			SpentUSD     float64 `json:"spent_usd"`
		} `json:"month"`
	}
	reader.call("GET", "/v1/costs", nil, http.StatusOK, &costs)
	if costs.Month.MCPReviewUSD != *v.Review.CostUSD || costs.Month.SpentUSD < costs.Month.MCPReviewUSD {
		t.Errorf("the month's costs = %+v, want the review's %v counted", costs.Month, *v.Review.CostUSD)
	}
}

// TestMCPServerToolsOverMCP calls each grade tool over a real OAuth
// connection, which validates every output, a full report included,
// against its schema.
func TestMCPServerToolsOverMCP(t *testing.T) {
	anon, person := newLocalServer(t)
	endpoint := northwindMCP(t)
	id := register(t, anon, "grader", loopbackRedirect)
	p := authorizeQuery(anon, id, loopbackRedirect)
	p.query.Set("scope", "results:read config:write runs:write")
	got := decide(t, person, consentHandle(t, anon, "/oauth/authorize?"+p.query.Encode()), true, core.ResultsRead, core.ConfigWrite, core.RunsWrite)
	tok, _ := tokenRequest(t, anon, url.Values{
		"grant_type": {"authorization_code"}, "client_id": {id}, "code": {got.Get("code")},
		"redirect_uri": {loopbackRedirect}, "code_verifier": {p.verifier},
	}, http.StatusOK)
	cs, err := mcpSession(anon, tok.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	call := func(name string, args map[string]any) mcpServerView {
		t.Helper()
		res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil || res.IsError {
			t.Fatalf("%s: %v %+v", name, err, res)
		}
		raw, _ := json.Marshal(res.StructuredContent)
		var v mcpServerView
		if err := json.Unmarshal(raw, &v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	if v := call("mcp_server_grade", nil); v.Latest != nil {
		t.Fatalf("mcp_server_grade before any grade = %+v", v)
	}
	if v := call("set_mcp_server", map[string]any{"url": endpoint}); v.URL != endpoint {
		t.Fatalf("set_mcp_server = %+v", v)
	}
	person.call("PUT", "/v1/mcp-server/token", map[string]string{"token": mcpToken}, http.StatusOK, nil)
	if v := call("grade_mcp_server", nil); v.Latest == nil || v.Latest.Partial != "" || len(v.Latest.Tools) != 1 {
		t.Fatalf("grade_mcp_server = %+v", v.Latest)
	}
	person.call("PUT", "/v1/mcp-server/token", map[string]string{"token": ""}, http.StatusOK, nil)
	if v := call("grade_mcp_server", nil); v.Latest == nil || v.Latest.Partial != "needs_token" || len(v.History) != 2 {
		t.Fatalf("grade_mcp_server without the token = %+v", v.Latest)
	}
	if v := call("mcp_server_grade", nil); v.TokenSet || len(v.History) != 2 {
		t.Fatalf("mcp_server_grade = %+v", v)
	}

	person.call("PUT", "/v1/mcp-server/token", map[string]string{"token": mcpToken}, http.StatusOK, nil)
	call("grade_mcp_server", nil)
	if res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "review_mcp_tools"}); err != nil || !res.IsError {
		t.Fatalf("review_mcp_tools without a Treg token = %v, %+v; want a tool error", err, res)
	}
	person.call("PUT", "/v1/credentials/treg", map[string]string{"secret": tregSecret}, http.StatusOK, nil)
	anon.treg.Reply(func(tregtest.Call) []byte { return tregtest.LLMBody(reviewReply) })
	if v := call("review_mcp_tools", nil); v.Review.Status != "queued" {
		t.Fatalf("review_mcp_tools = %+v", v.Review)
	}
	if err := anon.svc.MCPGrade.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if v := call("mcp_server_grade", nil); v.Review.Status != "completed" || v.toolReview(t, 0) == nil {
		t.Fatalf("mcp_server_grade after the review = %+v", v.Review)
	}
}
