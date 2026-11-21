package config

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/unkeyed/byname/internal/core"
	"github.com/unkeyed/byname/internal/engines"
)

// Surface is one registry surface with how this instance uses it.
type Surface struct {
	Key        engines.Surface       `json:"key"`
	Title      string                `json:"title"`
	Endpoint   string                `json:"endpoint" jsonschema:"the pinned Treg tool id"`
	Fidelity   engines.Fidelity      `json:"fidelity" jsonschema:"the surface's default label; each pair carries its own"`
	TakesModel bool                  `json:"takes_model" jsonschema:"false on app surfaces, where the app picks the model"`
	Search     engines.SearchControl `json:"search"`
	Enabled    bool                  `json:"enabled"`
	Pairs      []Pair                `json:"pairs"`
}

// Pair is a surface and model pair: its own series with its own fidelity.
type Pair struct {
	ID             int64            `json:"id"`
	Surface        engines.Surface  `json:"surface"`
	Model          string           `json:"model" jsonschema:"empty on app surfaces"`
	Fidelity       engines.Fidelity `json:"fidelity"`
	ForceWebSearch bool             `json:"force_web_search"`
	Enabled        bool             `json:"enabled"`
	// PausedReason is set when the model left the endpoint's live model
	// list. A paused pair is skipped until the model is back.
	PausedReason string     `json:"paused_reason"`
	LastTestedAt *time.Time `json:"last_tested_at"`
}

// SurfaceInput turns a surface on or off and, when Models is present,
// replaces its models: listed models are enabled, the rest disabled. App
// surfaces take no models.
type SurfaceInput struct {
	Enabled bool        `json:"enabled"`
	Models  []PairInput `json:"models,omitempty" jsonschema:"omit to keep the surface's models as they are"`
}

type PairInput struct {
	Model string `json:"model" jsonschema:"a name on the endpoint's live model list"`
	// ForceWebSearch defaults to true where the endpoint can force search.
	ForceWebSearch *bool `json:"force_web_search,omitempty"`
}

const pairColumns = `id, surface_key, coalesce(model, ''), fidelity, force_web_search, enabled, coalesce(paused_reason, ''), last_tested_at`

func scanPair(r pgx.CollectableRow) (Pair, error) {
	var p Pair
	return p, r.Scan(&p.ID, &p.Surface, &p.Model, &p.Fidelity, &p.ForceWebSearch, &p.Enabled, &p.PausedReason, &p.LastTestedAt)
}

