package mcpgrade

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/unkeyed/byname/internal/core"
	"github.com/unkeyed/byname/internal/db"
	"github.com/unkeyed/byname/internal/secretbox"
	"github.com/unkeyed/byname/internal/sources"
)

const (
	tokenSeal = "mcpgrade:token"
	keep      = 50
	maxURL    = 2048
	maxToken  = 4096
)

var gradeLock = db.EntityLock("byname.mcpgrade.grade")

type Service struct {
	db       db.Pool
	config   Config
	secrets  *secretbox.Box
	client   *http.Client
	registry string
	now      func() time.Time
}

type Options struct {
	Secrets      *secretbox.Box
	AllowPrivate bool
	UserAgent    string
	RegistryURL  string
	Now          func() time.Time
}

func New(pool db.Pool, cfg Config, o Options) *Service {
	c := sources.PublicClient(o.AllowPrivate)
	c.Transport = userAgent{next: c.Transport, agent: o.UserAgent}
	if o.Now == nil {
		o.Now = time.Now
	}
	return &Service{db: pool, config: cfg, secrets: o.Secrets, client: c, registry: o.RegistryURL, now: o.Now}
}

type userAgent struct {
	next  http.RoundTripper
	agent string
}

func (u userAgent) RoundTrip(r *http.Request) (*http.Response, error) {
	if u.agent == "" {
		return u.next.RoundTrip(r)
	}
	r = r.Clone(r.Context())
	r.Header.Set("User-Agent", u.agent)
	return u.next.RoundTrip(r)
}

type Server struct {
	URL      string    `json:"url" jsonschema:"the MCP server's Streamable HTTP endpoint; empty when none is set"`
	TokenSet bool      `json:"token_set" jsonschema:"whether a bearer token is stored for the server; the token itself is never returned"`
	Latest   *Graded   `json:"latest" jsonschema:"the newest grade in full; null before the first"`
	History  []Summary `json:"history" jsonschema:"the newest 50 grades, newest first"`
	Review   Review    `json:"review" jsonschema:"the paid review of the newest grade's tool documentation by a model; advisory, never part of the score"`
}

type Graded struct {
	ID       int64     `json:"id"`
	GradedAt time.Time `json:"graded_at"`
	URL      string    `json:"url" jsonschema:"the URL that was graded"`
	Report
}

type Summary struct {
	ID       int64     `json:"id"`
	GradedAt time.Time `json:"graded_at"`
	URL      string    `json:"url"`
	Score    int       `json:"score"`
	Grade    Letter    `json:"grade"`
	Rules    string    `json:"rules"`
}

type URLInput struct {
	URL string `json:"url" jsonschema:"the MCP server's Streamable HTTP endpoint, such as https://mcp.example.com/mcp; an empty string removes the server and its token"`
}

func (s *Service) Server(ctx context.Context) (Server, error) {
	v := Server{History: []Summary{}}
	var u *string
	if err := s.db.QueryRow(ctx, `SELECT mcp_server_url, mcp_server_token IS NOT NULL FROM instance_settings`).Scan(&u, &v.TokenSet); err != nil {
		return v, err
	}
	if u != nil {
		v.URL = *u
	}
	rows, err := s.db.Query(ctx, `SELECT id, graded_at, url, score, grade, rules FROM mcp_grade ORDER BY id DESC LIMIT $1`, keep)
	if err != nil {
		return v, err
	}
	if v.History, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (Summary, error) {
		var h Summary
		return h, r.Scan(&h.ID, &h.GradedAt, &h.URL, &h.Score, &h.Grade, &h.Rules)
	}); err != nil {
		return v, err
	}
	if len(v.History) == 0 {
		return v, s.review(ctx, &v)
	}
	g := Graded{ID: v.History[0].ID}
	var raw []byte
	if err := s.db.QueryRow(ctx, `SELECT graded_at, url, report FROM mcp_grade WHERE id = $1`, g.ID).Scan(&g.GradedAt, &g.URL, &raw); err != nil {
		return v, err
	}
	if err := json.Unmarshal(raw, &g.Report); err != nil {
		return v, fmt.Errorf("read grade %d: %w", g.ID, err)
	}
	v.Latest = &g
	return v, s.review(ctx, &v)
}

