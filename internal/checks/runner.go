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
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/unkeyed/byname/internal/config"
	"github.com/unkeyed/byname/internal/core"
	"github.com/unkeyed/byname/internal/engines"
	"github.com/unkeyed/byname/internal/money"
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

// runSpent is what run $1 has spent, read from its run row: its answers
// and the calls that read them for other brands, which its spend cap
// counts.
const runSpent = `(spend_usd + coalesce((SELECT sum(cost_usd) FROM brand_call WHERE run_id = $1), 0))::float8`

// plan is what one run asks and within which limits.
type plan struct {
	runID    int64
	settings config.SettingsView
	calls    []call
	// monthBase is what the month's other runs, social searches and calls
	// for other brands have spent, since the monthly cap covers them all.
	monthBase float64
	// find is whether each answer is read for other brands.
	find bool
}

type call struct {
	question config.Question
	pair     config.Pair
	// via is the pair's surface on its route: the adapter that asks and the
	// endpoint stored on the answer.
	via config.Binding
}

// outcome is why a run stopped queueing calls, the first reason wins.
// halted is set instead when the database failed for longer than the
// retries cover: the run is not finished, and the next pass resumes it.
type outcome struct {
	mu      sync.Mutex
	status  Status
	message string
	code    string
	spent   float64
	halted  error
}

func (o *outcome) halt(err error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.halted == nil {
		o.halted = err
	}
}

