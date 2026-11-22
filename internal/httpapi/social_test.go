package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/unkeyed/byname/internal/checks/checkstest"
	"github.com/unkeyed/byname/internal/core"
)

// socialFixtures is the body the fake Treg answers each social tool with,
// from internal/social/testdata.
var socialFixtures = map[string]string{
	"anyapi.x.search.posts":              "anyapi.x.search.posts.json",
	"crawl4ai.reddit.search.posts":       "crawl4ai.reddit.search.posts.json",
	"crawl4ai.hackernews.search.stories": "crawl4ai.hackernews.search.stories.json",
	"anyapi.linkedin.search.posts":       "anyapi.linkedin.search.posts.json",
	"crawl4ai.youtube.search.videos":     "crawl4ai.youtube.search.videos.json",
	"tikhub.x.threads-web-search-recent": "tikhub.x.threads-web-search-recent.synthetic.json",
}

func socialFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "social", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// fakeBluesky serves the recorded Bluesky search answer, so no test calls
// the real Bluesky.
func fakeBluesky(t *testing.T) string {
	body := socialFixture(t, "bluesky.searchPosts.json")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(body) }))
	t.Cleanup(srv.Close)
	return srv.URL
}

// seedMentions sets the brand up as Northwind and stores the fixtures' 14
// posts, two per network.
func seedMentions(t *testing.T, c client) {
	t.Helper()
	ctx := context.Background()
	for tool, file := range socialFixtures {
		c.treg.SetBody(tool, socialFixture(t, file))
	}
	if _, err := c.svc.Config.PutSettings(ctx, checkstest.Settings()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.svc.Config.SetCredential(ctx, "treg", "treg_test_token", ""); err != nil {
		t.Fatal(err)
	}
	if err := c.svc.Social.Fetch(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestSocialOverHTTP(t *testing.T) {
	c := newServer(t)
	person, agent := login(t, c)
	reader := c.withKey(t, person, core.ResultsRead)
	seedMentions(t, c)

	var list struct {
		Mentions []struct {
			Network      string `json:"network"`
			AuthorHandle string `json:"author_handle"`
			Hidden       bool   `json:"hidden"`
		} `json:"mentions"`
		Total  int `json:"total"`
		Hidden int `json:"hidden"`
	}
	reader.call("GET", "/v1/social/mentions", nil, http.StatusOK, &list)
	if list.Total != 14 || list.Hidden != 0 || len(list.Mentions) != 14 {
		t.Fatalf("mentions = total %d hidden %d listed %d, want 14, 0, 14", list.Total, list.Hidden, len(list.Mentions))
	}
	reader.call("GET", "/v1/social/mentions?network=mastodon", nil, http.StatusBadRequest, nil)
	reader.call("GET", "/v1/social/settings", nil, http.StatusOK, nil)
	reader.call("GET", "/v1/social/left-out", nil, http.StatusOK, nil)
	reader.call("GET", "/v1/social/digest?day=2026-11-18", nil, http.StatusOK, nil)

	leave := map[string]string{"network": "threads", "handle": "https://www.threads.com/@Northwind.Cloud", "label": "Our accounts"}
	settings := map[string]any{"terms": []string{"Northwind Cloud"}, "fetch_every_hours": 12, "digest_cadence": "daily", "digest_weekday": 1}
	reader.call("POST", "/v1/social/left-out", leave, http.StatusForbidden, nil)
	reader.call("PUT", "/v1/social/settings", settings, http.StatusForbidden, nil)

	var left struct {
		ID       int64  `json:"id"`
		Handle   string `json:"handle"`
		Mentions int    `json:"mentions"`
	}
	agent.call("POST", "/v1/social/left-out", leave, http.StatusOK, &left)
	if left.Handle != "northwind.cloud" || left.Mentions != 1 {
		t.Fatalf("left out = %+v, want northwind.cloud hiding 1 mention", left)
	}
	reader.call("GET", "/v1/social/mentions?include_hidden=true&network=threads", nil, http.StatusOK, &list)
	if list.Total != 1 || list.Hidden != 1 || len(list.Mentions) != 2 {
		t.Fatalf("threads with hidden = %+v, want 1 shown, 1 hidden, both listed", list)
	}
	reader.call("DELETE", fmt.Sprintf("/v1/social/left-out/%d", left.ID), nil, http.StatusForbidden, nil)
	agent.call("DELETE", fmt.Sprintf("/v1/social/left-out/%d", left.ID), nil, http.StatusNoContent, nil)
	agent.call("DELETE", fmt.Sprintf("/v1/social/left-out/%d", left.ID), nil, http.StatusNotFound, nil)

	var st struct {
		FetchEveryHours int    `json:"fetch_every_hours"`
		DigestCadence   string `json:"digest_cadence"`
		Networks        []struct {
			Network string `json:"network"`
		} `json:"networks"`
	}
	agent.call("PUT", "/v1/social/settings", settings, http.StatusOK, &st)
	if st.FetchEveryHours != 12 || st.DigestCadence != "daily" || len(st.Networks) != 7 {
		t.Fatalf("settings = %+v", st)
	}
	agent.call("PUT", "/v1/social/settings", map[string]any{"terms": []string{}, "fetch_every_hours": 5, "digest_cadence": "daily", "digest_weekday": 1}, http.StatusBadRequest, nil)
}

// TestSocialToolsOverMCP calls each social read tool over a real OAuth
// connection, which validates every output against its schema.
func TestSocialToolsOverMCP(t *testing.T) {
	anon, person := newLocalServer(t)
	seedMentions(t, anon)
	_, tok := connect(t, anon, person, "social reader")
	cs, err := mcpSession(anon, tok.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	for name, args := range map[string]map[string]any{
		"list_social_mentions": {"network": "bluesky", "include_hidden": true}, "get_social_digest": {"day": "2026-11-18"},
		"get_social_settings": nil, "list_left_out_accounts": nil,
	} {
		res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil || res.IsError {
			t.Errorf("%s: %v %+v", name, err, res)
		}
		if b, _ := res.StructuredContent.(map[string]any); name == "list_social_mentions" && b["total"] != float64(2) {
			t.Errorf("list_social_mentions = %v, want the two Bluesky posts", res.StructuredContent)
		}
	}
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "leave_out_account", Arguments: map[string]any{"network": "x", "handle": "@someone", "label": "Team"}})
	if err != nil || !res.IsError {
		t.Fatalf("leave_out_account over a results:read connection: %v %+v, want a tool error", err, res)
	}
}
