package httpapi

import (
	"net/http"

	"github.com/unkeyed/byname/internal/core"
	"github.com/unkeyed/byname/internal/mcpgrade"
)

func (s *server) mcpServer(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	v, err := s.svc.MCPGrade.Server(r.Context())
	return respond(w, http.StatusOK, v, err)
}

func (s *server) setMCPServer(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	var b mcpgrade.URLInput
	if err := readJSON(r, &b); err != nil {
		return err
	}
	v, err := s.svc.MCPGrade.SetURL(r.Context(), b.URL)
	return respond(w, http.StatusOK, v, err)
}

func (s *server) setMCPServerToken(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	var b struct {
		Token string `json:"token"`
	}
	if err := readJSON(r, &b); err != nil {
		return err
	}
	return s.recentOnly(r, func() error {
		v, err := s.svc.MCPGrade.SetToken(r.Context(), b.Token)
		return respond(w, http.StatusOK, v, err)
	})
}

func (s *server) gradeMCPServer(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	v, err := s.svc.MCPGrade.Run(r.Context())
	return respond(w, http.StatusOK, v, err)
}

func (s *server) reviewMCPTools(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	v, err := s.svc.MCPGrade.StartReview(r.Context())
	return respond(w, http.StatusOK, v, err)
}
