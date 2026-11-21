package httpapi

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/unkeyed/byname/internal/auth"
	"github.com/unkeyed/byname/internal/core"
)

// Admin-only routes: keys, apps and the team. Agents cannot mint keys or
// invite people, because an agent holding one leaked key should not be able
// to turn it into more access.

func (s *server) listKeys(w http.ResponseWriter, r *http.Request, a core.Actor) error {
	keys, err := s.keys.List(r.Context())
	return respond(w, http.StatusOK, keys, keyStoreErr(err))
}

// createdKey is the one response that carries a key's plaintext.
type createdKey struct {
	Plaintext string `json:"key"`
	auth.Key
}

func (s *server) createKey(w http.ResponseWriter, r *http.Request, a core.Actor) error {
	var b auth.KeyOptions
	if err := readJSON(r, &b); err != nil {
		return err
	}
	return s.recentOnly(r, func() error {
		plaintext, k, err := s.keys.Create(r.Context(), a, b)
		return respond(w, http.StatusCreated, createdKey{plaintext, k}, keyStoreErr(err))
	})
}

func (s *server) revokeKey(w http.ResponseWriter, r *http.Request, a core.Actor) error {
	return noContent(w, keyStoreErr(s.keys.Revoke(r.Context(), r.PathValue("id"))))
}

// revokeCurrentKey lets an agent revoke the key it calls with, for example
// when it finds the key in a log. A key with no scopes left may call it too.
func (s *server) revokeCurrentKey(w http.ResponseWriter, r *http.Request, a core.Actor) error {
	if a.Via != core.ViaAPIKey {
		return fmt.Errorf("%w: call this with the API key to revoke; people revoke keys under Settings > API keys", core.ErrInvalid)
	}
	return noContent(w, keyStoreErr(s.keys.Revoke(r.Context(), a.ID)))
}

func (s *server) listUsers(w http.ResponseWriter, r *http.Request, a core.Actor) error {
	users, err := s.users.List(r.Context())
	return respond(w, http.StatusOK, users, err)
}

// setUserRole body {"role": "admin" | "editor" | "viewer"}.
func (s *server) setUserRole(w http.ResponseWriter, r *http.Request, a core.Actor) error {
	id, err := userPathID(r)
	if err != nil {
		return err
	}
	var b struct {
		Role core.Role `json:"role"`
	}
	if err := readJSON(r, &b); err != nil {
		return err
	}
	return s.recentOnly(r, func() error {
		return noContent(w, s.users.SetRole(r.Context(), a, id, b.Role))
	})
}

func (s *server) removeUser(w http.ResponseWriter, r *http.Request, a core.Actor) error {
	id, err := userPathID(r)
	if err != nil {
		return err
	}
	return s.recentOnly(r, func() error {
		return noContent(w, s.users.Remove(r.Context(), a, id, s.keys))
	})
}

func userPathID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: no such user", core.ErrNotFound)
	}
	return id, nil
}

// createInvite body {"email": "...", "role": "editor"}; responds
// {"token": "..."}, which the UI turns into the invite link.
func (s *server) createInvite(w http.ResponseWriter, r *http.Request, a core.Actor) error {
	var b struct {
		Email string    `json:"email"`
		Role  core.Role `json:"role"`
	}
	if err := readJSON(r, &b); err != nil {
		return err
	}
	return s.recentOnly(r, func() error {
		token, err := s.users.Invite(r.Context(), a, b.Email, b.Role)
		return respond(w, http.StatusCreated, map[string]string{"token": token}, err)
	})
}

func (s *server) me(w http.ResponseWriter, r *http.Request, a core.Actor) error {
	writeJSON(w, http.StatusOK, a)
	return nil
}
