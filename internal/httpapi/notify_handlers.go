package httpapi

import (
	"net/http"

	"github.com/unkeyed/byname/internal/core"
	"github.com/unkeyed/byname/internal/notify"
)

// Alerts are results:read, with read state per person. Notification
// channels hold secrets, so only a signed-in admin manages them.

// listAlerts query ?unread=true&limit=N.
func (s *server) listAlerts(w http.ResponseWriter, r *http.Request, a core.Actor) error {
	limit, err := queryInt(r, "limit")
	if err != nil {
		return err
	}
	v, err := s.svc.Notify.Alerts(r.Context(), a, notify.AlertQuery{Unread: r.URL.Query().Get("unread") == "true", Limit: limit})
	return respond(w, http.StatusOK, v, err)
}

// markAlertsRead body {"ids": [1, 2]} or {"all": true}.
func (s *server) markAlertsRead(w http.ResponseWriter, r *http.Request, a core.Actor) error {
	var b notify.ReadInput
	if err := readJSON(r, &b); err != nil {
		return err
	}
	v, err := s.svc.Notify.MarkRead(r.Context(), a, b)
	return respond(w, http.StatusOK, v, err)
}

func (s *server) listChannels(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	v, err := s.svc.Notify.Channels(r.Context())
	return respond(w, http.StatusOK, v, err)
}

func (s *server) createChannel(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	var b notify.ChannelInput
	if err := readJSON(r, &b); err != nil {
		return err
	}
	return s.recentOnly(r, func() error {
		v, err := s.svc.Notify.CreateChannel(r.Context(), b)
		return respond(w, http.StatusCreated, v, err)
	})
}

func (s *server) updateChannel(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	id, err := pathID(r, "notification channel")
	if err != nil {
		return err
	}
	var b notify.ChannelInput
	if err := readJSON(r, &b); err != nil {
		return err
	}
	return s.recentOnly(r, func() error {
		v, err := s.svc.Notify.UpdateChannel(r.Context(), id, b)
		return respond(w, http.StatusOK, v, err)
	})
}

func (s *server) deleteChannel(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	id, err := pathID(r, "notification channel")
	if err != nil {
		return err
	}
	return noContent(w, s.svc.Notify.DeleteChannel(r.Context(), id))
}

func (s *server) testChannel(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	id, err := pathID(r, "notification channel")
	if err != nil {
		return err
	}
	v, err := s.svc.Notify.TestChannel(r.Context(), id)
	return respond(w, http.StatusOK, v, err)
}

// listDeliveries query ?channel=ID&limit=N.
func (s *server) listDeliveries(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	channel, err := queryInt(r, "channel")
	if err != nil {
		return err
	}
	limit, err := queryInt(r, "limit")
	if err != nil {
		return err
	}
	v, err := s.svc.Notify.Deliveries(r.Context(), notify.DeliveryQuery{Channel: int64(channel), Limit: limit})
	return respond(w, http.StatusOK, v, err)
}
