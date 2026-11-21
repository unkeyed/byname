package changes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/unkeyed/byname/internal/core"
	"github.com/unkeyed/byname/internal/notify"
)

// tokenSeal is the secretbox context the GitHub token is sealed under.
const tokenSeal = "changes:github-token"

// expiryWarning is how long before the token expires token_expiring fires.
const expiryWarning = 14 * 24 * time.Hour

// gitHub is GitHub's REST API with one token. Every call reads the token's
// expiry from the header GitHub sends with fine-grained tokens.
type gitHub struct {
	client  *http.Client
	base    string
	token   string
	expires *time.Time
}

func (s *Service) gitHub(token string) *gitHub {
	return &gitHub{client: s.http, base: s.github, token: token}
}

// expiryLayouts are the forms GitHub writes the token expiry header in.
var expiryLayouts = []string{"2006-01-02 15:04:05 MST", "2006-01-02 15:04:05 -0700", time.RFC3339}

// get decodes GET path into out. GitHub's refusals become messages that
// say what to fix, never echoing the token.
func (g *gitHub) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.base+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+g.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	res, err := g.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: GitHub did not answer: %v", core.ErrUpstream, err)
	}
	defer res.Body.Close()
	if raw := res.Header.Get("GitHub-Authentication-Token-Expiration"); raw != "" {
		for _, layout := range expiryLayouts {
			if t, err := time.Parse(layout, raw); err == nil {
				g.expires = &t
				break
			}
		}
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, 16<<20))
	if err != nil {
		return fmt.Errorf("%w: reading GitHub's answer: %v", core.ErrUpstream, err)
	}
	switch res.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized:
		return fmt.Errorf("%w: GitHub refused the token, which has expired or been revoked; an admin can paste a new one under Settings > Site changes", core.ErrUpstream)
	case http.StatusNotFound:
		return fmt.Errorf("%w: GitHub found no such repository, or the token cannot read it; give the token read access to its contents and pull requests", core.ErrUpstream)
	default:
		var e struct {
			Message string `json:"message"`
		}
		msg := strings.TrimSpace(string(body))
		if json.Unmarshal(body, &e) == nil && e.Message != "" {
			msg = e.Message
		}
		if len(msg) > 300 {
			msg = msg[:300]
		}
		return fmt.Errorf("%w: GitHub answered %d: %s", core.ErrUpstream, res.StatusCode, msg)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("%w: GitHub sent something that is not the expected JSON: %v", core.ErrUpstream, err)
	}
	return nil
}

type repoInfo struct {
	FullName      string `json:"full_name"`
	DefaultBranch string `json:"default_branch"`
}

type pull struct {
	Number   int        `json:"number"`
	Title    string     `json:"title"`
	HTMLURL  string     `json:"html_url"`
	MergedAt *time.Time `json:"merged_at"`
	Updated  time.Time  `json:"updated_at"`
}

type pullFile struct {
	Filename string `json:"filename"`
	Status   string `json:"status"`
}