func (o *outcome) haltedBy() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.halted
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
	return o.status != "" || o.halted != nil
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
	err := s.db.QueryRow(ctx, `SELECT status, question_set_id, `+runSpent+` FROM run WHERE id = $1`, runID).Scan(&status, &setID, &spent)
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
	if err := out.haltedBy(); err != nil {
		return fmt.Errorf("check #%d paused, the next pass resumes it: %w", runID, err)
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
	questions, err := s.config.Questions(ctx, setID)
	if err != nil {
		return p, nil, err
	}
	pairs, err := s.config.ActivePairs(ctx)
	if err != nil {
		return p, nil, err
	}
	if s.finder != nil {
		if p.find, err = s.finder.Ready(ctx); err != nil {
			return p, nil, err
		}
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
	err = s.db.QueryRow(ctx, `SELECT coalesce(sum(usd), 0)::float8 FROM spend WHERE at >= $2 AND run_id IS DISTINCT FROM $1`, runID, monthStart).Scan(&p.monthBase)
	return p, nil, err
}

// overBudget stops the run when its spend, or the month's, has reached a cap.
func (s *Service) overBudget(p plan, out *outcome) bool {
	spent := out.spentSoFar()
	if spent >= p.settings.SpendCapRunUSD {
		out.stop(Capped, fmt.Sprintf("stopped asking questions at %s; the spend cap for one check is %s", money.Format(spent), money.Format(p.settings.SpendCapRunUSD)), "")
		return true
	}
	if month := p.settings.SpendCapMonthUSD; month != nil && p.monthBase+spent >= *month {
		out.stop(Capped, fmt.Sprintf("stopped asking questions: checks and social searches have spent %s this month; the monthly spend cap is %s", money.Format(p.monthBase+spent), money.Format(*month)), "")
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
				if permanent(err) {
					out.stop(Failed, "could not store an answer: "+err.Error(), "")
				} else {
					out.halt(err)
				}
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
	answerID, spent, err := s.storeRetrying(ctx, p, c, res, attempt)
	if err != nil {
		return err
	}
	out.setSpent(spent)
	if p.find && res.Status == engines.StatusOK && !s.overBudget(p, out) {
		s.findBrands(ctx, p, answerID, res.Text, out)
	}
	return nil
}

// findBrands reads a stored answer for other brands and adds what that
// cost to the run's spend. Nothing that goes wrong here fails the check:
// the answer is left unread for a later pass.
func (s *Service) findBrands(ctx context.Context, p plan, answerID int64, text string, out *outcome) {
	if err := s.finder.Find(ctx, answerID, p.runID, text); err != nil {
		if ctx.Err() == nil {
			s.log.Warn("read an answer for other brands", "run", p.runID, "answer", answerID, "err", err)
		}
		return
	}
	var spent float64
	if err := s.db.QueryRow(ctx, `SELECT `+runSpent+` FROM run WHERE id = $1`, p.runID).Scan(&spent); err != nil {
		if ctx.Err() == nil {
			s.log.Warn("read a run's spend", "run", p.runID, "err", err)
		}
		return
	}
	out.setSpent(spent)
}

func backoffFor(r *engines.Refusal, n int) time.Duration {
	d := r.RetryAfter
	if d <= 0 {
		d = defaultBackoff * time.Duration(n)
	}
	return min(d, maxBackoff)
}

// storeRetries is how many times a store that failed on the connection,
// not on the data, is tried again before the pass halts.
const storeRetries = 4

// storeRetrying stores one answer, one at a time across the run's calls so
// a check holds a single connection for writing however many calls are in
// flight, and tries again with backoff when the database is unreachable.
func (s *Service) storeRetrying(ctx context.Context, p plan, c call, res engines.Result, attempts int) (int64, float64, error) {
	s.writer.Lock()
	defer s.writer.Unlock()
	for try := 0; ; try++ {
		id, spent, err := s.store(ctx, p, c, res, attempts)
		if err == nil || permanent(err) || try == storeRetries || ctx.Err() != nil {
			return id, spent, err
		}
		if s.sleep(ctx, time.Second<<try) != nil {
			return 0, 0, err
		}
	}
}

// permanent reports whether a database error comes from the statement or
// its data, which retrying cannot fix, rather than from the connection.
func permanent(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	switch pgErr.Code[:2] {
	case "22", "23", "42":
		return true
	}
	return false
}

// store upserts one answer with its mentions and citations and returns
// its id and the run's spend. The run row is locked first, so the spend is
// summed one answer after another. The statements go in two batches, so an
// answer costs the same few round trips however far the database is.
func (s *Service) store(ctx context.Context, p plan, c call, res engines.Result, attempts int) (int64, float64, error) {
	// The brands are read for each answer, so a competitor tracked while
	// the check runs counts in the answers stored after it.
	brands, mode, err := s.config.MatchRules(ctx)
	if err != nil {
		return 0, 0, err
	}
	scored := panel.Score(res, brands, panel.Settings{MatchMode: mode})
	var spent float64
	var answerID int64
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		first := &pgx.Batch{}
		first.Queue(`SELECT 1 FROM run WHERE id = $1 FOR UPDATE`, p.runID)
		first.Queue(`INSERT INTO answer (run_id, question_id, surface_model_id, endpoint, fidelity, reported_model,
				status, error, attempts, mentioned, cited, position, cost_usd, text, raw, fan_out, call_id)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
			ON CONFLICT (run_id, question_id, surface_model_id, sample) DO UPDATE SET endpoint = excluded.endpoint,
				fidelity = excluded.fidelity, reported_model = excluded.reported_model, status = excluded.status,
				error = excluded.error, attempts = excluded.attempts, mentioned = excluded.mentioned, cited = excluded.cited,
				position = excluded.position, cost_usd = excluded.cost_usd, text = excluded.text, raw = excluded.raw,
				fan_out = excluded.fan_out, call_id = excluded.call_id, answered_at = now()
			RETURNING id`,
			p.runID, c.question.ID, c.pair.ID, c.via.Endpoint, c.pair.Fidelity, res.ReportedModel,
			res.Status, res.Error, attempts, scored.Mentioned, scored.Cited, nullPosition(scored.Position), res.CostUSD,
			res.Text, rawJSON(res.Raw), append([]string{}, res.FanOut...), res.CallID).QueryRow(func(row pgx.Row) error {
			return row.Scan(&answerID)
		})
		if err := tx.SendBatch(ctx, first).Close(); err != nil {
			return err
		}
		rest := &pgx.Batch{}
		rest.Queue(`DELETE FROM brand_mention WHERE answer_id = $1`, answerID)
		rest.Queue(`DELETE FROM citation WHERE answer_id = $1`, answerID)
		for _, m := range scored.Mentions {
			rest.Queue(`INSERT INTO brand_mention (answer_id, brand_id, position) VALUES ($1, $2, $3)`, answerID, m.Brand, nullPosition(m.Position))
		}
		for i, ci := range scored.Citations {
			rest.Queue(`INSERT INTO citation (answer_id, ord, url, domain, is_self) VALUES ($1, $2, $3, $4, $5)`, answerID, i, ci.URL, ci.Domain, ci.IsSelf)
		}
		rest.Queue(`UPDATE run SET
				spend_usd = (SELECT coalesce(sum(cost_usd), 0) FROM answer WHERE run_id = $1),
				spend_missing_calls = (SELECT count(*) FROM answer WHERE run_id = $1 AND cost_usd IS NULL AND status <> 'failed')
			WHERE id = $1 RETURNING `+runSpent, p.runID).QueryRow(func(row pgx.Row) error {
			return row.Scan(&spent)
		})
		return tx.SendBatch(ctx, rest).Close()
	})
	return answerID, spent, err
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
