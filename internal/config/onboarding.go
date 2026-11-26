package config

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"

	"github.com/unkeyed/byname/internal/core"
	"github.com/unkeyed/byname/internal/db"
	"github.com/unkeyed/byname/internal/engines"
)

// StepKey is one onboarding step. Each step's data is saved through its
// own route (settings, brands, questions, surfaces); a step is marked saved
// once its data is, or when an optional step is skipped.
type StepKey string

const (
	StepBrand       StepKey = "brand"
	StepCompetitors StepKey = "competitors"
	StepQuestions   StepKey = "questions"
	StepSurfaces    StepKey = "surfaces"
	StepSearch      StepKey = "search"
	StepSocial      StepKey = "social"
	StepSchedule    StepKey = "schedule"
	StepReview      StepKey = "review"
)

type stepSpec struct {
	key      StepKey
	title    string
	required bool
}

// steps is the one list of onboarding steps, in order. Review is where
// onboarding finishes, so it is never saved on its own.
var steps = []stepSpec{
	{StepBrand, "Brand and domain", true},
	{StepCompetitors, "Competitors", false},
	{StepQuestions, "Questions", true},
	{StepSurfaces, "AI apps", true},
	{StepSearch, "Search engines", false},
	{StepSocial, "Social mentions", false},
	{StepSchedule, "Schedule and spend caps", true},
	{StepReview, "Review", true},
}

type Step struct {
	Key      StepKey `json:"key"`
	Title    string  `json:"title"`
	Saved    bool    `json:"saved"`
	Required bool    `json:"required"`
}

// Onboarding is how far setup has come. Missing lists, in plain words,
// what still blocks finishing.
type Onboarding struct {
	Steps      []Step     `json:"steps"`
	FinishedAt *time.Time `json:"finished_at"`
	Missing    []string   `json:"missing"`
	// FirstScheduledCheck is the scheduled check finishing now would set,
	// so the review can say when the second check runs.
	FirstScheduledCheck *UpcomingCheck `json:"first_scheduled_check" jsonschema:"the scheduled check finishing now would set, in the instance's time zone; null once finished"`
}

// Onboarded reports whether onboarding is finished.
func (s *Service) Onboarded(ctx context.Context) (bool, error) {
	var done bool
	err := s.db.QueryRow(ctx, `SELECT onboarded_at IS NOT NULL FROM instance_settings`).Scan(&done)
	return done, err
}

func (s *Service) Onboarding(ctx context.Context) (Onboarding, error) {
	return s.onboarding(ctx, s.db)
}

func (s *Service) onboarding(ctx context.Context, q querier) (Onboarding, error) {
	var saved []string
	var out Onboarding
	if err := q.QueryRow(ctx, `SELECT onboarding_steps, onboarded_at FROM instance_settings`).Scan(&saved, &out.FinishedAt); err != nil {
		return out, err
	}
	for _, st := range steps {
		done := slices.Contains(saved, string(st.key)) || (st.key == StepReview && out.FinishedAt != nil)
		out.Steps = append(out.Steps, Step{Key: st.key, Title: st.title, Saved: done, Required: st.required})
	}
	out.Missing = []string{}
	if out.FinishedAt != nil {
		return out, nil
	}
	cur, err := s.settingsView(ctx, q)
	if err != nil {
		return out, err
	}
	now := s.now()
	out.FirstScheduledCheck = upcoming(NextCheck(now, cur.LastCheckAt, cur.Schedule()), now, cur.Schedule())
	missing, err := s.readiness(ctx)
	if err != nil {
		return out, err
	}
	for _, st := range out.Steps {
		if st.Required && !st.Saved && st.Key != StepReview {
			missing = append(missing, fmt.Sprintf("save the %s step", sentenceCase(st.Title)))
		}
	}
	untested, err := s.untestedPairs(ctx, q)
	if err != nil {
		return out, err
	}
	if len(untested) > 0 {
		missing = append(missing, "a passing test for "+strings.Join(untested, ", "))
	}
	out.Missing = append(out.Missing, missing...)
	return out, nil
}

