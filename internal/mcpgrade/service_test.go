package mcpgrade_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/unkeyed/byname/internal/core"
	"github.com/unkeyed/byname/internal/db"
	"github.com/unkeyed/byname/internal/mcpgrade"
	"github.com/unkeyed/byname/internal/secretbox"
	"github.com/unkeyed/byname/internal/testdb"
)

const token = "mcp_secret_northwind_0123456789"

type server struct {
	URL    string
	signIn bool

	mu     sync.Mutex
	auth   []string
	agents []string
}

func newServer(t *testing.T, signIn bool) *server {
	t.Helper()
	s := &server{signIn: signIn}
	srv := mcp.NewServer(&mcp.Implementation{Name: "northwind", Title: "Northwind", Version: "1.0.0"}, nil)
	mcp.AddTool(srv, &mcp.Tool{Name: "search", Description: "Searches Northwind's documentation and returns the best pages."},
		func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, struct{}, error) {
			t.Error("the grade called a tool")
			return nil, struct{}{}, nil
		})
	h := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, &mcp.StreamableHTTPOptions{Stateless: true})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.auth = append(s.auth, r.Header.Get("Authorization"))
		s.agents = append(s.agents, r.UserAgent())
		s.mu.Unlock()
		if s.signIn && r.Header.Get("Authorization") != "Bearer "+token {
			w.Header().Set("WWW-Authenticate", `Bearer realm="northwind"`)
			http.Error(w, "sign in", http.StatusUnauthorized)
			return
		}
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(ts.Close)
	s.URL = ts.URL + "/mcp"
	return s
}

func (s *server) sawBearer() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, a := range s.auth {
		if a == "Bearer "+token {
			return true
		}
	}
	return false
}

