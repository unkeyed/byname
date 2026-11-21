package httpapi

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/unkeyed/byname/internal/auth"
	"github.com/unkeyed/byname/internal/core"
)

// send posts an arbitrary body, which client.do cannot.
func (c client) send(method, path string, header http.Header, body io.Reader) reply {
	c.t.Helper()
	req, err := http.NewRequest(method, c.base+path, body)
	if err != nil {
		c.t.Fatal(err)
	}
	for k, v := range header {
		req.Header[k] = v
	}
	hc := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := hc.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return reply{code: res.StatusCode, header: res.Header, body: b}
}

type createdKeyReply struct {
	Key       string       `json:"key"`
	ID        string       `json:"id"`
	Scopes    []core.Scope `json:"scopes"`
	ExpiresAt *time.Time   `json:"expires_at"`
}

// A key reaches a scoped route only with that scope, and never an admin or
// session route, while any key can revoke itself.
func TestScopedKeys(t *testing.T) {
	c := newServer(t)
	person, reader := login(t, c)

	var k createdKeyReply
	person.call("POST", "/v1/keys", map[string]any{"name": "runner", "scopes": []string{"runs:write"}}, http.StatusCreated, &k)
	if !slices.Equal(k.Scopes, []core.Scope{core.RunsWrite}) {
		t.Fatalf("created key %+v, want runs:write only", k)
	}
	runner := c
	runner.key = k.Key

	reader.call("GET", "/v1/status", nil, http.StatusOK, nil)
	r := runner.do("GET", "/v1/status", nil)
	if r.code != http.StatusForbidden || !bytes.Contains(r.body, []byte("results:read")) {
		t.Fatalf("runs:write key on GET /v1/status = %d %s, want 403 naming results:read", r.code, r.body)
	}
	for _, path := range []string{"/v1/keys", "/v1/users", "/v1/apps", "/v1/me", "/v1/account"} {
		if r := reader.do("GET", path, nil); r.code != http.StatusForbidden {
			t.Errorf("a key with every scope on GET %s = %d %s, want 403", path, r.code, r.body)
		}
	}

	person.call("DELETE", "/v1/keys/current", nil, http.StatusBadRequest, nil)
	runner.call("DELETE", "/v1/keys/current", nil, http.StatusNoContent, nil)
	runner.call("GET", "/v1/status", nil, http.StatusUnauthorized, nil)
	reader.call("GET", "/v1/status", nil, http.StatusOK, nil)
}

// Keys, apps and the team are for admins. An editor or viewer signs in, sees
// their own account and the status, and is refused the rest.
func TestAdminRoutesNeedAnAdmin(t *testing.T) {
	c := newServer(t)
	admin, _ := login(t, c)
	join := func(email string, role core.Role) client {
		var inv struct{ Token string }
		admin.call("POST", "/v1/invites", map[string]string{"email": email, "role": string(role)}, http.StatusCreated, &inv)
		return c.signup(map[string]string{"invite_token": inv.Token, "name": email})
	}
	editor, viewer := join("ed@example.com", core.Editor), join("vi@example.com", core.Viewer)

	for _, p := range []client{editor, viewer} {
		var me core.Actor
		p.call("GET", "/v1/me", nil, http.StatusOK, &me)
		p.call("GET", "/v1/account", nil, http.StatusOK, nil)
		p.call("GET", "/v1/status", nil, http.StatusOK, nil)
		for _, tc := range []struct {
			method, path string
			body         any
		}{
			{"GET", "/v1/keys", nil},
			{"POST", "/v1/keys", map[string]any{"name": "x", "scopes": []string{"results:read"}}},
			{"GET", "/v1/apps", nil},
			{"GET", "/v1/users", nil},
			{"PATCH", fmt.Sprintf("/v1/users/%d", me.UserID), map[string]string{"role": "admin"}},
			{"POST", "/v1/invites", map[string]string{"email": "x@example.com", "role": "admin"}},
		} {
			if r := p.do(tc.method, tc.path, tc.body); r.code != http.StatusForbidden || !bytes.Contains(r.body, []byte("admin")) {
				t.Errorf("%s: %s %s = %d %s, want 403 naming admin", me.Role, tc.method, tc.path, r.code, r.body)
			}
		}
	}
	var viewerMe core.Actor
	viewer.call("GET", "/v1/me", nil, http.StatusOK, &viewerMe)
	if !slices.Equal(viewerMe.Scopes, []core.Scope{core.ResultsRead}) {
		t.Fatalf("viewer's scopes = %v, want results:read only", viewerMe.Scopes)
	}

	var users []auth.User
	admin.call("GET", "/v1/users", nil, http.StatusOK, &users)
	admin.call("PATCH", fmt.Sprintf("/v1/users/%d", viewerMe.UserID), map[string]string{"role": "admin"}, http.StatusNoContent, nil)
	viewer.call("GET", "/v1/keys", nil, http.StatusOK, nil)
	admin.call("PATCH", fmt.Sprintf("/v1/users/%d", users[0].ID), map[string]string{"role": "viewer"}, http.StatusNoContent, nil)
	viewer.call("PATCH", fmt.Sprintf("/v1/users/%d", viewerMe.UserID), map[string]string{"role": "editor"}, http.StatusConflict, nil)
}

