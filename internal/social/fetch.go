package social

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/unkeyed/byname/internal/engines"
	"github.com/unkeyed/byname/internal/notify"
)

// lock keeps two replicas from fetching or sending a digest at once.
const lock = `hashtext('byname.social')`

// Tick is the scheduler's call each minute: fetch when the period since
// the last fetch has passed, then emit the digest of a period that has
// ended. It holds an advisory lock and returns at once when another
// process holds it.
func (s *Service) Tick(ctx context.Context) error {
	return s.locked(ctx, func() error {
		st, err := s.settings(ctx)
		if err != nil {
			return err
		}
		due := st.lastStartsAt == nil || !st.lastStartsAt.Add(time.Duration(st.every)*time.Hour).After(s.now())
		var fetchErr error
		if due {
			fetchErr = s.fetchNow(ctx, st)
		}
		return errors.Join(fetchErr, s.digestIfDue(ctx, st))
	})
}

// Fetch searches every enabled network now, whether or not it is due.
func (s *Service) Fetch(ctx context.Context) error {
	return s.locked(ctx, func() error {
		st, err := s.settings(ctx)
		if err != nil {
			return err
		}
		return s.fetchNow(ctx, st)
	})
}

// fetchNow runs one social_fetch job when social is on, onboarding is
// finished and there is something to search: a term, and a network turned
// on that can run, which without a Treg token means a direct one.
// Otherwise it does nothing, calls nothing and logs nothing.
func (s *Service) fetchNow(ctx context.Context, st settings) error {
	if !st.on {
		return nil
	}
	treg, token, err := s.keys.Treg(ctx)
	if err != nil {
		return err
	}
	nets := st.networks(token)
	if len(nets) == 0 || len(st.terms) == 0 {
		return nil
	}
	return s.job(ctx, func() error { return s.fetch(ctx, st, nets, treg) })
}

// stop ends a fetch with a job status other than failed: capped or
// refused, which end it early, or completed with a note.
type stop struct {
	status string
	msg    string
}

func (e *stop) Error() string { return e.msg }

// unavailable is a direct network's public search asking for sign-in.
type unavailable struct {
	title  string
	status int
}

func (e *unavailable) Error() string {
	return fmt.Sprintf("%s search is unavailable: its public API now asks for sign-in (HTTP %d)", e.title, e.status)
}

// fetch searches each network for each term, newest first, paging back
// to the newest post already stored for that network and term, at most
// spec.Pages calls each. A call that fails is recorded and the next
// network goes on. Before each Treg call it checks the monthly spend cap,
// which AI checks and social searches share; once the cap is reached or
// Treg refuses a call, the rest of the Treg networks are skipped and the
// free direct ones still run. A direct network that asks for sign-in is
// skipped for the rest of the fetch and noted on the job.
func (s *Service) fetch(ctx context.Context, st settings, nets []spec, treg engines.Treg) error {
	spent, err := s.monthSpend(ctx, st)
	if err != nil {
		return err
	}
	var failed []string
	var notes []string
	var tregStop *stop
	for _, n := range nets {
		if n.Route == ViaTreg && tregStop != nil {
			continue
		}
	terms:
		for _, term := range st.terms {
			var newest *time.Time
			if err := s.db.QueryRow(ctx, `SELECT max(posted_at) FROM social_mention WHERE network = $1 AND term = $2 AND NOT posted_approx`,
				n.Network, term).Scan(&newest); err != nil {
				return err
			}
			q := search{Term: term, Newest: newest}
			for range n.Pages {
				if n.Route == ViaTreg && st.monthCap != nil && spent >= *st.monthCap {
					tregStop = &stop{"capped", fmt.Sprintf("stopped searching: checks and social searches have spent $%.4f this month; the monthly spend cap is $%.2f", spent, *st.monthCap)}
					break terms
				}
				q.At = s.now()
				page, cost, err := s.call(ctx, treg, n, q)
				spent += cost
				var refusal *engines.Refusal
				var off *unavailable
				switch {
				case errors.As(err, &refusal):
					tregStop = &stop{"refused", refusal.Error()}
					break terms
				case errors.As(err, &off):
					notes = append(notes, off.Error())
					break terms
				case err != nil:
					failed = append(failed, fmt.Sprintf("%s %q: %v", n.Title, term, err))
				}
				if err != nil || page.Next == "" || len(page.Posts) == 0 || reached(page, newest) {
					break
				}
				q.Cursor = page.Next
			}
		}
	}
	switch {
	case len(failed) > 0:
		if tregStop != nil {
			failed = append(failed, tregStop.msg)
		}
		return errors.New(strings.Join(append(failed, notes...), "; "))
	case tregStop != nil:
		return &stop{tregStop.status, strings.Join(append([]string{tregStop.msg}, notes...), "; ")}
	case len(notes) > 0:
		return &stop{"completed", strings.Join(notes, "; ")}
	}
	return nil
}

