package httpapi

import (
	"net/http"

	"github.com/unkeyed/byname/internal/core"
	"github.com/unkeyed/byname/internal/social"
)

// Social mentions: results:read reads mentions, digests, the settings and
// the left-out list; config:write edits the settings and the list.

// listMentions query ?network=&since=&include_hidden=true&limit=N.
func (s *server) listMentions(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	limit, err := queryInt(r, "limit")
	if err != nil {
		return err
	}
	q := r.URL.Query()
	v, err := s.svc.Social.Mentions(r.Context(), social.MentionQuery{
		Network: social.Network(q.Get("network")), Since: q.Get("since"), IncludeHidden: q.Get("include_hidden") == "true", Limit: limit,
	})
	return respond(w, http.StatusOK, v, err)
}

// socialDigest query ?day=YYYY-MM-DD, any day in the period.
func (s *server) socialDigest(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	v, err := s.svc.Social.Digest(r.Context(), social.DigestQuery{Day: r.URL.Query().Get("day")})
	return respond(w, http.StatusOK, v, err)
}

func (s *server) socialSettings(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	v, err := s.svc.Social.Settings(r.Context())
	return respond(w, http.StatusOK, v, err)
}

func (s *server) putSocialSettings(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	var b social.SettingsInput
	if err := readJSON(r, &b); err != nil {
		return err
	}
	v, err := s.svc.Social.PutSettings(r.Context(), b)
	return respond(w, http.StatusOK, v, err)
}

func (s *server) listLeftOut(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	v, err := s.svc.Social.LeftOutAccounts(r.Context())
	return respond(w, http.StatusOK, v, err)
}

func (s *server) leaveOut(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	var b social.LeftOutInput
	if err := readJSON(r, &b); err != nil {
		return err
	}
	v, err := s.svc.Social.LeaveOut(r.Context(), b)
	return respond(w, http.StatusOK, v, err)
}

func (s *server) removeLeftOut(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	id, err := pathID(r, "left-out account")
	if err != nil {
		return err
	}
	return noContent(w, s.svc.Social.RemoveLeftOut(r.Context(), id))
}
