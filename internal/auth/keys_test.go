package auth

import (
	"context"
	"errors"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/unkeyed/byname/internal/core"
	"github.com/unkeyed/byname/internal/testdb"
)

func TestBuiltinKeyLifecycle(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testdb.New(t)
	keys := NewKeyStore(pool, UnkeyConfig{})
	if keys.Mode() != "builtin" {
		t.Fatalf("mode %q without Unkey config, want builtin", keys.Mode())
	}

	admin := addPerson(t, pool, core.Admin)
	token, created, err := keys.Create(ctx, admin, KeyOptions{Name: "  zapier  ", Scopes: []core.Scope{core.RunsWrite, core.ResultsRead}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(token, "byn_") || len(token) != len("byn_")+52 {
		t.Fatalf("token %q is not byn_ + 52 base32 characters", token)
	}
	if created.Name != "zapier" || created.Prefix != token[:len(keyPrefix)+4] || !slices.Equal(created.Scopes, []core.Scope{core.ResultsRead, core.RunsWrite}) {
		t.Fatalf("created %+v, want name zapier, prefix %q and both scopes", created, token[:len(keyPrefix)+4])
	}
	for _, opts := range []KeyOptions{
		{Name: " ", Scopes: []core.Scope{core.ResultsRead}},
		{Name: "no scopes"},
		{Name: "unknown scope", Scopes: []core.Scope{"billing:write"}},
	} {
		if _, _, err := keys.Create(ctx, admin, opts); !errors.Is(err, core.ErrInvalid) {
			t.Fatalf("%+v: %v, want core.ErrInvalid", opts, err)
		}
	}

	got, err := keys.Verify(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != created.ID || got.Name != "zapier" || got.CreatedBy != admin.UserID || !slices.Equal(got.Scopes, created.Scopes) {
		t.Fatalf("verify gave %+v, want key %s by user %d with its scopes", got, created.ID, admin.UserID)
	}
	for _, bad := range []string{"", "byn_", token[:len(token)-1], token + "a", strings.ToUpper(token)} {
		if _, err := keys.Verify(ctx, bad); !errors.Is(err, ErrInvalidKey) {
			t.Fatalf("verify %q: %v, want ErrInvalidKey", bad, err)
		}
	}

	list, err := keys.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != created.ID || list[0].Prefix != created.Prefix || list[0].LastUsedAt == nil {
		t.Fatalf("list %+v, want the one key with last_used_at set by Verify", list)
	}

	if err := keys.Revoke(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
	if err := keys.Revoke(ctx, created.ID); err != nil {
		t.Fatalf("second revoke: %v, want idempotent success", err)
	}
	if _, err := keys.Verify(ctx, token); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("revoked key verified: %v", err)
	}
	if list, _ := keys.List(ctx); len(list) != 0 {
		t.Fatalf("list after revoke %+v, want empty", list)
	}
	for _, id := range []string{"999999", "not-a-number"} {
		if err := keys.Revoke(ctx, id); !errors.Is(err, core.ErrNotFound) {
			t.Fatalf("revoke %q: %v, want core.ErrNotFound", id, err)
		}
	}
}

func TestBuiltinVerifyWritesLastUsedAtMostOnceAMinute(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testdb.New(t)
	keys := NewKeyStore(pool, UnkeyConfig{})
	token, k, err := keys.Create(ctx, addPerson(t, pool, core.Admin), KeyOptions{Name: "agent", Scopes: []core.Scope{core.ResultsRead}})
	if err != nil {
		t.Fatal(err)
	}

	lastUsed := func() (stamp string) {
		t.Helper()
		if err := pool.QueryRow(ctx, `SELECT coalesce(last_used_at::text, '') FROM api_keys WHERE id = $1::bigint`, k.ID).Scan(&stamp); err != nil {
			t.Fatal(err)
		}
		return stamp
	}
	if _, err := keys.Verify(ctx, token); err != nil {
		t.Fatal(err)
	}
	first := lastUsed()
	if first == "" {
		t.Fatal("first verify left last_used_at NULL")
	}
	if _, err := keys.Verify(ctx, token); err != nil {
		t.Fatal(err)
	}
	if again := lastUsed(); again != first {
		t.Fatalf("second verify within a minute rewrote last_used_at: %s -> %s", first, again)
	}

	if _, err := pool.Exec(ctx, `UPDATE api_keys SET last_used_at = now() - interval '2 minutes'`); err != nil {
		t.Fatal(err)
	}
	stale := lastUsed()
	if _, err := keys.Verify(ctx, token); err != nil {
		t.Fatal(err)
	}
	if lastUsed() == stale {
		t.Fatal("verify after a stale minute did not refresh last_used_at")
	}
}

// addPerson inserts a user with role and returns them as a signed-in actor.
func addPerson(t *testing.T, pool *pgxpool.Pool, role core.Role) core.Actor {
	t.Helper()
	var id int64
	email := "person" + strconv.Itoa(int(personSeq.Add(1))) + "@example.com"
	if err := pool.QueryRow(context.Background(), `INSERT INTO users (email, role) VALUES ($1, $2) RETURNING id`, email, role).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return core.Actor{Via: core.ViaSession, ID: strconv.FormatInt(id, 10), UserID: id, Name: email, Role: role, Scopes: core.RoleScopes[role]}
}

var personSeq atomic.Int64

// Keys and apps carry at most their creator's role, at creation and on
// every request after it, so demoting a person takes scopes from their keys
// at once and a viewer can never hand out more than reading.
func TestKeysFollowTheirCreatorsRole(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	keys := NewKeyStore(f.pool, UnkeyConfig{})
	authn := &Authenticator{Keys: keys, Users: f.users}
	editor := addPerson(t, f.pool, core.Editor)
	viewer := addPerson(t, f.pool, core.Viewer)

	if _, _, err := keys.Create(f.ctx, viewer, KeyOptions{Name: "writer", Scopes: []core.Scope{core.RunsWrite}}); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("viewer creating a runs:write key: %v, want core.ErrForbidden", err)
	}
	token, _, err := keys.Create(f.ctx, editor, KeyOptions{Name: "ci", Scopes: []core.Scope{core.ResultsRead, core.RunsWrite}})
	f.must(err)
	caller := func() core.Actor {
		t.Helper()
		r := httptest.NewRequest("GET", "/v1/status", nil)
		r.Header.Set("Authorization", "Bearer "+token)
		a, err := authn.Caller(r)
		f.must(err)
		return a
	}
	if a := caller(); a.Via != core.ViaAPIKey || a.UserID != editor.UserID || a.Role != core.Editor || !a.Has(core.RunsWrite) {
		t.Fatalf("editor's key: %+v, want an api_key actor for the editor with runs:write", a)
	}

	f.must(f.users.SetRole(f.ctx, addPerson(t, f.pool, core.Admin), editor.UserID, core.Viewer))
	if a := caller(); a.Role != core.Viewer || !slices.Equal(a.Scopes, []core.Scope{core.ResultsRead}) {
		t.Fatalf("after demotion: %+v, want only results:read", a)
	}

	f.must(f.users.Remove(f.ctx, addPerson(t, f.pool, core.Admin), editor.UserID, keys))
	r := httptest.NewRequest("GET", "/v1/status", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	if _, err := authn.Caller(r); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("removed creator's key: %v, want ErrInvalidKey", err)
	}
}

func TestTheLastAdminStays(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	keys := NewKeyStore(f.pool, UnkeyConfig{})
	admin := addPerson(t, f.pool, core.Admin)
	editor := addPerson(t, f.pool, core.Editor)

	if err := f.users.SetRole(f.ctx, admin, admin.UserID, core.Editor); !errors.Is(err, core.ErrConflict) {
		t.Fatalf("last admin stepping down: %v, want core.ErrConflict", err)
	}
	if err := f.users.Remove(f.ctx, admin, admin.UserID, keys); !errors.Is(err, core.ErrConflict) {
		t.Fatalf("removing the last admin: %v, want core.ErrConflict", err)
	}
	if err := f.users.SetRole(f.ctx, admin, editor.UserID, "owner"); !errors.Is(err, core.ErrInvalid) {
		t.Fatalf("unknown role: %v, want core.ErrInvalid", err)
	}
	f.must(f.users.SetRole(f.ctx, admin, editor.UserID, core.Admin))
	f.must(f.users.SetRole(f.ctx, admin, admin.UserID, core.Viewer))
	f.must(f.users.Remove(f.ctx, editor, admin.UserID, keys))
	if n := f.count(`SELECT count(*) FROM users WHERE role = 'admin'`); n != 1 {
		t.Fatalf("%d admins left, want 1", n)
	}
}
