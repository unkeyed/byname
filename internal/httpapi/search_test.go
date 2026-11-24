package httpapi

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/unkeyed/byname/internal/auth"
	"github.com/unkeyed/byname/internal/checks/checkstest"
	"github.com/unkeyed/byname/internal/core"
	"github.com/unkeyed/byname/internal/search"
	"github.com/unkeyed/byname/internal/search/searchtest"
	"github.com/unkeyed/byname/internal/secretbox"
)

// bingKey is the API key the fake Bing accepts.
const bingKey = "9a8b7c6d5e4f30211203f4e5d6c7b8a9"

// seedGoogle gives the fake Google two properties for Northwind and 40
// days of numbers, the last two still partial.
func seedGoogle(g *searchtest.Google) {
	g.AddSite("sc-domain:northwind.example", "siteOwner")
	g.AddSite("https://www.northwind.example/", "siteFullUser")
	today := time.Now().In(time.FixedZone("PT", -8*3600))
	for n := 1; n <= 40; n++ {
		d := today.AddDate(0, 0, -n).Format(time.DateOnly)
		g.SetRows("sc-domain:northwind.example",
			searchtest.Row{Date: d, Query: "northwind pricing", Page: "https://www.northwind.example/pricing", Clicks: 6, Impressions: 60, Position: 1.4},
			searchtest.Row{Date: d, Query: "go api hosting", Page: "https://www.northwind.example/docs", Clicks: 3, Impressions: 200, Position: 8.2},
			searchtest.Row{Date: d, Page: "https://www.northwind.example/docs", Clicks: 1, Impressions: 30, Position: 11})
	}
	g.SetFirstIncomplete(today.AddDate(0, 0, -2).Format(time.DateOnly))
}

// seedBing gives the fake Bing Northwind's site and 70 days of totals
// with ten weekly top lists, dated Fridays, up to today.
func seedBing(b *searchtest.Bing) {
	b.AddSite("https://www.northwind.example/", true)
	today := time.Now().UTC()
	var days []searchtest.BingDay
	for n := 70; n >= 1; n-- {
		days = append(days, searchtest.BingDay{Date: today.AddDate(0, 0, -n).Format(time.DateOnly), Clicks: 7, Impressions: 90})
	}
	b.SetTraffic("https://www.northwind.example/", days...)
	friday := today.AddDate(0, 0, -((int(today.Weekday()) + 2) % 7))
	var weeks []searchtest.BingWeek
	for n := range 10 {
		d := friday.AddDate(0, 0, -7*n).Format(time.DateOnly)
		weeks = append(weeks, searchtest.BingWeek{Date: d, Key: "northwind pricing", Clicks: 12, Impressions: 80, Position: 2},
			searchtest.BingWeek{Date: d, Key: "go api hosting", Clicks: 20, Impressions: 400, Position: 9})
	}
	b.SetQueries("https://www.northwind.example/", weeks...)
	b.SetPages("https://www.northwind.example/", weeks...)
}

// callbackHere sends the browser's return from Google's consent page to
// the test server, which PUBLIC_URL does not point at.
func callbackHere(t *testing.T, c client, start string) string {
	t.Helper()
	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := noFollow.Get(start)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	back, err := url.Parse(res.Header.Get("Location"))
	if err != nil || back.Path != "/auth/gsc/callback" {
		t.Fatalf("consent sent the browser to %q", res.Header.Get("Location"))
	}
	return "/auth/gsc/callback?" + back.RawQuery
}

