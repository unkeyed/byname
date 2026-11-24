package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/unkeyed/byname/internal/search"
	"github.com/unkeyed/byname/internal/search/searchtest"
	"github.com/unkeyed/byname/internal/secretbox"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/unkeyed/byname/internal/app"
	"github.com/unkeyed/byname/internal/auth"
	"github.com/unkeyed/byname/internal/authtest"
	"github.com/unkeyed/byname/internal/changes"
	"github.com/unkeyed/byname/internal/changes/githubtest"
	"github.com/unkeyed/byname/internal/checks"
	"github.com/unkeyed/byname/internal/config"
	"github.com/unkeyed/byname/internal/core"
	"github.com/unkeyed/byname/internal/discovery"
	"github.com/unkeyed/byname/internal/engines/tregtest"
	"github.com/unkeyed/byname/internal/instance"
	"github.com/unkeyed/byname/internal/logos"
	"github.com/unkeyed/byname/internal/mcpserver"
	"github.com/unkeyed/byname/internal/notify"
	"github.com/unkeyed/byname/internal/notify/notifytest"
	"github.com/unkeyed/byname/internal/results"
	"github.com/unkeyed/byname/internal/social"
	"github.com/unkeyed/byname/internal/testdb"
)

const (
	adminEmail = "admin@example.com"
	testOrigin = "http://localhost:8080"
)

// client calls a test server as a person (session cookie) or an agent
// (API key), or anonymously when both are empty.
type client struct {
	t          *testing.T
	base       string
	origin     string // PUBLIC_URL, which passkeys are bound to
	session    string
	pending    string
	key        string
	setupToken string
	passkey    *authtest.Passkey
	db         *pgxpool.Pool
	svc        app.Services
	treg       *tregtest.Server
	resend     *notifytest.Resend
	google     *searchtest.Google
	bing       *searchtest.Bing
	github     *githubtest.Server
}

type reply struct {
	code   int
	header http.Header
	body   []byte
}

func (c client) do(method, path string, body any) reply {
	c.t.Helper()
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			c.t.Fatal(err)
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.base+path, r)
	if err != nil {
		c.t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.key != "" {
		req.Header.Set("Authorization", "Bearer "+c.key)
	}
	if c.session != "" {
		req.AddCookie(&http.Cookie{Name: auth.SessionCookie, Value: c.session})
	}
	if c.pending != "" {
		req.AddCookie(&http.Cookie{Name: auth.PendingCookie, Value: c.pending})
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		c.t.Fatal(err)
	}
	return reply{code: res.StatusCode, header: res.Header, body: b}
}

// call fails the test unless the status is want, then decodes into out.
func (c client) call(method, path string, body any, want int, out any) reply {
	c.t.Helper()
	r := c.do(method, path, body)
	if r.code != want {
		c.t.Fatalf("%s %s = %d %s, want %d", method, path, r.code, bytes.TrimSpace(r.body), want)
	}
	if out != nil {
		if err := json.Unmarshal(r.body, out); err != nil {
			c.t.Fatalf("%s %s: decode %s: %v", method, path, r.body, err)
		}
	}
	noNullLists(c.t, method+" "+path, r.body)
	return r
}

// listKeys are the JSON fields that hold arrays. None may be null.
var listKeys = []string{"scopes", "providers", "passkeys", "identities", "sessions"}

func noNullLists(t *testing.T, what string, body []byte) {
	t.Helper()
	if len(bytes.TrimSpace(body)) == 0 {
		return
	}
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		return // not JSON (openapi.yaml)
	}
	if v == nil {
		t.Errorf("%s: body is null, want [] or an object", what)
	}
	var walk func(path string, v any)
	walk = func(path string, v any) {
		switch v := v.(type) {
		case map[string]any:
			for k, child := range v {
				if child == nil && slices.Contains(listKeys, k) {
					t.Errorf("%s: %s.%s is null, want []", what, path, k)
				}
				walk(path+"."+k, child)
			}
		case []any:
			for i, child := range v {
				walk(fmt.Sprintf("%s[%d]", path, i), child)
			}
		}
	}
	walk("$", v)
}