func registry(t *testing.T) string {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"servers":[]}`)
	}))
	t.Cleanup(ts.Close)
	return ts.URL
}

type env struct {
	t   *testing.T
	ctx context.Context
	db  *pgxpool.Pool
	svc *mcpgrade.Service
}

func newEnv(t *testing.T) *env {
	t.Helper()
	pool := testdb.New(t)
	return &env{t: t, ctx: context.Background(), db: pool, svc: mcpgrade.New(pool, mcpgrade.Options{
		Secrets: secretbox.ForTest(), AllowPrivate: true, UserAgent: "Byname/test", RegistryURL: registry(t),
	})}
}

func (e *env) sealed() string {
	e.t.Helper()
	var v *string
	if err := e.db.QueryRow(e.ctx, `SELECT mcp_server_token FROM instance_settings`).Scan(&v); err != nil {
		e.t.Fatal(err)
	}
	if v == nil {
		return ""
	}
	return *v
}

func TestURLMustBeAnEndpointWeMayReach(t *testing.T) {
	e := newEnv(t)
	for _, u := range []string{
		"mcp.example.com/mcp", "ftp://mcp.example.com/mcp", "http://mcp.example.com/mcp", "https://user:pass@mcp.example.com/mcp",
		"https://", "https://mcp.example.com/" + strings.Repeat("a", 2048),
	} {
		if _, err := e.svc.SetURL(e.ctx, u); !errors.Is(err, core.ErrInvalid) {
			t.Errorf("SetURL(%q) = %v, want ErrInvalid", u, err)
		}
	}
	for in, want := range map[string]string{
		"  https://MCP.Example.com/mcp#top ": "https://mcp.example.com/mcp",
		"http://127.0.0.1:8080/mcp":          "http://127.0.0.1:8080/mcp",
		"http://localhost:3000/mcp?x=1":      "http://localhost:3000/mcp?x=1",
	} {
		v, err := e.svc.SetURL(e.ctx, in)
		if err != nil || v.URL != want {
			t.Errorf("SetURL(%q) = %q, %v, want %q", in, v.URL, err, want)
		}
	}
}

func TestTokenIsSealedAndFollowsItsOrigin(t *testing.T) {
	e := newEnv(t)
	if _, err := e.svc.SetToken(e.ctx, token); !errors.Is(err, core.ErrInvalid) {
		t.Fatalf("SetToken without a URL = %v, want ErrInvalid", err)
	}
	if _, err := e.svc.SetURL(e.ctx, "https://mcp.northwind.example/mcp"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.SetToken(e.ctx, "two words"); !errors.Is(err, core.ErrInvalid) {
		t.Fatalf("SetToken with a space = %v, want ErrInvalid", err)
	}
	v, err := e.svc.SetToken(e.ctx, " "+token+"\n")
	if err != nil || !v.TokenSet {
		t.Fatalf("SetToken = %+v, %v", v, err)
	}
	if s := e.sealed(); strings.Contains(s, token) {
		t.Fatalf("the token is stored in the clear: %s", s)
	} else if got, err := secretbox.ForTest().Open(s, "mcpgrade:token"); err != nil || got != token {
		t.Fatalf("the stored token opens to %q, %v", got, err)
	}

	if v, _ := e.svc.SetURL(e.ctx, "https://mcp.northwind.example/v2/mcp"); !v.TokenSet {
		t.Error("a new path on the same origin dropped the token")
	}
	if v, _ := e.svc.SetURL(e.ctx, "https://mcp.northwind.example:8443/mcp"); v.TokenSet {
		t.Error("a URL on another port kept the token, which would send it to another server")
	}
	if _, err := e.svc.SetToken(e.ctx, token); err != nil {
		t.Fatal(err)
	}
	if v, _ := e.svc.SetURL(e.ctx, "https://evil.example/mcp"); v.TokenSet || e.sealed() != "" {
		t.Error("a URL on another host kept the token")
	}
	if _, err := e.svc.SetToken(e.ctx, token); err != nil {
		t.Fatal(err)
	}
	if v, _ := e.svc.SetToken(e.ctx, ""); v.TokenSet || e.sealed() != "" {
		t.Error("an empty token did not delete the stored one")
	}
	if _, err := e.svc.SetToken(e.ctx, token); err != nil {
		t.Fatal(err)
	}
	if v, _ := e.svc.SetURL(e.ctx, ""); v.URL != "" || v.TokenSet || e.sealed() != "" {
		t.Errorf("removing the server left %+v", v)
	}
}

func TestRunGradesAndStores(t *testing.T) {
	e := newEnv(t)
	if _, err := e.svc.Run(e.ctx); !errors.Is(err, core.ErrInvalid) {
		t.Fatalf("Run without a URL = %v, want ErrInvalid", err)
	}
	srv := newServer(t, true)
	if _, err := e.svc.SetURL(e.ctx, srv.URL); err != nil {
		t.Fatal(err)
	}
	v, err := e.svc.Run(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if v.Latest == nil || v.Latest.Partial != mcpgrade.NeedsToken || v.Latest.Server.TokenUsed || v.Latest.URL != srv.URL || len(v.History) != 1 {
		t.Fatalf("a grade behind sign-in without a token = %+v", v)
	}
	partial := v.Latest.Score

	if _, err := e.svc.SetToken(e.ctx, token); err != nil {
		t.Fatal(err)
	}
	if v, err = e.svc.Run(e.ctx); err != nil {
		t.Fatal(err)
	}
	if v.Latest.Partial != mcpgrade.Complete || !v.Latest.Server.TokenUsed || len(v.Latest.Tools) != 1 || v.Latest.Score <= partial {
		t.Fatalf("a grade with the token = partial %q, token used %v, %d tools, score %d after %d", v.Latest.Partial, v.Latest.Server.TokenUsed, len(v.Latest.Tools), v.Latest.Score, partial)
	}
	if !srv.sawBearer() {
		t.Error("the server never saw the token")
	}
	if len(v.History) != 2 || v.History[0].ID != v.Latest.ID || v.History[0].Score != v.Latest.Score || v.History[1].Score != partial {
		t.Errorf("history %+v, want the two grades newest first", v.History)
	}
	for _, a := range srv.agents {
		if a != "Byname/test" {
			t.Errorf("a request carried User-Agent %q", a)
		}
	}
	got, err := e.svc.Server(e.ctx)
	if err != nil || got.Latest == nil || got.Latest.ID != v.Latest.ID || got.Latest.Grade != v.Latest.Grade || len(got.Latest.Categories) != 6 {
		t.Fatalf("Server = %+v, %v", got, err)
	}
}

func TestRunKeepsTheNewest50(t *testing.T) {
	e := newEnv(t)
	srv := newServer(t, false)
	if _, err := e.svc.SetURL(e.ctx, srv.URL); err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.Exec(e.ctx, `INSERT INTO mcp_grade (graded_at, url, score, grade, rules, report)
		SELECT now() - make_interval(days => 60 - i), 'https://old.example/mcp', i, 'F', '2026-11', '{}' FROM generate_series(1, 55) i`); err != nil {
		t.Fatal(err)
	}
	v, err := e.svc.Run(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	var n, oldest int
	if err := e.db.QueryRow(e.ctx, `SELECT count(*), min(score) FILTER (WHERE url = 'https://old.example/mcp') FROM mcp_grade`).Scan(&n, &oldest); err != nil {
		t.Fatal(err)
	}
	if n != 50 || oldest != 7 || len(v.History) != 50 || v.History[0].URL != srv.URL || v.History[1].Score != 55 {
		t.Errorf("after a grade on 55 rows: %d rows, oldest kept score %d, history %d long starting %+v", n, oldest, len(v.History), v.History[:2])
	}
}

func TestOneGradeAtATime(t *testing.T) {
	e := newEnv(t)
	srv := newServer(t, false)
	if _, err := e.svc.SetURL(e.ctx, srv.URL); err != nil {
		t.Fatal(err)
	}
	unlock, ok, err := db.TryLock(e.ctx, e.db, db.EntityLock("byname.mcpgrade.grade"))
	if err != nil || !ok {
		t.Fatalf("TryLock = %v, %v", ok, err)
	}
	defer unlock()
	if _, err := e.svc.Run(e.ctx); !errors.Is(err, core.ErrConflict) {
		t.Fatalf("Run while a grade runs = %v, want ErrConflict", err)
	}
}

func TestSealSecretsMovesTheTokenToTheCurrentKey(t *testing.T) {
	e := newEnv(t)
	if _, err := e.svc.SetURL(e.ctx, "https://mcp.northwind.example/mcp"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.SetToken(e.ctx, token); err != nil {
		t.Fatal(err)
	}
	next, err := secretbox.New(bytes.Repeat([]byte{7}, 32), []byte("byname test key, 32 bytes long!!"))
	if err != nil {
		t.Fatal(err)
	}
	rotated := mcpgrade.New(e.db, mcpgrade.Options{Secrets: next})
	if n, err := rotated.SealSecrets(e.ctx); err != nil || n != 1 {
		t.Fatalf("SealSecrets = %d, %v, want 1 resealed", n, err)
	}
	if n, err := rotated.SealSecrets(e.ctx); err != nil || n != 0 {
		t.Fatalf("SealSecrets again = %d, %v, want nothing left to reseal", n, err)
	}
	if got, err := next.Open(e.sealed(), "mcpgrade:token"); err != nil || !next.Current(e.sealed()) || got != token {
		t.Fatalf("after resealing, the token opens to %q, %v", got, err)
	}
}
