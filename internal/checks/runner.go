package checks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/unkeyed/byname/internal/config"
	"github.com/unkeyed/byname/internal/core"
	"github.com/unkeyed/byname/internal/engines"
	"github.com/unkeyed/byname/internal/panel"
)

// Refusals Treg asks the caller to wait out. Any other refusal stops the
// run, because retrying cannot help (an empty balance, a bad token).
var backoffCodes = map[int]bool{http.StatusTooManyRequests: true, http.StatusServiceUnavailable: true}

const (
	maxBackoffs    = 3
	defaultBackoff = 5 * time.Second
	maxBackoff     = time.Minute
)

// plan is what one run asks and within which limits.
type plan struct {
	runID    int64
	settings config.SettingsView
	brands   []panel.Brand
	calls    []call
	// monthBase is what the month's other runs and social searches have
	// spent, since the monthly cap covers both.
	monthBase float64
}

type call struct {
	question config.Question
	pair     config.Pair
	// via is the pair's surface on its route: the adapter that asks and the
	// endpoint stored on the answer.
	via config.Binding
}

// outcome is why a run stopped queueing calls, the first reason wins.
type outcome struct {
	mu      sync.Mutex
	status  Status
	message string
	code    string
	spent   float64
}

func (o *outcome) stop(status Status, message, code string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.status == "" {
		o.status, o.message, o.code = status, message, code
	}
}

func (o *outcome) stopped() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.status != ""
}

func (o *outcome) setSpent(v float64) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.spent = v
}

func (o *outcome) spentSoFar() float64 {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.spent
}

// RunCheck executes a queued run, or resumes a running one, to the end.
// It returns nil once the run is finished, and an error only when the run
// could not be advanced (the database is down, or ctx ended), in which case
// the run stays as it was and the next call resumes it. A finished run is
// left alone.
func (s *Service) RunCheck(ctx context.Context, runID int64) error {
	var status Status
	var setID int64
	var spent float64
	err := s.db.QueryRow(ctx, `SELECT status, question_set_id, spend_usd::float8 FROM run WHERE id = $1`, runID).Scan(&status, &setID, &spent)
	if err != nil {
		return err
	}
	if status != Queued && status != Running {
		return nil
	}
	p, stop, err := s.plan(ctx, runID, setID)
	if err != nil {
		return err
	}
	if stop != nil {
		return s.finish(ctx, runID, stop)
	}
	if _, err := s.db.Exec(ctx, `UPDATE run SET status = 'running', started_at = coalesce(started_at, $2) WHERE id = $1`, runID, s.now()); err != nil {
		return err
	}
	if _, err := s.db.Exec(ctx, `UPDATE job_run SET status = 'running' WHERE run_id = $1`, runID); err != nil {
		return err
	}
	out := &outcome{spent: spent}
	if s.overBudget(p, out) {
		return s.finish(ctx, runID, out)
	}
	s.execute(ctx, p, out)
	if err := ctx.Err(); err != nil {
		return err
	}
	if !out.stopped() {
		out.stop(Completed, "", "")
	}
	return s.finish(ctx, runID, out)
}

// plan reads what the run asks. A non-nil outcome means the run cannot
// start and should finish with it.
func (s *Service) plan(ctx context.Context, runID, setID int64) (plan, *outcome, error) {
	p := plan{runID: runID}
	var err error
	if p.settings, err = s.config.Settings(ctx); err != nil {
		return p, nil, err
	}
	if p.brands, err = s.config.PanelBrands(ctx); err != nil {
		return p, nil, err
	}
	questions, err := s.config.Questions(ctx, setID)
	if err != nil {
		return p, nil, err
	}
	pairs, err := s.config.ActivePairs(ctx)
	if err != nil {
		return p, nil, err
	}
	if len(pairs) == 0 {
		return p, &outcome{status: Failed, message: "no surface and model pair is enabled"}, nil
	}
	bindings, err := s.config.Bindings(ctx)
	if errors.Is(err, core.ErrInvalid) {
		return p, &outcome{status: Failed, message: err.Error()}, nil
	}
	if err != nil {
		return p, nil, err
	}
	rows, err := s.db.Query(ctx, `SELECT question_id, surface_model_id FROM answer WHERE run_id = $1`, runID)
	if err != nil {
		return p, nil, err
	}
	done := map[panel.Key]bool{}
	for rows.Next() {
		var k panel.Key
		if err := rows.Scan(&k.Question, &k.SurfaceModel); err != nil {
			return p, nil, err
		}
		done[k] = true
	}
	if err := rows.Err(); err != nil {
		return p, nil, err
	}
	for _, q := range questions {
		for _, pair := range pairs {
			if !done[panel.Key{Question: panel.QuestionID(q.ID), SurfaceModel: panel.SurfaceModelID(pair.ID)}] {
				p.calls = append(p.calls, call{question: q, pair: pair, via: bindings[pair.Surface]})
			}
		}
	}
	loc := p.settings.Schedule().Location
	local := s.now().In(loc)
	monthStart := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, loc)
	err = s.db.QueryRow(ctx, `SELECT coalesce((SELECT sum(spend_usd) FROM run WHERE id <> $1 AND started_at >= $2), 0)::float8
		+ coalesce((SELECT sum(cost_usd) FROM social_call WHERE called_at >= $2), 0)::float8`, runID, monthStart).Scan(&p.monthBase)
	return p, nil, err
}

