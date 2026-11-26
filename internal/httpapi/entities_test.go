package httpapi

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/unkeyed/byname/internal/checks/checkstest"
	"github.com/unkeyed/byname/internal/config"
	"github.com/unkeyed/byname/internal/entities"
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

// Each entity answers under its own /e/<slug> prefix, paths without one
// are the default entity's, and only an admin signed in adds one.
func TestEntitiesOverHTTP(t *testing.T) {
	c := newServer(t)
	person, agent := login(t, c)

	var inv struct{ Token string }
	person.call("POST", "/v1/invites", map[string]string{"email": "ed@example.com", "role": "editor"}, http.StatusCreated, &inv)
	editor := c.signup(map[string]string{"invite_token": inv.Token})
	editor.call("POST", "/v1/entities", map[string]string{"name": "Kyring"}, http.StatusForbidden, nil)
	agent.call("POST", "/v1/entities", map[string]string{"name": "Kyring"}, http.StatusForbidden, nil)
	person.call("POST", "/v1/entities", map[string]string{"name": "Kyring", "slug": "Not A Slug"}, http.StatusBadRequest, nil)
	person.call("POST", "/v1/entities", map[string]string{"name": "Kyring", "slug": "default"}, http.StatusBadRequest, nil)
	person.call("POST", "/v1/entities", map[string]string{"name": "日本"}, http.StatusBadRequest, nil)

	var kyring entities.Summary
	person.call("POST", "/v1/entities", map[string]string{"name": "Kyring"}, http.StatusCreated, &kyring)
	if kyring.Slug != "kyring" || kyring.Default || kyring.Onboarded {
		t.Fatalf("POST /v1/entities = %+v, want kyring, not the default, not onboarded", kyring)
	}
	person.call("POST", "/v1/entities", map[string]string{"name": "Kyring CRM", "slug": "kyring"}, http.StatusConflict, nil)

	var onboarding config.Onboarding
	agent.call("GET", "/e/kyring/v1/onboarding", nil, http.StatusOK, &onboarding)
	if onboarding.FinishedAt != nil || slices.ContainsFunc(onboarding.Steps, func(s config.Step) bool { return s.Saved }) {
		t.Fatalf("a new entity's onboarding = %+v, want nothing saved and not finished", onboarding)
	}

	agent.call("POST", "/e/kyring/v1/brands", map[string]any{"name": "Contoso", "kind": "competitor"}, http.StatusOK, nil)
	agent.call("POST", "/v1/brands", map[string]any{"name": "Fabrikam", "kind": "competitor"}, http.StatusOK, nil)
	if got := brandNames(t, agent, "/e/kyring/v1/brands"); !slices.Equal(got, []string{"Contoso"}) {
		t.Errorf("Kyring's brands = %v, want Contoso only", got)
	}
	for _, path := range []string{"/v1/brands", "/e/default/v1/brands"} {
		if got := brandNames(t, agent, path); !slices.Equal(got, []string{"Fabrikam"}) {
			t.Errorf("GET %s = %v, want the default entity's Fabrikam only", path, got)
		}
	}
	for _, path := range []string{"/e/nope/v1/brands", "/e/nope/mcp", "/.well-known/oauth-protected-resource/e/nope/mcp"} {
		if r := agent.do("GET", path, nil); r.code != http.StatusNotFound || !json404(r) {
			t.Errorf("GET %s = %d %s, want a JSON 404", path, r.code, r.body)
		}
	}

	for _, path := range []string{"/v1/me", "/v1/keys", "/v1/users"} {
		here, there := person.call("GET", path, nil, http.StatusOK, nil), person.call("GET", "/e/kyring"+path, nil, http.StatusOK, nil)
		if !bytes.Equal(here.body, there.body) {
			t.Errorf("GET %s = %s, under /e/kyring %s, want the same", path, here.body, there.body)
		}
	}

	var list []entities.Summary
	agent.call("GET", "/v1/entities", nil, http.StatusOK, &list)
	if len(list) != 2 || !list[0].Default || list[0].Slug != "default" || list[1] != kyring {
		t.Fatalf("GET /v1/entities = %+v, want the default then %+v", list, kyring)
	}

	editor.call("PATCH", "/v1/entities/kyring", map[string]string{"name": "Kyring CRM"}, http.StatusForbidden, nil)
	var renamed entities.Summary
	person.call("PATCH", "/v1/entities/kyring", map[string]string{"name": "Kyring CRM"}, http.StatusOK, &renamed)
	if renamed.Name != "Kyring CRM" || renamed.Slug != "kyring" {
		t.Fatalf("PATCH = %+v, want the new name at the same slug", renamed)
	}
	person.call("PATCH", "/v1/entities/nope", map[string]string{"name": "x"}, http.StatusNotFound, nil)
}

