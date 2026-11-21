package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/unkeyed/byname/internal/checks/checkstest"
	"github.com/unkeyed/byname/internal/core"
)

const tregSecret = "treg_secret_never_returned"

// configure sets an instance up through REST the way the UI will: the
// token as a signed-in admin, everything else with config:write.
func configure(t *testing.T, person, editor client) {
	t.Helper()
	person.call("PUT", "/v1/credentials/treg", map[string]string{"token": tregSecret}, http.StatusOK, nil)
	editor.call("PUT", "/v1/settings", checkstest.Settings(), http.StatusOK, nil)
	editor.call("POST", "/v1/brands", map[string]any{"name": "Contoso", "aliases": []string{"Contoso Gateway"}, "kind": "competitor"}, http.StatusOK, nil)
	editor.call("POST", "/v1/brands", map[string]any{"name": "Redis", "kind": "technology"}, http.StatusOK, nil)
	editor.call("PUT", "/v1/questions", map[string]any{"questions": checkstest.Questions}, http.StatusOK, nil)
	editor.call("PUT", "/v1/surfaces/claude_code", map[string]any{"enabled": true, "models": []map[string]string{{"model": "claude-sonnet-5-5"}}}, http.StatusOK, nil)
	editor.call("PUT", "/v1/surfaces/google_ai_overview", map[string]any{"enabled": true}, http.StatusOK, nil)
}

func (c client) withKey(t *testing.T, person client, scopes ...core.Scope) client {
	t.Helper()
	var key struct {
		Key string `json:"key"`
	}
	person.call("POST", "/v1/keys", map[string]any{"name": fmt.Sprint(scopes), "scopes": scopes}, http.StatusCreated, &key)
	c.key = key.Key
	return c
}

func TestConfigureAndReadAChecksResults(t *testing.T) {
	c := newServer(t)
	person, agent := login(t, c)
	agent.call("PUT", "/v1/credentials/treg", map[string]string{"token": "x"}, http.StatusForbidden, nil)
	agent.call("POST", "/v1/runs", nil, http.StatusBadRequest, nil)
	configure(t, person, agent)

	var bodies []string
	get := func(path string, out any) {
		t.Helper()
		r := agent.call("GET", path, nil, http.StatusOK, out)
		bodies = append(bodies, string(r.body))
	}

	var run struct {
		ID     int64  `json:"id"`
		Status string `json:"status"`
	}
	agent.call("POST", "/v1/runs", nil, http.StatusCreated, &run)
	var again struct{ ID int64 }
	agent.call("POST", "/v1/runs", nil, http.StatusOK, &again)
	if run.Status != "queued" || again.ID != run.ID {
		t.Fatalf("run now = %+v then %+v, want one queued run returned twice", run, again)
	}
	if err := c.svc.Checks.Work(context.Background()); err != nil {
		t.Fatal(err)
	}

	var runs []struct {
		ID                int64   `json:"id"`
		Status            string  `json:"status"`
		Calls             int     `json:"calls"`
		Mentioned         int     `json:"mentioned"`
		SpendUSD          float64 `json:"spend_usd"`
		SpendMissingCalls int     `json:"spend_missing_calls"`
		Endpoints         []struct {
			Endpoint    string  `json:"endpoint"`
			SuccessRate float64 `json:"success_rate"`
		} `json:"endpoints"`
	}
	get("/v1/runs", &runs)
	if len(runs) != 1 || runs[0].Status != "completed" || runs[0].Calls != 4 || runs[0].Mentioned != 4 || len(runs[0].Endpoints) != 2 {
		t.Fatalf("runs = %+v, want one completed run of 2 questions x 2 pairs, all naming Northwind", runs)
	}
	var answers []struct {
		ID       int64  `json:"id"`
		Endpoint string `json:"endpoint"`
		Fidelity string `json:"fidelity"`
		Mentions []struct {
			Name string `json:"name"`
		} `json:"mentions"`
	}
	get(fmt.Sprintf("/v1/runs/%d/answers", run.ID), &answers)
	if len(answers) != 4 || len(answers[0].Mentions) == 0 {
		t.Fatalf("answers = %+v", answers)
	}
	var answer struct {
		Text string          `json:"text"`
		Raw  json.RawMessage `json:"raw"`
	}
	get(fmt.Sprintf("/v1/answers/%d", answers[0].ID), &answer)
	if answer.Text == "" || len(answer.Raw) < 2 || answer.Raw[0] != '{' {
		t.Fatalf("answer = %+v, want its text and the raw JSON object", answer)
	}
	var compare struct {
		Counts map[string]int `json:"counts"`
		Net    int            `json:"net"`
	}
	get(fmt.Sprintf("/v1/compare?before=%d&after=%d", run.ID, run.ID), &compare)
	if compare.Counts["unchanged"] != 4 || compare.Net != 0 {
		t.Fatalf("compare a run with itself = %+v", compare)
	}
	var questions struct {
		Questions []struct{ ID int64 } `json:"questions"`
	}
	get("/v1/questions", &questions)
	for _, path := range []string{
		"/v1/settings", "/v1/brands", "/v1/surfaces", "/v1/surfaces/claude_code/models", "/v1/question-sets",
		fmt.Sprintf("/v1/runs/%d", run.ID),
		fmt.Sprintf("/v1/questions/%d/history?since=2026-01-01", questions.Questions[0].ID),
		"/v1/metrics/rates", "/v1/metrics/share?since=2026-01-01T00:00:00Z", "/v1/gaps", "/v1/jobs?limit=5",
		"/v1/metrics/rates?by=surface&surface=claude_code", "/v1/metrics/summary?model=claude-sonnet-5-5",
		"/v1/metrics/competitors?by=surface", fmt.Sprintf("/v1/runs/%d/questions?surface=claude_code", run.ID),
	} {
		get(path, nil)
	}
	var creds []map[string]any
	r := person.call("GET", "/v1/credentials", nil, http.StatusOK, &creds)
	bodies = append(bodies, string(r.body))
	if len(creds) != 1 || creds[0]["set"] != true {
		t.Fatalf("credentials = %v", creds)
	}
	for _, b := range bodies {
		if strings.Contains(b, tregSecret) || strings.Contains(b, "sealed") {
			t.Fatalf("a response carries the Treg token: %s", b)
		}
	}

	for _, tc := range []struct{ method, path, want string }{
		{"GET", "/v1/compare?before=1", "before"},
		{"GET", "/v1/metrics/rates?since=yesterday", "since"},
		{"GET", "/v1/metrics/rates?by=engine", "by"},
		{"GET", "/v1/metrics/competitors?surface=myspace", "surface"},
		{"GET", "/v1/runs?limit=500", "limit"},
		{"PUT", "/v1/surfaces/claude_code", "model list"},
	} {
		var body any
		if tc.method == "PUT" {
			body = map[string]any{"enabled": true, "models": []map[string]string{{"model": "claude-opus"}}}
		}
		r := agent.do(tc.method, tc.path, body)
		if r.code != http.StatusBadRequest || !strings.Contains(string(r.body), tc.want) {
			t.Errorf("%s %s = %d %s, want 400 naming %s", tc.method, tc.path, r.code, r.body, tc.want)
		}
	}
	agent.call("GET", "/v1/runs/999", nil, http.StatusNotFound, nil)
	agent.call("GET", "/v1/surfaces/myspace/models", nil, http.StatusNotFound, nil)
}