// overBudget stops the run when its spend, or the month's, has reached a cap.
func (s *Service) overBudget(p plan, out *outcome) bool {
	spent := out.spentSoFar()
	if spent >= p.settings.SpendCapRunUSD {
		out.stop(Capped, fmt.Sprintf("stopped queueing calls at $%.4f, the spend cap per check is $%.2f", spent, p.settings.SpendCapRunUSD), "")
		return true
	}
	if month := p.settings.SpendCapMonthUSD; month != nil && p.monthBase+spent >= *month {
		out.stop(Capped, fmt.Sprintf("this month's checks and social searches have spent $%.4f, the monthly spend cap is $%.2f", p.monthBase+spent, *month), "")
		return true
	}
	return false
}

// execute asks every call with at most concurrency in flight. Before each
// new call it waits for a free slot and checks the caps against the spend
// recorded so far, so with concurrency 1 the run stops right after the call
// that reached a cap.
func (s *Service) execute(ctx context.Context, p plan, out *outcome) {
	slots := make(chan struct{}, p.settings.Concurrency)
	var wg sync.WaitGroup
	for _, c := range p.calls {
		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
			wg.Wait()
			return
		}
		if out.stopped() || s.overBudget(p, out) {
			<-slots
			break
		}
		wg.Go(func() {
			defer func() { <-slots }()
			if err := s.ask(ctx, p, c, out); err != nil && ctx.Err() == nil {
				s.log.Error("store answer", "run", p.runID, "question", c.question.ID, "pair", c.pair.ID, "err", err)
				out.stop(Failed, "could not store an answer: "+err.Error(), "")
			}
		})
	}
	wg.Wait()
}

// ask makes one call with retries through the pair's route and stores its
// answer. A Treg refusal is not an answer: it backs off when Treg asks to wait, and otherwise
// stops the run. When ctx ends mid-call nothing is stored, so a resumed run
// asks again with the same idempotency key and Treg replays the answer.
func (s *Service) ask(ctx context.Context, p plan, c call, out *outcome) error {
	req := engines.Request{
		Question: c.question.Text, Surface: c.pair.Surface, Model: c.pair.Model, ForceWebSearch: c.pair.ForceWebSearch,
		Location: p.settings.Market, Language: p.settings.Language,
	}
	timeout := time.Duration(p.settings.TimeoutS) * time.Second
	var res engines.Result
	var err error
	attempt, backoffs := 1, 0
	for {
		req.IdempotencyKey = fmt.Sprintf("byname/run-%d/q-%d/sm-%d/%d", p.runID, c.question.ID, c.pair.ID, attempt)
		callCtx, cancel := context.WithTimeout(ctx, timeout)
		res, err = c.via.Adapter.Call(callCtx, req)
		cancel()
		if ctx.Err() != nil {
			return nil
		}
		if refusal, ok := errors.AsType[*engines.Refusal](err); ok {
			if backoffCodes[refusal.StatusCode] && backoffs < maxBackoffs {
				backoffs++
				if s.sleep(ctx, backoffFor(refusal, backoffs)) != nil {
					return nil
				}
				continue
			}
			out.stop(Refused, refusal.Error(), refusal.Code)
			return nil
		}
		if (err == nil && res.Status != engines.StatusFailed) || attempt > p.settings.Retries {
			break
		}
		attempt++
	}
	if err != nil {
		res = engines.Result{Status: engines.StatusFailed, Error: err.Error()}
	}
	spent, err := s.store(ctx, p, c, res, attempt)
	if err != nil {
		return err
	}
	out.setSpent(spent)
	return nil
}

func backoffFor(r *engines.Refusal, n int) time.Duration {
	d := r.RetryAfter
	if d <= 0 {
		d = defaultBackoff * time.Duration(n)
	}
	return min(d, maxBackoff)
}