// returnFromGoogle is the browser landing on the callback with the
// person's session cookie, without following the redirect into the UI.
func returnFromGoogle(t *testing.T, person client, path string) (int, string) {
	t.Helper()
	req, err := http.NewRequest("GET", person.base+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(&http.Cookie{Name: auth.SessionCookie, Value: person.session})
	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := noFollow.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	return res.StatusCode, res.Header.Get("Location")
}

func TestSearchOverHTTP(t *testing.T) {
	c := newServer(t)
	person, agent := login(t, c)
	seedGoogle(c.google)
	agent.call("PUT", "/v1/settings", checkstest.Settings(), http.StatusOK, nil)
	reader := c.withKey(t, person, core.ResultsRead)
	var bodies []string
	keep := func(r reply) reply { bodies = append(bodies, string(r.body)); return r }

	var invite struct{ Token string }
	person.call("POST", "/v1/invites", map[string]string{"email": "vi@example.com", "role": "viewer"}, http.StatusCreated, &invite)
	viewer := c.signup(map[string]string{"invite_token": invite.Token})
	for _, as := range []client{reader, agent, viewer} {
		for _, rt := range []struct{ method, path string }{
			{"PUT", "/v1/search/google/key"}, {"POST", "/v1/search/google/oauth/start"}, {"PUT", "/v1/search/google/site"},
			{"GET", "/v1/search/google/sites"}, {"DELETE", "/v1/search/google"}, {"PUT", "/v1/search/bing/key"}, {"DELETE", "/v1/search/bing"},
		} {
			if r := as.do(rt.method, rt.path, map[string]string{"key": "{}"}); r.code != http.StatusForbidden {
				t.Errorf("%s %s by a non-admin = %d %s, want 403", rt.method, rt.path, r.code, r.body)
			}
		}
	}
	keep(viewer.call("GET", "/v1/search/providers", nil, http.StatusOK, nil))

	var start struct{ URL string }
	keep(person.call("POST", "/v1/search/google/oauth/start", nil, http.StatusOK, &start))
	if code, loc := returnFromGoogle(t, person, callbackHere(t, c, start.URL)); code != http.StatusSeeOther || loc != "/settings/search-engines?connected=google" {
		t.Fatalf("callback = %d to %q", code, loc)
	}
	if _, loc := returnFromGoogle(t, person, callbackHere(t, c, start.URL)+"x"); !strings.HasPrefix(loc, "/settings/search-engines?error=") {
		t.Fatalf("a callback with a bad code redirected to %q, want an error back on the tab", loc)
	}
	for _, as := range []client{c, viewer} {
		if code, loc := returnFromGoogle(t, as, callbackHere(t, c, start.URL)); code != http.StatusSeeOther || !strings.Contains(loc, "sign+in+to+Byname+as+an+admin") {
			t.Fatalf("a callback without an admin session = %d to %q, want an error back on the tab", code, loc)
		}
	}

	var props []struct {
		SiteURL   string `json:"url"`
		Suggested bool   `json:"suggested"`
	}
	keep(person.call("GET", "/v1/search/google/sites", nil, http.StatusOK, &props))
	if len(props) != 2 || props[0].SiteURL != "sc-domain:northwind.example" || !props[0].Suggested {
		t.Fatalf("properties = %+v", props)
	}
	keep(person.call("PUT", "/v1/search/google/site", map[string]any{"site": "sc-domain:northwind.example"}, http.StatusOK, nil))
	if err := c.svc.Search.Sync(context.Background(), search.Google); err != nil {
		t.Fatal(err)
	}

	var list struct {
		Providers []struct {
			Provider    string   `json:"provider"`
			Connected   bool     `json:"connected"`
			Method      string   `json:"method"`
			LatestFinal string   `json:"latest_final_day"`
			PartialDays []string `json:"partial_days"`
		} `json:"providers"`
	}
	keep(reader.call("GET", "/v1/search/providers", nil, http.StatusOK, &list))
	if len(list.Providers) != 2 || list.Providers[0].Provider != "google" || list.Providers[1].Provider != "bing" || list.Providers[1].Connected {
		t.Fatalf("providers = %+v; want Google then Bing, only Google connected", list.Providers)
	}
	if st := list.Providers[0]; !st.Connected || st.Method != "oauth" || st.LatestFinal == "" || len(st.PartialDays) != 2 {
		t.Fatalf("Google's status = %+v", st)
	}
	var sum struct {
		Current struct {
			Clicks         int64 `json:"clicks"`
			NonBrandClicks int64 `json:"non_brand_clicks"`
		} `json:"current"`
	}
	keep(reader.call("GET", "/v1/search/google/summary", nil, http.StatusOK, &sum))
	if sum.Current.Clicks != 10*28 || sum.Current.NonBrandClicks != 3*28 {
		t.Fatalf("summary = %+v", sum)
	}
	for _, path := range []string{"/v1/search/google/daily", "/v1/search/google/daily?from=2026-01-01&to=2026-02-01", "/v1/search/google/brand",
		"/v1/search/google/pages?days=90&limit=5", "/v1/search/google/queries?non_brand=true", "/v1/search/bing/summary", "/v1/goals", "/v1/settings"} {
		keep(reader.call("GET", path, nil, http.StatusOK, nil))
	}
	reader.call("GET", "/v1/search/google/queries?non_brand=maybe", nil, http.StatusBadRequest, nil)
	reader.call("GET", "/v1/search/google/daily?from=yesterday", nil, http.StatusBadRequest, nil)
	reader.call("GET", "/v1/search/yahoo/summary", nil, http.StatusBadRequest, nil)

	goal := map[string]any{"provider": "google", "metric": "non_brand_clicks", "window_days": 28, "target": 240, "due_on": "2028-03-15"}
	reader.call("POST", "/v1/goals", goal, http.StatusForbidden, nil)
	var g struct {
		ID       int64 `json:"id"`
		Progress struct {
			Value int64 `json:"value"`
		} `json:"progress"`
	}
	keep(agent.call("POST", "/v1/goals", goal, http.StatusOK, &g))
	if g.Progress.Value != 3*28 {
		t.Fatalf("goal progress = %+v", g)
	}
	goal["target"] = 90
	keep(agent.call("PUT", "/v1/goals/"+itoa(g.ID), goal, http.StatusOK, nil))
	agent.call("PUT", "/v1/settings/branded-terms", map[string]any{"terms": []string{"Northwind", "nw cloud"}}, http.StatusOK, nil)
	agent.call("PUT", "/v1/settings/branded-terms", map[string]any{}, http.StatusBadRequest, nil)
	agent.call("DELETE", "/v1/goals/"+itoa(g.ID), nil, http.StatusNoContent, nil)
	agent.call("DELETE", "/v1/goals/"+itoa(g.ID), nil, http.StatusNotFound, nil)

	var sealed string
	if err := c.db.QueryRow(context.Background(), `SELECT ciphertext FROM search_connection`).Scan(&sealed); err != nil {
		t.Fatal(err)
	}
	refresh, err := secretbox.ForTest().Open(sealed, "gsc:oauth")
	if err != nil || refresh == "" {
		t.Fatalf("open the stored refresh token: %v", err)
	}
	key := c.google.ServiceAccountKey(t, "byname@northwind-search.iam.gserviceaccount.com")
	keep(person.call("PUT", "/v1/search/google/key", map[string]string{"key": key}, http.StatusOK, nil))
	keep(reader.call("GET", "/v1/search/providers", nil, http.StatusOK, nil))
	for _, b := range bodies {
		if strings.Contains(b, refresh) || strings.Contains(b, sealed) || strings.Contains(b, "PRIVATE KEY") {
			t.Fatalf("an API response carries the Search Console credential: %s", b)
		}
	}
	keep(person.call("DELETE", "/v1/search/google", nil, http.StatusNoContent, nil))
	reader.call("GET", "/v1/search/google/summary", nil, http.StatusOK, &sum)
	if sum.Current.Clicks != 10*28 {
		t.Fatal("disconnecting dropped the synced numbers")
	}
}

// TestBingOverHTTP connects Bing with a pasted key, picks its site, syncs
// and reads it back, and never answers with the key.
func TestBingOverHTTP(t *testing.T) {
	c := newServer(t)
	person, agent := login(t, c)
	seedBing(c.bing)
	agent.call("PUT", "/v1/settings", checkstest.Settings(), http.StatusOK, nil)
	reader := c.withKey(t, person, core.ResultsRead)
	var bodies []string
	keep := func(r reply) reply { bodies = append(bodies, string(r.body)); return r }

	person.call("PUT", "/v1/search/bing/key", map[string]string{"key": "not-a-bing-key"}, http.StatusBadGateway, nil)
	keep(person.call("PUT", "/v1/search/bing/key", map[string]string{"key": bingKey}, http.StatusOK, nil))
	var sites []struct {
		URL       string `json:"url"`
		Suggested bool   `json:"suggested"`
	}
	keep(person.call("GET", "/v1/search/bing/sites", nil, http.StatusOK, &sites))
	if len(sites) != 1 || sites[0].URL != "https://www.northwind.example/" || !sites[0].Suggested {
		t.Fatalf("Bing sites = %+v", sites)
	}
	keep(person.call("PUT", "/v1/search/bing/site", map[string]any{"site": "https://www.northwind.example/"}, http.StatusOK, nil))
	if err := c.svc.Search.Sync(context.Background(), search.Bing); err != nil {
		t.Fatal(err)
	}
	var sum struct {
		Provider string `json:"provider"`
		Current  struct {
			From     string  `json:"from"`
			To       string  `json:"to"`
			Days     int     `json:"days"`
			Clicks   int64   `json:"clicks"`
			Position float64 `json:"position"`
		} `json:"current"`
	}
	keep(reader.call("GET", "/v1/search/bing/summary", nil, http.StatusOK, &sum))
	if sum.Provider != "bing" || sum.Current.Days != 28 || sum.Current.Clicks != 7*28 || sum.Current.Position != 0 {
		t.Fatalf("Bing summary = %+v", sum)
	}
	to, _ := time.Parse(time.DateOnly, sum.Current.To)
	if to.Weekday() != time.Friday {
		t.Fatalf("Bing's window ends on %s, want the end of a Bing week", sum.Current.To)
	}
	var top struct {
		From    string `json:"from"`
		Queries []struct {
			Query  string `json:"query"`
			Clicks int64  `json:"clicks"`
		} `json:"queries"`
	}
	keep(reader.call("GET", "/v1/search/bing/queries?days=7", nil, http.StatusOK, &top))
	if len(top.Queries) != 2 || top.Queries[0].Query != "go api hosting" || top.Queries[0].Clicks != 20 || top.From != to.AddDate(0, 0, -6).Format(time.DateOnly) {
		t.Fatalf("a week of Bing searches = %+v; want the week ending %s", top, sum.Current.To)
	}
	for _, path := range []string{"/v1/search/bing/daily", "/v1/search/bing/brand", "/v1/search/bing/pages", "/v1/search/providers"} {
		keep(reader.call("GET", path, nil, http.StatusOK, nil))
	}
	agent.call("POST", "/v1/goals", map[string]any{"provider": "bing", "metric": "clicks", "window_days": 90, "target": 100, "due_on": "2028-03-15"}, http.StatusOK, nil)
	agent.call("POST", "/v1/goals", map[string]any{"provider": "yahoo", "metric": "clicks", "window_days": 90, "target": 100, "due_on": "2028-03-15"}, http.StatusBadRequest, nil)
	keep(reader.call("GET", "/v1/goals", nil, http.StatusOK, nil))
	for _, b := range bodies {
		if strings.Contains(b, bingKey) {
			t.Fatalf("an API response carries the Bing API key: %s", b)
		}
	}
	keep(person.call("DELETE", "/v1/search/bing", nil, http.StatusNoContent, nil))
}

// TestSearchToolsOverMCP calls each search tool over a real OAuth
// connection, which validates every output against its schema.
func TestSearchToolsOverMCP(t *testing.T) {
	anon, person := newLocalServer(t)
	seedGoogle(anon.google)
	editor := anon.withKey(t, person, core.ConfigWrite)
	editor.call("PUT", "/v1/settings", checkstest.Settings(), http.StatusOK, nil)
	_, tok := connect(t, anon, person, "search reader")
	cs, err := mcpSession(anon, tok.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	call := func(name string, args map[string]any) {
		t.Helper()
		res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil || res.IsError {
			t.Errorf("%s: %v %+v", name, err, res)
		}
	}
	google := map[string]any{"provider": "google"}
	reads := map[string]map[string]any{
		"search_status": nil, "search_summary": google, "search_daily": {"provider": "bing", "from": "2026-01-01"}, "search_brand_split": google,
		"top_pages": {"provider": "google", "days": 90}, "top_queries": {"provider": "bing", "non_brand": true}, "list_goals": nil,
	}
	for name, args := range reads {
		call(name, args)
	}

	person.call("PUT", "/v1/search/google/key", map[string]string{"key": anon.google.ServiceAccountKey(t, "byname@northwind-search.iam.gserviceaccount.com")}, http.StatusOK, nil)
	person.call("PUT", "/v1/search/google/site", map[string]any{"site": "https://www.northwind.example/", "backfill": false}, http.StatusOK, nil)
	anon.google.SetRows("https://www.northwind.example/", searchtest.Row{Date: time.Now().AddDate(0, 0, -5).Format(time.DateOnly), Query: "northwind", Page: "https://www.northwind.example/", Clicks: 2, Impressions: 9, Position: 1})
	if err := anon.svc.Search.Sync(context.Background(), search.Google); err != nil {
		t.Fatal(err)
	}
	editor.call("POST", "/v1/goals", map[string]any{"provider": "google", "metric": "clicks", "window_days": 90, "target": 5, "due_on": "2028-03-15"}, http.StatusOK, nil)
	seedBing(anon.bing)
	person.call("PUT", "/v1/search/bing/key", map[string]string{"key": bingKey}, http.StatusOK, nil)
	person.call("PUT", "/v1/search/bing/site", map[string]any{"site": "https://www.northwind.example/"}, http.StatusOK, nil)
	if err := anon.svc.Search.Sync(context.Background(), search.Bing); err != nil {
		t.Fatal(err)
	}
	for name, args := range reads {
		call(name, args)
	}
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "search_summary", Arguments: map[string]any{"provider": "yahoo"}})
	if err != nil || !res.IsError {
		t.Fatalf("search_summary for an unknown provider: %v %+v, want a tool error", err, res)
	}
	res, err = cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "upsert_goal", Arguments: map[string]any{"provider": "google", "metric": "clicks", "window_days": 28, "target": 1, "due_on": "2028-03-15"}})
	if err != nil || !res.IsError {
		t.Fatalf("upsert_goal over a results:read connection: %v %+v, want a tool error", err, res)
	}
}

