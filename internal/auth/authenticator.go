package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/unkeyed/byname/internal/core"
)

// Authenticator resolves an inbound request to an actor. OAuth issues the
// tokens /mcp accepts; Caller never accepts them.
type Authenticator struct {
	Keys  KeyStore
	Users *Users
	OAuth *OAuthServer
}

// Caller reads "Authorization: Bearer <key>" first, then the session cookie.
// A present but invalid credential never falls through to the other one.
// Neither present is ErrUnauthorized, an invalid key ErrInvalidKey, an
// invalid session ErrUnauthorized. Any other error means the key store or
// database failed, and the request must fail with it.
func (a *Authenticator) Caller(r *http.Request) (core.Actor, error) {
	if h := r.Header.Get("Authorization"); h != "" {
		scheme, token, ok := strings.Cut(h, " ")
		if !ok || !strings.EqualFold(scheme, "Bearer") || strings.TrimSpace(token) == "" {
			return core.Actor{}, ErrUnauthorized
		}
		key, err := a.Keys.Verify(r.Context(), strings.TrimSpace(token))
		if err != nil {
			return core.Actor{}, err
		}
		return a.Users.keyActor(r.Context(), key)
	}
	return a.Users.Session(r.Context(), a.Users.Cookie(r, SessionCookie))
}

// keyActor is the actor for a verified key: its creator's current role,
// and the key's scopes cut down to that role. A key whose creator is gone
// does not verify.
func (u *Users) keyActor(ctx context.Context, k VerifiedKey) (core.Actor, error) {
	var role core.Role
	err := u.db.QueryRow(ctx, `SELECT role FROM users WHERE id = $1`, k.CreatedBy).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return core.Actor{}, ErrInvalidKey
	}
	if err != nil {
		return core.Actor{}, err
	}
	return core.Actor{Via: core.ViaAPIKey, ID: k.ID, UserID: k.CreatedBy, Name: k.Name, Role: role, Scopes: core.Effective(role, k.Scopes)}, nil
}
