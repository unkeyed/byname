package config

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/unkeyed/byname/internal/core"
	"github.com/unkeyed/byname/internal/engines"
)

// Surface is one registry surface with how this instance uses it.
type Surface struct {
	Key   engines.Surface `json:"key"`
	Title string          `json:"title"`
	Group SurfaceGroup    `json:"group"`
	// Endpoint, Fidelity, TakesModel and Search are on the current route.
	Endpoint   string                 `json:"endpoint" jsonschema:"the endpoint id answers on the current route store"`
	Fidelity   engines.Fidelity       `json:"fidelity" jsonschema:"the label on the current route before a test; each pair carries its own"`
	TakesModel bool                   `json:"takes_model" jsonschema:"false on app surfaces read by Treg or a scraping key, where the app picks the model"`
	Search     engines.SearchControl  `json:"search"`
	Route      engines.Route          `json:"route"`
	Credential engines.CredentialKind `json:"credential" jsonschema:"the key the current route calls with"`
	BaseURL    string                 `json:"base_url" jsonschema:"the gateway's base URL, only on the gateway route"`
	Routes     []RouteOption          `json:"routes" jsonschema:"every route the surface can take, Treg first"`
	Enabled    bool                   `json:"enabled"`
	DefaultOn  bool                   `json:"default_on" jsonschema:"ticked by default in onboarding"`
	Pairs      []Pair                 `json:"pairs"`
}

// SurfaceGroup is how onboarding groups surfaces.
type SurfaceGroup string

const (
	GroupApps           SurfaceGroup = "apps"
	GroupDeveloperTools SurfaceGroup = "developer_tools"
	GroupApproximations SurfaceGroup = "approximations"
)

var surfaceGroups = map[engines.Fidelity]SurfaceGroup{
	engines.FidelityApp:           GroupApps,
	engines.FidelityModelMatch:    GroupDeveloperTools,
	engines.FidelityApproximation: GroupApproximations,
}

// defaultOn is what onboarding ticks: the apps most buyers use, which
// cost well under a cent an answer. Developer tools cost ten times more and
// matter only when developers are the buyers, so none is ticked.
var defaultOn = []engines.Surface{engines.ChatGPTApp, engines.GeminiApp, engines.GoogleAIMode}

// RouteOption is one way a surface can be asked, with the label its pairs
// get there before a test, so the change shows before it is saved.
type RouteOption struct {
	Route      engines.Route          `json:"route"`
	Credential engines.CredentialKind `json:"credential"`
	Provider   string                 `json:"provider"`
	Fidelity   engines.Fidelity       `json:"fidelity"`
	TakesModel bool                   `json:"takes_model"`
	Search     engines.SearchControl  `json:"search"`
}

// Pair is a surface and model pair: its own series with its own fidelity.
type Pair struct {
	ID             int64            `json:"id"`
	Surface        engines.Surface  `json:"surface"`
	Model          string           `json:"model" jsonschema:"empty where the app picks the model"`
	Fidelity       engines.Fidelity `json:"fidelity"`
	ForceWebSearch bool             `json:"force_web_search"`
	Enabled        bool             `json:"enabled"`
	// PausedReason is set when the model left the route's live model list.
	// A paused pair is skipped until the model is back.
	PausedReason string     `json:"paused_reason"`
	LastTestedAt *time.Time `json:"last_tested_at"`
	TestStatus   string     `json:"test_status" jsonschema:"empty until tested, ok, or why the last test failed"`
	TestSearched *bool      `json:"test_searched"`
}

// SurfaceInput turns a surface on or off, moves it to another route, and,
// when Models is present, replaces its models: listed models are enabled,
// the rest disabled.
type SurfaceInput struct {
	Enabled bool          `json:"enabled"`
	Route   engines.Route `json:"route,omitempty" jsonschema:"treg, scraper, provider or gateway; omit to keep the current route"`
	BaseURL string        `json:"base_url,omitempty" jsonschema:"the gateway's base URL, such as https://openrouter.ai/api/v1; gateway route only"`
	Models  []PairInput   `json:"models,omitempty" jsonschema:"omit to keep the surface's models as they are"`
}