// repoPattern is owner/name as GitHub allows them.
var repoPattern = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9-]{0,38})/([A-Za-z0-9._-]{1,100})$`)

func repoPath(owner, name string) string {
	return "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(name)
}

// RepoStatus is the repo connection as anyone with results:read sees it.
// It never carries the token.
type RepoStatus struct {
	Connected      bool       `json:"connected" jsonschema:"a token is stored"`
	Provider       string     `json:"provider" jsonschema:"github, empty before a repo was ever connected"`
	Repo           string     `json:"repo" jsonschema:"owner/name"`
	DefaultBranch  string     `json:"default_branch" jsonschema:"merges into this branch are changes"`
	TokenExpiresAt *time.Time `json:"token_expires_at" jsonschema:"when the token expires, as GitHub reported it; null for a token without expiry"`
	ExpiringSoon   bool       `json:"expiring_soon" jsonschema:"the token expires within 14 days"`
	Rules          []Rule     `json:"rules" jsonschema:"path rules, the first matching one gives a changed file's page"`
	ConnectedAt    *time.Time `json:"connected_at"`
	LastPollAt     *time.Time `json:"last_poll_at" jsonschema:"when the hourly poll last ran"`
	LastPollError  string     `json:"last_poll_error" jsonschema:"why the last poll failed, empty when it did not"`
}

// RepoStatus reports the connection, never the token.
func (s *Service) RepoStatus(ctx context.Context) (RepoStatus, error) {
	st := RepoStatus{Rules: []Rule{}}
	var owner, name string
	var rules []byte
	err := s.db.QueryRow(ctx, `SELECT ciphertext IS NOT NULL, provider, owner, name, default_branch, token_expires_at, path_rules,
			connected_at, last_poll_at, coalesce(last_poll_error, '')
		FROM repo_connection`).Scan(&st.Connected, &st.Provider, &owner, &name, &st.DefaultBranch, &st.TokenExpiresAt, &rules,
		&st.ConnectedAt, &st.LastPollAt, &st.LastPollError)
	if errors.Is(err, pgx.ErrNoRows) {
		return st, nil
	}
	if err != nil {
		return st, err
	}
	st.Repo = owner + "/" + name
	st.ExpiringSoon = st.Connected && st.TokenExpiresAt != nil && st.TokenExpiresAt.Sub(s.now()) < expiryWarning
	return st, json.Unmarshal(rules, &st.Rules)
}

// RepoInput connects a repo: owner/name and a fine-grained token with read
// access to its contents and pull requests.
type RepoInput struct {
	Repo  string `json:"repo"`
	Token string `json:"token"`
}

// ConnectRepo checks that the token reads the repo, then stores the token
// sealed. Connecting the same repo again replaces the token and keeps the
// rules and what was polled; another repo starts over with no rules. The
// next scheduler tick polls it.
func (s *Service) ConnectRepo(ctx context.Context, in RepoInput) (RepoStatus, error) {
	m := repoPattern.FindStringSubmatch(strings.TrimSpace(in.Repo))
	if m == nil {
		return RepoStatus{}, fmt.Errorf("%w: repo must be owner/name, such as northwind/site", core.ErrInvalid)
	}
	token := strings.TrimSpace(in.Token)
	if token == "" || len(token) > 500 || strings.ContainsAny(token, " \t\r\n") {
		return RepoStatus{}, fmt.Errorf("%w: token must be a GitHub fine-grained personal access token", core.ErrInvalid)
	}
	g := s.gitHub(token)
	var info repoInfo
	if err := g.get(ctx, repoPath(m[1], m[2]), &info); err != nil {
		return RepoStatus{}, err
	}
	_, err := s.db.Exec(ctx, `INSERT INTO repo_connection (owner, name, default_branch, ciphertext, token_expires_at, connected_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (id) DO UPDATE SET default_branch = excluded.default_branch, ciphertext = excluded.ciphertext,
			token_expires_at = excluded.token_expires_at, connected_at = excluded.connected_at, last_poll_at = NULL, last_poll_error = NULL,
			path_rules = CASE WHEN (repo_connection.owner, repo_connection.name) = (excluded.owner, excluded.name) THEN repo_connection.path_rules ELSE '[]' END,
			polled_through = CASE WHEN (repo_connection.owner, repo_connection.name) = (excluded.owner, excluded.name) THEN repo_connection.polled_through END,
			owner = excluded.owner, name = excluded.name`,
		m[1], m[2], info.DefaultBranch, s.secrets.Seal(token, tokenSeal), g.expires, s.now())
	if err != nil {
		return RepoStatus{}, err
	}
	return s.RepoStatus(ctx)
}

// repo is the stored connection with its token opened.
type repo struct {
	owner, name, branch string
	token               string
	rules               []compiled
	polledThrough       *time.Time
	connectedAt         time.Time
}

func (r repo) fullName() string { return r.owner + "/" + r.name }

// connection reads the connected repo; ok is false without a token.
func (s *Service) connection(ctx context.Context) (repo, bool, error) {
	var r repo
	var sealed *string
	var rules []byte
	err := s.db.QueryRow(ctx, `SELECT owner, name, default_branch, ciphertext, path_rules, polled_through, connected_at FROM repo_connection`).
		Scan(&r.owner, &r.name, &r.branch, &sealed, &rules, &r.polledThrough, &r.connectedAt)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && sealed == nil) {
		return r, false, nil
	}
	if err != nil {
		return r, false, err
	}
	if r.token, err = s.secrets.Open(*sealed, tokenSeal); err != nil {
		return r, false, fmt.Errorf("open the GitHub token: %w; an admin can paste it again under Settings > Site changes", err)
	}
	var raw []Rule
	if err := json.Unmarshal(rules, &raw); err != nil {
		return r, false, err
	}
	r.rules, err = compileRules(raw)
	return r, true, err
}

// TestRepo calls GitHub with the stored token and records the expiry it
// reports.
func (s *Service) TestRepo(ctx context.Context) (RepoStatus, error) {
	r, ok, err := s.connection(ctx)
	if err != nil {
		return RepoStatus{}, err
	}
	if !ok {
		return RepoStatus{}, fmt.Errorf("%w: no repo is connected", core.ErrNotFound)
	}
	g := s.gitHub(r.token)
	var info repoInfo
	if err := g.get(ctx, repoPath(r.owner, r.name), &info); err != nil {
		return RepoStatus{}, err
	}
	if _, err := s.db.Exec(ctx, `UPDATE repo_connection SET default_branch = $1, token_expires_at = $2`, info.DefaultBranch, g.expires); err != nil {
		return RepoStatus{}, err
	}
	if err := s.warnExpiry(ctx, r, g.expires); err != nil {
		return RepoStatus{}, err
	}
	return s.RepoStatus(ctx)
}

// DisconnectRepo deletes the token and stops polling. The repo, its rules
// and the changes found stay.
func (s *Service) DisconnectRepo(ctx context.Context) error {
	_, err := s.db.Exec(ctx, `UPDATE repo_connection SET ciphertext = NULL, token_expires_at = NULL`)
	return err
}

// SetRules replaces the path rules. The next poll reads the last 30 days
// of merges again with them, so rules added after connecting still find
// recent changes; the upsert keeps changes already found.
func (s *Service) SetRules(ctx context.Context, rules []Rule) (RepoStatus, error) {
	if rules == nil {
		return RepoStatus{}, fmt.Errorf("%w: rules is required; send [] for none", core.ErrInvalid)
	}
	c, err := compileRules(rules)
	if err != nil {
		return RepoStatus{}, err
	}
	clean := make([]Rule, len(c))
	for i, r := range c {
		clean[i] = r.Rule
	}
	raw, err := json.Marshal(clean)
	if err != nil {
		return RepoStatus{}, err
	}
	tag, err := s.db.Exec(ctx, `UPDATE repo_connection SET path_rules = $1,
		polled_through = CASE WHEN path_rules = $1 THEN polled_through END,
		last_poll_at = CASE WHEN path_rules = $1 THEN last_poll_at END`, raw)
	if err != nil {
		return RepoStatus{}, err
	}
	if tag.RowsAffected() == 0 {
		return RepoStatus{}, fmt.Errorf("%w: connect a repo under Settings > Site changes before adding path rules", core.ErrNotFound)
	}
	return s.RepoStatus(ctx)
}

// SealSecrets reseals a token sealed under ENCRYPTION_KEY_PREVIOUS.
func (s *Service) SealSecrets(ctx context.Context) (int, error) {
	var sealed *string
	err := s.db.QueryRow(ctx, `SELECT ciphertext FROM repo_connection`).Scan(&sealed)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && (sealed == nil || s.secrets.Current(*sealed))) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	token, err := s.secrets.Open(*sealed, tokenSeal)
	if err != nil {
		return 0, err
	}
	_, err = s.db.Exec(ctx, `UPDATE repo_connection SET ciphertext = $1 WHERE ciphertext = $2`, s.secrets.Seal(token, tokenSeal), *sealed)
	return 1, err
}

// warnExpiry raises token_expiring once per token, within 14 days of its
// expiry. The expiry day is in the subject, so a new token warns again.
func (s *Service) warnExpiry(ctx context.Context, r repo, expires *time.Time) error {
	if s.alerts == nil || expires == nil || expires.Sub(s.now()) >= expiryWarning {
		return nil
	}
	return s.alerts.Emit(ctx, notify.Event{
		Subject: fmt.Sprintf("github/%s/%s", r.fullName(), expires.UTC().Format(time.DateOnly)),
		Payload: notify.TokenExpiringPayload{Provider: "GitHub", Repo: r.fullName(), ExpiresAt: *expires},
	})
}

// maxPullPages and maxFilePages bound one poll: 10 pages of 100 pull requests, and 30 pages of
// files per pull request, GitHub's own limit.
const (
	maxPullPages = 10
	maxFilePages = 30
	// firstLookback is how far back the first poll after connecting, or
	// after the rules change, reads merges, so recent changes show up at
	// once.
	firstLookback = 30 * 24 * time.Hour
)

// poll reads pull requests merged into the default branch since the last
// poll and stores one change per pull request and mapped page. The list is
// sorted by last update, newest first, so it stops at the first one older
// than polled_through. Pull requests at that boundary are read again,
// which the upsert makes harmless.
func (s *Service) poll(ctx context.Context, r repo) (*time.Time, error) {
	g := s.gitHub(r.token)
	since := s.now().Add(-firstLookback)
	if r.polledThrough != nil {
		since = *r.polledThrough
	}
	newest := since
	for page := 1; page <= maxPullPages; page++ {
		var pulls []pull
		q := url.Values{"state": {"closed"}, "base": {r.branch}, "sort": {"updated"}, "direction": {"desc"}, "per_page": {"100"}, "page": {fmt.Sprint(page)}}
		if err := g.get(ctx, repoPath(r.owner, r.name)+"/pulls?"+q.Encode(), &pulls); err != nil {
			return g.expires, err
		}
		for _, p := range pulls {
			if p.Updated.Before(since) {
				return g.expires, s.advance(ctx, newest)
			}
			if p.Updated.After(newest) {
				newest = p.Updated
			}
			if p.MergedAt == nil || (r.polledThrough == nil && p.MergedAt.Before(s.now().Add(-firstLookback))) {
				continue
			}
			if err := s.storePull(ctx, g, r, p); err != nil {
				return g.expires, err
			}
		}
		if len(pulls) < 100 {
			break
		}
	}
	return g.expires, s.advance(ctx, newest)
}

func (s *Service) advance(ctx context.Context, through time.Time) error {
	_, err := s.db.Exec(ctx, `UPDATE repo_connection SET polled_through = greatest(polled_through, $1)`, through)
	return err
}

// fileKinds is what a pull request did to a page, from GitHub's file status.
// A page with several files takes the strongest: added, then removed.
var fileKinds = map[string]Kind{"added": NewPage, "removed": Removed}

// storePull maps one merged pull request's files to pages and upserts a
// change per page on (repo, pull request, page).
func (s *Service) storePull(ctx context.Context, g *gitHub, r repo, p pull) error {
	pages := map[string]Kind{}
	var order []string
	for page := 1; page <= maxFilePages; page++ {
		var files []pullFile
		q := url.Values{"per_page": {"100"}, "page": {fmt.Sprint(page)}}
		if err := g.get(ctx, fmt.Sprintf("%s/pulls/%d/files?%s", repoPath(r.owner, r.name), p.Number, q.Encode()), &files); err != nil {
			return err
		}
		for _, f := range files {
			u := pageFor(r.rules, f.Filename)
			if u == "" {
				continue
			}
			k, seen := pages[u]
			if !seen {
				order = append(order, u)
				k = Edit
			}
			if fk, ok := fileKinds[f.Status]; ok && k != NewPage {
				k = fk
			}
			pages[u] = k
		}
		if len(files) < 100 {
			break
		}
	}
	for _, u := range order {
		if _, err := s.db.Exec(ctx, `INSERT INTO change (url, kind, source, repo, pr_number, pr_title, pr_url, merged_at)
			VALUES ($1, $2, 'github', $3, $4, $5, $6, $7)
			ON CONFLICT (repo, pr_number, url) WHERE pr_number IS NOT NULL DO UPDATE SET
				kind = excluded.kind, pr_title = excluded.pr_title, pr_url = excluded.pr_url, merged_at = excluded.merged_at`,
			u, pages[u], r.fullName(), p.Number, p.Title, p.HTMLURL, *p.MergedAt); err != nil {
			return err
		}
	}
	return nil
}
