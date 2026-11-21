// Package results reads what checks recorded, shaped for the screens and
// for agents: runs with their spend and success rates, answers, run
// compare, a question's history, mention and citation rates, share of
// mentions, and gaps.
//
// Everything is plotted by run, not by day. A series is one surface and
// model pair on one endpoint at one fidelity: changing the endpoint or the
// fidelity starts a new series, and no number here averages across series.
package results

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/unkeyed/byname/internal/core"
	"github.com/unkeyed/byname/internal/engines"
	"github.com/unkeyed/byname/internal/panel"
)

type Service struct {
	db *pgxpool.Pool
}

func New(db *pgxpool.Pool) *Service { return &Service{db: db} }

// Run is one check with what it cost and how each endpoint did.
type Run struct {
	ID                 int64      `json:"id"`
	Status             string     `json:"status"`
	Trigger            string     `json:"trigger"`
	QuestionSetID      int64      `json:"question_set_id"`
	QuestionSetVersion int        `json:"question_set_version"`
	QueuedAt           time.Time  `json:"queued_at"`
	StartedAt          *time.Time `json:"started_at"`
	FinishedAt         *time.Time `json:"finished_at"`
	SpendUSD           float64    `json:"spend_usd"`
	// SpendMissingCalls counts answered calls that reported no cost. Their
	// cost is unknown, not $0.
	SpendMissingCalls int             `json:"spend_missing_calls"`
	Calls             int             `json:"calls" jsonschema:"answers stored, failed ones included"`
	Succeeded         int             `json:"succeeded" jsonschema:"answers that did not fail"`
	Mentioned         int             `json:"mentioned" jsonschema:"question and pair answers that named you"`
	Cited             int             `json:"cited" jsonschema:"question and pair answers that cited your domains"`
	Error             string          `json:"error" jsonschema:"why the run stopped early, if it did"`
	RefusalCode       string          `json:"refusal_code" jsonschema:"Treg's error code when it refused a call"`
	Endpoints         []EndpointStats `json:"endpoints"`
}

// EndpointStats is one endpoint's calls in a run, so a flaky provider
// shows up as a provider problem, not a visibility drop.
type EndpointStats struct {
	Endpoint    string   `json:"endpoint"`
	Calls       int      `json:"calls"`
	Succeeded   int      `json:"succeeded"`
	SuccessRate float64  `json:"success_rate"`
	CostUSD     float64  `json:"cost_usd"`
	MissingCost int      `json:"missing_cost" jsonschema:"answered calls that reported no cost"`
	CostPerCall *float64 `json:"cost_per_call" jsonschema:"over calls that reported a cost; null when none did"`
}

// RunsQuery pages through runs newest first.
type RunsQuery struct {
	Limit int `json:"limit,omitempty" jsonschema:"1 to 200, default 50"`
	// Before lists runs older than this run id.
	Before int64 `json:"before,omitempty"`
}

const runSelect = `SELECT r.id, r.status, r.trigger, r.question_set_id, qs.version, r.queued_at, r.started_at, r.finished_at,
		r.spend_usd::float8, r.spend_missing_calls, coalesce(r.error, ''), coalesce(r.refusal_code, ''),
		count(a.id), count(a.id) FILTER (WHERE a.status <> 'failed'),
		count(a.id) FILTER (WHERE a.mentioned), count(a.id) FILTER (WHERE a.cited)
	FROM run r JOIN question_set qs ON qs.id = r.question_set_id LEFT JOIN answer a ON a.run_id = r.id`

func scanRun(row pgx.CollectableRow) (Run, error) {
	var r Run
	err := row.Scan(&r.ID, &r.Status, &r.Trigger, &r.QuestionSetID, &r.QuestionSetVersion, &r.QueuedAt, &r.StartedAt, &r.FinishedAt,
		&r.SpendUSD, &r.SpendMissingCalls, &r.Error, &r.RefusalCode, &r.Calls, &r.Succeeded, &r.Mentioned, &r.Cited)
	r.Endpoints = []EndpointStats{}
	return r, err
}

// Runs lists runs newest first.
func (s *Service) Runs(ctx context.Context, q RunsQuery) ([]Run, error) {
	if q.Limit == 0 {
		q.Limit = 50
	}
	if q.Limit < 1 || q.Limit > 200 {
		return nil, fmt.Errorf("%w: limit must be 1 to 200", core.ErrInvalid)
	}
	rows, err := s.db.Query(ctx, runSelect+` WHERE ($1 = 0 OR r.id < $1) GROUP BY r.id, qs.version ORDER BY r.id DESC LIMIT $2`, q.Before, q.Limit)
	if err != nil {
		return nil, err
	}
	runs, err := pgx.CollectRows(rows, scanRun)
	if err != nil {
		return nil, err
	}
	return runs, s.addEndpoints(ctx, runs)
}