// TestGoogleSignInReturnsWhereItStarted: the callback sends the browser
// back to the page named when the sign-in started, from a fixed set, and
// never to a URL the request carries.
func TestGoogleSignInReturnsWhereItStarted(t *testing.T) {
	c := newServer(t)
	person, _ := login(t, c)
	c.google.AddSite("sc-domain:northwind.example", "siteOwner")
	for _, bad := range []string{"https://evil.example/", "//evil.example", "/settings/search-engines"} {
		person.call("POST", "/v1/search/google/oauth/start", map[string]string{"return_to": bad}, http.StatusBadRequest, nil)
	}
	var start struct{ URL string }
	person.call("POST", "/v1/search/google/oauth/start", map[string]string{"return_to": "onboarding"}, http.StatusOK, &start)
	callback := callbackHere(t, c, start.URL)
	if _, loc := returnFromGoogle(t, person, strings.Replace(callback, "code=", "code=x", 1)); !strings.HasPrefix(loc, "/onboarding/search?error=") {
		t.Fatalf("a failed sign-in from onboarding redirected to %q", loc)
	}
	if code, loc := returnFromGoogle(t, person, callback); code != http.StatusSeeOther || loc != "/onboarding/search?connected=google" {
		t.Fatalf("a sign-in from onboarding = %d to %q", code, loc)
	}
	forged := "/auth/gsc/callback?state=" + url.QueryEscape(`{"r":"https://evil.example/"}`) + "&code=x"
	if _, loc := returnFromGoogle(t, person, forged); !strings.HasPrefix(loc, "/settings/search-engines?error=") {
		t.Fatalf("a callback with a state this instance did not seal redirected to %q", loc)
	}
	person.call("POST", "/v1/search/google/oauth/start", nil, http.StatusOK, &start)
	if _, loc := returnFromGoogle(t, person, callbackHere(t, c, start.URL)); loc != "/settings/search-engines?connected=google" {
		t.Fatalf("a sign-in started without return_to redirected to %q", loc)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
