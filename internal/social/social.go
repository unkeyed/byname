// Package social is social mentions: public posts that name the brand on
// X, Reddit, Hacker News, LinkedIn, YouTube, Threads and Bluesky, found
// through one search per network (the registry in networks.go: a pinned
// Treg tool, or Bluesky's own public API), stored once per
// post, and read back without the accounts people chose to leave out,
// such as their own team, or the topics and subreddits they filtered out
// (filters.go). The digest groups a period's mentions by
// network for the social_digest event. The scheduler calls Tick each
// minute; it fetches when due and emits the digest when a period ends.
package social

import (
	"context"
	"errors"
	"fmt"
	"github.com/unkeyed/byname/internal/db"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/unkeyed/byname/internal/core"
	"github.com/unkeyed/byname/internal/engines"
	"github.com/unkeyed/byname/internal/notify"
)

// Keys opens the stored Treg token (internal/config). ok is false when
// none is stored.
type Keys interface {
	Treg(ctx context.Context) (t engines.Treg, ok bool, err error)
}

// Alerts stores and sends events (internal/notify). Emit is idempotent on
// the event's kind and subject.
type Alerts interface {
	Emit(ctx context.Context, e notify.Event) error
}

type Service struct {
	db       db.Pool
	keys     Keys
	alerts   Alerts
	client   *http.Client
	baseURLs map[Network]string
	now      func() time.Time
}

type Options struct {
	// Alerts, when set, hears about each finished digest period.
	Alerts Alerts
	// BaseURLs replace a direct network's public API, such as
	// https://api.bsky.app, for tests and local development against a fake.
	BaseURLs map[Network]string
	// HTTPClient calls direct networks.
	HTTPClient *http.Client
	Now        func() time.Time
}

func New(db db.Pool, keys Keys, o Options) *Service {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.HTTPClient == nil {
		o.HTTPClient = &http.Client{Timeout: time.Minute}
	}
	urls := map[Network]string{}
	for _, n := range registry {
		if n.Route == Direct {
			urls[n.Network] = n.BaseURL
			if u := o.BaseURLs[n.Network]; u != "" {
				urls[n.Network] = strings.TrimSuffix(u, "/")
			}
		}
	}
	return &Service{db: db, keys: keys, alerts: o.Alerts, client: o.HTTPClient, baseURLs: urls, now: o.Now}
}

// Cadence is how often a digest goes out.
type Cadence string

const (
	Daily  Cadence = "daily"
	Weekly Cadence = "weekly"
)

// fetchEvery is the fetch periods Settings offers, in hours.
var fetchEvery = []int{1, 3, 6, 12, 24}

const maxTerms = 5

// NetworkToggle turns one network on or off.
type NetworkToggle struct {
	Network Network `json:"network" jsonschema:"x, reddit, hackernews, linkedin, youtube, threads or bluesky"`
	Enabled bool    `json:"enabled"`
}

// SettingsInput replaces the social settings.
type SettingsInput struct {
	// Enabled turns social searches on or off; nil keeps the current
	// setting. They start off, because the Treg networks cost money.
	Enabled         *bool           `json:"enabled,omitempty" jsonschema:"true to search, false to stop; omit to keep the current setting. Off until someone turns it on"`
	Terms           []string        `json:"terms" jsonschema:"1 to 5 search terms, or [] to search for the brand's name and primary domain"`
	FetchEveryHours int             `json:"fetch_every_hours" jsonschema:"1, 3, 6, 12 or 24"`
	DigestCadence   Cadence         `json:"digest_cadence" jsonschema:"daily or weekly"`
	DigestWeekday   int             `json:"digest_weekday" jsonschema:"the day a weekly digest's week starts, 0 (Sunday) to 6 (Saturday)"`
	Networks        []NetworkToggle `json:"networks" jsonschema:"networks to turn on or off; a network left out keeps its setting"`
}

// NetworkStatus is one network as Settings shows it.
type NetworkStatus struct {
	Network        Network    `json:"network"`
	Title          string     `json:"title"`
	Enabled        bool       `json:"enabled"`
	Route          Route      `json:"route" jsonschema:"treg (through Treg, which needs the Treg token) or direct (the network's own public API, free)"`
	Tool           string     `json:"tool" jsonschema:"the Treg tool its search is pinned to, or the network's API method"`
	CostPerCallUSD float64    `json:"cost_per_call_usd" jsonschema:"the Treg catalog's price per call; 0 for a direct network"`
	LastCallAt     *time.Time `json:"last_call_at"`
	LastError      string     `json:"last_error" jsonschema:"why the latest call failed, empty when it worked"`
	Unavailable    bool       `json:"unavailable" jsonschema:"true when the latest call found the network's public search asking for sign-in"`
}

