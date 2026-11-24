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

// configure sets an instance up through REST the way onboarding does: the
// token as a signed-in admin, everything else with config:write, then a
// test of every pair and Finish. Run now refuses until Finish.
func configure(t *testing.T, person, editor client) {
	t.Helper()
	person.call("PUT", "/v1/credentials/treg", map[string]string{"secret": tregSecret}, http.StatusOK, nil)
	editor.call("PUT", "/v1/settings", checkstest.Settings(), http.StatusOK, nil)
	editor.call("POST", "/v1/brands", map[string]any{"name": "Contoso", "aliases": []string{"Contoso Gateway"}, "kind": "competitor"}, http.StatusOK, nil)
	editor.call("POST", "/v1/brands", map[string]any{"name": "Redis", "kind": "technology"}, http.StatusOK, nil)
	editor.call("PUT", "/v1/questions", map[string]any{"questions": checkstest.Questions}, http.StatusOK, nil)
	editor.call("PUT", "/v1/surfaces/claude_code", map[string]any{"enabled": true, "models": []map[string]string{{"model": "claude-sonnet-5-5"}}}, http.StatusOK, nil)
	editor.call("PUT", "/v1/surfaces/google_ai_overview", map[string]any{"enabled": true}, http.StatusOK, nil)
	for _, step := range []string{"brand", "competitors", "questions", "surfaces", "search", "schedule"} {
		editor.call("PUT", "/v1/onboarding/steps/"+step, nil, http.StatusOK, nil)
	}
	if r := editor.do("POST", "/v1/runs", nil); r.code != http.StatusBadRequest && r.code != http.StatusForbidden {
		t.Fatalf("POST /v1/runs before onboarding finished = %d %s, want a refusal", r.code, r.body)
	}
	editor.call("POST", "/v1/onboarding/finish", nil, http.StatusBadRequest, nil)
	var surfaces []struct {
		Enabled bool `json:"enabled"`
		Pairs   []struct {
			ID      int64 `json:"id"`
			Enabled bool  `json:"enabled"`
		} `json:"pairs"`
	}
	person.call("GET", "/v1/surfaces", nil, http.StatusOK, &surfaces)
	for _, s := range surfaces {
		for _, p := range s.Pairs {
			if !s.Enabled || !p.Enabled {
				continue
			}
			var res struct {
				Status string `json:"status"`
				Pair   struct {
					TestStatus string `json:"test_status"`
				} `json:"pair"`
			}
			editor.call("POST", fmt.Sprintf("/v1/pairs/%d/test", p.ID), nil, http.StatusOK, &res)
			if res.Pair.TestStatus != "ok" {
				t.Fatalf("pair %d test = %+v, want ok", p.ID, res)
			}
		}
	}
	var state struct {
		FinishedAt *string  `json:"finished_at"`
		Missing    []string `json:"missing"`
	}
	editor.call("POST", "/v1/onboarding/finish", nil, http.StatusOK, &state)
	if state.FinishedAt == nil || len(state.Missing) != 0 {
		t.Fatalf("finish = %+v", state)
	}
	var status struct{ Onboarded bool }
	person.call("GET", "/v1/status", nil, http.StatusOK, &status)
	if !status.Onboarded {
		t.Fatal("status says not onboarded after finishing")
	}
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
	agent.call("PUT", "/v1/credentials/treg", map[string]string{"secret": "x"}, http.StatusForbidden, nil)
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
		fmt.Sprintf("/v1/questions/%d/brief?surface=claude_code", questions.Questions[0].ID),
		"/v1/metrics/rates", "/v1/metrics/share?since=2026-01-01T00:00:00Z", "/v1/gaps", "/v1/jobs?limit=5",
		"/v1/metrics/rates?by=surface&surface=claude_code", "/v1/metrics/summary?model=claude-sonnet-5-5",
		"/v1/metrics/competitors?by=surface", fmt.Sprintf("/v1/runs/%d/questions?surface=claude_code", run.ID),
		"/v1/prompts?since=2026-01-01", "/v1/sources?surface=claude_code", "/v1/gaps?surface=claude_code",
		"/v1/metrics/engine?surface=claude_code&endpoint=dataforseo.x.ai-optimization-claude-llm-responses-live&fidelity=model_match",
		"/v1/onboarding", "/v1/estimate", "/v1/surfaces", "/v1/settings",
	} {
		get(path, nil)
	}
	var creds []map[string]any
	r := person.call("GET", "/v1/credentials", nil, http.StatusOK, &creds)
	bodies = append(bodies, string(r.body))
	if len(creds) != 9 || creds[0]["kind"] != "treg" || creds[0]["set"] != true || creds[1]["set"] != false {
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
		{"GET", "/v1/metrics/engine?surface=claude_code", "endpoint"},
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
		"content_brief": {"question_id": questions.Questions[0].ID},
		"mention_rates": {"since": "2026-01-01T00:00:00Z", "by": "surface"}, "share_of_mentions": {"surface": "claude_code"}, "gaps": nil, "list_jobs": {"limit": 5},
		"run_questions": {"id": run.ID}, "results_summary": nil, "competitors": {"by": "surface"},
		"prompts": nil, "cited_sources": {"surface": "claude_code"},
		"engine_view": {"surface": "claude_code", "endpoint": "dataforseo.x.ai-optimization-claude-llm-responses-live", "fidelity": "model_match"},
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

// GET /v1/costs prices an answer on every surface, a check, a month of
// checks and of social searches, and what the month has spent against the
// cap that finishing onboarding set.
func TestCostsOverHTTP(t *testing.T) {
	c := newServer(t)
	person, agent := login(t, c)
	reader := c.withKey(t, person, core.ResultsRead)
	configure(t, person, agent)

	type costs struct {
		PerAnswer []struct {
			Surface          string   `json:"surface"`
			DefaultOn        bool     `json:"default_on"`
			Enabled          bool     `json:"enabled"`
			Models           []string `json:"models"`
			CostPerAnswerUSD *float64 `json:"cost_per_answer_usd"`
			Source           string   `json:"source"`
		} `json:"per_answer"`
		Checks struct {
			Check struct {
				KnownUSD float64 `json:"known_usd"`
				Calls    int     `json:"calls"`
			} `json:"check"`
			ChecksPerMonth float64 `json:"checks_per_month"`
			MonthUSD       float64 `json:"month_usd"`
		} `json:"checks"`
		Social struct {
			Enabled         bool    `json:"enabled"`
			TregNetworks    int     `json:"treg_networks"`
			TregMonthUSD    float64 `json:"treg_month_usd"`
			BlueskyOn       bool    `json:"bluesky_on"`
			BlueskyMonthUSD float64 `json:"bluesky_month_usd"`
			MonthUSD        float64 `json:"month_usd"`
		} `json:"social"`
		MonthEstimateUSD   float64 `json:"month_estimate_usd"`
		DefaultMonthCapUSD float64 `json:"default_month_cap_usd"`
		Month              struct {
			ChecksUSD float64  `json:"checks_usd"`
			BrandsUSD float64  `json:"brands_usd"`
			SpentUSD  float64  `json:"spent_usd"`
			CapUSD    *float64 `json:"cap_usd"`
		} `json:"month"`
	}
	var got costs
	reader.call("GET", "/v1/costs", nil, http.StatusOK, &got)
	if len(got.PerAnswer) != 12 {
		t.Fatalf("per answer = %d surfaces, want all 12", len(got.PerAnswer))
	}
	for _, a := range got.PerAnswer {
		wantOn := a.Surface == "chatgpt_app" || a.Surface == "gemini_app" || a.Surface == "google_ai_mode"
		if a.DefaultOn != wantOn || a.CostPerAnswerUSD == nil {
			t.Errorf("%s: %+v, want a price and default_on %v: only the three consumer apps are ticked", a.Surface, a, wantOn)
		}
		if a.Surface == "claude_code" && (!a.Enabled || len(a.Models) != 1 || *a.CostPerAnswerUSD != 0.055 || a.Source != "sample") {
			t.Errorf("claude code: %+v, want its sampled $0.055 an answer on its one model", a)
		}
	}
	if got.Checks.Check.Calls != 4 || got.Checks.ChecksPerMonth != 365.25/12/3 || got.Checks.MonthUSD != got.Checks.Check.KnownUSD*got.Checks.ChecksPerMonth {
		t.Errorf("checks = %+v, want one check of 4 calls about ten times a month", got.Checks)
	}
	if s := got.Social; s.Enabled || s.MonthUSD != 0 || s.TregNetworks != 6 || s.TregMonthUSD <= 0 || !s.BlueskyOn || s.BlueskyMonthUSD != 0 {
		t.Errorf("social = %+v, want off and costing nothing, with what the six Treg networks would cost", s)
	}
	if got.MonthEstimateUSD != got.Checks.MonthUSD || got.Month.CapUSD == nil || *got.Month.CapUSD != 5 || got.Month.SpentUSD != 0 || got.DefaultMonthCapUSD != 5 {
		t.Fatalf("month = %+v, estimate %v, want nothing spent and the $5 minimum cap set by finishing onboarding", got.Month, got.MonthEstimateUSD)
	}

	agent.call("POST", "/v1/runs", nil, http.StatusCreated, nil)
	if err := c.svc.Checks.Work(context.Background()); err != nil {
		t.Fatal(err)
	}
	var runs []struct {
		SpendUSD float64 `json:"spend_usd"`
	}
	reader.call("GET", "/v1/runs", nil, http.StatusOK, &runs)
	reader.call("GET", "/v1/costs", nil, http.StatusOK, &got)
	// The check read its 4 answers for other brands, at the fake's $0.01 a call.
	if got.Month.ChecksUSD != runs[0].SpendUSD || got.Month.BrandsUSD != 0.04 || got.Month.SpentUSD != runs[0].SpendUSD+0.04 || got.Month.ChecksUSD == 0 {
		t.Fatalf("month = %+v after a check that spent %v on answers and read them for other brands", got.Month, runs[0].SpendUSD)
	}
}

// A reader can check names while typing; counting a question that names
// the brand needs config:write.
func TestQuestionsThatNameTheBrandOverHTTP(t *testing.T) {
	c := newServer(t)
	person, agent := login(t, c)
	reader := c.withKey(t, person, core.ResultsRead)
	agent.call("PUT", "/v1/settings", checkstest.Settings(), http.StatusOK, nil)
	var set struct {
		Questions []struct {
			ID            int64 `json:"id"`
			NamesBrand    bool  `json:"names_brand"`
			CountInScores bool  `json:"count_in_scores"`
		} `json:"questions"`
	}
	agent.call("PUT", "/v1/questions", map[string]any{"questions": []map[string]string{{"group": "Brand", "text": "Is Northwind a good host for Go?"}}}, http.StatusOK, &set)
	if q := set.Questions[0]; !q.NamesBrand || q.CountInScores {
		t.Fatalf("question = %+v, want it marked as naming the brand and left out of scores", q)
	}

	var check struct {
		Questions []struct {
			NamesBrand bool `json:"names_brand"`
		} `json:"questions"`
		Aliases []struct {
			CommonWord bool `json:"common_word"`
		} `json:"aliases"`
	}
	reader.call("POST", "/v1/naming-check", map[string]any{"questions": []string{"Northwind pricing"}, "aliases": []string{"Tally"}}, http.StatusOK, &check)
	if !check.Questions[0].NamesBrand || !check.Aliases[0].CommonWord {
		t.Fatalf("naming check = %+v", check)
	}
	reader.call("POST", "/v1/naming-check", map[string]any{"aliases": make([]string, 51)}, http.StatusBadRequest, nil)

	path := fmt.Sprintf("/v1/questions/%d/scoring", set.Questions[0].ID)
	reader.call("PUT", path, map[string]any{"count_in_scores": true}, http.StatusForbidden, nil)
	var q struct {
		CountInScores bool `json:"count_in_scores"`
	}
	agent.call("PUT", path, map[string]any{"count_in_scores": true}, http.StatusOK, &q)
	if !q.CountInScores {
		t.Fatal("count it anyway did not count it")
	}
	agent.call("PUT", "/v1/questions/999999/scoring", map[string]any{"count_in_scores": nil}, http.StatusNotFound, nil)
}