type PairInput struct {
	Model string `json:"model" jsonschema:"a name on the route's live model list"`
	// ForceWebSearch defaults to true where the route can force search.
	ForceWebSearch *bool `json:"force_web_search,omitempty"`
}

const pairColumns = `id, surface_key, coalesce(model, ''), fidelity, force_web_search, enabled, coalesce(paused_reason, ''),
	last_tested_at, coalesce(test_status, ''), test_searched`

func scanPair(r pgx.CollectableRow) (Pair, error) {
	var p Pair
	return p, r.Scan(&p.ID, &p.Surface, &p.Model, &p.Fidelity, &p.ForceWebSearch, &p.Enabled, &p.PausedReason,
		&p.LastTestedAt, &p.TestStatus, &p.TestSearched)
}

// surfaceState is a surface's row; a surface with no row is off, on Treg.
type surfaceState struct {
	enabled bool
	route   engines.Route
	baseURL string
}

func (s *Service) surfaceStates(ctx context.Context, q querier) (map[engines.Surface]surfaceState, error) {
	out := map[engines.Surface]surfaceState{}
	for _, spec := range engines.All() {
		out[spec.Surface] = surfaceState{route: engines.RouteTreg}
	}
	rows, err := q.Query(ctx, `SELECT key, enabled, route, coalesce(base_url, '') FROM surface`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var k engines.Surface
		var st surfaceState
		if err := rows.Scan(&k, &st.enabled, &st.route, &st.baseURL); err != nil {
			return nil, err
		}
		out[k] = st
	}
	return out, rows.Err()
}

// Surfaces lists every surface in the registry, configured or not.
func (s *Service) Surfaces(ctx context.Context) ([]Surface, error) {
	states, err := s.surfaceStates(ctx, s.db)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(ctx, `SELECT `+pairColumns+` FROM surface_model ORDER BY id`)
	if err != nil {
		return nil, err
	}
	pairs, err := pgx.CollectRows(rows, scanPair)
	if err != nil {
		return nil, err
	}
	var out []Surface
	for _, spec := range engines.All() {
		out = append(out, surfaceOf(spec, states[spec.Surface], pairs))
	}
	return out, nil
}

