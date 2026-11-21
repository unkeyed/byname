package httpapi

import (
	"fmt"
	"net/http"

	"github.com/unkeyed/byname/internal/changes"
	"github.com/unkeyed/byname/internal/core"
)

// Site changes: results:read reads changes, control pages, the repo status
// and the chart markers; config:write logs changes by hand and edits
// control pages, path rules and the read settings; only a signed-in admin
// connects, tests or disconnects the repo. No response carries the token.

func (s *server) listChanges(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	v, err := s.svc.Changes.List(r.Context())
	return respond(w, http.StatusOK, v, err)
}

func (s *server) getChange(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	id, err := pathID(r, "change")
	if err != nil {
		return err
	}
	v, err := s.svc.Changes.Get(r.Context(), id)
	return respond(w, http.StatusOK, v, err)
}

func (s *server) logChange(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	var b changes.ManualChange
	if err := readJSON(r, &b); err != nil {
		return err
	}
	v, err := s.svc.Changes.LogChange(r.Context(), b)
	return respond(w, http.StatusCreated, v, err)
}

func (s *server) deleteChange(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	id, err := pathID(r, "change")
	if err != nil {
		return err
	}
	return noContent(w, s.svc.Changes.DeleteChange(r.Context(), id))
}

func (s *server) changeMarkers(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	q := r.URL.Query()
	v, err := s.svc.Changes.Markers(r.Context(), changes.MarkerRange{From: q.Get("from"), To: q.Get("to")})
	return respond(w, http.StatusOK, v, err)
}

func (s *server) changeSettings(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	v, err := s.svc.Changes.Settings(r.Context())
	return respond(w, http.StatusOK, v, err)
}

func (s *server) putChangeSettings(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	var b changes.Settings
	if err := readJSON(r, &b); err != nil {
		return err
	}
	v, err := s.svc.Changes.PutSettings(r.Context(), b)
	return respond(w, http.StatusOK, v, err)
}

func (s *server) listControlPages(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	v, err := s.svc.Changes.ControlPages(r.Context())
	return respond(w, http.StatusOK, v, err)
}

func (s *server) upsertControlPage(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	var b changes.ControlInput
	if err := readJSON(r, &b); err != nil {
		return err
	}
	v, err := s.svc.Changes.UpsertControlPage(r.Context(), b)
	return respond(w, http.StatusOK, v, err)
}

func (s *server) deleteControlPage(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	id, err := pathID(r, "control page")
	if err != nil {
		return err
	}
	return noContent(w, s.svc.Changes.DeleteControlPage(r.Context(), id))
}

func (s *server) repoStatus(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	v, err := s.svc.Changes.RepoStatus(r.Context())
	return respond(w, http.StatusOK, v, err)
}

// putRepoRules body {"rules": [{"glob": "...", "url": "..."}]}.
func (s *server) putRepoRules(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	var b struct {
		Rules []changes.Rule `json:"rules"`
	}
	if err := readJSON(r, &b); err != nil {
		return err
	}
	if b.Rules == nil {
		return fmt.Errorf("%w: rules is required; send [] for none", core.ErrInvalid)
	}
	v, err := s.svc.Changes.SetRules(r.Context(), b.Rules)
	return respond(w, http.StatusOK, v, err)
}

// connectRepo body {"repo": "owner/name", "token": "github_pat_..."}.
func (s *server) connectRepo(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	var b changes.RepoInput
	if err := readJSON(r, &b); err != nil {
		return err
	}
	return s.recentOnly(r, func() error {
		v, err := s.svc.Changes.ConnectRepo(r.Context(), b)
		return respond(w, http.StatusOK, v, err)
	})
}

func (s *server) testRepo(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	v, err := s.svc.Changes.TestRepo(r.Context())
	return respond(w, http.StatusOK, v, err)
}

func (s *server) disconnectRepo(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	return noContent(w, s.svc.Changes.DisconnectRepo(r.Context()))
}
