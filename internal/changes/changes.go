// Package changes is site changes: merged pull requests from a connected
// GitHub repo (or changes logged by hand) mapped to page URLs, Google's
// first crawl of each page after its change (recrawl watch, through URL
// Inspection), and the interim and final difference-in-differences reads
// against control pages (verdicts). The hourly poll and the follow-up
// after each Search Console sync run from the scheduler through Tick.
package changes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/unkeyed/byname/internal/core"
	"github.com/unkeyed/byname/internal/gsc"
	"github.com/unkeyed/byname/internal/notify"
	"github.com/unkeyed/byname/internal/secretbox"
)

// Search is what changes reads from Search Console (internal/gsc).
type Search interface {
	Status(ctx context.Context) (gsc.Status, error)
	InspectURL(ctx context.Context, pageURL string) (gsc.Inspection, error)
	PageDaily(ctx context.Context, page string, from, to time.Time) ([]gsc.Day, error)
}

// Alerts stores and sends events (internal/notify). Emit is idempotent on
// the event's kind and subject.
type Alerts interface {
	Emit(ctx context.Context, e notify.Event) error
}

type Service struct {
	db      *pgxpool.Pool
	search  Search
	alerts  Alerts
	secrets *secretbox.Box
	github  string
	http    *http.Client
	now     func() time.Time
	cap     int
}

type Options struct {
	Secrets *secretbox.Box
	// Alerts, when set, hears about final verdicts and a token about to
	// expire.
	Alerts Alerts
	// GitHubBaseURL replaces https://api.github.com, for tests and local
	// development against a fake.
	GitHubBaseURL string
	HTTPClient    *http.Client
	Now           func() time.Time
	// InspectionsPerDay caps URL Inspection calls per Search Console day.
	// Google allows 2,000 per property; the default spends at most 200.
	InspectionsPerDay int
}

