package httpapi

import (
	"bytes"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/unkeyed/byname/internal/checks/checkstest"
	"github.com/unkeyed/byname/internal/config"
	"github.com/unkeyed/byname/internal/workspaces"
)

func brandNames(t *testing.T, as client, path string) []string {
	t.Helper()
	var brands []config.Brand
	as.call("GET", path, nil, http.StatusOK, &brands)
	var names []string
	for _, b := range brands {
		names = append(names, b.Name)
	}
	return names
}

// Each workspace answers under its own /w/<slug> prefix, paths without one
// are the default workspace's, and only an admin signed in adds one.
func TestWorkspacesOverHTTP(t *testing.T) {
	c := newServer(t)
	person, agent := login(t, c)

	var inv struct{ Token string }
	person.call("POST", "/v1/invites", map[string]string{"email": "ed@example.com", "role": "editor"}, http.StatusCreated, &inv)
	editor := c.signup(map[string]string{"invite_token": inv.Token})
	editor.call("POST", "/v1/workspaces", map[string]string{"name": "Kyring"}, http.StatusForbidden, nil)
	agent.call("POST", "/v1/workspaces", map[string]string{"name": "Kyring"}, http.StatusForbidden, nil)
	person.call("POST", "/v1/workspaces", map[string]string{"name": "Kyring", "slug": "Not A Slug"}, http.StatusBadRequest, nil)
	person.call("POST", "/v1/workspaces", map[string]string{"name": "Kyring", "slug": "default"}, http.StatusBadRequest, nil)
	person.call("POST", "/v1/workspaces", map[string]string{"name": "日本"}, http.StatusBadRequest, nil)

	var kyring workspaces.Summary
	person.call("POST", "/v1/workspaces", map[string]string{"name": "Kyring"}, http.StatusCreated, &kyring)
	if kyring.Slug != "kyring" || kyring.Default || kyring.Onboarded {
		t.Fatalf("POST /v1/workspaces = %+v, want kyring, not the default, not onboarded", kyring)
	}
	person.call("POST", "/v1/workspaces", map[string]string{"name": "Kyring CRM", "slug": "kyring"}, http.StatusConflict, nil)

	var onboarding config.Onboarding
	agent.call("GET", "/w/kyring/v1/onboarding", nil, http.StatusOK, &onboarding)
	if onboarding.FinishedAt != nil || slices.ContainsFunc(onboarding.Steps, func(s config.Step) bool { return s.Saved }) {
		t.Fatalf("a new workspace's onboarding = %+v, want nothing saved and not finished", onboarding)
	}

	agent.call("POST", "/w/kyring/v1/brands", map[string]any{"name": "Contoso", "kind": "competitor"}, http.StatusOK, nil)
	agent.call("POST", "/v1/brands", map[string]any{"name": "Fabrikam", "kind": "competitor"}, http.StatusOK, nil)
	if got := brandNames(t, agent, "/w/kyring/v1/brands"); !slices.Equal(got, []string{"Contoso"}) {
		t.Errorf("Kyring's brands = %v, want Contoso only", got)
	}
	for _, path := range []string{"/v1/brands", "/w/default/v1/brands"} {
		if got := brandNames(t, agent, path); !slices.Equal(got, []string{"Fabrikam"}) {
			t.Errorf("GET %s = %v, want the default workspace's Fabrikam only", path, got)
		}
	}
	for _, path := range []string{"/w/nope/v1/brands", "/w/nope/mcp", "/.well-known/oauth-protected-resource/w/nope/mcp"} {
		if r := agent.do("GET", path, nil); r.code != http.StatusNotFound || !json404(r) {
			t.Errorf("GET %s = %d %s, want a JSON 404", path, r.code, r.body)
		}
	}

	for _, path := range []string{"/v1/me", "/v1/keys", "/v1/users"} {
		here, there := person.call("GET", path, nil, http.StatusOK, nil), person.call("GET", "/w/kyring"+path, nil, http.StatusOK, nil)
		if !bytes.Equal(here.body, there.body) {
			t.Errorf("GET %s = %s, under /w/kyring %s, want the same", path, here.body, there.body)
		}
	}

	var list []workspaces.Summary
	agent.call("GET", "/v1/workspaces", nil, http.StatusOK, &list)
	if len(list) != 2 || !list[0].Default || list[0].Slug != "default" || list[1] != kyring {
		t.Fatalf("GET /v1/workspaces = %+v, want the default then %+v", list, kyring)
	}

	editor.call("PATCH", "/v1/workspaces/kyring", map[string]string{"name": "Kyring CRM"}, http.StatusForbidden, nil)
	var renamed workspaces.Summary
	person.call("PATCH", "/v1/workspaces/kyring", map[string]string{"name": "Kyring CRM"}, http.StatusOK, &renamed)
	if renamed.Name != "Kyring CRM" || renamed.Slug != "kyring" {
		t.Fatalf("PATCH = %+v, want the new name at the same slug", renamed)
	}
	person.call("PATCH", "/v1/workspaces/nope", map[string]string{"name": "x"}, http.StatusNotFound, nil)
}

func json404(r reply) bool {
	return r.header.Get("Content-Type") == "application/json"
}

// A new instance's first workspace takes its brand's name when onboarding
// saves the brand step, and is reachable at the new slug at once.
func TestTheFirstWorkspaceTakesItsBrandsName(t *testing.T) {
	c := newServer(t)
	_, agent := login(t, c)
	agent.call("PUT", "/v1/settings", checkstest.Settings(), http.StatusOK, nil)
	agent.call("PUT", "/v1/onboarding/steps/brand", nil, http.StatusOK, nil)
	var onboarding config.Onboarding
	agent.call("GET", "/w/northwind/v1/onboarding", nil, http.StatusOK, &onboarding)
	if !onboarding.Steps[0].Saved {
		t.Fatalf("onboarding at /w/northwind = %+v, want the brand step saved", onboarding)
	}
	var list []workspaces.Summary
	agent.call("GET", "/v1/workspaces", nil, http.StatusOK, &list)
	if len(list) != 1 || list[0].Name != "Northwind" || list[0].Slug != "northwind" || !list[0].Default {
		t.Fatalf("GET /v1/workspaces = %+v, want the default named Northwind", list)
	}
}

// A browser opening a page under a mistyped slug gets the app, which says
// there is no entity there and offers a way back. API paths stay JSON.
func TestAPageUnderAnUnknownSlugLoadsTheApp(t *testing.T) {
	c := newServer(t)
	page := map[string]string{"Accept": "text/html,application/xhtml+xml,*/*;q=0.8"}
	for _, path := range []string{"/w/nope/settings", "/w/nope", "/w/nope/ai/questions?range=30d"} {
		if r := c.raw("GET", path, nil, page); r.code != http.StatusOK || !strings.HasPrefix(r.header.Get("Content-Type"), "text/html") || !bytes.Contains(r.body, []byte(`id="root"`)) {
			t.Errorf("a page load of %s = %d %s %q, want the app", path, r.code, r.header.Get("Content-Type"), r.body)
		}
	}
	for _, tc := range []struct {
		path   string
		header map[string]string
	}{
		{"/w/nope/v1/brands", page},
		{"/w/nope/mcp", page},
		{"/w/nope/settings", map[string]string{"Accept": "application/json"}},
	} {
		if r := c.raw("GET", tc.path, nil, tc.header); r.code != http.StatusUnauthorized || !json404(r) {
			t.Errorf("GET %s with Accept %q = %d %s, want a JSON 401", tc.path, tc.header["Accept"], r.code, r.header.Get("Content-Type"))
		}
	}
}
