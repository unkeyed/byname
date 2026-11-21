package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/unkeyed/byname/internal/changes/githubtest"
	"github.com/unkeyed/byname/internal/checks/checkstest"
	"github.com/unkeyed/byname/internal/core"
)

const githubToken = "github_pat_11NORTHWIND0readonlytoken"

func TestSiteChangesOverHTTP(t *testing.T) {
	c := newServer(t)
	person, agent := login(t, c)
	agent.call("PUT", "/v1/settings", checkstest.Settings(), http.StatusOK, nil)
	reader := c.withKey(t, person, core.ResultsRead)
	var bodies []string
	keep := func(r reply) reply { bodies = append(bodies, string(r.body)); return r }

	expires := time.Now().Add(10 * 24 * time.Hour).UTC().Truncate(time.Second)
	c.github.SetExpiry(&expires)
	merged := time.Now().Add(-3 * time.Hour)
	c.github.AddPull(githubtest.Pull{Number: 12, Title: "New pricing page", MergedAt: &merged, Updated: merged,
		Files: []githubtest.File{{Name: "app/pricing/page.tsx", Status: "added"}}})

	connect := map[string]string{"repo": "northwind/site", "token": githubToken}
	for _, as := range []client{reader, agent} {
		for _, rt := range []struct {
			method, path string
		}{{"PUT", "/v1/repo"}, {"POST", "/v1/repo/test"}, {"DELETE", "/v1/repo"}} {
			if r := as.do(rt.method, rt.path, connect); r.code != http.StatusForbidden {
				t.Errorf("%s %s by an API key = %d %s, want 403", rt.method, rt.path, r.code, r.body)
			}
		}
	}
	person.call("PUT", "/v1/repo", map[string]string{"repo": "northwind/site", "token": "github_pat_wrong"}, http.StatusBadGateway, nil)
	var st struct {
		Connected      bool       `json:"connected"`
		Repo           string     `json:"repo"`
		DefaultBranch  string     `json:"default_branch"`
		TokenExpiresAt *time.Time `json:"token_expires_at"`
		ExpiringSoon   bool       `json:"expiring_soon"`
	}
	keep(person.call("PUT", "/v1/repo", connect, http.StatusOK, &st))
	if !st.Connected || st.Repo != "northwind/site" || st.DefaultBranch != "main" || st.TokenExpiresAt == nil || !st.TokenExpiresAt.Equal(expires) || !st.ExpiringSoon {
		t.Fatalf("repo status = %+v", st)
	}
	reader.call("PUT", "/v1/repo/rules", map[string]any{"rules": []map[string]string{}}, http.StatusForbidden, nil)
	agent.call("PUT", "/v1/repo/rules", map[string]any{"rules": []map[string]string{{"glob": "app/*/page.tsx", "url": "https://x.example/{2}"}}}, http.StatusBadRequest, nil)
	keep(agent.call("PUT", "/v1/repo/rules", map[string]any{"rules": []map[string]string{{"glob": "app/**/page.tsx", "url": "https://www.northwind.example/{1}"}}}, http.StatusOK, nil))
	keep(person.call("POST", "/v1/repo/test", nil, http.StatusOK, nil))
	if err := c.svc.Changes.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}

	var list []struct {
		ID       int64  `json:"id"`
		URL      string `json:"url"`
		Kind     string `json:"kind"`
		PRNumber int    `json:"pr_number"`
		Stage    string `json:"stage"`
	}
	keep(reader.call("GET", "/v1/changes", nil, http.StatusOK, &list))
	if len(list) != 1 || list[0].URL != "https://www.northwind.example/pricing" || list[0].Kind != "new_page" || list[0].PRNumber != 12 || list[0].Stage != "awaiting_crawl" {
		t.Fatalf("changes = %+v", list)
	}
	agent.call("DELETE", "/v1/changes/"+itoa(list[0].ID), nil, http.StatusConflict, nil)
	keep(reader.call("GET", "/v1/changes/"+itoa(list[0].ID), nil, http.StatusOK, nil))
	reader.call("GET", "/v1/changes/999", nil, http.StatusNotFound, nil)

	manual := map[string]string{"url": "https://www.northwind.example/contact", "kind": "edit", "merged_at": time.Now().Add(-26 * time.Hour).Format(time.DateOnly)}
	reader.call("POST", "/v1/changes", manual, http.StatusForbidden, nil)
	var logged struct {
		ID       int64 `json:"id"`
		Timeline []struct {
			Kind string `json:"kind"`
		} `json:"timeline"`
	}
	agent.call("POST", "/v1/changes", manual, http.StatusCreated, &logged)
	if len(logged.Timeline) != 1 || logged.Timeline[0].Kind != "merged" {
		t.Fatalf("logged change = %+v", logged)
	}
	agent.call("POST", "/v1/changes", map[string]string{"url": "/contact", "kind": "edit", "merged_at": "2026-11-01"}, http.StatusBadRequest, nil)
	agent.call("DELETE", "/v1/changes/"+itoa(logged.ID), nil, http.StatusNoContent, nil)

	from, to := time.Now().AddDate(0, 0, -7).Format(time.DateOnly), time.Now().AddDate(0, 0, 1).Format(time.DateOnly)
	var markers []struct {
		Label string `json:"label"`
	}
	reader.call("GET", "/v1/changes/markers?from="+from+"&to="+to, nil, http.StatusOK, &markers)
	if len(markers) != 1 || markers[0].Label != "#12 New pricing page" {
		t.Fatalf("markers = %+v", markers)
	}
	reader.call("GET", "/v1/changes/markers?from=last+week", nil, http.StatusBadRequest, nil)

	var control struct {
		ID     int64 `json:"id"`
		Active bool  `json:"active"`
	}
	agent.call("POST", "/v1/control-pages", map[string]string{"url": "https://www.northwind.example/docs", "hold_until": "2028-03-15"}, http.StatusOK, &control)
	if !control.Active {
		t.Fatalf("control page held to 2027 = %+v, want active", control)
	}
	reader.call("GET", "/v1/control-pages", nil, http.StatusOK, nil)
	reader.call("DELETE", "/v1/control-pages/"+itoa(control.ID), nil, http.StatusForbidden, nil)
	agent.call("DELETE", "/v1/control-pages/"+itoa(control.ID), nil, http.StatusNoContent, nil)

	var settings map[string]float64
	reader.call("GET", "/v1/changes/settings", nil, http.StatusOK, &settings)
	if settings["interim_days"] != 17 || settings["verdict_days"] != 31 || settings["position"] != 2.5 || settings["ctr_points"] != 0.8 || settings["impressions_ratio"] != 2.7 {
		t.Fatalf("default settings = %v", settings)
	}
	settings["interim_days"] = 40
	agent.call("PUT", "/v1/changes/settings", settings, http.StatusBadRequest, nil)

	keep(reader.call("GET", "/v1/repo", nil, http.StatusOK, nil))
	keep(person.call("DELETE", "/v1/repo", nil, http.StatusNoContent, nil))
	keep(reader.call("GET", "/v1/repo", nil, http.StatusOK, &st))
	if st.Connected || st.Repo != "northwind/site" {
		t.Fatalf("after disconnecting, repo status = %+v; want the repo kept and no token", st)
	}
	var sealed string
	if err := c.db.QueryRow(context.Background(), `SELECT coalesce(ciphertext, '') FROM repo_connection`).Scan(&sealed); err != nil || sealed != "" {
		t.Fatalf("disconnecting left ciphertext %q, %v", sealed, err)
	}
	for _, b := range bodies {
		if strings.Contains(b, githubToken) || strings.Contains(b, "github_pat") {
			t.Fatalf("an API response carries the GitHub token: %s", b)
		}
	}
}