// Settings is what is searched, where, how often, and when the digest
// goes out.
type Settings struct {
	Enabled         bool            `json:"enabled" jsonschema:"whether social searches run; off until someone turns them on, and nothing runs before onboarding is finished either way"`
	Terms           []string        `json:"terms" jsonschema:"what each search looks for"`
	DefaultTerms    []string        `json:"default_terms" jsonschema:"the brand's name and primary domain, searched when no terms are set"`
	CustomTerms     bool            `json:"custom_terms" jsonschema:"false while terms are the default"`
	FetchEveryHours int             `json:"fetch_every_hours"`
	DigestCadence   Cadence         `json:"digest_cadence"`
	DigestWeekday   int             `json:"digest_weekday" jsonschema:"0 (Sunday) to 6 (Saturday)"`
	TimeZone        string          `json:"time_zone" jsonschema:"the instance's time zone, which periods follow"`
	TokenSet        bool            `json:"token_set" jsonschema:"whether a Treg token is stored; without one only direct networks are searched"`
	Networks        []NetworkStatus `json:"networks"`
	LastFetchAt     *time.Time      `json:"last_fetch_at"`
	NextFetchAt     *time.Time      `json:"next_fetch_at" jsonschema:"null while nothing will be searched: social is off, onboarding is not finished, there is no term, or no network on can run without the missing Treg token"`
}

// settings is the stored settings with the time zone loaded.
type settings struct {
	// on is social turned on and onboarding finished: nothing is
	// searched otherwise.
	on           bool
	enabled      bool
	terms        []string
	defaults     []string
	custom       bool
	every        int
	cadence      Cadence
	weekday      int
	loc          *time.Location
	sentThrough  *string
	monthCap     *float64
	networkOn    map[Network]bool
	lastFetchAt  *time.Time
	lastStartsAt *time.Time
}

func (s *Service) settings(ctx context.Context) (settings, error) {
	var st settings
	var zone string
	var stored []string
	var onboarded bool
	err := s.db.QueryRow(ctx, `SELECT social_enabled, onboarded_at IS NOT NULL, social_terms, social_fetch_every_hours, social_digest_cadence, social_digest_weekday, time_zone,
			to_char(social_digest_sent_through, 'YYYY-MM-DD'), spend_cap_month_usd::float8,
			(SELECT max(started_at) FROM job_run WHERE job = 'social_fetch'),
			(SELECT max(finished_at) FROM job_run WHERE job = 'social_fetch' AND status = 'completed')
		FROM instance_settings`).Scan(&st.enabled, &onboarded, &stored, &st.every, &st.cadence, &st.weekday, &zone, &st.sentThrough, &st.monthCap, &st.lastStartsAt, &st.lastFetchAt)
	if err != nil {
		return st, err
	}
	st.on = st.enabled && onboarded
	if st.loc, err = time.LoadLocation(zone); err != nil {
		st.loc = time.UTC
	}
	var name string
	var domains []string
	err = s.db.QueryRow(ctx, `SELECT name, domains FROM brand WHERE kind = 'self'`).Scan(&name, &domains)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return st, err
	}
	st.defaults = []string{}
	if name != "" {
		st.defaults = append(st.defaults, name)
	}
	if len(domains) > 0 && !strings.EqualFold(domains[0], name) {
		st.defaults = append(st.defaults, domains[0])
	}
	st.terms, st.custom = st.defaults, stored != nil
	if st.custom {
		st.terms = stored
	}
	rows, err := s.db.Query(ctx, `SELECT network, enabled FROM social_network`)
	if err != nil {
		return st, err
	}
	st.networkOn = map[Network]bool{}
	for rows.Next() {
		var n Network
		var on bool
		if err := rows.Scan(&n, &on); err != nil {
			return st, err
		}
		st.networkOn[n] = on
	}
	return st, rows.Err()
}

// networks is what a fetch searches: the networks turned on, less those
// that need the Treg token when none is stored.
func (st settings) networks(token bool) []spec {
	var out []spec
	for _, n := range registry {
		if st.networkOn[n.Network] && (token || n.Route == Direct) {
			out = append(out, n)
		}
	}
	return out
}

