// Package search is search traffic from each search engine's webmaster
// tools: connecting read-only, the daily sync of clicks, impressions and
// position into Postgres, the numbers the Search page reads, branded
// search terms, goals and alerts. registry is the one list of providers.
// Each provider is one adapter file (google.go) that parses its engine's
// API into reports and never touches SQL; everything else is shared.
package search

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/unkeyed/byname/internal/core"
	"github.com/unkeyed/byname/internal/db"
	"github.com/unkeyed/byname/internal/secretbox"
)

// Provider is a search engine whose webmaster tools Byname reads. The
// values are stored in search_connection, the stats tables, goal and
// job_run, and never change once shipped.
type Provider string

const Google Provider = "google"

// registry is the one list of providers, in the order every list shows
// them. Adding a provider is its adapter file and one entry here.
var registry = []providerSpec{googleSpec}

type providerSpec struct {
	Info
	// zone is the provider's calendar: every day it reports is a day here.
	zone *time.Location
	// keyMethod is what a pasted key connects as.
	keyMethod Method
	build     func(o Options) adapter
}

// Info is what a provider is and how it reports, the same on every
// instance. The UI renders its connect form, the engine switch and the
// caveats from it.
type Info struct {
	Provider Provider     `json:"provider"`
	Title    string       `json:"title" jsonschema:"the search engine, such as Google"`
	Product  string       `json:"product" jsonschema:"its webmaster tools, such as Search Console"`
	Methods  []MethodInfo `json:"methods" jsonschema:"the ways to connect it, in the order to offer them"`
	StepDays int          `json:"step_days" jsonschema:"the days one stored row of searches or pages covers, 1 or 7; every window is a whole number of them"`
	// SitePosition is false when the provider's site totals carry no
	// average position.
	SitePosition   bool     `json:"site_position" jsonschema:"whether it reports the whole site's average position"`
	BackfillMonths int      `json:"backfill_months" jsonschema:"how far back a first sync reaches with backfill on; 0 when every sync reads all the provider keeps"`
	Notes          []string `json:"notes" jsonschema:"how its numbers differ from a plain daily count, in plain words"`
}

// MethodInfo is one way to connect a provider.
type MethodInfo struct {
	Method Method `json:"method"`
	Label  string `json:"label"`
	// Available is false for Google sign-in on an instance without its
	// own OAuth client.
	Available bool     `json:"available"`
	Steps     []string `json:"steps,omitempty" jsonschema:"for a pasted key: how to get one"`
}

// Method is how Byname signs in to a provider.
type Method string

const (
	MethodOAuth          Method = "oauth"           // Google: a person's account, through the instance's OAuth client
	MethodServiceAccount Method = "service_account" // Google: a service account's JSON key
)

func lookup(p Provider) (providerSpec, error) {
	for _, sp := range registry {
		if sp.Provider == p {
			return sp, nil
		}
	}
	names := make([]string, len(registry))
	for i, sp := range registry {
		names[i] = string(sp.Provider)
	}
	return providerSpec{}, fmt.Errorf("%w: provider must be %s, got %q", core.ErrInvalid, strings.Join(names, " or "), p)
}

type Service struct {
	db       db.Pool
	secrets  *secretbox.Box
	adapters map[Provider]adapter
	// google is adapters[Google], for what only Google has: sign-in and
	// URL Inspection.
	google *google
	terms  Terms
	alerts Alerts
	now    func() time.Time
}

// OAuthClient is the instance's own Google OAuth client, from
// GSC_GOOGLE_CLIENT_ID and GSC_GOOGLE_CLIENT_SECRET. Without one, only a
// service account can connect Google.
type OAuthClient struct {
	ID     string
	Secret string
}

func (c OAuthClient) set() bool { return c.ID != "" }

type Options struct {
	Secrets *secretbox.Box
	// BaseURLs send a provider's calls to a fake at that URL, for tests
	// and local development; a missing provider calls the real engine.
	BaseURLs map[Provider]string
	// GoogleClient is the OAuth client Sign in with Google uses.
	GoogleClient OAuthClient
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

func New(db db.Pool, o Options) *Service {
	if o.HTTPClient == nil {
		o.HTTPClient = &http.Client{Timeout: 2 * time.Minute}
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.PublicURL == nil {
		o.PublicURL = &url.URL{Scheme: "http", Host: "localhost:8080"}
	}
	s := &Service{db: db, secrets: o.Secrets, adapters: map[Provider]adapter{}, terms: o.Terms, alerts: o.Alerts, now: o.Now}
	for _, sp := range registry {
		s.adapters[sp.Provider] = sp.build(o)
	}
	s.google = s.adapters[Google].(*google)
	return s
}

// day is a calendar date in its provider's calendar, held as midnight UTC
// so arithmetic never crosses a daylight saving change.
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

// sub is the days from o to d.
func (d day) sub(o day) int { return int(d.t.Sub(o.t).Hours() / 24) }

// span is the days from to to, inclusive.
type span struct{ from, to day }

func (w span) days() int { return w.to.sub(w.from) + 1 }

// window is the span ending on anchor that is a whole number of steps
// long, as near to days as whole steps get and at least one step. A week
// is never split, so for a weekly provider 28 days stays 28 and 90 becomes
// 91.
func window(anchor day, days, step int) span {
	n := max(1, (days+step/2)/step) * step
	return span{anchor.add(1 - n), anchor}
}

// today is the provider's current day.
func (s *Service) today(sp providerSpec) day { return dayOf(s.now().In(sp.zone)) }