// reached reports whether a page goes back as far as the newest post
// already stored, so the pages after it hold nothing new.
func reached(p Page, newest *time.Time) bool {
	if newest == nil {
		return false
	}
	for _, post := range p.Posts {
		if !post.PostedAt.After(*newest) {
			return true
		}
	}
	return false
}

func (s *Service) monthSpend(ctx context.Context, st settings) (float64, error) {
	local := s.now().In(st.loc)
	start := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, st.loc)
	var spent float64
	err := s.db.QueryRow(ctx, `SELECT coalesce(sum(usd), 0)::float8 FROM spend WHERE at >= $1`, start).Scan(&spent)
	return spent, err
}

// answer is a search call's outcome on either route.
type answer struct {
	status int
	body   []byte
	// cost is nil when Treg reported none: a missing cost, never $0. A
	// direct call's is a known 0.
	cost   *float64
	callID string
}

// send makes one search call on the network's route. An error means no
// answer: the connection failed, Treg refused (*engines.Refusal), or a
// direct network asked for sign-in (*unavailable).
func (s *Service) send(ctx context.Context, treg engines.Treg, n spec, q search) (answer, error) {
	req := n.request(q)
	if n.Route == ViaTreg {
		res, err := treg.CallTool(ctx, engines.ToolCall{Method: n.Method, Tool: n.Tool, Query: req.Query, Body: req.Body, MaxCostUSD: n.MaxCostUSD})
		return answer{status: res.StatusCode, body: res.Body, cost: res.CostUSD, callID: res.CallID}, err
	}
	free := answer{cost: new(0.0)}
	target := s.baseURLs[n.Network] + n.Path
	if len(req.Query) > 0 {
		target += "?" + req.Query.Encode()
	}
	var body io.Reader
	if req.Body != nil {
		b, err := json.Marshal(req.Body)
		if err != nil {
			return free, err
		}
		body = bytes.NewReader(b)
	}
	hreq, err := http.NewRequestWithContext(ctx, n.Method, target, body)
	if err != nil {
		return free, err
	}
	hreq.Header.Set("Accept", "application/json")
	resp, err := s.client.Do(hreq)
	if err != nil {
		return free, fmt.Errorf("%s: %w", n.Title, err)
	}
	defer resp.Body.Close()
	free.status = resp.StatusCode
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return free, &unavailable{title: n.Title, status: resp.StatusCode}
	}
	free.body, err = io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	return free, err
}

// callStatus is how a call ended, as social_call.status stores it.
func callStatus(err error) string {
	var refusal *engines.Refusal
	var off *unavailable
	switch {
	case err == nil:
		return "ok"
	case errors.As(err, &refusal):
		return "refused"
	case errors.As(err, &off):
		return "unavailable"
	}
	return "failed"
}

