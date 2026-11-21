package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"time"

	"github.com/unkeyed/byname/internal/core"
)

func (s *server) healthz(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	w.WriteHeader(http.StatusOK) // process only; Unkey advises not to check the DB here
	return nil
}

// openapiETag changes whenever openapi.yaml does.
var openapiETag = func() string {
	sum := sha256.Sum256(openapiYAML)
	return `"` + hex.EncodeToString(sum[:8]) + `"`
}()

// openapi is public and the same for everyone, so caches may keep it but
// must revalidate, and a client holding the current ETag gets a 304.
func (s *server) openapi(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	h := w.Header()
	h.Set("Content-Type", "application/yaml")
	h.Set("Cache-Control", "public, no-cache")
	h.Del("Vary")
	h.Set("ETag", openapiETag)
	http.ServeContent(w, r, "openapi.yaml", time.Time{}, bytes.NewReader(openapiYAML))
	return nil
}

func (s *server) status(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	st, err := s.svc.Status(r.Context())
	return respond(w, http.StatusOK, st, err)
}