// TestChangeToolsOverMCP calls each site changes read tool over a real
// OAuth connection, which validates every output against its schema.
func TestChangeToolsOverMCP(t *testing.T) {
	anon, person := newLocalServer(t)
	person.call("PUT", "/v1/repo", map[string]string{"repo": "northwind/site", "token": githubToken}, http.StatusOK, nil)
	editor := anon.withKey(t, person, core.ConfigWrite)
	editor.call("POST", "/v1/changes", map[string]string{"url": "https://www.northwind.example/contact", "kind": "edit", "merged_at": "2026-11-01"}, http.StatusCreated, nil)
	editor.call("POST", "/v1/control-pages", map[string]string{"url": "https://www.northwind.example/docs", "hold_until": "2028-03-15"}, http.StatusOK, nil)
	_, tok := connect(t, anon, person, "changes reader")
	cs, err := mcpSession(anon, tok.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	var id int64
	if err := anon.db.QueryRow(context.Background(), `SELECT id FROM change`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	for name, args := range map[string]map[string]any{
		"list_changes": nil, "get_change": {"id": id}, "change_markers": {"from": "2026-10-01", "to": "2026-11-30"},
		"change_settings": nil, "list_control_pages": nil, "repo_status": nil,
	} {
		res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil || res.IsError {
			t.Errorf("%s: %v %+v", name, err, res)
		}
		if b, _ := res.StructuredContent.(map[string]any); name == "repo_status" && b["connected"] != true {
			t.Errorf("repo_status = %v", res.StructuredContent)
		}
	}
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "log_change", Arguments: map[string]any{"url": "https://www.northwind.example/", "kind": "edit", "merged_at": "2026-11-01"}})
	if err != nil || !res.IsError {
		t.Fatalf("log_change over a results:read connection: %v %+v, want a tool error", err, res)
	}
}
