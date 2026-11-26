package httpapi

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/unkeyed/byname/internal/core"
	"github.com/unkeyed/byname/internal/search"
)

// Search engines: results:read reads each provider's numbers and goals,
// config:write changes goals and branded search terms, and only a
// signed-in admin connects a provider, picks its site or disconnects it.
// No response carries a credential. The provider is a path segment, such
// as google or bing, which the service checks against its registry.

func provider(r *http.Request) search.Provider { return search.Provider(r.PathValue("provider")) }

func (s *server) searchProviders(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	v, err := s.svc.Search.Providers(r.Context())
	return respond(w, http.StatusOK, v, err)
}

func (s *server) searchSummary(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	v, err := s.svc.Search.Summary(r.Context(), provider(r))
	return respond(w, http.StatusOK, v, err)
}

func searchRange(r *http.Request) search.Range {
	q := r.URL.Query()
	return search.Range{From: q.Get("from"), To: q.Get("to")}
}

func (s *server) searchDaily(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	v, err := s.svc.Search.Daily(r.Context(), provider(r), searchRange(r))
	return respond(w, http.StatusOK, v, err)
}

func (s *server) searchBrand(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	v, err := s.svc.Search.Brand(r.Context(), provider(r), searchRange(r))
	return respond(w, http.StatusOK, v, err)
}

func searchTop(r *http.Request) (search.Top, error) {
	var t search.Top
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
	v, err := s.svc.Search.TopPages(r.Context(), provider(r), t)
	return respond(w, http.StatusOK, v, err)
}

func (s *server) searchQueries(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	t, err := searchTop(r)
	if err != nil {
		return err
	}
	v, err := s.svc.Search.TopQueries(r.Context(), provider(r), t)
	return respond(w, http.StatusOK, v, err)
}

func (s *server) listGoals(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	v, err := s.svc.Search.Goals(r.Context())
	return respond(w, http.StatusOK, v, err)
}

func (s *server) upsertGoal(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	var b search.GoalInput
	if err := readJSON(r, &b); err != nil {
		return err
	}
	v, err := s.svc.Search.UpsertGoal(r.Context(), b)
	return respond(w, http.StatusOK, v, err)
}

func (s *server) updateGoal(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	id, err := pathID(r, "goal")
	if err != nil {
		return err
	}
	var b search.GoalInput
	if err := readJSON(r, &b); err != nil {
		return err
	}
	v, err := s.svc.Search.UpdateGoal(r.Context(), id, b)
	return respond(w, http.StatusOK, v, err)
}

func (s *server) deleteGoal(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	id, err := pathID(r, "goal")
	if err != nil {
		return err
	}
	return noContent(w, s.svc.Search.DeleteGoal(r.Context(), id))
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

func (s *server) searchSites(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	v, err := s.svc.Search.Sites(r.Context(), provider(r))
	return respond(w, http.StatusOK, v, err)
}

func (s *server) searchSetSite(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	var b search.SiteInput
	if err := readJSON(r, &b); err != nil {
		return err
	}
	v, err := s.svc.Search.SetSite(r.Context(), provider(r), b)
	return respond(w, http.StatusOK, v, err)
}

// searchKey body {"key": "..."}: Google's service account JSON key file,
// or Bing's API key.
func (s *server) searchKey(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	var b struct {
		Key string `json:"key"`
	}
	if err := readJSON(r, &b); err != nil {
		return err
	}
	return s.recentOnly(r, func() error {
		v, err := s.svc.Search.ConnectKey(r.Context(), provider(r), b.Key)
		return respond(w, http.StatusOK, v, err)
	})
}

func (s *server) searchDisconnect(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	return noContent(w, s.svc.Search.Disconnect(r.Context(), provider(r)))
}

// googleOAuthStart takes an optional body {"return_to": "settings" or
// "onboarding"} and responds {"url": "..."}: Google's consent page, which
// the browser opens next.
func (s *server) googleOAuthStart(w http.ResponseWriter, r *http.Request, a core.Actor) error {
	var b struct {
		ReturnTo string `json:"return_to"`
	}
	if r.ContentLength != 0 {
		if err := readJSON(r, &b); err != nil {
			return err
		}
	}
	back, err := search.ParseReturn(b.ReturnTo)
	if err != nil {
		return err
	}
	return s.recentOnly(r, func() error {
		u, err := s.svc.Search.StartGoogleOAuth(r.Context(), a, back)
		return respond(w, http.StatusOK, map[string]string{"url": u}, err)
	})
}

// googleBack is the page each Return sends the browser to after Google.
var googleBack = map[search.Return]string{
	search.ReturnSettings:   "/settings/search-engines",
	search.ReturnOnboarding: "/onboarding/search",
}

// googleOAuthCallback is where Google sends the browser back. Its path
// stays /auth/gsc/callback, the redirect URI registered on each instance's
// Google OAuth client, for every entity: the state names the entity
// the sign-in started in, and the connection lands there. It is public so
// that it can always redirect to the page the sign-in started from
// (Settings > Search engines or onboarding, in that entity), with
// ?error= when connecting failed, including when the admin's session ended
// during Google's consent page; it checks for that session itself.
func (s *server) googleOAuthCallback(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	q := r.URL.Query()
	origin := s.svc.Search.GoogleOAuthOrigin(q.Get("state"))
	e := s.reg.Default()
	var err error
	if origin.Entity != 0 {
		if found, ferr := s.reg.ByID(r.Context(), origin.Entity); ferr != nil {
			err = ferr
		} else {
			e = found
		}
	}
	back := "/e/" + e.Entity.Slug + googleBack[origin.Return]
	a, aerr := s.authn.Caller(r)
	switch {
	case err != nil:
	case aerr != nil && statusOf(callerErr(aerr)) >= 500:
		err = callerErr(aerr)
	case aerr != nil || admin.allows(a) != nil:
		err = fmt.Errorf("%w: sign in to Byname as an admin, then connect Google again", core.ErrForbidden)
	default:
		err = e.Services.Search.FinishGoogleOAuth(r.Context(), a, search.OAuthCallback{State: q.Get("state"), Code: q.Get("code"), Error: q.Get("error")})
	}
	if err == nil {
		http.Redirect(w, r, back+"?connected=google", http.StatusSeeOther)
		return nil
	}
	msg := err.Error()
	if code := statusOf(err); code >= 500 && code != http.StatusBadGateway {
		s.log.Error("Google sign-in failed", "err", err)
		msg = "connecting Google failed on the server; try again"
	}
	http.Redirect(w, r, back+"?error="+url.QueryEscape(msg), http.StatusSeeOther)
	return nil
}
