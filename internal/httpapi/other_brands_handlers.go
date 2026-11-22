package httpapi

import (
	"net/http"
	"strconv"

	"github.com/unkeyed/byname/internal/core"
	"github.com/unkeyed/byname/internal/discovery"
)

// Other brands: results:read lists the brands answers name that you don't
// track, the hidden ones, the settings, reading past answers and logos;
// config:write tracks, hides and unhides a brand and changes the
// settings; runs:write starts reading past answers, which spends money.

// otherBrands query ?since=&surface=&model=, like the competitors.
func (s *server) otherBrands(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	since, err := querySince(r)
	if err != nil {
		return err
	}
	v, err := s.svc.Results.OtherBrands(r.Context(), since, queryScope(r))
	return respond(w, http.StatusOK, v, err)
}

func (s *server) hiddenBrands(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	v, err := s.svc.Results.HiddenBrands(r.Context())
	return respond(w, http.StatusOK, v, err)
}

func (s *server) trackBrand(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	id, err := pathID(r, "brand")
	if err != nil {
		return err
	}
	v, err := s.svc.Discovery.Track(r.Context(), id)
	return respond(w, http.StatusOK, v, err)
}

func (s *server) hideBrand(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	id, err := pathID(r, "brand")
	if err != nil {
		return err
	}
	return noContent(w, s.svc.Discovery.Hide(r.Context(), id))
}

func (s *server) unhideBrand(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	id, err := pathID(r, "brand")
	if err != nil {
		return err
	}
	return noContent(w, s.svc.Discovery.Unhide(r.Context(), id))
}

func (s *server) otherBrandsSettings(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	v, err := s.svc.Discovery.Settings(r.Context())
	return respond(w, http.StatusOK, v, err)
}

func (s *server) putOtherBrandsSettings(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	var b discovery.SettingsInput
	if err := readJSON(r, &b); err != nil {
		return err
	}
	v, err := s.svc.Discovery.PutSettings(r.Context(), b)
	return respond(w, http.StatusOK, v, err)
}

func (s *server) brandBackfill(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	v, err := s.svc.Discovery.Backfill(r.Context())
	return respond(w, http.StatusOK, v, err)
}

func (s *server) startBrandBackfill(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	v, err := s.svc.Discovery.StartBackfill(r.Context())
	return respond(w, http.StatusAccepted, v, err)
}

func (s *server) listLogos(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	v, err := s.svc.Logos.Logos(r.Context())
	return respond(w, http.StatusOK, v, err)
}

// brandLogo serves a stored logo. An SVG could carry script, so the
// response forbids everything but drawing it, even opened on its own.
func (s *server) brandLogo(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	id, err := pathID(r, "brand")
	if err != nil {
		return err
	}
	img, err := s.svc.Logos.Image(r.Context(), id)
	if err != nil {
		return err
	}
	h := w.Header()
	h.Set("Content-Type", img.MIME)
	h.Set("Content-Length", strconv.Itoa(len(img.Bytes)))
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "private, max-age=604800")
	w.WriteHeader(http.StatusOK)
	_, err = w.Write(img.Bytes)
	return err
}
