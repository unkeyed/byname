package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/unkeyed/byname/internal/core"
)

type otherBrandsReply struct {
	Answered int `json:"answered"`
	Read     int `json:"read"`
	Hidden   int `json:"hidden"`
	Brands   []struct {
		BrandID int64  `json:"brand_id"`
		Name    string `json:"name"`
		Named   int    `json:"named"`
	} `json:"brands"`
}

// A check reads its answers for other brands; a reader lists them, and
// config:write tracks, hides and unhides them.
func TestOtherBrandsOverHTTP(t *testing.T) {
	c := newServer(t)
	person, agent := login(t, c)
	reader := c.withKey(t, person, core.ResultsRead)
	configure(t, person, agent)
	// The configured questions are fewer than the default minimum for listing a brand.
	agent.call("PUT", "/v1/other-brands/settings", map[string]int{"min_questions": 1}, http.StatusOK, nil)
	agent.call("POST", "/v1/runs", nil, http.StatusCreated, nil)
	if err := c.svc.Checks.Work(context.Background()); err != nil {
		t.Fatal(err)
	}

	var got otherBrandsReply
	reader.call("GET", "/v1/other-brands", nil, http.StatusOK, &got)
	if len(got.Brands) != 2 || got.Read != got.Answered || got.Answered == 0 {
		t.Fatalf("other brands = %+v, want the two the fake model names, every answer read", got)
	}
	id := got.Brands[0].BrandID
	reader.call("POST", fmt.Sprintf("/v1/other-brands/%d/track", id), nil, http.StatusForbidden, nil)
	agent.call("POST", fmt.Sprintf("/v1/other-brands/%d/hide", got.Brands[1].BrandID), nil, http.StatusNoContent, nil)
	var hidden []struct {
		BrandID int64 `json:"brand_id"`
	}
	reader.call("GET", "/v1/other-brands/hidden", nil, http.StatusOK, &hidden)
	if len(hidden) != 1 || hidden[0].BrandID != got.Brands[1].BrandID {
		t.Errorf("hidden = %+v, want the hidden brand", hidden)
	}
	agent.call("POST", fmt.Sprintf("/v1/other-brands/%d/unhide", got.Brands[1].BrandID), nil, http.StatusNoContent, nil)
	var tracked struct {
		ID   int64  `json:"id"`
		Kind string `json:"kind"`
	}
	agent.call("POST", fmt.Sprintf("/v1/other-brands/%d/track", id), nil, http.StatusOK, &tracked)
	if tracked.ID != id || tracked.Kind != "competitor" {
		t.Errorf("tracked = %+v, want brand %d as a competitor", tracked, id)
	}
	agent.call("POST", fmt.Sprintf("/v1/other-brands/%d/track", id), nil, http.StatusNotFound, nil)
	reader.call("GET", "/v1/other-brands", nil, http.StatusOK, &got)
	if len(got.Brands) != 1 {
		t.Errorf("after tracking one: %+v, want one left", got.Brands)
	}

	var settings struct {
		HasToken bool `json:"has_token"`
		Find     struct {
			On bool `json:"on"`
		} `json:"find"`
		Logos struct {
			On         bool `json:"on"`
			MonthLimit int  `json:"month_limit"`
		} `json:"logos"`
	}
	reader.call("GET", "/v1/other-brands/settings", nil, http.StatusOK, &settings)
	if !settings.HasToken || !settings.Find.On || !settings.Logos.On || settings.Logos.MonthLimit != 100 {
		t.Errorf("settings = %+v, want both on and a limit of 100 new brands a month", settings)
	}
	agent.call("PUT", "/v1/other-brands/settings", map[string]any{"logo_month_limit": 5000}, http.StatusBadRequest, nil)
	agent.call("PUT", "/v1/other-brands/settings", map[string]any{"find": false}, http.StatusOK, &settings)
	if settings.Find.On || !settings.Logos.On {
		t.Errorf("after turning reading off: %+v, want logos kept on", settings)
	}
	reader.call("POST", "/v1/other-brands/backfill", nil, http.StatusForbidden, nil)
	agent.call("POST", "/v1/other-brands/backfill", nil, http.StatusBadRequest, nil)
}

// A logo is served from Byname with a policy that stops an SVG running
// anything, even opened on its own.
func TestBrandLogoIsServedSandboxed(t *testing.T) {
	c := newServer(t)
	person, agent := login(t, c)
	reader := c.withKey(t, person, core.ResultsRead)
	configure(t, person, agent)
	var id int64
	if err := c.db.QueryRow(context.Background(), `SELECT id FROM brand WHERE kind = 'self'`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	reader.call("GET", fmt.Sprintf("/v1/brands/%d/logo", id), nil, http.StatusNotFound, nil)
	svg := `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`
	if _, err := c.db.Exec(context.Background(), `INSERT INTO brand_logo (brand_id, image, mime, looked_up_at) VALUES ($1, $2, 'image/svg+xml', now())`, id, []byte(svg)); err != nil {
		t.Fatal(err)
	}
	r := reader.do("GET", fmt.Sprintf("/v1/brands/%d/logo", id), nil)
	if r.code != http.StatusOK || r.header.Get("Content-Type") != "image/svg+xml" || string(r.body) != svg ||
		!strings.Contains(r.header.Get("Content-Security-Policy"), "sandbox") || r.header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("logo = %d %v %q", r.code, r.header, r.body)
	}
	var logos []struct {
		BrandID int64 `json:"brand_id"`
	}
	reader.call("GET", "/v1/logos", nil, http.StatusOK, &logos)
	if len(logos) != 1 || logos[0].BrandID != id {
		t.Errorf("logos = %+v, want yours", logos)
	}
}

func TestOtherBrandsToolsOverMCP(t *testing.T) {
	anon, person := newLocalServer(t)
	_, tok := connect(t, anon, person, "other brands reader")
	cs, err := mcpSession(anon, tok.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	for _, name := range []string{"list_other_brands", "list_hidden_brands", "get_other_brands_settings", "get_brand_backfill", "list_logos"} {
		res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name})
		if err != nil || res.IsError {
			t.Errorf("%s: %v %+v", name, err, res)
		}
	}
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "track_brand", Arguments: map[string]any{"id": 1}})
	if err != nil || !res.IsError {
		t.Fatalf("track_brand over a results:read connection: %v %+v, want a tool error", err, res)
	}
}
