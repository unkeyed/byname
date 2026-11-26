package httpapi

import (
	"net/http"

	"github.com/unkeyed/byname/internal/core"
)

func (s *server) listWorkspaces(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	v, err := s.reg.List(r.Context())
	return respond(w, http.StatusOK, v, err)
}

// createWorkspace takes {"name": "...", "slug": "..."}; slug is optional
// and comes from the name when left out.
func (s *server) createWorkspace(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	var b struct {
		Name string `json:"name"`
		Slug string `json:"slug"`
	}
	if err := readJSON(r, &b); err != nil {
		return err
	}
	v, err := s.reg.Create(r.Context(), b.Name, b.Slug)
	return respond(w, http.StatusCreated, v, err)
}

// renameWorkspace takes {"name": "..."}. A slug never changes.
func (s *server) renameWorkspace(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	var b struct {
		Name string `json:"name"`
	}
	if err := readJSON(r, &b); err != nil {
		return err
	}
	v, err := s.reg.Rename(r.Context(), r.PathValue("slug"), b.Name)
	return respond(w, http.StatusOK, v, err)
}