// sentenceCase lowercases a title's first letter to sit inside a sentence,
// and leaves an acronym such as "AI" alone.
func sentenceCase(title string) string {
	if len(title) > 1 && unicode.IsUpper(rune(title[1])) {
		return title
	}
	return strings.ToLower(title[:1]) + title[1:]
}

// untestedPairs names each active pair whose last test did not pass.
func (s *Service) untestedPairs(ctx context.Context, q querier) ([]string, error) {
	rows, err := q.Query(ctx, `SELECT m.surface_key, coalesce(m.model, '') FROM surface_model m JOIN surface s ON s.key = m.surface_key
		WHERE s.enabled AND m.enabled AND m.paused_reason IS NULL AND m.test_status IS DISTINCT FROM 'ok'
		ORDER BY m.id`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (string, error) {
		var key engines.Surface
		var model string
		if err := r.Scan(&key, &model); err != nil {
			return "", err
		}
		spec, _ := engines.Lookup(key)
		if model == "" {
			return spec.Title, nil
		}
		return spec.Title + " on " + model, nil
	})
}

// SaveStep marks an onboarding step saved. Marking it again changes
// nothing.
func (s *Service) SaveStep(ctx context.Context, key StepKey) (Onboarding, error) {
	i := slices.IndexFunc(steps, func(st stepSpec) bool { return st.key == key })
	if i < 0 || key == StepReview {
		return Onboarding{}, fmt.Errorf("%w: no onboarding step %q; review is finished with POST /v1/onboarding/finish", core.ErrNotFound, key)
	}
	if _, err := s.db.Exec(ctx, `UPDATE instance_settings SET onboarding_steps = array_append(onboarding_steps, $1), updated_at = now()
		WHERE NOT ($1 = ANY(onboarding_steps))`, string(key)); err != nil {
		return Onboarding{}, err
	}
	if key == StepBrand {
		if err := s.nameFirstWorkspace(ctx); err != nil {
			return Onboarding{}, err
		}
	}
	return s.Onboarding(ctx)
}

// nameFirstWorkspace names the instance's first workspace after its brand
// when it still has the name and slug a new instance starts with, before
// any link to it exists. A workspace added later keeps the name it was
// given.
func (s *Service) nameFirstWorkspace(ctx context.Context) error {
	var name string
	err := s.db.QueryRow(ctx, `SELECT name FROM brand WHERE kind = 'self'`).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return db.NameFirstWorkspace(ctx, s.db, name)
}

// FinishOnboarding ends onboarding once every required step is saved, the
// instance is ready to check and every active pair has passed its test.
// It sets the first scheduled check; "Start first check" then queues one
// with Run now. With no monthly spend cap set, it sets DefaultMonthCap of
// the checks' monthly estimate plus socialMonthUSD, what social searches
// cost a month at their current settings. Finishing again changes nothing.
func (s *Service) FinishOnboarding(ctx context.Context, socialMonthUSD float64) (Onboarding, error) {
	costs, err := s.CheckCosts(ctx)
	if err != nil {
		return Onboarding{}, err
	}
	monthCap := DefaultMonthCap(costs.MonthUSD + socialMonthUSD)
	var out Onboarding
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		cur, err := s.settingsView(ctx, tx)
		if err != nil {
			return err
		}
		if cur.OnboardedAt != nil {
			out, err = s.onboarding(ctx, tx)
			return err
		}
		state, err := s.onboarding(ctx, tx)
		if err != nil {
			return err
		}
		if len(state.Missing) > 0 {
			return fmt.Errorf("%w: onboarding needs %s", core.ErrInvalid, strings.Join(state.Missing, "; "))
		}
		next := NextCheck(s.now(), cur.LastCheckAt, cur.Schedule())
		if _, err := tx.Exec(ctx, `UPDATE instance_settings SET onboarded_at = $1, next_check_at = $2,
				spend_cap_month_usd = coalesce(spend_cap_month_usd, $3), updated_at = now()`, s.now(), next, monthCap); err != nil {
			return err
		}
		out, err = s.onboarding(ctx, tx)
		return err
	})
	return out, err
}