// store upserts one answer with its mentions and citations and returns the
// run's spend. The run row is locked first, so concurrent answers sum the
// spend one after another and none is lost.
func (s *Service) store(ctx context.Context, p plan, c call, res engines.Result, attempts int) (float64, error) {
	scored := panel.Score(res, p.brands, panel.Settings{MatchMode: p.settings.MatchMode})
	var spent float64
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT 1 FROM run WHERE id = $1 FOR UPDATE`, p.runID); err != nil {
			return err
		}
		var answerID int64
		err := tx.QueryRow(ctx, `INSERT INTO answer (run_id, question_id, surface_model_id, endpoint, fidelity, reported_model,
				status, error, attempts, mentioned, cited, position, cost_usd, text, raw, fan_out, call_id)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
			ON CONFLICT (run_id, question_id, surface_model_id) DO UPDATE SET endpoint = excluded.endpoint,
				fidelity = excluded.fidelity, reported_model = excluded.reported_model, status = excluded.status,
				error = excluded.error, attempts = excluded.attempts, mentioned = excluded.mentioned, cited = excluded.cited,
				position = excluded.position, cost_usd = excluded.cost_usd, text = excluded.text, raw = excluded.raw,
				fan_out = excluded.fan_out, call_id = excluded.call_id, answered_at = now()
			RETURNING id`,
			p.runID, c.question.ID, c.pair.ID, c.via.Endpoint, c.pair.Fidelity, res.ReportedModel,
			res.Status, res.Error, attempts, scored.Mentioned, scored.Cited, nullPosition(scored.Position), res.CostUSD,
			res.Text, rawJSON(res.Raw), append([]string{}, res.FanOut...), res.CallID).Scan(&answerID)
		if err != nil {
			return err
		}
		for _, q := range []string{`DELETE FROM brand_mention WHERE answer_id = $1`, `DELETE FROM citation WHERE answer_id = $1`} {
			if _, err := tx.Exec(ctx, q, answerID); err != nil {
				return err
			}
		}
		for _, m := range scored.Mentions {
			if _, err := tx.Exec(ctx, `INSERT INTO brand_mention (answer_id, brand_id, position) VALUES ($1, $2, $3)`,
				answerID, m.Brand, nullPosition(m.Position)); err != nil {
				return err
			}
		}
		for i, ci := range scored.Citations {
			if _, err := tx.Exec(ctx, `INSERT INTO citation (answer_id, ord, url, domain, is_self) VALUES ($1, $2, $3, $4, $5)`,
				answerID, i, ci.URL, ci.Domain, ci.IsSelf); err != nil {
				return err
			}
		}
		return tx.QueryRow(ctx, `UPDATE run SET
				spend_usd = (SELECT coalesce(sum(cost_usd), 0) FROM answer WHERE run_id = $1),
				spend_missing_calls = (SELECT count(*) FROM answer WHERE run_id = $1 AND cost_usd IS NULL AND status <> 'failed')
			WHERE id = $1 RETURNING spend_usd::float8`, p.runID).Scan(&spent)
	})
	return spent, err
}

func nullPosition(p int) *int {
	if p == 0 {
		return nil
	}
	return &p
}

// rawJSON is the body as jsonb: itself when it is JSON, else a JSON string,
// so an HTML error page is kept too.
func rawJSON(raw []byte) any {
	if len(raw) == 0 {
		return nil
	}
	if json.Valid(raw) {
		return json.RawMessage(raw)
	}
	b, _ := json.Marshal(string(raw))
	return json.RawMessage(b)
}

// finish records how a run ended on the run and its job_run row.
func (s *Service) finish(ctx context.Context, runID int64, out *outcome) error {
	var msg, code *string
	if out.message != "" {
		msg = &out.message
	}
	if out.code != "" {
		code = &out.code
	}
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE run SET status = $2, error = $3, refusal_code = $4, finished_at = $5,
				started_at = coalesce(started_at, $5)
			WHERE id = $1 AND status IN ('queued', 'running')`, runID, out.status, msg, code, s.now()); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE job_run SET status = $2, error = $3, finished_at = $4 WHERE run_id = $1`, runID, out.status, msg, s.now())
		return err
	})
	if err != nil {
		return err
	}
	s.alertFinished(ctx, runID)
	return nil
}

// alertFinished tells Alerts the run ended. A failure to alert is logged,
// never the run's failure.
func (s *Service) alertFinished(ctx context.Context, runID int64) {
	if s.alerts == nil {
		return
	}
	if err := s.alerts.RunFinished(ctx, runID); err != nil && ctx.Err() == nil {
		s.log.Error("alert on a finished run", "run", runID, "err", err)
	}
}