// TestResultsReadKeyCannotRunNow is Phase 5's acceptance check: a key with
// only results:read reads results and cannot queue a check or change
// anything.
func TestResultsReadKeyCannotRunNow(t *testing.T) {
	c := newServer(t)
	person, agent := login(t, c)
	configure(t, person, agent)
	reader := c.withKey(t, person, core.ResultsRead)

	r := reader.do("POST", "/v1/runs", nil)
	if r.code != http.StatusForbidden || !strings.Contains(string(r.body), "runs:write") {
		t.Fatalf("POST /v1/runs with results:read = %d %s, want 403 naming runs:write", r.code, r.body)
	}
	reader.call("POST", "/v1/checks/pause", nil, http.StatusForbidden, nil)
	reader.call("PUT", "/v1/settings", checkstest.Settings(), http.StatusForbidden, nil)
	reader.call("POST", "/v1/brands", map[string]string{"name": "Fabrikam"}, http.StatusForbidden, nil)
	reader.call("GET", "/v1/runs", nil, http.StatusOK, nil)
	reader.call("GET", "/v1/settings", nil, http.StatusOK, nil)

	runner := c.withKey(t, person, core.RunsWrite)
	runner.call("POST", "/v1/runs", nil, http.StatusCreated, nil)
	runner.call("GET", "/v1/runs", nil, http.StatusForbidden, nil)
}

// TestMCPToolsServeTheSameResults calls each read tool over a real OAuth
// connection, which validates every tool's output against its schema.
func TestMCPToolsServeTheSameResults(t *testing.T) {
	anon, person := newLocalServer(t)
	editor := anon.withKey(t, person, core.ConfigWrite, core.RunsWrite)
	configure(t, person, editor)
	var run struct{ ID int64 }
	editor.call("POST", "/v1/runs", nil, http.StatusCreated, &run)
	if err := anon.svc.Checks.Work(context.Background()); err != nil {
		t.Fatal(err)
	}
	var answers []struct{ ID int64 }
	person.call("GET", fmt.Sprintf("/v1/runs/%d/answers", run.ID), nil, http.StatusOK, &answers)
	var questions struct {
		Questions []struct{ ID int64 } `json:"questions"`
	}
	person.call("GET", "/v1/questions", nil, http.StatusOK, &questions)

	_, tok := connect(t, anon, person, "results reader")
	cs, err := mcpSession(anon, tok.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	for name, args := range map[string]map[string]any{
		"get_settings": nil, "list_brands": nil, "get_questions": nil, "list_question_sets": nil,
		"get_question_set": {"id": 1}, "list_surfaces": nil, "list_surface_models": {"surface": "claude_code"},
		"list_runs": nil, "get_run": {"id": run.ID}, "list_run_answers": {"id": run.ID}, "get_answer": {"id": answers[0].ID},
		"compare_runs": {"before": run.ID, "after": run.ID}, "question_history": {"question_id": questions.Questions[0].ID},
		"mention_rates": {"since": "2026-01-01T00:00:00Z", "by": "surface"}, "share_of_mentions": {"surface": "claude_code"}, "gaps": nil, "list_jobs": {"limit": 5},
		"run_questions": {"id": run.ID}, "results_summary": nil, "competitors": {"by": "surface"},
	} {
		res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil || res.IsError {
			t.Errorf("%s: %v %+v", name, err, res)
		}
	}
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "run_now"})
	if err != nil || !res.IsError {
		t.Fatalf("run_now with results:read: %v %+v, want a tool error", err, res)
	}
}
