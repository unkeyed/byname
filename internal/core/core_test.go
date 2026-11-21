package core

import (
	"errors"
	"slices"
	"testing"
)

func TestEveryRoleHasScopes(t *testing.T) {
	for _, r := range Roles {
		scopes, ok := RoleScopes[r]
		if !ok || len(scopes) == 0 {
			t.Errorf("RoleScopes has no scopes for %s", r)
		}
		for _, s := range scopes {
			if !slices.Contains(Scopes, s) {
				t.Errorf("RoleScopes gives %s the unknown scope %q", r, s)
			}
		}
	}
	if len(RoleScopes) != len(Roles) {
		t.Errorf("RoleScopes lists %d roles, Roles has %d", len(RoleScopes), len(Roles))
	}
}

func TestParseScopes(t *testing.T) {
	got, err := ParseScopes([]string{"config:write", "results:read", "config:write"})
	if err != nil || !slices.Equal(got, []Scope{ResultsRead, ConfigWrite}) {
		t.Fatalf("got %v, %v; want [results:read config:write] once each, in Scopes order", got, err)
	}
	if got, err := ParseScopes(nil); err != nil || got == nil || len(got) != 0 {
		t.Errorf("no scopes: got %#v, %v; want an empty list", got, err)
	}
	if _, err := ParseScopes([]string{"results:read", "admin"}); !errors.Is(err, ErrInvalid) {
		t.Errorf("unknown scope: got %v, want ErrInvalid", err)
	}
}

func TestGrantStopsAtTheRole(t *testing.T) {
	if err := Grant(Admin, Scopes); err != nil {
		t.Errorf("admin granting every scope: %v", err)
	}
	if err := Grant(Viewer, []Scope{ResultsRead}); err != nil {
		t.Errorf("viewer granting results:read: %v", err)
	}
	if err := Grant(Viewer, []Scope{ResultsRead, RunsWrite}); !errors.Is(err, ErrForbidden) {
		t.Errorf("viewer granting runs:write: got %v, want ErrForbidden", err)
	}
	if err := Grant(Role("owner"), []Scope{ResultsRead}); !errors.Is(err, ErrForbidden) {
		t.Errorf("unknown role: got %v, want ErrForbidden", err)
	}
}

func TestEffectiveFollowsADemotion(t *testing.T) {
	granted := []Scope{ConfigWrite, ResultsRead}
	if got := Effective(Editor, granted); !slices.Equal(got, []Scope{ResultsRead, ConfigWrite}) {
		t.Errorf("editor: got %v", got)
	}
	if got := Effective(Viewer, granted); !slices.Equal(got, []Scope{ResultsRead}) {
		t.Errorf("demoted to viewer: got %v, want [results:read]", got)
	}
	if got := Effective(Viewer, []Scope{RunsWrite}); got == nil || len(got) != 0 {
		t.Errorf("nothing left: got %#v, want an empty list", got)
	}
}

func TestParseRole(t *testing.T) {
	for _, r := range Roles {
		if got, err := ParseRole(string(r)); err != nil || got != r {
			t.Errorf("ParseRole(%q) = %q, %v", r, got, err)
		}
	}
	if _, err := ParseRole("Admin"); !errors.Is(err, ErrInvalid) {
		t.Errorf("ParseRole(Admin): got %v, want ErrInvalid", err)
	}
}