// call makes one search call and stores it with the posts it found, in
// one transaction, so a run cut off midway leaves whole calls behind and
// the next fetch picks up from the newest post stored.
func (s *Service) call(ctx context.Context, treg engines.Treg, n spec, q search) (Page, float64, error) {
	res, err := s.send(ctx, treg, n, q)
	var page Page
	switch {
	case err != nil:
	case res.status/100 != 2:
		err = fmt.Errorf("provider HTTP %d", res.status)
	default:
		page, err = n.parse(res.body, q.At)
	}
	var httpStatus *int
	if res.status != 0 {
		httpStatus = &res.status
	}
	var msg *string
	if err != nil {
		msg = new(err.Error())
	}
	var cost float64
	if res.cost != nil {
		cost = *res.cost
	}
	txErr := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var callID int64
		if err := tx.QueryRow(ctx, `INSERT INTO social_call (network, route, term, tool, treg_call_id, status, http_status, cost_usd, posts, error, called_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11) RETURNING id`,
			n.Network, n.Route, q.Term, n.Tool, res.callID, callStatus(err), httpStatus, res.cost, len(page.Posts), msg, q.At).Scan(&callID); err != nil {
			return err
		}
		for _, p := range page.Posts {
			if p.ID == "" {
				continue
			}
			e := p.Engagement
			if _, err := tx.Exec(ctx, `INSERT INTO social_mention (network, post_id, url, author_handle, author_name, text, posted_at, posted_approx,
					likes, replies, reposts, score, views, term, call_id, first_seen_at, last_seen_at)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $16)
				ON CONFLICT (network, post_id) DO UPDATE SET url = excluded.url, author_name = excluded.author_name, text = excluded.text,
					likes = coalesce(excluded.likes, social_mention.likes), replies = coalesce(excluded.replies, social_mention.replies),
					reposts = coalesce(excluded.reposts, social_mention.reposts), score = coalesce(excluded.score, social_mention.score),
					views = coalesce(excluded.views, social_mention.views), last_seen_at = excluded.last_seen_at`,
				n.Network, p.ID, p.URL, storedHandle(n.Network, p.AuthorHandle), p.AuthorName, p.Text, p.PostedAt, p.PostedApprox,
				e.Likes, e.Replies, e.Reposts, e.Score, e.Views, q.Term, callID, q.At); err != nil {
				return err
			}
		}
		return nil
	})
	return page, cost, errors.Join(err, txErr)
}

// digestIfDue emits the digest of the latest period that has ended, once,
// when searches ran during it. Emit is idempotent on the period, so a
// crash before the period is marked sent re-emits nothing.
func (s *Service) digestIfDue(ctx context.Context, st settings) error {
	if s.alerts == nil {
		return nil
	}
	p := st.latestFinished(s.now())
	if st.sentThrough != nil && *st.sentThrough >= p.from() {
		return nil
	}
	var ran bool
	if err := s.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM social_call WHERE called_at >= $1 AND called_at < $2)`, p.start, p.end).Scan(&ran); err != nil || !ran {
		return err
	}
	d, err := s.digest(ctx, p)
	if err != nil {
		return err
	}
	if err := s.alerts.Emit(ctx, notify.Event{Subject: p.subject(), Payload: d.payload()}); err != nil {
		return err
	}
	_, err = s.db.Exec(ctx, `UPDATE instance_settings SET social_digest_sent_through = $1`, p.from())
	return err
}

// job logs f as one social_fetch run. A *stop ends it with its status and
// note; anything else fails it.
func (s *Service) job(ctx context.Context, f func() error) error {
	var id int64
	if err := s.db.QueryRow(ctx, `INSERT INTO job_run (job, trigger, started_at) VALUES ('social_fetch', 'scheduled', $1) RETURNING id`, s.now()).Scan(&id); err != nil {
		return err
	}
	err := f()
	status, msg := "completed", (*string)(nil)
	var st *stop
	if err != nil {
		status, msg = "failed", new(err.Error())
		if errors.As(err, &st) {
			status = st.status
		}
	}
	if _, ferr := s.db.Exec(context.WithoutCancel(ctx), `UPDATE job_run SET status = $2, error = $3, finished_at = $4 WHERE id = $1`,
		id, status, msg, s.now()); ferr != nil {
		return errors.Join(err, ferr)
	}
	if err != nil && status != "completed" {
		return fmt.Errorf("social_fetch: %w", err)
	}
	return nil
}

// locked runs f holding the session advisory lock on a dedicated
// connection, so the lock dies with the process. With the lock held no
// fetch runs anywhere, so one still marked running was cut off; its calls
// and posts are stored and the next fetch carries on from them.
func (s *Service) locked(ctx context.Context, f func() error) error {
	conn, err := s.db.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	var got bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(`+lock+`)`).Scan(&got); err != nil || !got {
		return err
	}
	defer func() {
		if _, err := conn.Exec(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock(`+lock+`)`); err != nil {
			_ = conn.Conn().Close(context.WithoutCancel(ctx))
		}
	}()
	if _, err := conn.Exec(ctx, `UPDATE job_run SET status = 'failed', finished_at = $1,
			error = 'the process stopped before this fetch finished; the posts it stored stay and the next fetch carries on'
		WHERE job = 'social_fetch' AND status = 'running'`, s.now()); err != nil {
		return err
	}
	return f()
}
