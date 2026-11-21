// Package config is everything an instance is set up with: its settings
// and schedule, the brand table, versioned question sets, the surfaces and
// models it asks on, and the Treg token. REST and MCP both call it, and so
// does the runner, which reads its plan from here.
package config

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/unkeyed/byname/internal/core"
	"github.com/unkeyed/byname/internal/engines"
	"github.com/unkeyed/byname/internal/secretbox"
)

type Service struct {
	db      *pgxpool.Pool
	secrets *secretbox.Box
	treg    engines.Treg
	now     func() time.Time
	alerts  Alerts
}

// Alerts hears when a model leaves its surface's live list and its pair is
// paused (internal/notify). It must be idempotent.
type Alerts interface {
	PairPaused(ctx context.Context, pairID int64, surface engines.Surface, model, reason string) error
}

type Options struct {
	// Secrets seals the Treg token.
	Secrets *secretbox.Box
	// TregBaseURL replaces https://treg.to, for tests and local development
	// against a fake Treg.
	TregBaseURL string
	HTTPClient  *http.Client
	// Resolve follows redirect links in citations. Nil keeps them as sent.
	Resolve engines.Resolver
	Now     func() time.Time
	// Alerts, when set, hears about each pair the model refresh pauses.
	Alerts Alerts
}

func New(db *pgxpool.Pool, o Options) *Service {
	if o.Now == nil {
		o.Now = time.Now
	}
	return &Service{
		db:      db,
		secrets: o.Secrets,
		treg:    engines.Treg{Client: o.HTTPClient, BaseURL: o.TregBaseURL, Resolve: o.Resolve},
		now:     o.Now,
		alerts:  o.Alerts,
	}
}

// Ready says what is missing before a check can run: the brand, a
// question set, an enabled surface and model pair, and the Treg token.
// It returns nil when nothing is.
func (s *Service) Ready(ctx context.Context) error {
	var brand, questions, pairs, token bool
	err := s.db.QueryRow(ctx, `SELECT
		EXISTS (SELECT 1 FROM brand WHERE kind = 'self'),
		EXISTS (SELECT 1 FROM question_set),
		EXISTS (SELECT 1 FROM surface_model m JOIN surface s ON s.key = m.surface_key
		        WHERE s.enabled AND m.enabled AND m.paused_reason IS NULL),
		EXISTS (SELECT 1 FROM credential WHERE kind = 'treg')`).Scan(&brand, &questions, &pairs, &token)
	if err != nil {
		return err
	}
	var missing []string
	for _, m := range []struct {
		ok   bool
		what string
	}{
		{brand, "your brand (PUT /v1/settings)"},
		{questions, "questions (PUT /v1/questions)"},
		{pairs, "an enabled surface (PUT /v1/surfaces/{key})"},
		{token, "the Treg token (an admin sets it under Settings)"},
	} {
		if !m.ok {
			missing = append(missing, m.what)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("%w: set %s before a check can run", core.ErrInvalid, strings.Join(missing, ", "))
	}
	return nil
}

var hostname = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)

// hostnames lowercases, trims and dedupes, keeping order, and refuses
// anything that is not a bare hostname.
func hostnames(field string, in []string) ([]string, error) {
	out := []string{}
	for _, h := range in {
		h = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(h)), ".")
		h = strings.TrimPrefix(h, "www.")
		if h == "" || slices.Contains(out, h) {
			continue
		}
		if !hostname.MatchString(h) {
			return nil, fmt.Errorf("%w: %s: %q is not a hostname; give the host only, such as example.com, without a scheme or path", core.ErrInvalid, field, h)
		}
		out = append(out, h)
	}
	return out, nil
}

// oneLine collapses runs of whitespace and trims the ends.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// words trims and dedupes a list of names, keeping order.
func words(in []string) []string {
	out := []string{}
	for _, w := range in {
		if w = oneLine(w); w != "" && !slices.Contains(out, w) {
			out = append(out, w)
		}
	}
	return out
}

// querier is a pool or a transaction.
type querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func isUniqueViolation(err error, constraint string) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23505" && pg.ConstraintName == constraint
}