// Settings reads the social settings with each network's latest call.
func (s *Service) Settings(ctx context.Context) (Settings, error) {
	st, err := s.settings(ctx)
	if err != nil {
		return Settings{}, err
	}
	_, token, err := s.keys.Treg(ctx)
	if err != nil {
		return Settings{}, err
	}
	out := Settings{Enabled: st.enabled, Terms: st.terms, DefaultTerms: st.defaults, CustomTerms: st.custom, FetchEveryHours: st.every, DigestCadence: st.cadence,
		DigestWeekday: st.weekday, TimeZone: st.loc.String(), TokenSet: token, LastFetchAt: st.lastFetchAt, Networks: []NetworkStatus{}}
	rows, err := s.db.Query(ctx, `SELECT DISTINCT ON (network) network, called_at, coalesce(error, ''), status = 'unavailable'
		FROM social_call ORDER BY network, called_at DESC, id DESC`)
	if err != nil {
		return Settings{}, err
	}
	type last struct {
		at          time.Time
		err         string
		unavailable bool
	}
	latest := map[Network]last{}
	for rows.Next() {
		var n Network
		var l last
		if err := rows.Scan(&n, &l.at, &l.err, &l.unavailable); err != nil {
			return Settings{}, err
		}
		latest[n] = l
	}
	if err := rows.Err(); err != nil {
		return Settings{}, err
	}
	for _, n := range registry {
		ns := NetworkStatus{Network: n.Network, Title: n.Title, Enabled: st.networkOn[n.Network], Route: n.Route, Tool: n.Tool, CostPerCallUSD: n.CostUSD}
		if l, ok := latest[n.Network]; ok {
			ns.LastCallAt, ns.LastError, ns.Unavailable = &l.at, l.err, l.unavailable
		}
		out.Networks = append(out.Networks, ns)
	}
	if st.on && len(st.terms) > 0 && len(st.networks(token)) > 0 {
		next := s.now()
		if st.lastStartsAt != nil && st.lastStartsAt.Add(time.Duration(st.every)*time.Hour).After(next) {
			next = st.lastStartsAt.Add(time.Duration(st.every) * time.Hour)
		}
		out.NextFetchAt = &next
	}
	return out, nil
}

// cleanTerms trims terms and drops repeats, ignoring case.
func cleanTerms(in []string) ([]string, error) {
	var out []string
	for _, t := range in {
		t = strings.Join(strings.Fields(t), " ")
		if t == "" || slices.ContainsFunc(out, func(o string) bool { return strings.EqualFold(o, t) }) {
			continue
		}
		if n := len([]rune(t)); n < 2 || n > 100 {
			return nil, fmt.Errorf("%w: a search term is 2 to 100 characters, got %q", core.ErrInvalid, t)
		}
		out = append(out, t)
	}
	if len(out) > maxTerms {
		return nil, fmt.Errorf("%w: at most %d search terms", core.ErrInvalid, maxTerms)
	}
	return out, nil
}