func surfaceOf(spec engines.Spec, st surfaceState, pairs []Pair) Surface {
	rs, err := engines.RouteOf(spec.Surface, st.route)
	if err != nil {
		rs, _ = engines.RouteOf(spec.Surface, engines.RouteTreg)
	}
	out := Surface{
		Key: spec.Surface, Title: spec.Title, Group: surfaceGroups[spec.Fidelity],
		Endpoint: rs.EndpointID(st.baseURL), Fidelity: engines.PairFidelity(rs, nil), TakesModel: rs.TakesModel(), Search: rs.Search(),
		Route: rs.Route, Credential: rs.Credential(), BaseURL: st.baseURL,
		Enabled: st.enabled, DefaultOn: slices.Contains(defaultOn, spec.Surface), Pairs: []Pair{},
	}
	for _, o := range engines.Offered(spec.Surface) {
		out.Routes = append(out.Routes, RouteOption{
			Route: o.Route, Credential: o.Credential(), Provider: o.Adapter.Title,
			Fidelity: engines.PairFidelity(o, nil), TakesModel: o.TakesModel(), Search: o.Search(),
		})
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

// Binding is a surface on its route with the adapter that makes its calls.
type Binding struct {
	Route   engines.RouteSpec
	Adapter engines.Adapter
	// Endpoint is stored on each answer: a change of route, provider or
	// gateway starts a new series.
	Endpoint string
}

// Bindings returns every enabled surface bound to its route's key.
func (s *Service) Bindings(ctx context.Context) (map[engines.Surface]Binding, error) {
	states, err := s.surfaceStates(ctx, s.db)
	if err != nil {
		return nil, err
	}
	out := map[engines.Surface]Binding{}
	for key, st := range states {
		if !st.enabled {
			continue
		}
		b, err := s.bind(ctx, key, st.route, st.baseURL)
		if err != nil {
			return nil, err
		}
		out[key] = b
	}
	return out, nil
}

func (s *Service) bind(ctx context.Context, key engines.Surface, route engines.Route, baseURL string) (Binding, error) {
	rs, err := engines.RouteOf(key, route)
	if err != nil {
		return Binding{}, fmt.Errorf("%w: %v", core.ErrInvalid, err)
	}
	conn, err := s.conn(ctx, rs.Credential())
	if err != nil {
		return Binding{}, err
	}
	if route == engines.RouteGateway {
		conn.BaseURL = baseURL
	}
	return Binding{Route: rs, Adapter: rs.Bind(conn), Endpoint: rs.EndpointID(baseURL)}, nil
}

func gatewayURL(raw string) (string, error) {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("%w: base_url must be the gateway's http(s) base URL, such as https://openrouter.ai/api/v1", core.ErrInvalid)
	}
	return raw, nil
}

// PutSurface turns a surface on or off, sets its route and its models.
// Each model is checked against the route's live model list. Changing the
// route resets every pair's test and gives each the new route's label.
func (s *Service) PutSurface(ctx context.Context, key engines.Surface, in SurfaceInput) (Surface, error) {
	spec, ok := engines.Lookup(key)
	if !ok {
		return Surface{}, fmt.Errorf("%w: no surface %q; GET /v1/surfaces lists them", core.ErrNotFound, key)
	}
	states, err := s.surfaceStates(ctx, s.db)
	if err != nil {
		return Surface{}, err
	}
	cur := states[key]
	route := cur.route
	if in.Route != "" {
		route = in.Route
	}
	rs, err := engines.RouteOf(key, route)
	if err != nil {
		return Surface{}, fmt.Errorf("%w: %s cannot use the %q route; GET /v1/surfaces lists its routes", core.ErrInvalid, spec.Title, route)
	}
	baseURL := ""
	switch {
	case route == engines.RouteGateway && in.BaseURL == "" && cur.route == engines.RouteGateway:
		baseURL = cur.baseURL
	case route == engines.RouteGateway:
		if baseURL, err = gatewayURL(in.BaseURL); err != nil {
			return Surface{}, err
		}
	case in.BaseURL != "":
		return Surface{}, fmt.Errorf("%w: base_url is only for the gateway route", core.ErrInvalid)
	}
	var credentialID *int64
	if route != engines.RouteTreg {
		var id int64
		err := s.db.QueryRow(ctx, `SELECT id FROM credential WHERE kind = $1`, rs.Credential()).Scan(&id)
		if err == pgx.ErrNoRows {
			return Surface{}, fmt.Errorf("%w: the %s route needs a %s key; an admin sets it under Settings > Provider keys", core.ErrInvalid, route, credentialTitle(rs.Credential()))
		}
		if err != nil {
			return Surface{}, err
		}
		credentialID = &id
	}
	moved := route != cur.route || baseURL != cur.baseURL

	type want struct {
		model *string
		force bool
	}
	var pairs []want
	forceable := rs.Search() == engines.SearchForceable
	switch {
	case !rs.TakesModel():
		if len(in.Models) > 0 {
			return Surface{}, fmt.Errorf("%w: %s takes no model on the %s route, the app decides; send no models", core.ErrInvalid, spec.Title, route)
		}
		pairs = []want{{}}
	case in.Models != nil:
		var names []string
		for i, m := range in.Models {
			name := strings.TrimSpace(m.Model)
			if name == "" || slices.Contains(names, name) {
				return Surface{}, fmt.Errorf("%w: models[%d].model is empty or repeated", core.ErrInvalid, i)
			}
			force := forceable
			if m.ForceWebSearch != nil {
				if *m.ForceWebSearch && !forceable {
					return Surface{}, fmt.Errorf("%w: %s cannot force web search on the %s route; leave force_web_search out", core.ErrInvalid, spec.Title, route)
				}
				force = *m.ForceWebSearch
			}
			names = append(names, name)
			pairs = append(pairs, want{&name, force})
		}
		if len(names) > 0 {
			if err := s.checkModels(ctx, key, route, baseURL, names); err != nil {
				return Surface{}, err
			}
		}
	case moved:
		return Surface{}, fmt.Errorf("%w: %s on the %s route needs its models; pick from GET /v1/surfaces/%s/models?route=%s", core.ErrInvalid, spec.Title, route, key, route)
	}

	label := engines.PairFidelity(rs, nil)
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO surface (key, enabled, route, base_url, credential_id) VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (key) DO UPDATE SET enabled = excluded.enabled, route = excluded.route, base_url = excluded.base_url,
				credential_id = excluded.credential_id, updated_at = now()`,
			key, in.Enabled, route, nullString(baseURL), credentialID); err != nil {
			return err
		}
		if moved {
			if _, err := tx.Exec(ctx, `UPDATE surface_model SET fidelity = $2, last_tested_at = NULL, test_status = NULL, test_searched = NULL
				WHERE surface_key = $1`, key, label); err != nil {
				return err
			}
		}
		if pairs != nil {
			var keep []int64
			for _, p := range pairs {
				var id int64
				if err := tx.QueryRow(ctx, `INSERT INTO surface_model (surface_key, model, fidelity, force_web_search) VALUES ($1, $2, $3, $4)
					ON CONFLICT (surface_key, model) DO UPDATE SET enabled = true, force_web_search = excluded.force_web_search, paused_reason = NULL,
						fidelity = CASE WHEN surface_model.force_web_search = excluded.force_web_search THEN surface_model.fidelity ELSE excluded.fidelity END,
						last_tested_at = CASE WHEN surface_model.force_web_search = excluded.force_web_search THEN surface_model.last_tested_at END,
						test_status = CASE WHEN surface_model.force_web_search = excluded.force_web_search THEN surface_model.test_status END,
						test_searched = CASE WHEN surface_model.force_web_search = excluded.force_web_search THEN surface_model.test_searched END
					RETURNING id`, key, p.model, label, p.force).Scan(&id); err != nil {
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
	return surfaceOf(spec, surfaceState{enabled: in.Enabled, route: route, baseURL: baseURL}, all), err
}

func nullString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func (s *Service) checkModels(ctx context.Context, key engines.Surface, route engines.Route, baseURL string, names []string) error {
	live, err := s.liveModels(ctx, key, route, baseURL)
	if err != nil {
		return err
	}
	var listed []string
	for _, m := range live {
		listed = append(listed, m.Name)
	}
	spec, _ := engines.Lookup(key)
	for _, n := range names {
		if !slices.Contains(listed, n) {
			return fmt.Errorf("%w: %q is not on %s's model list on the %s route; it lists %s", core.ErrInvalid, n, spec.Title, route, strings.Join(listed, ", "))
		}
	}
	return nil
}

// Model is one name on a route's live model list.
type Model struct {
	Name      string `json:"name"`
	WebSearch bool   `json:"web_search"`
}

// ModelsQuery picks the route whose list to read; empty fields mean the
// surface's current route and gateway.
type ModelsQuery struct {
	Route   engines.Route `json:"route,omitempty" jsonschema:"the route whose list to read; omit for the surface's current route"`
	BaseURL string        `json:"base_url,omitempty" jsonschema:"a gateway's base URL, for the gateway route"`
}

// LiveModels reads a surface's live model list on a route. Model lists on
// Treg and on the providers are free.
func (s *Service) LiveModels(ctx context.Context, key engines.Surface, q ModelsQuery) ([]Model, error) {
	states, err := s.surfaceStates(ctx, s.db)
	if err != nil {
		return nil, err
	}
	st, ok := states[key]
	if !ok {
		return nil, fmt.Errorf("%w: no surface %q", core.ErrNotFound, key)
	}
	route, baseURL := st.route, st.baseURL
	if q.Route != "" && q.Route != route {
		route, baseURL = q.Route, ""
	}
	if q.BaseURL != "" {
		if baseURL, err = gatewayURL(q.BaseURL); err != nil {
			return nil, err
		}
	}
	return s.liveModels(ctx, key, route, baseURL)
}

func (s *Service) liveModels(ctx context.Context, key engines.Surface, route engines.Route, baseURL string) ([]Model, error) {
	rs, err := engines.RouteOf(key, route)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", core.ErrInvalid, err)
	}
	if !rs.TakesModel() {
		return nil, fmt.Errorf("%w: %s takes no model on the %s route; the app decides", core.ErrInvalid, rs.Spec.Title, route)
	}
	if route == engines.RouteGateway && baseURL == "" {
		return nil, fmt.Errorf("%w: give the gateway's base_url to read its model list", core.ErrInvalid)
	}
	b, err := s.bind(ctx, key, route, baseURL)
	if err != nil {
		return nil, err
	}
	live, err := b.Adapter.ListModels(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("%w: reading %s's model list from %s: %v", core.ErrUpstream, rs.Spec.Title, rs.Adapter.Title, err)
	}
	out := make([]Model, len(live))
	for i, m := range live {
		out[i] = Model{Name: m.Name, WebSearch: m.WebSearch}
	}
	return out, nil
}

// testQuestion is asked when no question set exists yet. It names no brand.
const testQuestion = "What should I look for when choosing a project management tool for a small team?"

// PairTest is what one short call through a pair's route showed.
type PairTest struct {
	Pair          Pair           `json:"pair" jsonschema:"the pair after the test, with the fidelity it confirmed"`
	Status        engines.Status `json:"status"`
	Error         string         `json:"error"`
	CostUSD       *float64       `json:"cost_usd" jsonschema:"null when the route reported no cost, as token-billed APIs do"`
	CostReported  bool           `json:"cost_reported"`
	Searched      bool           `json:"searched"`
	Cited         bool           `json:"cited"`
	Citations     int            `json:"citations"`
	ReportedModel string         `json:"reported_model"`
}

// TestPair makes one call for a pair with the current set's first
// question, records whether it answered and searched, and sets the pair's
// fidelity from what it showed. A failed call is a result, not an error;
// it leaves the fidelity as it was and the pair untested for onboarding.
func (s *Service) TestPair(ctx context.Context, id int64) (PairTest, error) {
	rows, err := s.db.Query(ctx, `SELECT `+pairColumns+` FROM surface_model WHERE id = $1`, id)
	if err != nil {
		return PairTest{}, err
	}
	pair, err := pgx.CollectExactlyOneRow(rows, scanPair)
	if errors.Is(err, pgx.ErrNoRows) {
		return PairTest{}, fmt.Errorf("%w: no surface and model pair %d; GET /v1/surfaces lists them", core.ErrNotFound, id)
	}
	if err != nil {
		return PairTest{}, err
	}
	states, err := s.surfaceStates(ctx, s.db)
	if err != nil {
		return PairTest{}, err
	}
	st := states[pair.Surface]
	b, err := s.bind(ctx, pair.Surface, st.route, st.baseURL)
	if err != nil {
		return PairTest{}, err
	}
	if b.Route.TakesModel() != (pair.Model != "") {
		return PairTest{}, fmt.Errorf("%w: pair %d belongs to the surface's previous route; pick models for the current route first", core.ErrInvalid, id)
	}
	settings, err := s.Settings(ctx)
	if err != nil {
		return PairTest{}, err
	}
	question := testQuestion
	if set, err := s.CurrentQuestions(ctx); err == nil && len(set.Questions) > 0 {
		question = set.Questions[0].Text
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(settings.TimeoutS)*time.Second)
	defer cancel()
	outcome := engines.TestOutcome{ModelAccepted: pair.Model == ""}
	var res engines.Result
	if pair.Model != "" {
		live, lerr := b.Adapter.ListModels(ctx, pair.Surface)
		outcome.ModelAccepted = lerr == nil && slices.ContainsFunc(live, func(m engines.Model) bool { return m.Name == pair.Model })
	}
	res, err = b.Adapter.Call(ctx, engines.Request{
		Question: question, Surface: pair.Surface, Model: pair.Model, ForceWebSearch: pair.ForceWebSearch,
		Location: settings.Market, Language: settings.Language,
	})
	if err != nil {
		res = engines.Result{Status: engines.StatusFailed, Error: err.Error()}
	}
	answered := res.Status != engines.StatusFailed
	outcome.ForceAccepted = pair.ForceWebSearch && answered
	outcome.Searched = res.Searched()

	status, fidelity := res.Error, pair.Fidelity
	if status == "" {
		status = "the call failed"
	}
	if answered {
		status, fidelity = "ok", engines.PairFidelity(b.Route, &outcome)
	}
	searched := outcome.Searched
	if _, err := s.db.Exec(context.WithoutCancel(ctx), `UPDATE surface_model SET last_tested_at = $2, test_status = $3, test_searched = $4, fidelity = $5 WHERE id = $1`,
		id, s.now(), status, searched, fidelity); err != nil {
		return PairTest{}, err
	}
	pair.LastTestedAt, pair.TestStatus, pair.TestSearched, pair.Fidelity = new(s.now()), status, &searched, fidelity
	return PairTest{
		Pair: pair, Status: res.Status, Error: res.Error, CostUSD: res.CostUSD, CostReported: res.CostUSD != nil,
		Searched: outcome.Searched, Cited: len(res.Citations) > 0, Citations: len(res.Citations), ReportedModel: res.ReportedModel,
	}, nil
}

// RefreshModelsIfDue rereads the model lists once a day, and an hour after
// a failed attempt. It does nothing until a model pair is enabled.
func (s *Service) RefreshModelsIfDue(ctx context.Context) error {
	var due bool
	err := s.db.QueryRow(ctx, `SELECT
		NOT EXISTS (SELECT 1 FROM job_run WHERE job = 'model_refresh' AND status = 'completed' AND started_at > $1::timestamptz - interval '1 day')
		AND NOT EXISTS (SELECT 1 FROM job_run WHERE job = 'model_refresh' AND started_at > $1::timestamptz - interval '1 hour')
		AND EXISTS (SELECT 1 FROM surface_model m JOIN surface s ON s.key = m.surface_key WHERE s.enabled AND m.enabled AND m.model IS NOT NULL)`, s.now()).Scan(&due)
	if err != nil || !due {
		return err
	}
	return s.RefreshModels(ctx)
}

// RefreshModels pauses each enabled pair whose model has left its route's
// live list and unpauses each whose model is back. It never swaps a model
// for another.
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
	rows, err := s.db.Query(ctx, `SELECT `+pairColumns+` FROM surface_model m
		WHERE enabled AND model IS NOT NULL AND EXISTS (SELECT 1 FROM surface s WHERE s.key = m.surface_key AND s.enabled)
		ORDER BY id`)
	if err != nil {
		return err
	}
	pairs, err := pgx.CollectRows(rows, scanPair)
	if err != nil {
		return err
	}
	states, err := s.surfaceStates(ctx, s.db)
	if err != nil {
		return err
	}
	lists := map[engines.Surface][]string{}
	for _, p := range pairs {
		listed, ok := lists[p.Surface]
		if !ok {
			st := states[p.Surface]
			live, err := s.liveModels(ctx, p.Surface, st.route, st.baseURL)
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
