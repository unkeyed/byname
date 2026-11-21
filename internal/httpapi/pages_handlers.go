package httpapi

import (
	"net/http"

	"github.com/unkeyed/byname/internal/core"
	"github.com/unkeyed/byname/internal/engines"
	"github.com/unkeyed/byname/internal/results"
)

// Prompts, cited sources and the engine drawer: results:read.

func (s *server) prompts(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	since, err := querySince(r)
	if err != nil {
		return err
	}
	v, err := s.svc.Results.Prompts(r.Context(), since, queryScope(r))
	return respond(w, http.StatusOK, v, err)
}

func (s *server) citedSources(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	since, err := querySince(r)
	if err != nil {
		return err
	}
	v, err := s.svc.Results.Sources(r.Context(), since, queryScope(r))
	return respond(w, http.StatusOK, v, err)
}

// engineView reads one chart line from ?surface=&pair=&endpoint=&fidelity=.
func (s *server) engineView(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	since, err := querySince(r)
	if err != nil {
		return err
	}
	pair, err := queryInt(r, "pair")
	if err != nil {
		return err
	}
	q := r.URL.Query()
	line := results.Line{Surface: engines.Surface(q.Get("surface")), PairID: int64(pair), Endpoint: q.Get("endpoint"), Fidelity: q.Get("fidelity")}
	v, err := s.svc.Results.Engine(r.Context(), since, line)
	return respond(w, http.StatusOK, v, err)
}