// Surfaces lists every surface in the registry, configured or not.
func (s *Service) Surfaces(ctx context.Context) ([]Surface, error) {
	enabled := map[engines.Surface]bool{}
	rows, err := s.db.Query(ctx, `SELECT key, enabled FROM surface`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var k engines.Surface
		var on bool
		if err := rows.Scan(&k, &on); err != nil {
			return nil, err
		}
		enabled[k] = on
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows, err = s.db.Query(ctx, `SELECT `+pairColumns+` FROM surface_model ORDER BY id`)
	if err != nil {
		return nil, err
	}
	pairs, err := pgx.CollectRows(rows, scanPair)
	if err != nil {
		return nil, err
	}
	var out []Surface
	for _, spec := range engines.All() {
		out = append(out, surfaceOf(spec, enabled[spec.Surface], pairs))
	}
	return out, nil
}

func surfaceOf(spec engines.Spec, enabled bool, pairs []Pair) Surface {
	out := Surface{
		Key: spec.Surface, Title: spec.Title, Endpoint: spec.Endpoint.ID, Fidelity: spec.Fidelity,
		TakesModel: spec.Endpoint.TakesModel(), Search: spec.Endpoint.Search, Enabled: enabled, Pairs: []Pair{},
	}
	for _, p := range pairs {
		if p.Surface == spec.Surface {
			out.Pairs = append(out.Pairs, p)
		}
	}
	return out
}

// ActivePairs are the pairs a check asks: enabled, on an enabled surface,
// and not paused.
func (s *Service) ActivePairs(ctx context.Context) ([]Pair, error) {
	rows, err := s.db.Query(ctx, `SELECT `+pairColumns+` FROM surface_model m
		WHERE enabled AND paused_reason IS NULL
		  AND EXISTS (SELECT 1 FROM surface s WHERE s.key = m.surface_key AND s.enabled)
		ORDER BY id`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanPair)
}

// PutSurface turns a surface on or off and sets its models. Each model is
// checked against the endpoint's live model list through Treg, so the
// Treg token must be set first.
func (s *Service) PutSurface(ctx context.Context, key engines.Surface, in SurfaceInput) (Surface, error) {
	spec, ok := engines.Lookup(key)
	if !ok {
		return Surface{}, fmt.Errorf("%w: no surface %q; GET /v1/surfaces lists them", core.ErrNotFound, key)
	}
	type want struct {
		model *string
		force bool
	}
	var pairs []want
	forceable := spec.Endpoint.Search == engines.SearchForceable
	if !spec.Endpoint.TakesModel() {
		if len(in.Models) > 0 {
			return Surface{}, fmt.Errorf("%w: %s takes no model, the app decides; send no models", core.ErrInvalid, spec.Title)
		}
		pairs = []want{{}}
	} else if in.Models != nil {
		var names []string
		for i, m := range in.Models {
			name := strings.TrimSpace(m.Model)
			if name == "" || slices.Contains(names, name) {
				return Surface{}, fmt.Errorf("%w: models[%d].model is empty or repeated", core.ErrInvalid, i)
			}
			force := forceable
			if m.ForceWebSearch != nil {
				if *m.ForceWebSearch && !forceable {
					return Surface{}, fmt.Errorf("%w: %s cannot force web search; leave force_web_search out", core.ErrInvalid, spec.Title)
				}
				force = *m.ForceWebSearch
			}
			names = append(names, name)
			pairs = append(pairs, want{&name, force})
		}
		if len(names) > 0 {
			if err := s.checkModels(ctx, spec, names); err != nil {
				return Surface{}, err
			}
		}
	}

	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO surface (key, enabled) VALUES ($1, $2)
			ON CONFLICT (key) DO UPDATE SET enabled = excluded.enabled, updated_at = now()`, key, in.Enabled); err != nil {
			return err
		}
		if pairs != nil {
			var keep []int64
			for _, p := range pairs {
				var id int64
				if err := tx.QueryRow(ctx, `INSERT INTO surface_model (surface_key, model, fidelity, force_web_search) VALUES ($1, $2, $3, $4)
					ON CONFLICT (surface_key, model) DO UPDATE SET enabled = true, force_web_search = excluded.force_web_search, paused_reason = NULL
					RETURNING id`, key, p.model, spec.Fidelity, p.force).Scan(&id); err != nil {
					return err
				}
				keep = append(keep, id)
			}
			if _, err := tx.Exec(ctx, `UPDATE surface_model SET enabled = false WHERE surface_key = $1 AND id <> ALL($2)`, key, keep); err != nil {
				return err
			}
		}
		var active int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM surface_model WHERE surface_key = $1 AND enabled`, key).Scan(&active); err != nil {
			return err
		}
		if in.Enabled && active == 0 {
			return fmt.Errorf("%w: %s needs at least one model; pick from GET /v1/surfaces/%s/models", core.ErrInvalid, spec.Title, key)
		}
		return nil
	})
	if err != nil {
		return Surface{}, err
	}
	rows, err := s.db.Query(ctx, `SELECT `+pairColumns+` FROM surface_model WHERE surface_key = $1 ORDER BY id`, key)
	if err != nil {
		return Surface{}, err
	}
	all, err := pgx.CollectRows(rows, scanPair)
	return surfaceOf(spec, in.Enabled, all), err
}

func (s *Service) checkModels(ctx context.Context, spec engines.Spec, names []string) error {
	live, err := s.LiveModels(ctx, spec.Surface)
	if err != nil {
		return err
	}
	var listed []string
	for _, m := range live {
		listed = append(listed, m.Name)
	}
	for _, n := range names {
		if !slices.Contains(listed, n) {
			return fmt.Errorf("%w: %q is not on %s's model list; it lists %s", core.ErrInvalid, n, spec.Title, strings.Join(listed, ", "))
		}
	}
	return nil
}

// Model is one name on an endpoint's live model list.
type Model struct {
	Name      string `json:"name"`
	WebSearch bool   `json:"web_search"`
}