// PutSettings replaces the social settings. Terms of [] go back to the
// brand's name and domain. A shorter fetch period takes effect at once.
func (s *Service) PutSettings(ctx context.Context, in SettingsInput) (Settings, error) {
	terms, err := cleanTerms(in.Terms)
	if err != nil {
		return Settings{}, err
	}
	switch {
	case !slices.Contains(fetchEvery, in.FetchEveryHours):
		return Settings{}, fmt.Errorf("%w: fetch_every_hours must be 1, 3, 6, 12 or 24", core.ErrInvalid)
	case in.DigestCadence != Daily && in.DigestCadence != Weekly:
		return Settings{}, fmt.Errorf("%w: digest_cadence must be daily or weekly", core.ErrInvalid)
	case in.DigestWeekday < 0 || in.DigestWeekday > 6:
		return Settings{}, fmt.Errorf("%w: digest_weekday must be 0 (Sunday) to 6 (Saturday)", core.ErrInvalid)
	}
	seen := map[Network]bool{}
	for _, t := range in.Networks {
		if _, err := parseNetwork(string(t.Network)); err != nil {
			return Settings{}, fmt.Errorf("%w: %v", core.ErrInvalid, err)
		}
		if seen[t.Network] {
			return Settings{}, fmt.Errorf("%w: network %s is listed twice", core.ErrInvalid, t.Network)
		}
		seen[t.Network] = true
	}
	var stored []string
	if len(terms) > 0 {
		stored = terms
	}
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE instance_settings SET social_terms = $1, social_fetch_every_hours = $2, social_digest_cadence = $3,
				social_digest_weekday = $4, social_enabled = coalesce($5, social_enabled), updated_at = now()`,
			stored, in.FetchEveryHours, in.DigestCadence, in.DigestWeekday, in.Enabled); err != nil {
			return err
		}
		for _, t := range in.Networks {
			if _, err := tx.Exec(ctx, `UPDATE social_network SET enabled = $2 WHERE network = $1`, t.Network, t.Enabled); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return Settings{}, err
	}
	return s.Settings(ctx)
}

// LeftOut is an account whose posts reads hide.
type LeftOut struct {
	ID      int64   `json:"id"`
	Network Network `json:"network"`
	Handle  string  `json:"handle" jsonschema:"lowercase, with no @ or u/"`
	Label   string  `json:"label" jsonschema:"who it is, such as a person's name or Our accounts; the app groups handles by it"`
	Profile string  `json:"profile" jsonschema:"the account's page on its network"`
	// Mentions is how many stored mentions it hides.
	Mentions int `json:"mentions"`
}

// LeftOutInput leaves an account out, or relabels the one already left
// out on that network.
type LeftOutInput struct {
	Network Network `json:"network" jsonschema:"x, reddit, hackernews, linkedin, youtube, threads or bluesky"`
	Handle  string  `json:"handle" jsonschema:"the handle (@name, u/name, name), a YouTube channel's name, or a link to the profile"`
	Label   string  `json:"label" jsonschema:"who it is, such as a person's name or Our accounts"`
}

func leftOutRow(r pgx.CollectableRow) (LeftOut, error) {
	var l LeftOut
	err := r.Scan(&l.ID, &l.Network, &l.Handle, &l.Label, &l.Mentions)
	if sp, ok := lookup(l.Network); ok {
		l.Profile = sp.Profile + l.Handle
	}
	return l, err
}

// LeftOutAccounts lists the left-out accounts by label, then network.
func (s *Service) LeftOutAccounts(ctx context.Context) ([]LeftOut, error) {
	rows, err := s.db.Query(ctx, `SELECT l.id, l.network, l.handle, l.label,
			(SELECT count(*) FROM social_mention m WHERE m.network = l.network AND m.author_handle = l.handle)
		FROM social_left_out l ORDER BY lower(l.label), l.label, array_position($1::text[], l.network), l.handle`, networkOrder())
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, leftOutRow)
}

func networkOrder() []string {
	out := make([]string, len(registry))
	for i, n := range registry {
		out[i] = string(n.Network)
	}
	return out
}

// LeaveOut adds an account to the left-out list, or relabels it. Its past
// mentions are hidden from then on.
func (s *Service) LeaveOut(ctx context.Context, in LeftOutInput) (LeftOut, error) {
	n, err := parseNetwork(string(in.Network))
	if err != nil {
		return LeftOut{}, fmt.Errorf("%w: %v", core.ErrInvalid, err)
	}
	handle, err := normalizeHandle(n, in.Handle)
	if err != nil {
		return LeftOut{}, fmt.Errorf("%w: %v", core.ErrInvalid, err)
	}
	label := strings.Join(strings.Fields(in.Label), " ")
	if label == "" || len([]rune(label)) > 80 {
		return LeftOut{}, fmt.Errorf("%w: label is 1 to 80 characters, such as a person's name or Our accounts", core.ErrInvalid)
	}
	rows, err := s.db.Query(ctx, `INSERT INTO social_left_out (network, handle, label) VALUES ($1, $2, $3)
		ON CONFLICT (network, handle) DO UPDATE SET label = excluded.label
		RETURNING id, network, handle, label, (SELECT count(*) FROM social_mention m WHERE m.network = $1 AND m.author_handle = $2)`, n, handle, label)
	if err != nil {
		return LeftOut{}, err
	}
	return pgx.CollectExactlyOneRow(rows, leftOutRow)
}

// RemoveLeftOut takes an account off the list, so its mentions show again.
func (s *Service) RemoveLeftOut(ctx context.Context, id int64) error {
	tag, err := s.db.Exec(ctx, `DELETE FROM social_left_out WHERE id = $1`, id)
	if err == nil && tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: no left-out account %d", core.ErrNotFound, id)
	}
	return err
}
