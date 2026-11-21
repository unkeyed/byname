package httpapi

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/unkeyed/byname/internal/core"
	"github.com/unkeyed/byname/internal/gsc"
)

// Search Console: results:read reads the numbers and goals, config:write
// changes goals and branded search terms, and only a signed-in admin
// connects, picks the property or disconnects. No response carries the
// credential.

func (s *server) searchSummary(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	v, err := s.svc.GSC.Summary(r.Context())
	return respond(w, http.StatusOK, v, err)
}

func searchRange(r *http.Request) gsc.Range {
	q := r.URL.Query()
	return gsc.Range{From: q.Get("from"), To: q.Get("to")}
}

func (s *server) searchDaily(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	v, err := s.svc.GSC.Daily(r.Context(), searchRange(r))
	return respond(w, http.StatusOK, v, err)
}

func (s *server) searchBrand(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	v, err := s.svc.GSC.Brand(r.Context(), searchRange(r))
	return respond(w, http.StatusOK, v, err)
}

func searchTop(r *http.Request) (gsc.Top, error) {
	var t gsc.Top
	var err error
	if t.Days, err = queryInt(r, "days"); err != nil {
		return t, err
	}
	if t.Limit, err = queryInt(r, "limit"); err != nil {
		return t, err
	}
	if raw := r.URL.Query().Get("non_brand"); raw != "" {
		if t.NonBrand, err = strconv.ParseBool(raw); err != nil {
			return t, fmt.Errorf("%w: non_brand must be true or false, got %q", core.ErrInvalid, raw)
		}
	}
	return t, nil
}

func (s *server) searchPages(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	t, err := searchTop(r)
	if err != nil {
		return err
	}
	v, err := s.svc.GSC.TopPages(r.Context(), t)
	return respond(w, http.StatusOK, v, err)
}

func (s *server) searchQueries(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	t, err := searchTop(r)
	if err != nil {
		return err
	}
	v, err := s.svc.GSC.TopQueries(r.Context(), t)
	return respond(w, http.StatusOK, v, err)
}

func (s *server) listGoals(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	v, err := s.svc.GSC.Goals(r.Context())
	return respond(w, http.StatusOK, v, err)
}

func (s *server) upsertGoal(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	var b gsc.GoalInput
	if err := readJSON(r, &b); err != nil {
		return err
	}
	v, err := s.svc.GSC.UpsertGoal(r.Context(), b)
	return respond(w, http.StatusOK, v, err)
}

func (s *server) updateGoal(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	id, err := pathID(r, "goal")
	if err != nil {
		return err
	}
	var b gsc.GoalInput
	if err := readJSON(r, &b); err != nil {
		return err
	}
	v, err := s.svc.GSC.UpdateGoal(r.Context(), id, b)
	return respond(w, http.StatusOK, v, err)
}

func (s *server) deleteGoal(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	id, err := pathID(r, "goal")
	if err != nil {
		return err
	}
	return noContent(w, s.svc.GSC.DeleteGoal(r.Context(), id))
}

// putBrandedTerms body {"terms": ["..."]}; an empty list goes back to the
// brand's name and aliases.
func (s *server) putBrandedTerms(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	var b struct {
		Terms *[]string `json:"terms"`
	}
	if err := readJSON(r, &b); err != nil {
		return err
	}
	if b.Terms == nil {
		return fmt.Errorf("%w: terms is required; send [] to go back to your brand's name and aliases", core.ErrInvalid)
	}
	v, err := s.svc.Config.SetBrandedTerms(r.Context(), *b.Terms)
	return respond(w, http.StatusOK, v, err)
}

func (s *server) gscStatus(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	v, err := s.svc.GSC.Status(r.Context())
	return respond(w, http.StatusOK, v, err)
}

func (s *server) gscProperties(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	v, err := s.svc.GSC.Properties(r.Context())
	return respond(w, http.StatusOK, v, err)
}

func (s *server) gscSetProperty(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	var b gsc.PropertyInput
	if err := readJSON(r, &b); err != nil {
		return err
	}
	v, err := s.svc.GSC.SetProperty(r.Context(), b)
	return respond(w, http.StatusOK, v, err)
}

// gscServiceAccount body {"key": "<the JSON key file's contents>"}.
func (s *server) gscServiceAccount(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	var b struct {
		Key string `json:"key"`
	}
	if err := readJSON(r, &b); err != nil {
		return err
	}
	return s.recentOnly(r, func() error {
		v, err := s.svc.GSC.ConnectServiceAccount(r.Context(), b.Key)
		return respond(w, http.StatusOK, v, err)
	})
}

// gscOAuthStart responds {"url": "..."}: Google's consent page, which the
// browser opens next.
func (s *server) gscOAuthStart(w http.ResponseWriter, r *http.Request, a core.Actor) error {
	return s.recentOnly(r, func() error {
		u, err := s.svc.GSC.StartOAuth(a)
		return respond(w, http.StatusOK, map[string]string{"url": u}, err)
	})
}

// gscBack is the Settings tab the Google sign-in returns to.
const gscBack = "/settings/search-console"

// gscOAuthCallback is where Google sends the browser back. It is public so
// that it can always redirect to Settings > Search Console, with ?error=
// when connecting failed, including when the admin's session ended during
// Google's consent page; it checks for that session itself.
func (s *server) gscOAuthCallback(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	q := r.URL.Query()
	a, err := s.authn.Caller(r)
	switch {
	case err != nil && statusOf(callerErr(err)) >= 500:
		err = callerErr(err)
	case err != nil || admin.allows(a) != nil:
		err = fmt.Errorf("%w: sign in to Byname as an admin, then connect Search Console again", core.ErrForbidden)
	default:
		err = s.svc.GSC.FinishOAuth(r.Context(), a, gsc.OAuthCallback{State: q.Get("state"), Code: q.Get("code"), Error: q.Get("error")})
	}
	if err == nil {
		http.Redirect(w, r, gscBack+"?connected=google", http.StatusSeeOther)
		return nil
	}
	msg := err.Error()
	if code := statusOf(err); code >= 500 && code != http.StatusBadGateway {
		s.log.Error("Search Console sign-in failed", "err", err)
		msg = "connecting Search Console failed on the server; try again"
	}
	http.Redirect(w, r, gscBack+"?error="+url.QueryEscape(msg), http.StatusSeeOther)
	return nil
}

func (s *server) gscDisconnect(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	return noContent(w, s.svc.GSC.Disconnect(r.Context()))
}
