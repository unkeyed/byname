// Package core is what every transport and service agrees on: who is
// calling (Actor), what they may do (Role and Scope), and the errors each
// transport maps to a status code or tool error.
package core

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// Errors every transport maps to a status code or tool error. Wrap them with
// fmt.Errorf("%w: detail", ErrInvalid) to add a message.
var (
	ErrNotFound  = errors.New("not found")
	ErrInvalid   = errors.New("invalid input")
	ErrConflict  = errors.New("conflict") // the thing to create already exists
	ErrForbidden = errors.New("forbidden")
	// ErrUpstream is Treg failing to answer a call Byname made for the
	// caller, such as reading a model list. Retrying later may work.
	ErrUpstream = errors.New("upstream unavailable")
)

// Role is what a team member may do. Each role grants the scopes RoleScopes
// lists; Admin alone also manages credentials, keys, apps and the team.
type Role string

const (
	Admin  Role = "admin"
	Editor Role = "editor"
	Viewer Role = "viewer"
)

// Roles in order of decreasing access.
var Roles = []Role{Admin, Editor, Viewer}

// Scope is one kind of access an API key, OAuth grant or session carries.
type Scope string

const (
	ResultsRead Scope = "results:read" // runs, answers, questions, brands, Search Console data and settings, never secrets
	RunsWrite   Scope = "runs:write"   // queue, pause and resume checks
	ConfigWrite Scope = "config:write" // brand, competitors, questions, surfaces and the schedule
)

// Scopes is every scope, in the order lists and the consent page show them.
var Scopes = []Scope{ResultsRead, RunsWrite, ConfigWrite}

// RoleScopes is the only place that says which role has which scope.
var RoleScopes = map[Role][]Scope{
	Admin:  {ResultsRead, RunsWrite, ConfigWrite},
	Editor: {ResultsRead, RunsWrite, ConfigWrite},
	Viewer: {ResultsRead},
}

// ParseRole accepts one of Roles.
func ParseRole(s string) (Role, error) {
	r := Role(s)
	if _, ok := RoleScopes[r]; !ok {
		return "", fmt.Errorf("%w: role must be one of %s, got %q", ErrInvalid, joinRoles(), s)
	}
	return r, nil
}

func joinRoles() string {
	names := make([]string, len(Roles))
	for i, r := range Roles {
		names[i] = string(r)
	}
	return strings.Join(names, ", ")
}

// ParseScopes accepts names from Scopes and returns them once each, in
// Scopes order. An unknown name is ErrInvalid.
func ParseScopes[S ~string](names []S) ([]Scope, error) {
	for _, n := range names {
		if !slices.Contains(Scopes, Scope(n)) {
			return nil, fmt.Errorf("%w: unknown scope %q; scopes are %s", ErrInvalid, n, joinScopes())
		}
	}
	return canonical(func(s Scope) bool { return slices.Contains(names, S(s)) }), nil
}

// Grant checks that role can hand out every scope in want, for a new API
// key or OAuth grant. A scope the role lacks is ErrForbidden.
func Grant(role Role, want []Scope) error {
	for _, s := range want {
		if !slices.Contains(RoleScopes[role], s) {
			return fmt.Errorf("%w: the %s role cannot grant %s", ErrForbidden, role, s)
		}
	}
	return nil
}

// Effective is the part of granted that role still has, so a key or grant
// loses a scope as soon as its creator's role does.
func Effective(role Role, granted []Scope) []Scope {
	return canonical(func(s Scope) bool { return slices.Contains(granted, s) && slices.Contains(RoleScopes[role], s) })
}

func canonical(keep func(Scope) bool) []Scope {
	out := []Scope{}
	for _, s := range Scopes {
		if keep(s) {
			out = append(out, s)
		}
	}
	return out
}

func joinScopes() string {
	names := make([]string, len(Scopes))
	for i, s := range Scopes {
		names[i] = string(s)
	}
	return strings.Join(names, ", ")
}

// Via is how a request authenticated.
type Via string

const (
	ViaSession Via = "session"
	ViaAPIKey  Via = "api_key"
	ViaOAuth   Via = "oauth" // an MCP client acting for the person who approved it
)

// Actor is who is calling. Transports build it from the authenticated
// caller and pass it explicitly.
type Actor struct {
	Via Via `json:"via"`
	// ID is the credential: the user's id for a session, the key's id, or
	// the OAuth grant's id.
	ID     string `json:"id"`
	UserID int64  `json:"user_id"`
	// Name is what a timeline shows, "Claude Code (via Maya Chen)" for an
	// MCP client.
	Name string `json:"name"`
	Role Role   `json:"role"`
	// Scopes is the effective set: what the credential was granted, cut
	// down to what Role still allows.
	Scopes []Scope `json:"scopes"`
}

// Has reports whether the actor may use scope s.
func (a Actor) Has(s Scope) bool { return slices.Contains(a.Scopes, s) }