func json404(r reply) bool {
	return r.header.Get("Content-Type") == "application/json"
}

// A new instance's first entity takes its brand's name when onboarding
// saves the brand step, and is reachable at the new slug at once.
func TestTheFirstEntityTakesItsBrandsName(t *testing.T) {
	c := newServer(t)
	_, agent := login(t, c)
	agent.call("PUT", "/v1/settings", checkstest.Settings(), http.StatusOK, nil)
	agent.call("PUT", "/v1/onboarding/steps/brand", nil, http.StatusOK, nil)
	var onboarding config.Onboarding
	agent.call("GET", "/e/northwind/v1/onboarding", nil, http.StatusOK, &onboarding)
	if !onboarding.Steps[0].Saved {
		t.Fatalf("onboarding at /e/northwind = %+v, want the brand step saved", onboarding)
	}
	var list []entities.Summary
	agent.call("GET", "/v1/entities", nil, http.StatusOK, &list)
	if len(list) != 1 || list[0].Name != "Northwind" || list[0].Slug != "northwind" || !list[0].Default {
		t.Fatalf("GET /v1/entities = %+v, want the default named Northwind", list)
	}
}

// A browser opening a page under a mistyped slug gets the app, which says
// there is no entity there and offers a way back. API paths stay JSON.
func TestAPageUnderAnUnknownSlugLoadsTheApp(t *testing.T) {
	c := newServer(t)
	page := map[string]string{"Accept": "text/html,application/xhtml+xml,*/*;q=0.8"}
	for _, path := range []string{"/e/nope/settings", "/e/nope", "/e/nope/ai/questions?range=30d"} {
		if r := c.raw("GET", path, nil, page); r.code != http.StatusOK || !strings.HasPrefix(r.header.Get("Content-Type"), "text/html") || !bytes.Contains(r.body, []byte(`id="root"`)) {
			t.Errorf("a page load of %s = %d %s %q, want the app", path, r.code, r.header.Get("Content-Type"), r.body)
		}
	}
	for _, tc := range []struct {
		path   string
		header map[string]string
	}{
		{"/e/nope/v1/brands", page},
		{"/e/nope/mcp", page},
		{"/e/nope/settings", map[string]string{"Accept": "application/json"}},
	} {
		if r := c.raw("GET", tc.path, nil, tc.header); r.code != http.StatusUnauthorized || !json404(r) {
			t.Errorf("GET %s with Accept %q = %d %s, want a JSON 401", tc.path, tc.header["Accept"], r.code, r.header.Get("Content-Type"))
		}
	}
}