// LiveModels reads a surface's live model list through Treg. The call is
// free.
func (s *Service) LiveModels(ctx context.Context, key engines.Surface) ([]Model, error) {
	spec, ok := engines.Lookup(key)
	if !ok {
		return nil, fmt.Errorf("%w: no surface %q", core.ErrNotFound, key)
	}
	if !spec.Endpoint.TakesModel() {
		return nil, fmt.Errorf("%w: %s takes no model; the app decides", core.ErrInvalid, spec.Title)
	}
	t, err := s.Treg(ctx)
	if err != nil {
		return nil, err
	}
	live, err := t.ListModels(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("%w: reading %s's model list from Treg: %v", core.ErrUpstream, spec.Title, err)
	}
	out := make([]Model, len(live))
	for i, m := range live {
		out[i] = Model{Name: m.Name, WebSearch: m.WebSearch}
	}
	return out, nil
}

// RefreshModelsIfDue rereads the model lists once a day, and an hour after
// a failed attempt. It does nothing until a model pair is enabled.
func (s *Service) RefreshModelsIfDue(ctx context.Context) error {
	var due bool
	err := s.db.QueryRow(ctx, `SELECT
		NOT EXISTS (SELECT 1 FROM job_run WHERE job = 'model_refresh' AND status = 'completed' AND started_at > $1::timestamptz - interval '1 day')
		AND NOT EXISTS (SELECT 1 FROM job_run WHERE job = 'model_refresh' AND started_at > $1::timestamptz - interval '1 hour')
		AND EXISTS (SELECT 1 FROM surface_model WHERE enabled AND model IS NOT NULL)
		AND EXISTS (SELECT 1 FROM credential WHERE kind = 'treg')`, s.now()).Scan(&due)
	if err != nil || !due {
		return err
	}
	return s.RefreshModels(ctx)
}

// RefreshModels pauses each enabled pair whose model has left its
// endpoint's live list and unpauses each whose model is back. It never
// swaps a model for another.
func (s *Service) RefreshModels(ctx context.Context) error {
	var jobID int64
	if err := s.db.QueryRow(ctx, `INSERT INTO job_run (job, trigger, started_at) VALUES ('model_refresh', 'scheduled', $1) RETURNING id`, s.now()).Scan(&jobID); err != nil {
		return err
	}
	err := s.refreshModels(ctx)
	status, msg := "completed", (*string)(nil)
	if err != nil {
		status, msg = "failed", new(err.Error())
	}
	if _, ferr := s.db.Exec(ctx, `UPDATE job_run SET status = $2, error = $3, finished_at = $4 WHERE id = $1`, jobID, status, msg, s.now()); ferr != nil && err == nil {
		err = ferr
	}
	return err
}

func (s *Service) refreshModels(ctx context.Context) error {
	rows, err := s.db.Query(ctx, `SELECT `+pairColumns+` FROM surface_model WHERE enabled AND model IS NOT NULL ORDER BY id`)
	if err != nil {
		return err
	}
	pairs, err := pgx.CollectRows(rows, scanPair)
	if err != nil {
		return err
	}
	lists := map[engines.Surface][]string{}
	for _, p := range pairs {
		listed, ok := lists[p.Surface]
		if !ok {
			live, err := s.LiveModels(ctx, p.Surface)
			if err != nil {
				return err
			}
			listed = []string{}
			for _, m := range live {
				listed = append(listed, m.Name)
			}
			lists[p.Surface] = listed
		}
		if slices.Contains(listed, p.Model) {
			_, err = s.db.Exec(ctx, `UPDATE surface_model SET paused_reason = NULL WHERE id = $1 AND paused_reason IS NOT NULL`, p.ID)
		} else if p.PausedReason == "" {
			spec, _ := engines.Lookup(p.Surface)
			reason := fmt.Sprintf("%s left %s's model list on %s; pick another model or wait for it to return", p.Model, spec.Title, s.now().UTC().Format(time.DateOnly))
			// Alert first: if pausing then fails, the next refresh pauses
			// and alerts again, and the alert is idempotent.
			if s.alerts != nil {
				if err := s.alerts.PairPaused(ctx, p.ID, p.Surface, p.Model, reason); err != nil {
					return err
				}
			}
			_, err = s.db.Exec(ctx, `UPDATE surface_model SET paused_reason = $2 WHERE id = $1 AND paused_reason IS NULL`, p.ID, reason)
		}
		if err != nil {
			return err
		}
	}
	return nil
}