// Run is one run by id.
func (s *Service) Run(ctx context.Context, id int64) (Run, error) {
	rows, err := s.db.Query(ctx, runSelect+` WHERE r.id = $1 GROUP BY r.id, qs.version`, id)
	if err != nil {
		return Run{}, err
	}
	runs, err := pgx.CollectRows(rows, scanRun)
	if err != nil {
		return Run{}, err
	}
	if len(runs) == 0 {
		return Run{}, fmt.Errorf("%w: no run %d", core.ErrNotFound, id)
	}
	return runs[0], s.addEndpoints(ctx, runs)
}

func (s *Service) addEndpoints(ctx context.Context, runs []Run) error {
	ids := make([]int64, len(runs))
	byID := map[int64]*Run{}
	for i := range runs {
		ids[i] = runs[i].ID
		byID[runs[i].ID] = &runs[i]
	}
	rows, err := s.db.Query(ctx, `SELECT run_id, endpoint, count(*), count(*) FILTER (WHERE status <> 'failed'),
			coalesce(sum(cost_usd), 0)::float8, count(*) FILTER (WHERE cost_usd IS NULL AND status <> 'failed'),
			(sum(cost_usd) / nullif(count(cost_usd), 0))::float8
		FROM answer WHERE run_id = ANY($1) GROUP BY run_id, endpoint ORDER BY run_id, endpoint`, ids)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id int64
		var e EndpointStats
		if err := rows.Scan(&id, &e.Endpoint, &e.Calls, &e.Succeeded, &e.CostUSD, &e.MissingCost, &e.CostPerCall); err != nil {
			return err
		}
		e.SuccessRate = float64(e.Succeeded) / float64(e.Calls)
		byID[id].Endpoints = append(byID[id].Endpoints, e)
	}
	return rows.Err()
}

type QuestionRef struct {
	ID    int64  `json:"id"`
	Group string `json:"group"`
	Text  string `json:"text"`
}

// PairRef names a surface and model pair. Model is empty on app surfaces.
type PairRef struct {
	ID      int64           `json:"id"`
	Surface engines.Surface `json:"surface"`
	Model   string          `json:"model"`
}

type Mention struct {
	BrandID int64           `json:"brand_id"`
	Name    string          `json:"name"`
	Kind    panel.BrandKind `json:"kind"`
	// Position is the order of first mention among non-technology brands,
	// null for a technology.
	Position *int `json:"position"`
}

// AnswerSummary is one answer as the answers list shows it.
type AnswerSummary struct {
	ID            int64       `json:"id"`
	RunID         int64       `json:"run_id"`
	Question      QuestionRef `json:"question"`
	Pair          PairRef     `json:"pair"`
	Endpoint      string      `json:"endpoint"`
	Fidelity      string      `json:"fidelity"`
	ReportedModel string      `json:"reported_model"`
	Status        string      `json:"status" jsonschema:"ok, no_answer (the surface showed no AI answer) or failed"`
	Error         string      `json:"error"`
	Attempts      int         `json:"attempts"`
	Mentioned     bool        `json:"mentioned"`
	Cited         bool        `json:"cited"`
	Position      *int        `json:"position"`
	CostUSD       *float64    `json:"cost_usd" jsonschema:"null when the call reported no cost"`
	CitationCount int         `json:"citation_count"`
	Mentions      []Mention   `json:"mentions"`
	AnsweredAt    time.Time   `json:"answered_at"`
}

// Answer is one answer with its text, raw response and citations.
type Answer struct {
	AnswerSummary
	Text      string          `json:"text"`
	Raw       json.RawMessage `json:"raw" jsonschema:"the provider's response body, null once past raw_retention_days"`
	FanOut    []string        `json:"fan_out" jsonschema:"the engine's own web searches, where the endpoint exposes them"`
	Citations []Citation      `json:"citations"`
	CallID    string          `json:"call_id"`
}

type Citation struct {
	URL    string `json:"url"`
	Domain string `json:"domain"`
	IsSelf bool   `json:"is_self"`
	// Page is what the cited page said the last time Byname read it, null
	// until it has been fetched.
	Page *PageCheck `json:"page"`
}

const answerSelect = `SELECT a.id, a.run_id, q.id, q.group_name, q.text, m.id, m.surface_key, coalesce(m.model, ''),
		a.endpoint, a.fidelity, a.reported_model, a.status, a.error, a.attempts, a.mentioned, a.cited, a.position,
		a.cost_usd::float8, (SELECT count(*) FROM citation c WHERE c.answer_id = a.id), a.answered_at
	FROM answer a JOIN question q ON q.id = a.question_id JOIN surface_model m ON m.id = a.surface_model_id`

func scanAnswer(row pgx.CollectableRow) (AnswerSummary, error) {
	var a AnswerSummary
	err := row.Scan(&a.ID, &a.RunID, &a.Question.ID, &a.Question.Group, &a.Question.Text, &a.Pair.ID, &a.Pair.Surface, &a.Pair.Model,
		&a.Endpoint, &a.Fidelity, &a.ReportedModel, &a.Status, &a.Error, &a.Attempts, &a.Mentioned, &a.Cited, &a.Position,
		&a.CostUSD, &a.CitationCount, &a.AnsweredAt)
	a.Mentions = []Mention{}
	return a, err
}