// Only an admin signed in deletes an entity, never the first. It is gone
// from every path at once, its slug is free, and once its tables are
// dropped the first entity's rows are exactly what they were.
func TestDeletingAnEntity(t *testing.T) {
	ctx := context.Background()
	c := newServer(t)
	person, agent := login(t, c)
	var inv struct{ Token string }
	person.call("POST", "/v1/invites", map[string]string{"email": "ed@example.com", "role": "editor"}, http.StatusCreated, &inv)
	editor := c.signup(map[string]string{"invite_token": inv.Token})

	var kyring entities.Summary
	person.call("POST", "/v1/entities", map[string]string{"name": "Kyring"}, http.StatusCreated, &kyring)
	agent.call("POST", "/e/kyring/v1/brands", map[string]any{"name": "Contoso", "kind": "competitor"}, http.StatusOK, nil)
	agent.call("POST", "/v1/brands", map[string]any{"name": "Fabrikam", "kind": "competitor"}, http.StatusOK, nil)
	var before []string
	if err := c.db.QueryRow(ctx, `SELECT array_agg(name ORDER BY id) FROM brand`).Scan(&before); err != nil {
		t.Fatal(err)
	}

	editor.call("DELETE", "/v1/entities/kyring", nil, http.StatusForbidden, nil)
	agent.call("DELETE", "/v1/entities/kyring", nil, http.StatusForbidden, nil)
	person.call("DELETE", "/v1/entities/default", nil, http.StatusConflict, nil)
	person.call("DELETE", "/v1/entities/nope", nil, http.StatusNotFound, nil)
	person.call("DELETE", "/v1/entities/kyring", nil, http.StatusNoContent, nil)
	person.call("DELETE", "/v1/entities/kyring", nil, http.StatusNotFound, nil)

	for _, path := range []string{"/e/kyring/v1/brands", "/e/kyring/mcp"} {
		if r := agent.do("GET", path, nil); r.code != http.StatusNotFound || !json404(r) {
			t.Errorf("GET %s after deleting = %d %s, want a JSON 404", path, r.code, r.body)
		}
	}
	if r := agent.do("POST", "/e/kyring/v1/brands", map[string]any{"name": "Northwind", "kind": "competitor"}); r.code != http.StatusNotFound {
		t.Errorf("POST /e/kyring/v1/brands after deleting = %d %s, want 404", r.code, r.body)
	}
	var list []entities.Summary
	agent.call("GET", "/v1/entities", nil, http.StatusOK, &list)
	if len(list) != 1 || !list[0].Default {
		t.Fatalf("GET /v1/entities = %+v, want the default only", list)
	}

	var again entities.Summary
	person.call("POST", "/v1/entities", map[string]string{"name": "Kyring"}, http.StatusCreated, &again)
	if again.ID == kyring.ID || again.Slug != "kyring" {
		t.Fatalf("POST /v1/entities at the deleted slug = %+v, want a new entity at kyring", again)
	}
	if got := brandNames(t, agent, "/e/kyring/v1/brands"); len(got) != 0 {
		t.Fatalf("the new kyring's brands = %v, want none of the deleted one's", got)
	}

	if _, err := c.db.Exec(ctx, `UPDATE entity SET deleted_at = now() - interval '1 day' WHERE id = $1`, kyring.ID); err != nil {
		t.Fatal(err)
	}
	if err := c.reg.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	var left int
	if err := c.db.QueryRow(ctx, `SELECT count(*) FROM pg_namespace WHERE nspname = $1`, fmt.Sprintf("entity_%d", kyring.ID)).Scan(&left); err != nil || left != 0 {
		t.Fatalf("the deleted entity's schema after the sweep: %d %v, want gone", left, err)
	}
	var after []string
	if err := c.db.QueryRow(ctx, `SELECT array_agg(name ORDER BY id) FROM brand`).Scan(&after); err != nil || !slices.Equal(after, before) {
		t.Fatalf("the first entity's brands went from %v to %v %v, want unchanged", before, after, err)
	}
	if got := brandNames(t, agent, "/v1/brands"); !slices.Contains(got, "Fabrikam") || slices.Contains(got, "Contoso") {
		t.Fatalf("GET /v1/brands = %v, want the first entity's Fabrikam and not Contoso", got)
	}
	agent.call("GET", "/e/kyring/v1/brands", nil, http.StatusOK, nil)
}