func newServer(t *testing.T, mod ...func(*auth.Config)) client {
	t.Helper()
	srv := httptest.NewUnstartedServer(nil)
	return serve(t, srv, testOrigin, mod...)
}

// serve starts srv as Byname with PUBLIC_URL publicURL, mounting /mcp the
// way main does.
func serve(t *testing.T, srv *httptest.Server, publicURL string, mod ...func(*auth.Config)) client {
	t.Helper()
	pool := testdb.New(t)
	u, _ := url.Parse(publicURL)
	cfg := auth.Config{PublicURL: u, Secrets: secretbox.ForTest()}
	for _, m := range mod {
		m(&cfg)
	}
	users, err := auth.NewUsers(pool, cfg)
	if err != nil {
		t.Fatal(err)
	}
	token, err := users.PrepareSetup(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	authn := &auth.Authenticator{Keys: auth.NewKeyStore(pool, auth.UnkeyConfig{}), Users: users, OAuth: auth.NewOAuthServer(pool, u)}
	treg := tregtest.New(t)
	settings := config.New(pool, config.Options{Secrets: cfg.Secrets, TregBaseURL: treg.URL})
	res := results.New(pool)
	resend, resendSrv := notifytest.NewResend(t, resendKey)
	alerts := notify.New(pool, res, notify.Options{Secrets: cfg.Secrets, PublicURL: u, ResendBaseURL: resendSrv.URL})
	google := searchtest.NewGoogle(t)
	bing := searchtest.NewBing(t, bingKey)
	traffic := search.New(pool, search.Options{Secrets: cfg.Secrets, BaseURLs: map[search.Provider]string{search.Google: google.URL, search.Bing: bing.URL},
		GoogleClient: search.OAuthClient{ID: "test-client", Secret: "test-secret"}, PublicURL: u, Terms: settings})
	github := githubtest.New(t, "northwind", "site", githubToken)
	site := changes.New(pool, traffic, changes.Options{Secrets: cfg.Secrets, Alerts: alerts, GitHubBaseURL: github.URL})
	mentions := social.New(pool, settings, social.Options{Alerts: alerts, BaseURLs: map[social.Network]string{social.Bluesky: fakeBluesky(t)}})
	found := discovery.New(pool, settings, discovery.Options{})
	svc := app.Services{Instance: instance.New("test", pool, settings, mentions, found), Config: settings,
		Checks: checks.New(pool, settings, checks.Options{Alerts: alerts, Finder: found}), Results: res, Notify: alerts, Search: traffic, Changes: site, Social: mentions,
		Discovery: found, Logos: logos.New(pool, settings, logos.Options{})}
	mux := NewMux(svc, authn, slog.New(slog.DiscardHandler))
	mux.Handle("/mcp", mcpserver.Handler(svc, authn.OAuth, "test"))
	srv.Config.Handler = mux
	srv.Start()
	t.Cleanup(srv.Close)
	return client{t: t, base: srv.URL, origin: publicURL, setupToken: token, db: pool, svc: svc, treg: treg, resend: resend, google: google, bing: bing, github: github}
}

func cookieValue(r reply, name string) string {
	for _, ck := range (&http.Response{Header: r.header}).Cookies() {
		if ck.Name == name && ck.MaxAge >= 0 {
			return ck.Value
		}
	}
	return ""
}

type ceremony struct {
	Handle  string          `json:"handle"`
	Options json.RawMessage `json:"options"`
}

func (c client) signup(body map[string]string) client {
	c.t.Helper()
	var begun ceremony
	c.call("POST", "/auth/signup/passkey/begin", body, http.StatusOK, &begun)
	pk, cred, err := authtest.NewPasskey(c.origin, begun.Options)
	if err != nil {
		c.t.Fatal(err)
	}
	var res struct{ Status string }
	r := c.call("POST", "/auth/signup/passkey/finish", map[string]any{"handle": begun.Handle, "credential": cred}, http.StatusOK, &res)
	if res.Status != "signed_in" {
		c.t.Fatalf("signup answered %q, want signed_in", res.Status)
	}
	c.session, c.passkey = cookieValue(r, auth.SessionCookie), pk
	if c.session == "" {
		c.t.Fatal("signup set no session cookie")
	}
	return c
}

func (c client) passkeyLogin(pk *authtest.Passkey) client {
	c.t.Helper()
	var begun ceremony
	c.call("POST", "/auth/passkey/begin", nil, http.StatusOK, &begun)
	cred, err := pk.Sign(begun.Options)
	if err != nil {
		c.t.Fatal(err)
	}
	r := c.call("POST", "/auth/passkey/finish", map[string]any{"handle": begun.Handle, "credential": cred}, http.StatusOK, nil)
	c.session, c.passkey = cookieValue(r, auth.SessionCookie), pk
	return c
}

func login(t *testing.T, c client) (person, agent client) {
	t.Helper()
	person = c.signup(map[string]string{"setup_token": c.setupToken, "email": adminEmail})
	var key struct {
		Key    string `json:"key"`
		Prefix string `json:"prefix"`
	}
	person.call("POST", "/v1/keys", map[string]any{"name": "test agent", "scopes": core.Scopes}, http.StatusCreated, &key)
	if !strings.HasPrefix(key.Key, key.Prefix) || key.Prefix == "" {
		t.Fatalf("created key %q with prefix %q", key.Key, key.Prefix)
	}
	agent = c
	agent.key = key.Key
	return person, agent
}

func TestErrorStatuses(t *testing.T) {
	c := newServer(t)
	person, agent := login(t, c)

	cases := []struct {
		name   string
		as     client
		method string
		path   string
		body   any
		want   int
	}{
		{"no credentials", c, "GET", "/v1/status", nil, http.StatusUnauthorized},
		{"unknown key", client{t: t, base: c.base, key: "byn_nope"}, "GET", "/v1/status", nil, http.StatusUnauthorized},
		{"unknown session", client{t: t, base: c.base, session: "nope"}, "GET", "/v1/status", nil, http.StatusUnauthorized},
		{"agent on an admin route", agent, "GET", "/v1/keys", nil, http.StatusForbidden},
		{"agent mints a key", agent, "POST", "/v1/keys", map[string]any{"name": "x", "scopes": core.Scopes}, http.StatusForbidden},
		{"agent on a session route", agent, "GET", "/v1/me", nil, http.StatusForbidden},
		{"unknown body field", person, "POST", "/v1/invites", map[string]string{"emial": "a@b.co", "role": "viewer"}, http.StatusBadRequest},
		{"empty body", person, "POST", "/v1/invites", nil, http.StatusBadRequest},
		{"bad email", person, "POST", "/v1/invites", map[string]string{"email": "nope", "role": "viewer"}, http.StatusBadRequest},
		{"unknown role", person, "POST", "/v1/invites", map[string]string{"email": "grace@example.com", "role": "owner"}, http.StatusBadRequest},
		{"invite an existing user", person, "POST", "/v1/invites", map[string]string{"email": adminEmail, "role": "editor"}, http.StatusConflict},
		{"key without scopes", person, "POST", "/v1/keys", map[string]any{"name": "x", "scopes": []string{}}, http.StatusBadRequest},
		{"key with an unknown scope", person, "POST", "/v1/keys", map[string]any{"name": "x", "scopes": []string{"everything"}}, http.StatusBadRequest},
		{"bad user id", person, "PATCH", "/v1/users/abc", map[string]string{"role": "viewer"}, http.StatusNotFound},
		{"unknown /v1 route", agent, "GET", "/v1/nope", nil, http.StatusNotFound},
		{"wrong method", agent, "PUT", "/v1/status", map[string]string{}, http.StatusMethodNotAllowed},
	}
	for _, tc := range cases {
		r := tc.as.do(tc.method, tc.path, tc.body)
		if r.code != tc.want {
			t.Errorf("%s: %s %s = %d %s, want %d", tc.name, tc.method, tc.path, r.code, bytes.TrimSpace(r.body), tc.want)
			continue
		}
		var e struct{ Error string }
		if err := json.Unmarshal(r.body, &e); err != nil || e.Error == "" {
			t.Errorf("%s: body %s, want {\"error\": ...}", tc.name, r.body)
		}
	}
}

func TestSignInAndOut(t *testing.T) {
	c := newServer(t)
	var opts auth.Options
	c.call("GET", "/auth/options", nil, http.StatusOK, &opts)
	if !opts.SetupRequired || opts.PasswordLogin || len(opts.Providers) != 0 {
		t.Fatalf("options before setup = %+v", opts)
	}
	var setup struct{ Recovery bool }
	c.call("GET", "/auth/setup/"+c.setupToken, nil, http.StatusOK, &setup)
	if setup.Recovery {
		t.Fatal("the first-account setup link reports itself as a recovery link")
	}

	person, _ := login(t, c)
	c.call("GET", "/auth/options", nil, http.StatusOK, &opts)
	if opts.SetupRequired {
		t.Fatal("options still ask for setup after the first user")
	}
	c.call("GET", "/auth/setup/"+c.setupToken, nil, http.StatusNotFound, nil)
	c.call("POST", "/auth/signup/passkey/begin", map[string]string{"setup_token": c.setupToken, "email": "eve@example.com"}, http.StatusNotFound, nil)

	var me core.Actor
	person.call("GET", "/v1/me", nil, http.StatusOK, &me)
	if me.Via != core.ViaSession || me.Name != adminEmail || me.Role != core.Admin || !slices.Equal(me.Scopes, core.Scopes) {
		t.Fatalf("me = %+v, want the setup link's admin with every scope", me)
	}
	person.call("POST", "/auth/logout", nil, http.StatusNoContent, nil)
	person.call("GET", "/v1/me", nil, http.StatusUnauthorized, nil)

	again := c.passkeyLogin(person.passkey)
	again.call("GET", "/v1/me", nil, http.StatusOK, nil)
	c.call("POST", "/auth/password", map[string]string{"email": adminEmail, "password": "anything-at-all"}, http.StatusNotFound, nil)
}

func TestPasswordNeedsASecondFactor(t *testing.T) {
	c := newServer(t, func(cfg *auth.Config) { cfg.PasswordLogin = true })
	person, _ := login(t, c)
	person.call("PUT", "/v1/account/password", map[string]string{"password": "correct-horse-battery"}, http.StatusNoContent, nil)

	var res struct {
		Status        string   `json:"status"`
		SecondFactors []string `json:"second_factors"`
	}
	r := c.call("POST", "/auth/password", map[string]string{"email": adminEmail, "password": "correct-horse-battery"}, http.StatusOK, &res)
	if res.Status != "second_factor" || cookieValue(r, auth.SessionCookie) != "" {
		t.Fatalf("password sign-in answered %+v with a session cookie %q; a password alone must never sign in", res, cookieValue(r, auth.SessionCookie))
	}
	pending := c
	pending.pending = cookieValue(r, auth.PendingCookie)
	pending.call("GET", "/auth/second-factor", nil, http.StatusOK, &res)
	if !slices.Equal(res.SecondFactors, []string{"passkey"}) {
		t.Fatalf("second factors %v, want passkey", res.SecondFactors)
	}
	var begun ceremony
	pending.call("POST", "/auth/second-factor/passkey/begin", nil, http.StatusOK, &begun)
	cred, err := person.passkey.Sign(begun.Options)
	if err != nil {
		t.Fatal(err)
	}
	r = pending.call("POST", "/auth/second-factor/passkey/finish", map[string]any{"handle": begun.Handle, "credential": cred}, http.StatusOK, &res)
	if res.Status != "signed_in" || cookieValue(r, auth.SessionCookie) == "" {
		t.Fatalf("second factor answered %+v", res)
	}

	wrong := map[string]string{"email": adminEmail, "password": "wrong-password-123"}
	for range 5 {
		c.call("POST", "/auth/password", wrong, http.StatusUnauthorized, nil)
	}
	r = c.call("POST", "/auth/password", wrong, http.StatusTooManyRequests, nil)
	if r.header.Get("Retry-After") == "" || r.header.Get("Retry-After") == "0" {
		t.Fatalf("429 Retry-After = %q, want whole seconds", r.header.Get("Retry-After"))
	}
}

func TestInviteFlow(t *testing.T) {
	c := newServer(t)
	person, _ := login(t, c)
	var inv struct{ Token string }
	person.call("POST", "/v1/invites", map[string]string{"email": "grace@example.com", "role": "admin"}, http.StatusCreated, &inv)
	var info auth.InviteStatus
	c.call("GET", "/auth/invites/"+inv.Token, nil, http.StatusOK, &info)
	if !info.Live || info.Invitation == nil || *info.Invitation != (auth.Invitation{Email: "grace@example.com", Role: core.Admin}) {
		t.Fatalf("invite = %+v", info)
	}

	grace := c.signup(map[string]string{"invite_token": inv.Token, "name": "Grace"})
	var me core.Actor
	grace.call("GET", "/v1/me", nil, http.StatusOK, &me)
	if me.Name != "Grace" || me.Role != core.Admin {
		t.Fatalf("invited user's session is %+v, want Grace as admin", me)
	}
	var key struct{ Key string }
	grace.call("POST", "/v1/keys", map[string]any{"name": "grace's agent", "scopes": []string{"results:read"}}, http.StatusCreated, &key)
	gracesAgent := c
	gracesAgent.key = key.Key
	gracesAgent.call("GET", "/v1/status", nil, http.StatusOK, nil)
	c.call("POST", "/auth/signup/passkey/begin", map[string]string{"invite_token": inv.Token, "name": "Grace"}, http.StatusNotFound, nil)
	var gone map[string]any
	c.call("GET", "/auth/invites/"+inv.Token, nil, http.StatusOK, &gone)
	if len(gone) != 1 || gone["live"] != false {
		t.Fatalf("used invite = %v, want only live false", gone)
	}

	var users []auth.User
	person.call("GET", "/v1/users", nil, http.StatusOK, &users)
	if len(users) != 2 || users[1].Name != "Grace" {
		t.Fatalf("users = %+v, want the admin and Grace", users)
	}

	person.call("DELETE", fmt.Sprintf("/v1/users/%d", users[1].ID), nil, http.StatusNoContent, nil)
	grace.call("GET", "/v1/me", nil, http.StatusUnauthorized, nil)
	gracesAgent.call("GET", "/v1/status", nil, http.StatusUnauthorized, nil)
	person.call("DELETE", fmt.Sprintf("/v1/users/%d", users[0].ID), nil, http.StatusConflict, nil)
}

func TestAccountChangesNeedARecentSignIn(t *testing.T) {
	c := newServer(t)
	person, _ := login(t, c)
	var account auth.Account
	person.call("GET", "/v1/account", nil, http.StatusOK, &account)
	if !account.Recent || len(account.Passkeys) != 1 || len(account.Sessions) != 1 || !account.Sessions[0].Current {
		t.Fatalf("account = %+v", account)
	}

	if _, err := c.db.Exec(context.Background(), `UPDATE sessions SET authenticated_at = now() - interval '1 hour'`); err != nil {
		t.Fatal(err)
	}
	r := person.call("POST", "/v1/account/totp/begin", nil, http.StatusForbidden, nil)
	var e struct{ Code string }
	if err := json.Unmarshal(r.body, &e); err != nil || e.Code != "reauth_required" {
		t.Fatalf("stale session changing sign-in methods: %s, want code reauth_required", r.body)
	}

	var begun ceremony
	person.call("POST", "/v1/account/reauth/passkey/begin", nil, http.StatusOK, &begun)
	cred, err := person.passkey.Sign(begun.Options)
	if err != nil {
		t.Fatal(err)
	}
	person.call("POST", "/v1/account/reauth/passkey", map[string]any{"handle": begun.Handle, "credential": cred}, http.StatusNoContent, nil)
	person.call("POST", "/v1/account/totp/begin", nil, http.StatusOK, nil)
}

func TestTeamAndKeyChangesNeedARecentSignIn(t *testing.T) {
	c := newServer(t)
	person, _ := login(t, c)
	var inv struct{ Token string }
	person.call("POST", "/v1/invites", map[string]string{"email": "grace@example.com", "role": "editor"}, http.StatusCreated, &inv)
	c.signup(map[string]string{"invite_token": inv.Token, "name": "Grace"})
	var users []auth.User
	person.call("GET", "/v1/users", nil, http.StatusOK, &users)

	if _, err := c.db.Exec(context.Background(), `UPDATE sessions SET authenticated_at = now() - interval '1 hour'`); err != nil {
		t.Fatal(err)
	}
	changes := []struct {
		method, path string
		body         any
		want         int
	}{
		{"POST", "/v1/keys", map[string]any{"name": "agent", "scopes": core.Scopes}, http.StatusCreated},
		{"POST", "/v1/invites", map[string]string{"email": "hal@example.com", "role": "viewer"}, http.StatusCreated},
		{"PATCH", fmt.Sprintf("/v1/users/%d", users[1].ID), map[string]string{"role": "viewer"}, http.StatusNoContent},
		{"DELETE", fmt.Sprintf("/v1/users/%d", users[1].ID), nil, http.StatusNoContent},
	}
	for _, ch := range changes {
		r := person.call(ch.method, ch.path, ch.body, http.StatusForbidden, nil)
		var e struct{ Code string }
		if err := json.Unmarshal(r.body, &e); err != nil || e.Code != "reauth_required" {
			t.Errorf("%s %s on a stale session: %s, want code reauth_required", ch.method, ch.path, r.body)
		}
	}
	person.call("GET", "/v1/users", nil, http.StatusOK, &users)
	if len(users) != 2 {
		t.Fatalf("a refused removal still removed someone: %+v", users)
	}

	var begun ceremony
	person.call("POST", "/v1/account/reauth/passkey/begin", nil, http.StatusOK, &begun)
	cred, err := person.passkey.Sign(begun.Options)
	if err != nil {
		t.Fatal(err)
	}
	person.call("POST", "/v1/account/reauth/passkey", map[string]any{"handle": begun.Handle, "credential": cred}, http.StatusNoContent, nil)
	for _, ch := range changes {
		person.call(ch.method, ch.path, ch.body, ch.want, nil)
	}
}

func TestOAuthSignInSetsCookiesAndRedirects(t *testing.T) {
	iss := authtest.NewIssuer()
	defer iss.Close()
	c := newServer(t, func(cfg *auth.Config) {
		cfg.Providers = []auth.ProviderConfig{{ID: "oidc", Name: "Acme SSO", ClientID: "byname", ClientSecret: "s3cret", Issuer: iss.URL}}
	})
	person, _ := login(t, c)
	jar, _ := cookiejar.New(nil)
	browser := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	get := func(u string) *http.Response {
		t.Helper()
		res, err := browser.Get(u)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res
	}
	base, _ := url.Parse(c.base)
	toServer := func(loc string) string {
		u, err := url.Parse(loc)
		if err != nil {
			t.Fatal(err)
		}
		u.Scheme, u.Host = base.Scheme, base.Host
		return u.String()
	}

	crossSite, err := http.NewRequest("GET", c.base+"/auth/oauth/oidc/start?intent=reauth", nil)
	if err != nil {
		t.Fatal(err)
	}
	crossSite.Header.Set("Sec-Fetch-Site", "cross-site")
	refused, err := browser.Do(crossSite)
	if err != nil {
		t.Fatal(err)
	}
	refused.Body.Close()
	if loc := refused.Header.Get("Location"); !strings.HasPrefix(loc, "/settings/account?error=") {
		t.Fatalf("a reauth flow started by another site redirected to %q, want an error on the account page", loc)
	}

	iss.SetNext(authtest.Identity{Subject: "mallory", Email: "mallory@evil.com", EmailVerified: true})
	res := get(c.base + "/auth/oauth/oidc/start?next=/ai/questions")
	res = get(res.Header.Get("Location"))
	res = get(toServer(res.Header.Get("Location")))
	if loc := res.Header.Get("Location"); !strings.HasPrefix(loc, "/login?error=") {
		t.Fatalf("a stranger's callback redirected to %q, want /login?error=", loc)
	}

	iss.SetNext(authtest.Identity{Subject: "admin", Email: adminEmail, EmailVerified: true})
	res = get(c.base + "/auth/oauth/oidc/start?next=/ai/questions")
	if res.StatusCode != http.StatusSeeOther || !strings.HasPrefix(res.Header.Get("Location"), iss.URL+"/authorize") {
		t.Fatalf("start = %d to %q", res.StatusCode, res.Header.Get("Location"))
	}
	callback := toServer(get(res.Header.Get("Location")).Header.Get("Location"))

	stranger := &http.Client{CheckRedirect: browser.CheckRedirect}
	other, err := stranger.Get(callback)
	if err != nil {
		t.Fatal(err)
	}
	other.Body.Close()
	if loc := other.Header.Get("Location"); !strings.HasPrefix(loc, "/login?error=") {
		t.Fatalf("callback in another browser redirected to %q, want an error", loc)
	}

	res = get(callback)
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/login?second_factor=1&next=%2Fai%2Fquestions" {
		t.Fatalf("callback = %d to %q, want 303 to the second factor step: the admin has a passkey and this login matched by email", res.StatusCode, res.Header.Get("Location"))
	}
	jarCookie := func(name string) string {
		for _, ck := range jar.Cookies(base) {
			if ck.Name == name {
				return ck.Value
			}
		}
		return ""
	}
	if jarCookie(auth.SessionCookie) != "" {
		t.Fatal("the callback set a session before the passkey step")
	}
	pending := c
	pending.pending = jarCookie(auth.PendingCookie)
	var step struct {
		Status        string   `json:"status"`
		SecondFactors []string `json:"second_factors"`
	}
	pending.call("GET", "/auth/second-factor", nil, http.StatusOK, &step)
	if !slices.Equal(step.SecondFactors, []string{"passkey"}) {
		t.Fatalf("second factors %v, want passkey", step.SecondFactors)
	}
	var begun ceremony
	pending.call("POST", "/auth/second-factor/passkey/begin", nil, http.StatusOK, &begun)
	cred, err := person.passkey.Sign(begun.Options)
	if err != nil {
		t.Fatal(err)
	}
	r := pending.call("POST", "/auth/second-factor/passkey/finish", map[string]any{"handle": begun.Handle, "credential": cred}, http.StatusOK, &step)
	signedIn := c
	signedIn.session = cookieValue(r, auth.SessionCookie)
	var me core.Actor
	signedIn.call("GET", "/v1/me", nil, http.StatusOK, &me)
	if me.Name != adminEmail {
		t.Fatalf("OIDC sign-in is %+v", me)
	}
}

