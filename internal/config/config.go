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
	db       *pgxpool.Pool
	secrets  *secretbox.Box
	client   *http.Client
	resolve  engines.Resolver
	baseURLs map[engines.CredentialKind]string
	now      func() time.Time
	alerts   Alerts
}

// Alerts hears when a model leaves its surface's live list and its pair is
// paused (internal/notify). It must be idempotent.
type Alerts interface {
	PairPaused(ctx context.Context, pairID int64, surface engines.Surface, model, reason string) error
}

type Options struct {
	// Secrets seals the Treg token and provider keys.
	Secrets *secretbox.Box
	// TregBaseURL replaces https://treg.to, for tests and local development
	// against a fake Treg.
	TregBaseURL string
	// BaseURLs replace providers' public URLs the same way, per key kind.
	BaseURLs   map[engines.CredentialKind]string
	HTTPClient *http.Client
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
	urls := map[engines.CredentialKind]string{engines.CredentialTreg: o.TregBaseURL}
	for k, v := range o.BaseURLs {
		urls[k] = v
	}
	return &Service{db: db, secrets: o.Secrets, client: o.HTTPClient, resolve: o.Resolve, baseURLs: urls, now: o.Now, alerts: o.Alerts}
}

// Ready says what is missing before a check can run: everything
// readiness lists, and a finished onboarding. It returns nil when nothing is.
func (s *Service) Ready(ctx context.Context) error {
	missing, err := s.readiness(ctx)
	if err != nil {
		return err
	}
	var onboarded bool
	if err := s.db.QueryRow(ctx, `SELECT onboarded_at IS NOT NULL FROM instance_settings`).Scan(&onboarded); err != nil {
		return err
	}
	if !onboarded {
		missing = append(missing, "a finished onboarding")
	}
	if len(missing) > 0 {
		return fmt.Errorf("%w: a check needs %s", core.ErrInvalid, strings.Join(missing, "; "))
	}
	return nil
}

// readiness lists what a check needs and the instance lacks: the brand, a
// question set, an enabled surface and model pair, and the Treg token when
// an enabled surface uses the Treg route. Other routes cannot lack their
// key: a surface cannot move to a route whose key is unset, and a key in
// use cannot be deleted.
func (s *Service) readiness(ctx context.Context) ([]string, error) {
	var brand, questions, pairs, token bool
	err := s.db.QueryRow(ctx, `SELECT
		EXISTS (SELECT 1 FROM brand WHERE kind = 'self'),
		EXISTS (SELECT 1 FROM question_set),
		EXISTS (SELECT 1 FROM surface_model m JOIN surface s ON s.key = m.surface_key
		        WHERE s.enabled AND m.enabled AND m.paused_reason IS NULL),
		EXISTS (SELECT 1 FROM credential WHERE kind = 'treg')
		OR NOT EXISTS (SELECT 1 FROM surface WHERE enabled AND route = 'treg')`).Scan(&brand, &questions, &pairs, &token)
	if err != nil {
		return nil, err
	}
	var missing []string
	for _, m := range []struct {
		ok   bool
		what string
	}{
		{brand, "your brand and domain"},
		{questions, "at least one question"},
		{pairs, "at least one surface where people ask, turned on with its models"},
		{token, "the Treg token, which an admin sets under Settings > Credentials, or every surface on its own key or a gateway"},
	} {
		if !m.ok {
			missing = append(missing, m.what)
		}
	}
	return missing, nil
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