func New(db *pgxpool.Pool, search Search, o Options) *Service {
	if o.GitHubBaseURL == "" {
		o.GitHubBaseURL = "https://api.github.com"
	}
	if o.HTTPClient == nil {
		o.HTTPClient = &http.Client{Timeout: time.Minute}
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.InspectionsPerDay == 0 {
		o.InspectionsPerDay = 200
	}
	return &Service{db: db, search: search, alerts: o.Alerts, secrets: o.Secrets, github: strings.TrimSuffix(o.GitHubBaseURL, "/"),
		http: o.HTTPClient, now: o.Now, cap: o.InspectionsPerDay}
}

// Kind is what happened to the page.
type Kind string

const (
	NewPage Kind = "new_page"
	Edit    Kind = "edit"
	Removed Kind = "removed"
)

var kinds = map[Kind]bool{NewPage: true, Edit: true, Removed: true}

// Source is where a change came from.
type Source string

const (
	FromGitHub Source = "github"
	Manual     Source = "manual"
)

// Stage is where a change is in its life, derived from what is stored.
type Stage string

const (
	AwaitingCrawl   Stage = "awaiting_crawl"   // merged, Google has not crawled the page since
	AwaitingInterim Stage = "awaiting_interim" // crawled, the interim read is not in yet
	AwaitingVerdict Stage = "awaiting_verdict" // the interim read is in, the final one is not
	Decided         Stage = "decided"          // the final read is in
)

// Before is the page's numbers over the verdict-length window that ends
// the day before the merge.
type Before struct {
	From string `json:"from"`
	To   string `json:"to"`
	gsc.Totals
}

// Change is one page changed once, with where it is in the watch.
type Change struct {
	ID           int64      `json:"id"`
	URL          string     `json:"url"`
	Kind         Kind       `json:"kind" jsonschema:"new_page, edit or removed"`
	Source       Source     `json:"source" jsonschema:"github or manual"`
	Repo         string     `json:"repo" jsonschema:"owner/name, empty for a manual change"`
	PRNumber     int        `json:"pr_number" jsonschema:"0 for a manual change"`
	PRTitle      string     `json:"pr_title"`
	PRURL        string     `json:"pr_url"`
	MergedAt     time.Time  `json:"merged_at"`
	Notes        string     `json:"notes"`
	Stage        Stage      `json:"stage" jsonschema:"awaiting_crawl, awaiting_interim, awaiting_verdict or decided"`
	FirstCrawlAt *time.Time `json:"first_crawl_at" jsonschema:"Google's first crawl seen after the merge, null until then"`
	InterimOn    *string    `json:"interim_on" jsonschema:"the interim read's day in the instance's time zone, null until the first crawl"`
	VerdictOn    *string    `json:"verdict_on" jsonschema:"the final read's day in the instance's time zone, null until the first crawl"`
	Before       *Before    `json:"before" jsonschema:"null until those days are final in Search Console"`
	Interim      *Read      `json:"interim" jsonschema:"the provisional read, null until taken"`
	Final        *Read      `json:"final" jsonschema:"the final read, null until taken"`
	Verdict      *Outcome   `json:"verdict" jsonschema:"the final read's outcome, null until taken"`
}

func (c *Change) stage() Stage {
	switch {
	case c.Final != nil:
		return Decided
	case c.Interim != nil:
		return AwaitingVerdict
	case c.FirstCrawlAt != nil:
		return AwaitingInterim
	}
	return AwaitingCrawl
}

const changeColumns = `id, url, kind, source, coalesce(repo, ''), coalesce(pr_number, 0), coalesce(pr_title, ''), coalesce(pr_url, ''),
	merged_at, notes, first_crawl_at, to_char(interim_on, 'YYYY-MM-DD'), to_char(verdict_on, 'YYYY-MM-DD'), before, interim, final, verdict`

func scanChange(r pgx.CollectableRow) (Change, error) {
	var c Change
	var before, interim, final []byte
	if err := r.Scan(&c.ID, &c.URL, &c.Kind, &c.Source, &c.Repo, &c.PRNumber, &c.PRTitle, &c.PRURL, &c.MergedAt, &c.Notes,
		&c.FirstCrawlAt, &c.InterimOn, &c.VerdictOn, &before, &interim, &final, &c.Verdict); err != nil {
		return c, err
	}
	for _, j := range []struct {
		raw []byte
		to  any
	}{{before, &c.Before}, {interim, &c.Interim}, {final, &c.Final}} {
		if j.raw != nil {
			if err := json.Unmarshal(j.raw, j.to); err != nil {
				return c, err
			}
		}
	}
	c.Stage = c.stage()
	return c, nil
}

// List is every change, the latest merge first.
func (s *Service) List(ctx context.Context) ([]Change, error) {
	rows, err := s.db.Query(ctx, `SELECT `+changeColumns+` FROM change ORDER BY merged_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanChange)
}

// Step is one dated event in a change's life.
type Step struct {
	Kind string `json:"kind" jsonschema:"merged, inspected, crawled, interim or verdict"`
	// At is when it happened, or for interim and verdict the day it falls
	// on (YYYY-MM-DD), in the future while it is due.
	At     string `json:"at"`
	Done   bool   `json:"done" jsonschema:"false for an interim or verdict day still to come"`
	Detail string `json:"detail"`
}

// Inspection is one URL Inspection call recrawl watch made for the page.
type Inspection struct {
	CheckedAt     time.Time  `json:"checked_at"`
	Verdict       string     `json:"verdict" jsonschema:"Google's verdict: PASS, PARTIAL, FAIL or NEUTRAL"`
	CoverageState string     `json:"coverage_state"`
	LastCrawlAt   *time.Time `json:"last_crawl_at"`
	Error         string     `json:"error" jsonschema:"why Google refused the inspection, empty when it answered"`
}

// Detail is a change with its timeline and the inspections behind it.
type Detail struct {
	Change
	Timeline    []Step       `json:"timeline"`
	Inspections []Inspection `json:"inspections" jsonschema:"the latest inspections of the page since the merge, newest first"`
}

// Get is change id with its timeline.
func (s *Service) Get(ctx context.Context, id int64) (Detail, error) {
	rows, err := s.db.Query(ctx, `SELECT `+changeColumns+` FROM change WHERE id = $1`, id)
	if err != nil {
		return Detail{}, err
	}
	c, err := pgx.CollectExactlyOneRow(rows, scanChange)
	if errors.Is(err, pgx.ErrNoRows) {
		return Detail{}, fmt.Errorf("%w: no change %d", core.ErrNotFound, id)
	}
	if err != nil {
		return Detail{}, err
	}
	rows, err = s.db.Query(ctx, `SELECT checked_at, verdict, coverage_state, last_crawl_at, coalesce(error, '') FROM url_inspection
		WHERE url = $1 AND checked_at >= $2 ORDER BY checked_at DESC LIMIT 30`, c.URL, c.MergedAt)
	if err != nil {
		return Detail{}, err
	}
	ins, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Inspection])
	if err != nil {
		return Detail{}, err
	}
	return Detail{Change: c, Timeline: timeline(c, ins), Inspections: ins}, nil
}

// timeline orders a change's events: the merge, each inspection, the
// first crawl, then the interim and verdict days with their reads.
func timeline(c Change, ins []Inspection) []Step {
	steps := []Step{{Kind: "merged", At: c.MergedAt.Format(time.RFC3339), Done: true, Detail: mergedDetail(c)}}
	for i := len(ins) - 1; i >= 0; i-- {
		in := ins[i]
		d := in.CoverageState
		if in.Error != "" {
			d = "Google refused: " + in.Error
		}
		steps = append(steps, Step{Kind: "inspected", At: in.CheckedAt.Format(time.RFC3339), Done: true, Detail: d})
	}
	if c.FirstCrawlAt == nil {
		return steps
	}
	steps = append(steps, Step{Kind: "crawled", At: c.FirstCrawlAt.Format(time.RFC3339), Done: true, Detail: "Google's first crawl after the change"})
	for _, r := range []struct {
		kind string
		on   *string
		read *Read
	}{{"interim", c.InterimOn, c.Interim}, {"verdict", c.VerdictOn, c.Final}} {
		st := Step{Kind: r.kind, At: *r.on, Done: r.read != nil, Detail: "due"}
		if r.read != nil {
			st.Detail = string(r.read.Outcome)
		}
		steps = append(steps, st)
	}
	return steps
}

func mergedDetail(c Change) string {
	if c.Source == Manual {
		return "logged by hand"
	}
	return fmt.Sprintf("%s#%d %s", c.Repo, c.PRNumber, c.PRTitle)
}

// ManualChange is a change logged by hand, for a site without GitHub.
type ManualChange struct {
	URL      string `json:"url" jsonschema:"the page's full URL, as Search Console reports it"`
	Kind     Kind   `json:"kind" jsonschema:"new_page, edit or removed"`
	MergedAt string `json:"merged_at" jsonschema:"when it went live: a time such as 2026-11-18T15:04:05Z, or a day such as 2026-11-18 (midnight in the instance's time zone)"`
	Notes    string `json:"notes,omitempty"`
}

// LogChange stores a change made outside GitHub.
func (s *Service) LogChange(ctx context.Context, in ManualChange) (Detail, error) {
	page, err := pageURL(in.URL)
	if err != nil {
		return Detail{}, err
	}
	if !kinds[in.Kind] {
		return Detail{}, fmt.Errorf("%w: kind must be new_page, edit or removed, got %q", core.ErrInvalid, in.Kind)
	}
	if len(in.Notes) > 2000 {
		return Detail{}, fmt.Errorf("%w: notes are at most 2,000 characters", core.ErrInvalid)
	}
	st, err := s.settings(ctx)
	if err != nil {
		return Detail{}, err
	}
	at, err := time.Parse(time.RFC3339, in.MergedAt)
	if err != nil {
		day, derr := time.ParseInLocation(time.DateOnly, in.MergedAt, st.loc)
		if derr != nil {
			return Detail{}, fmt.Errorf("%w: merged_at must be a time such as 2026-11-18T15:04:05Z or a day such as 2026-11-18, got %q", core.ErrInvalid, in.MergedAt)
		}
		at = day
	}
	if at.After(s.now().Add(time.Hour)) {
		return Detail{}, fmt.Errorf("%w: merged_at is in the future; log a change once it is live", core.ErrInvalid)
	}
	var id int64
	if err := s.db.QueryRow(ctx, `INSERT INTO change (url, kind, source, merged_at, notes) VALUES ($1, $2, 'manual', $3, $4) RETURNING id`,
		page, in.Kind, at, strings.TrimSpace(in.Notes)).Scan(&id); err != nil {
		return Detail{}, err
	}
	return s.Get(ctx, id)
}

// DeleteChange deletes a change logged by hand. Changes from GitHub follow
// the repo and its path rules, so they cannot be deleted one by one.
func (s *Service) DeleteChange(ctx context.Context, id int64) error {
	var source Source
	err := s.db.QueryRow(ctx, `DELETE FROM change WHERE id = $1 AND source = 'manual' RETURNING source`, id).Scan(&source)
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if err := s.db.QueryRow(ctx, `SELECT source FROM change WHERE id = $1`, id).Scan(&source); errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: no change %d", core.ErrNotFound, id)
	} else if err != nil {
		return err
	}
	return fmt.Errorf("%w: change %d came from GitHub; edit the path rules instead", core.ErrConflict, id)
}

// pageURL parses a page URL at the boundary.
func pageURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || len(raw) > 2000 {
		return "", fmt.Errorf("%w: url must be a page's full URL such as https://www.example.com/pricing, got %q", core.ErrInvalid, raw)
	}
	return raw, nil
}

// Marker is one merge to mark on the Search page's daily chart.
type Marker struct {
	Date      string  `json:"date" jsonschema:"the Search Console day (Pacific Time) of the merge"`
	Label     string  `json:"label"`
	ChangeIDs []int64 `json:"change_ids"`
}

// MarkerRange asks for merges on days From to To, Search Console days.
type MarkerRange struct {
	From string `json:"from" jsonschema:"first day, such as 2026-08-01"`
	To   string `json:"to" jsonschema:"last day, such as 2026-11-18"`
}

// Markers is one marker per pull request (or manual change) merged in r,
// oldest first.
func (s *Service) Markers(ctx context.Context, r MarkerRange) ([]Marker, error) {
	from, err1 := time.Parse(time.DateOnly, r.From)
	to, err2 := time.Parse(time.DateOnly, r.To)
	if err1 != nil || err2 != nil || to.Before(from) || to.Sub(from) > 500*24*time.Hour {
		return nil, fmt.Errorf("%w: from and to must be days such as 2026-08-01, from on or before to, at most 500 days apart", core.ErrInvalid)
	}
	rows, err := s.db.Query(ctx, `SELECT to_char((merged_at AT TIME ZONE 'America/Los_Angeles')::date, 'YYYY-MM-DD') AS day,
			CASE WHEN source = 'github' THEN '#' || pr_number || ' ' || min(pr_title) ELSE 'Logged: ' || min(url) END,
			array_agg(id ORDER BY id)
		FROM change WHERE (merged_at AT TIME ZONE 'America/Los_Angeles')::date BETWEEN $1 AND $2
		GROUP BY day, source, repo, pr_number, CASE WHEN source = 'manual' THEN id END
		ORDER BY day, min(id)`, from, to)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Marker])
}

// ControlPage is a page held unchanged as the verdicts' baseline.
type ControlPage struct {
	ID        int64  `json:"id"`
	URL       string `json:"url"`
	HoldUntil string `json:"hold_until" jsonschema:"the last day it counts as a control"`
	Note      string `json:"note"`
	// Active is false once hold_until has passed in the instance's time
	// zone, when the page drops out of the control set.
	Active bool `json:"active"`
}

// ControlInput adds a control page, or changes the hold date and note of
// the one with the same URL.
type ControlInput struct {
	URL       string `json:"url"`
	HoldUntil string `json:"hold_until" jsonschema:"the last day it counts as a control, such as 2028-03-15"`
	Note      string `json:"note,omitempty"`
}

// ControlPages lists the control pages by URL.
func (s *Service) ControlPages(ctx context.Context) ([]ControlPage, error) {
	st, err := s.settings(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(ctx, `SELECT id, url, to_char(hold_until, 'YYYY-MM-DD'), note, hold_until >= $1::date FROM control_page ORDER BY url`, st.today(s.now()))
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[ControlPage])
}

// UpsertControlPage adds a control page or updates the one with its URL.
func (s *Service) UpsertControlPage(ctx context.Context, in ControlInput) (ControlPage, error) {
	page, err := pageURL(in.URL)
	if err != nil {
		return ControlPage{}, err
	}
	if _, err := time.Parse(time.DateOnly, in.HoldUntil); err != nil {
		return ControlPage{}, fmt.Errorf("%w: hold_until must be a day such as 2028-03-15, got %q", core.ErrInvalid, in.HoldUntil)
	}
	if len(in.Note) > 500 {
		return ControlPage{}, fmt.Errorf("%w: a note is at most 500 characters", core.ErrInvalid)
	}
	var id int64
	if err := s.db.QueryRow(ctx, `INSERT INTO control_page (url, hold_until, note) VALUES ($1, $2, $3)
		ON CONFLICT (url) DO UPDATE SET hold_until = excluded.hold_until, note = excluded.note RETURNING id`,
		page, in.HoldUntil, strings.TrimSpace(in.Note)).Scan(&id); err != nil {
		return ControlPage{}, err
	}
	pages, err := s.ControlPages(ctx)
	for _, p := range pages {
		if p.ID == id {
			return p, err
		}
	}
	return ControlPage{}, err
}

// DeleteControlPage removes a control page.
func (s *Service) DeleteControlPage(ctx context.Context, id int64) error {
	tag, err := s.db.Exec(ctx, `DELETE FROM control_page WHERE id = $1`, id)
	if err == nil && tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: no control page %d", core.ErrNotFound, id)
	}
	return err
}