func (s *Service) SetURL(ctx context.Context, raw string) (Server, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		if _, err := s.db.Exec(ctx, `UPDATE instance_settings SET mcp_server_url = NULL, mcp_server_token = NULL, updated_at = now()`); err != nil {
			return Server{}, err
		}
		return s.Server(ctx)
	}
	endpoint, origin, err := parseURL(raw)
	if err != nil {
		return Server{}, err
	}
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var stored *string
		if err := tx.QueryRow(ctx, `SELECT mcp_server_url FROM instance_settings FOR UPDATE`).Scan(&stored); err != nil {
			return err
		}
		keepToken := false
		if stored != nil {
			_, was, err := parseURL(*stored)
			keepToken = err == nil && was == origin
		}
		_, err := tx.Exec(ctx, `UPDATE instance_settings SET mcp_server_url = $1,
			mcp_server_token = CASE WHEN $2 THEN mcp_server_token END, updated_at = now()`, endpoint, keepToken)
		return err
	})
	if err != nil {
		return Server{}, err
	}
	return s.Server(ctx)
}

func parseURL(raw string) (endpoint, origin string, err error) {
	bad := func(why string) (string, string, error) {
		return "", "", fmt.Errorf("%w: url %s", core.ErrInvalid, why)
	}
	if len(raw) > maxURL {
		return bad("must be at most 2048 characters")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.Opaque != "" {
		return bad("must be an absolute URL, such as https://mcp.example.com/mcp")
	}
	if u.User != nil {
		return bad("must not carry a user name or password; add a token instead")
	}
	u.Scheme, u.Host, u.Fragment, u.RawFragment = strings.ToLower(u.Scheme), strings.ToLower(u.Host), "", ""
	switch {
	case u.Scheme == "https":
	case u.Scheme == "http" && loopback(u.Hostname()):
	case u.Scheme == "http":
		return bad("must use https; http is only for localhost")
	default:
		return bad("must be http or https")
	}
	return u.String(), u.Scheme + "://" + u.Host, nil
}

func loopback(host string) bool {
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (s *Service) SetToken(ctx context.Context, token string) (Server, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		if _, err := s.db.Exec(ctx, `UPDATE instance_settings SET mcp_server_token = NULL, updated_at = now()`); err != nil {
			return Server{}, err
		}
		return s.Server(ctx)
	}
	if len(token) > maxToken || strings.ContainsAny(token, " \t\r\n") {
		return Server{}, fmt.Errorf("%w: token must be one bearer token of at most 4096 characters, without spaces", core.ErrInvalid)
	}
	tag, err := s.db.Exec(ctx, `UPDATE instance_settings SET mcp_server_token = $1, updated_at = now() WHERE mcp_server_url IS NOT NULL`,
		s.secrets.Seal(token, tokenSeal))
	if err != nil {
		return Server{}, err
	}
	if tag.RowsAffected() == 0 {
		return Server{}, fmt.Errorf("%w: set the MCP server URL before its token", core.ErrInvalid)
	}
	return s.Server(ctx)
}

func (s *Service) Run(ctx context.Context) (Server, error) {
	unlock, ok, err := db.TryLock(ctx, s.db, gradeLock)
	if err != nil {
		return Server{}, err
	}
	if !ok {
		return Server{}, fmt.Errorf("%w: a grade of this MCP server is already running", core.ErrConflict)
	}
	defer unlock()

	var u, sealed *string
	if err := s.db.QueryRow(ctx, `SELECT mcp_server_url, mcp_server_token FROM instance_settings`).Scan(&u, &sealed); err != nil {
		return Server{}, err
	}
	if u == nil {
		return Server{}, fmt.Errorf("%w: set the MCP server URL first", core.ErrInvalid)
	}
	t := Target{URL: *u, Client: s.client, RegistryURL: s.registry}
	if sealed != nil {
		if t.Token, err = s.secrets.Open(*sealed, tokenSeal); err != nil {
			return Server{}, fmt.Errorf("open the MCP server token: %w; an admin can set it again under Settings > MCP server", err)
		}
	}
	r, err := Grade(ctx, t)
	if err != nil {
		return Server{}, err
	}
	report, err := json.Marshal(r)
	if err != nil {
		return Server{}, err
	}
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO mcp_grade (url, score, grade, rules, report) VALUES ($1, $2, $3, $4, $5)`,
			t.URL, r.Score, r.Grade, r.Rules, report); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `DELETE FROM mcp_grade WHERE id NOT IN (SELECT id FROM mcp_grade ORDER BY id DESC LIMIT $1)`, keep)
		return err
	})
	if err != nil {
		return Server{}, err
	}
	return s.Server(ctx)
}

func (s *Service) SealSecrets(ctx context.Context) (int, error) {
	var sealed *string
	err := s.db.QueryRow(ctx, `SELECT mcp_server_token FROM instance_settings`).Scan(&sealed)
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
	_, err = s.db.Exec(ctx, `UPDATE instance_settings SET mcp_server_token = $1 WHERE mcp_server_token = $2`, s.secrets.Seal(token, tokenSeal), *sealed)
	return 1, err
}