// downKeys is a key store that cannot reach its backend, like Unkey during
// an outage.
type downKeys struct{ auth.KeyStore }

func (downKeys) Verify(context.Context, string) (auth.VerifiedKey, error) {
	return auth.VerifiedKey{}, errors.New("unkey keys.verifyKey: HTTP 502")
}

func TestKeyStoreOutageIs503(t *testing.T) {
	pool := testdb.New(t)
	u, _ := url.Parse(testOrigin)
	users, err := auth.NewUsers(pool, auth.Config{PublicURL: u, Secrets: secretbox.ForTest()})
	if err != nil {
		t.Fatal(err)
	}
	authn := &auth.Authenticator{Keys: downKeys{}, Users: users, OAuth: auth.NewOAuthServer(pool, u)}
	srv := httptest.NewServer(NewMux(app.Services{Instance: instance.New("test", nil, nil, nil, nil)}, authn, slog.New(slog.DiscardHandler)))
	t.Cleanup(srv.Close)
	r := client{t: t, base: srv.URL, key: "byn_whatever"}.do("GET", "/v1/status", nil)
	var e struct {
		Error     string `json:"error"`
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal(r.body, &e); err != nil || r.code != http.StatusServiceUnavailable || e.RequestID == "" {
		t.Fatalf("key store down: %d %s, want 503 with a request_id", r.code, r.body)
	}
}

func TestSettings(t *testing.T) {
	c := newServer(t)
	person, agent := login(t, c)

	var st instance.Status
	agent.call("GET", "/v1/status", nil, http.StatusOK, &st)
	if st != (instance.Status{Version: "test"}) {
		t.Fatalf("status = %+v, want version test and onboarded false", st)
	}

	var keys []auth.Key
	person.call("GET", "/v1/keys", nil, http.StatusOK, &keys)
	if len(keys) != 1 || !slices.Equal(keys[0].Scopes, core.Scopes) {
		t.Fatalf("keys = %+v, want the one key with every scope", keys)
	}
	person.call("DELETE", "/v1/keys/"+keys[0].ID, nil, http.StatusNoContent, nil)
	agent.call("GET", "/v1/status", nil, http.StatusUnauthorized, nil)

	r := c.call("GET", "/v1/openapi.yaml", nil, http.StatusOK, nil)
	if !bytes.HasPrefix(r.body, []byte("openapi: 3.1")) {
		t.Fatalf("openapi.yaml starts %q", r.body[:min(len(r.body), 20)])
	}
}

func TestSecurityHeaders(t *testing.T) {
	const csp = "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; frame-ancestors 'none'; object-src 'none'; base-uri 'none'; form-action 'self'"
	for _, publicURL := range []string{"http://localhost:8080", "https://byname.example.com"} {
		u, _ := url.Parse(publicURL)
		users, err := auth.NewUsers(nil, auth.Config{PublicURL: u, Secrets: secretbox.ForTest()})
		if err != nil {
			t.Fatal(err)
		}
		mux := NewMux(app.Services{}, &auth.Authenticator{Users: users, OAuth: auth.NewOAuthServer(nil, u)}, slog.New(slog.DiscardHandler))
		mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "<!doctype html>") }))
		srv := httptest.NewServer(SecurityHeaders(u, mux))
		t.Cleanup(srv.Close)
		for _, path := range []string{"/", "/ai/questions", "/healthz", "/v1/nope"} {
			h := client{t: t, base: srv.URL}.do("GET", path, nil).header
			if h.Get("Content-Security-Policy") != csp || h.Get("X-Content-Type-Options") != "nosniff" ||
				h.Get("Referrer-Policy") != "same-origin" || h.Get("X-Frame-Options") != "DENY" {
				t.Errorf("%s %s: headers %v", publicURL, path, h)
			}
			if hsts := h.Get("Strict-Transport-Security"); (hsts != "") != (u.Scheme == "https") {
				t.Errorf("%s %s: Strict-Transport-Security %q", publicURL, path, hsts)
			}
			if path != "/" && path != "/ai/questions" && (h.Get("Cache-Control") != "no-store" || h.Get("Vary") != "Cookie, Authorization") {
				t.Errorf("%s %s: Cache-Control %q, Vary %q, want no-store varying on Cookie and Authorization", publicURL, path, h.Get("Cache-Control"), h.Get("Vary"))
			}
		}
	}
}
