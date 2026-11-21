// Package gsc is Search Console: connecting a Google account or service
// account read-only, the daily sync of clicks, impressions and position
// into Postgres, the numbers the Search page reads, branded search terms
// and goals. It talks to Google's Search Console API directly.
package gsc

import (
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/unkeyed/byname/internal/core"
	"github.com/unkeyed/byname/internal/secretbox"
)

type Service struct {
	db        *pgxpool.Pool
	secrets   *secretbox.Box
	endpoints Endpoints
	client    OAuthClient
	publicURL *url.URL
	http      *http.Client
	terms     Terms
	alerts    Alerts
	now       func() time.Time
}

// OAuthClient is the instance's own Google OAuth client, from
// GSC_GOOGLE_CLIENT_ID and GSC_GOOGLE_CLIENT_SECRET. Without one, only a
// service account can connect.
type OAuthClient struct {
	ID     string
	Secret string
}

func (c OAuthClient) set() bool { return c.ID != "" }

type Options struct {
	Secrets *secretbox.Box
	// Endpoints default to Google's; tests and local development pass a
	// fake's (GoogleEndpoints with its base URL).
	Endpoints Endpoints
	Client    OAuthClient
	// PublicURL is where Google sends the browser back after consent, at
	// /auth/gsc/callback.
	PublicURL  *url.URL
	HTTPClient *http.Client
	// Terms decides brand against non-brand: config's branded search terms.
	Terms Terms
	// Alerts, when set, hears about goals reached and search moving after
	// each sync.
	Alerts Alerts
	Now    func() time.Time
}

func New(db *pgxpool.Pool, o Options) *Service {
	if o.Endpoints == (Endpoints{}) {
		o.Endpoints = GoogleEndpoints("")
	}
	if o.HTTPClient == nil {
		o.HTTPClient = &http.Client{Timeout: 2 * time.Minute}
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.PublicURL == nil {
		o.PublicURL = &url.URL{Scheme: "http", Host: "localhost:8080"}
	}
	return &Service{db: db, secrets: o.Secrets, endpoints: o.Endpoints, client: o.Client, publicURL: o.PublicURL, http: o.HTTPClient, terms: o.Terms, alerts: o.Alerts, now: o.Now}
}

// pacific is Search Console's day: every date Google reports is a Pacific
// Time calendar day.
var pacific = func() *time.Location {
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		return time.FixedZone("PT", -8*3600)
	}
	return loc
}()

// day is a calendar date, held as midnight UTC so arithmetic never
// crosses a daylight saving change.
type day struct{ t time.Time }

func dayOf(t time.Time) day {
	y, m, d := t.Date()
	return day{time.Date(y, m, d, 0, 0, 0, 0, time.UTC)}
}

func parseDay(field, s string) (day, error) {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return day{}, fmt.Errorf("%w: %s must be a date such as 2026-11-18, got %q", core.ErrInvalid, field, s)
	}
	return day{t}, nil
}

func (d day) String() string    { return d.t.Format(time.DateOnly) }
func (d day) add(n int) day     { return day{d.t.AddDate(0, 0, n)} }
func (d day) before(o day) bool { return d.t.Before(o.t) }
func (d day) date() time.Time   { return d.t }

// today is the current Search Console day.
func (s *Service) today() day { return dayOf(s.now().In(pacific)) }