// RunAnswers lists a run's answers in question order.
func (s *Service) RunAnswers(ctx context.Context, runID int64) ([]AnswerSummary, error) {
	if _, err := s.Run(ctx, runID); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(ctx, answerSelect+` WHERE a.run_id = $1 ORDER BY q.ord, m.id`, runID)
	if err != nil {
		return nil, err
	}
	answers, err := pgx.CollectRows(rows, scanAnswer)
	if err != nil {
		return nil, err
	}
	return answers, s.addMentions(ctx, answers)
}

func (s *Service) addMentions(ctx context.Context, answers []AnswerSummary) error {
	ids := make([]int64, len(answers))
	byID := map[int64]*AnswerSummary{}
	for i := range answers {
		ids[i] = answers[i].ID
		byID[answers[i].ID] = &answers[i]
	}
	rows, err := s.db.Query(ctx, `SELECT bm.answer_id, b.id, b.name, b.kind, bm.position
		FROM brand_mention bm JOIN brand b ON b.id = bm.brand_id
		WHERE bm.answer_id = ANY($1) ORDER BY bm.answer_id, bm.position NULLS LAST, b.name`, ids)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id int64
		var m Mention
		if err := rows.Scan(&id, &m.BrandID, &m.Name, &m.Kind, &m.Position); err != nil {
			return err
		}
		byID[id].Mentions = append(byID[id].Mentions, m)
	}
	return rows.Err()
}

// Answer is one answer in full.
func (s *Service) Answer(ctx context.Context, id int64) (Answer, error) {
	rows, err := s.db.Query(ctx, answerSelect+` WHERE a.id = $1`, id)
	if err != nil {
		return Answer{}, err
	}
	found, err := pgx.CollectRows(rows, scanAnswer)
	if err != nil {
		return Answer{}, err
	}
	if len(found) == 0 {
		return Answer{}, fmt.Errorf("%w: no answer %d", core.ErrNotFound, id)
	}
	if err := s.addMentions(ctx, found); err != nil {
		return Answer{}, err
	}
	a := Answer{AnswerSummary: found[0], Citations: []Citation{}}
	var raw []byte
	if err := s.db.QueryRow(ctx, `SELECT text, raw, fan_out, call_id FROM answer WHERE id = $1`, id).Scan(&a.Text, &raw, &a.FanOut, &a.CallID); err != nil {
		return Answer{}, err
	}
	if raw != nil {
		a.Raw = raw
	}
	kinds, err := s.brandKinds(ctx)
	if err != nil {
		return Answer{}, err
	}
	rows, err = s.db.Query(ctx, `SELECT c.url, c.domain, c.is_self, p.status, p.fetched_at, p.self_present, coalesce(p.brands_present, '{}'), coalesce(p.error, '')
		FROM citation c LEFT JOIN source_page p ON p.url = c.url WHERE c.answer_id = $1 ORDER BY c.ord`, id)
	if err != nil {
		return Answer{}, err
	}
	a.Citations, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (Citation, error) {
		var c Citation
		var status *string
		var fetched *time.Time
		var check PageCheck
		if err := r.Scan(&c.URL, &c.Domain, &c.IsSelf, &status, &fetched, &check.SelfPresent, &check.Brands, &check.Error); err != nil {
			return c, err
		}
		if status != nil {
			check.Status, check.FetchedAt, check.Competitors = *status, *fetched, competitorsIn(check.Brands, kinds)
			c.Page = &check
		}
		return c, nil
	})
	return a, err
}

// Job is one entry of the jobs log.
type Job struct {
	ID         int64      `json:"id"`
	Job        string     `json:"job" jsonschema:"ai_check, model_refresh or source_fetch"`
	Trigger    string     `json:"trigger"`
	RunID      *int64     `json:"run_id"`
	Status     string     `json:"status"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
	Error      string     `json:"error"`
	SpendUSD   *float64   `json:"spend_usd" jsonschema:"the run's spend, for an AI check"`
}

// Jobs lists the jobs log newest first.
func (s *Service) Jobs(ctx context.Context, limit int) ([]Job, error) {
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 200 {
		return nil, fmt.Errorf("%w: limit must be 1 to 200", core.ErrInvalid)
	}
	rows, err := s.db.Query(ctx, `SELECT j.id, j.job, j.trigger, j.run_id, j.status, j.started_at, j.finished_at,
			coalesce(j.error, ''), r.spend_usd::float8
		FROM job_run j LEFT JOIN run r ON r.id = j.run_id ORDER BY j.started_at DESC, j.id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Job, error) {
		var j Job
		return j, r.Scan(&j.ID, &j.Job, &j.Trigger, &j.RunID, &j.Status, &j.StartedAt, &j.FinishedAt, &j.Error, &j.SpendUSD)
	})
}