func TestKeysExpire(t *testing.T) {
	c := newServer(t)
	person, _ := login(t, c)
	read := []string{"results:read"}
	past := time.Now().Add(-time.Hour)
	person.call("POST", "/v1/keys", map[string]any{"name": "old", "scopes": read, "expires_at": past}, http.StatusBadRequest, nil)
	person.call("POST", "/v1/keys", map[string]any{"name": "forever", "scopes": read, "expires_at": time.Now().AddDate(5, 0, 0)}, http.StatusBadRequest, nil)

	var k createdKeyReply
	soon := time.Now().Add(time.Hour)
	person.call("POST", "/v1/keys", map[string]any{"name": "ci", "scopes": read, "expires_at": soon}, http.StatusCreated, &k)
	if k.ExpiresAt == nil || k.ExpiresAt.Sub(soon).Abs() > time.Second {
		t.Fatalf("created key expires %v, want %v", k.ExpiresAt, soon)
	}
	agent := c
	agent.key = k.Key
	agent.call("GET", "/v1/status", nil, http.StatusOK, nil)

	if _, err := c.db.Exec(t.Context(), `UPDATE api_keys SET expires_at = now() - interval '1 second' WHERE id = $1::bigint`, k.ID); err != nil {
		t.Fatal(err)
	}
	agent.call("GET", "/v1/status", nil, http.StatusUnauthorized, nil)

	var keys []createdKeyReply
	person.call("GET", "/v1/keys", nil, http.StatusOK, &keys)
	listed := false
	for _, l := range keys {
		listed = listed || (l.ID == k.ID && l.ExpiresAt != nil)
	}
	if !listed {
		t.Fatalf("expired key missing from the list, or listed without expires_at: %+v", keys)
	}
}

func TestAuthorizeIsThrottledPerAddress(t *testing.T) {
	c := newServer(t)
	path := "/oauth/authorize?client_id=nope&response_type=code"
	for i := range 30 {
		if r := c.raw("GET", path, nil, nil); r.code != http.StatusFound {
			t.Fatalf("authorize %d = %d %s, want a redirect to the consent page", i+1, r.code, r.body)
		}
	}
	if r := c.raw("GET", path, nil, nil); r.code != http.StatusTooManyRequests {
		t.Fatalf("31st authorize = %d %s, want 429", r.code, r.body)
	}
}

func TestUnknownV1RoutesNeedCredentials(t *testing.T) {
	c := newServer(t)
	_, agent := login(t, c)
	c.call("GET", "/v1/nope", nil, http.StatusUnauthorized, nil)
	c.call("GET", "/v1/keys/", nil, http.StatusUnauthorized, nil)
	agent.call("GET", "/v1/nope", nil, http.StatusNotFound, nil)
}

// openapi.yaml is the same for everyone, so unlike every other response it
// may be cached, and a client holding the current ETag gets a 304.
func TestOpenAPIRevalidates(t *testing.T) {
	c := newServer(t)
	r := c.raw("GET", "/v1/openapi.yaml", nil, nil)
	etag := r.header.Get("ETag")
	if r.code != http.StatusOK || etag == "" || r.header.Get("Cache-Control") != "public, no-cache" || r.header.Get("Vary") != "" {
		t.Fatalf("openapi.yaml = %d, ETag %q, Cache-Control %q, Vary %q", r.code, etag, r.header.Get("Cache-Control"), r.header.Get("Vary"))
	}
	if r := c.raw("GET", "/v1/openapi.yaml", nil, map[string]string{"If-None-Match": etag}); r.code != http.StatusNotModified {
		t.Fatalf("openapi.yaml with its ETag = %d, want 304", r.code)
	}
}
