// Package notify tells people when something moves. Emit stores each event
// once as an alert, which everyone who may read it sees in the app, and
// queues one delivery per enabled channel subscribed to its kind. Run sends
// queued deliveries off the request path, retrying a failed one with
// backoff a bounded number of times and logging each attempt's outcome.
//
// Channels are email through SMTP, email through Resend's HTTP API, a Slack
// incoming webhook, and a generic webhook signed with HMAC-SHA256. Their
// secrets are sealed with ENCRYPTION_KEY and no API returns them.
package notify

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/unkeyed/byname/internal/db"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/unkeyed/byname/internal/core"
	"github.com/unkeyed/byname/internal/engines"
	"github.com/unkeyed/byname/internal/panel"
	"github.com/unkeyed/byname/internal/results"
	"github.com/unkeyed/byname/internal/secretbox"
)

const (
	// MaxAttempts is how many times a delivery is tried before it is
	// marked failed.
	MaxAttempts = 5
	// lease is how long a claimed delivery waits before another process
	// may retry it, longer than any one send takes.
	lease = 2 * time.Minute
	// topQuestions is how many changed questions a check's alert names.
	topQuestions = 3
	pollEvery    = 15 * time.Second
)

// backoff is the wait after the nth failed attempt: 30s, 2m, 8m, 32m.
func backoff(attempt int) time.Duration { return 30 * time.Second << (2 * (attempt - 1)) }

type Service struct {
	db      db.Pool
	results *results.Service
	secrets *secretbox.Box
	public  *url.URL
	client  *http.Client
	resend  string
	rootCAs *x509.CertPool
	now     func() time.Time
	log     *slog.Logger
	wake    chan struct{}
}

type Options struct {
	// Secrets seals channel secrets.
	Secrets *secretbox.Box
	// PublicURL is where links in messages point.
	PublicURL *url.URL
	// HTTPClient sends to Resend, Slack and webhooks.
	HTTPClient *http.Client
	// ResendBaseURL replaces https://api.resend.com, for tests and local
	// development against a fake.
	ResendBaseURL string
	// SMTPRootCAs replaces the system roots for SMTP TLS, for tests.
	SMTPRootCAs *x509.CertPool
	Now         func() time.Time
	Log         *slog.Logger
}

func New(db db.Pool, res *results.Service, o Options) *Service {
	if o.HTTPClient == nil {
		o.HTTPClient = &http.Client{}
	}
	if o.ResendBaseURL == "" {
		o.ResendBaseURL = "https://api.resend.com"
	}
	if o.PublicURL == nil {
		o.PublicURL = &url.URL{Scheme: "http", Host: "localhost:8080"}
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Log == nil {
		o.Log = slog.Default()
	}
	return &Service{db: db, results: res, secrets: o.Secrets, public: o.PublicURL, client: o.HTTPClient, resend: o.ResendBaseURL,
		rootCAs: o.SMTPRootCAs, now: o.Now, log: o.Log, wake: make(chan struct{}, 1)}
}

// Emit stores e as an alert and queues its delivery to every enabled
// channel subscribed to its kind. It is idempotent on (kind, subject): an
// event emitted again, by a retried job or a resumed run, changes nothing.
// It returns once the alert and its deliveries are stored; sending happens
// in Run.
func (s *Service) Emit(ctx context.Context, e Event) error {
	if e.Payload == nil || e.Subject == "" {
		return errors.New("notify: an event needs a subject and a payload")
	}
	k := e.Payload.Kind()
	if _, ok := lookup(k); !ok {
		return fmt.Errorf("notify: event kind %q is not in the registry", k)
	}
	payload, err := json.Marshal(e.Payload)
	if err != nil {
		return err
	}
	queued := false
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var id int64
		err := tx.QueryRow(ctx, `INSERT INTO alert (kind, subject, payload, created_at) VALUES ($1, $2, $3, $4)
			ON CONFLICT (kind, subject) DO NOTHING RETURNING id`, k, e.Subject, payload, s.now()).Scan(&id)
		if err == pgx.ErrNoRows {
			return nil
		}
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `INSERT INTO alert_delivery (alert_id, channel_id, next_attempt_at)
			SELECT $1, id, $3 FROM notification_channel WHERE enabled AND $2 = ANY (events)`, id, k, s.now())
		queued = tag.RowsAffected() > 0
		return err
	})
	if queued {
		select {
		case s.wake <- struct{}{}:
		default:
		}
	}
	return err
}

// runEvents builds the event a finished run raises, by its status.
var runEvents = map[string]func(context.Context, *Service, results.Run) (Payload, error){
	"completed": func(ctx context.Context, s *Service, r results.Run) (Payload, error) { return s.checkCompleted(ctx, r) },
	"capped": func(_ context.Context, _ *Service, r results.Run) (Payload, error) {
		return SpendCapReachedPayload{RunID: r.ID, SpendUSD: r.SpendUSD, Message: r.Error}, nil
	},
	"refused": func(_ context.Context, _ *Service, r results.Run) (Payload, error) {
		return CheckRefusedPayload{RunID: r.ID, Code: r.RefusalCode, Message: r.Error}, nil
	},
	"failed": func(_ context.Context, _ *Service, r results.Run) (Payload, error) {
		return CheckFailedPayload{RunID: r.ID, Message: r.Error}, nil
	},
}

// RunFinished emits the event for a finished run: check_completed with the
// comparison to the previous run, or spend_cap_reached, check_refused or
// check_failed, then page_linked for each marked page its answers were the
// first to link to. A run still open raises nothing.
func (s *Service) RunFinished(ctx context.Context, runID int64) error {
	r, err := s.results.Run(ctx, runID)
	if err != nil {
		return err
	}
	build, ok := runEvents[r.Status]
	if !ok {
		return nil
	}
	p, err := build(ctx, s, r)
	if err != nil {
		return err
	}
	if err := s.Emit(ctx, Event{Subject: fmt.Sprintf("run/%d", runID), Payload: p}); err != nil {
		return err
	}
	links, err := s.results.LinkedFirstTime(ctx, runID)
	if err != nil {
		return err
	}
	for _, l := range links {
		apps := []string{}
		for _, pair := range l.Pairs {
			app := string(pair.Surface)
			if spec, ok := engines.Lookup(pair.Surface); ok {
				app = spec.Title
			}
			if !slices.Contains(apps, app) {
				apps = append(apps, app)
			}
		}
		if err := s.Emit(ctx, Event{Subject: fmt.Sprintf("page/%d/linked", l.PageID),
			Payload: PageLinkedPayload{PageID: l.PageID, URL: l.URL, QuestionText: l.Question, Apps: apps, Answers: l.Answers}}); err != nil {
			return err
		}
	}
	return nil
}

// checkCompleted compares a run with the previous run that has answers,
// through the same results.Compare that GET /v1/compare serves.
func (s *Service) checkCompleted(ctx context.Context, r results.Run) (Payload, error) {
	p := CheckCompletedPayload{RunID: r.ID, Named: r.Mentioned, Answered: r.Scored, Band: panel.Wilson(r.Mentioned, r.Scored), SpendUSD: r.SpendUSD, Top: []QuestionChange{}}
	table, err := s.results.RunQuestions(ctx, r.ID, results.Scope{})
	if err != nil || table.PreviousRunID == nil {
		return p, err
	}
	p.PreviousRunID = *table.PreviousRunID
	c, err := s.results.Compare(ctx, p.PreviousRunID, r.ID)
	if err != nil {
		return p, err
	}
	p.Gained, p.Lost, p.Moved = c.Counts["gained"], c.Counts["lost"], c.Counts["moved"]
	p.Unchanged, p.NotComparable, p.Net, p.Move = c.Counts["unchanged"], c.Counts["not_comparable"], c.Net, &c.Move
	rows := slices.Clone(table.Rows)
	changed := func(r results.QuestionRow) int { return r.Gained + r.Lost + r.Moved }
	slices.SortStableFunc(rows, func(a, b results.QuestionRow) int { return changed(b) - changed(a) })
	rows = slices.DeleteFunc(rows, func(r results.QuestionRow) bool { return !r.Question.CountsInScores })
	for _, row := range rows {
		if len(p.Top) == topQuestions || changed(row) == 0 {
			break
		}
		p.Top = append(p.Top, QuestionChange{QuestionID: row.Question.ID, Question: row.Question.Text, Gained: row.Gained, Lost: row.Lost, Moved: row.Moved})
	}
	return p, nil
}

// PairPaused emits pair_paused for a model that left its surface's live
// model list. One alert per pair per day, however often refresh runs.
func (s *Service) PairPaused(ctx context.Context, pairID int64, surface engines.Surface, model, reason string) error {
	return s.Emit(ctx, Event{
		Subject: fmt.Sprintf("pair/%d/%s", pairID, s.now().UTC().Format(time.DateOnly)),
		Payload: PairPausedPayload{PairID: pairID, Surface: surface, Model: model, Reason: reason},
	})
}

// Run sends due deliveries until ctx ends: at start, every pollEvery, and
// whenever Emit queues one.
func (s *Service) Run(ctx context.Context) {
	t := time.NewTicker(pollEvery)
	defer t.Stop()
	for {
		if _, err := s.DeliverDue(ctx); err != nil && ctx.Err() == nil {
			s.log.Error("sending notifications failed", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-s.wake:
		}
	}
}

// DeliverDue makes one attempt at every delivery due now and returns how
// many it attempted. Claiming a delivery counts the attempt before sending,
// so a crash mid-send never gives a delivery more than MaxAttempts.
func (s *Service) DeliverDue(ctx context.Context) (int, error) {
	n := 0
	for {
		var id, alertID, channelID int64
		var attempts int
		err := s.db.QueryRow(ctx, `UPDATE alert_delivery SET attempts = attempts + 1, next_attempt_at = $2
			WHERE id = (SELECT id FROM alert_delivery WHERE status = 'pending' AND next_attempt_at <= $1
				ORDER BY next_attempt_at, id LIMIT 1 FOR UPDATE SKIP LOCKED)
			RETURNING id, alert_id, channel_id, attempts`, s.now(), s.now().Add(lease)).Scan(&id, &alertID, &channelID, &attempts)
		if err == pgx.ErrNoRows {
			return n, nil
		}
		if err != nil {
			return n, err
		}
		n++
		sendErr := s.deliver(ctx, id, alertID, channelID)
		if ctx.Err() != nil {
			return n, ctx.Err()
		}
		if err := s.record(ctx, id, attempts, sendErr); err != nil {
			return n, err
		}
	}
}

func (s *Service) deliver(ctx context.Context, id, alertID, channelID int64) error {
	var k Kind
	var payload json.RawMessage
	var created time.Time
	if err := s.db.QueryRow(ctx, `SELECT kind, payload, created_at FROM alert WHERE id = $1`, alertID).Scan(&k, &payload, &created); err != nil {
		return err
	}
	ch, t, err := s.openChannel(ctx, channelID)
	if err != nil {
		return err
	}
	if !ch.Enabled {
		return errors.New("notifications here were turned off before this was sent")
	}
	info, ok := lookup(k)
	if !ok {
		return fmt.Errorf("event kind %q is no longer in the registry", k)
	}
	p, err := info.decode(payload)
	if err != nil {
		return err
	}
	e, err := s.envelope(ctx, fmt.Sprintf("alert-%d/channel-%d", alertID, channelID), p, ch.Name, created)
	if err != nil {
		return err
	}
	e.Payload = payload
	return t.send(ctx, s, e)
}

// record stores how an attempt went: sent, due again after backoff, or
// failed for good after MaxAttempts.
func (s *Service) record(ctx context.Context, id int64, attempts int, sendErr error) error {
	now := s.now()
	if sendErr == nil {
		_, err := s.db.Exec(ctx, `UPDATE alert_delivery SET status = 'sent', sent_at = $2, error = NULL WHERE id = $1`, id, now)
		return err
	}
	if attempts >= MaxAttempts {
		_, err := s.db.Exec(ctx, `UPDATE alert_delivery SET status = 'failed', error = $2 WHERE id = $1`, id, sendErr.Error())
		return err
	}
	_, err := s.db.Exec(ctx, `UPDATE alert_delivery SET next_attempt_at = $2, error = $3 WHERE id = $1`, id, now.Add(backoff(attempts)), sendErr.Error())
	return err
}

func (s *Service) envelope(ctx context.Context, id string, p Payload, channel string, created time.Time) (envelope, error) {
	m := p.message()
	if m.Lines == nil {
		m.Lines = []string{}
	}
	if m.Action == "" {
		m.Action = "Open in Byname"
	}
	w, err := db.CurrentWorkspace(ctx, s.db)
	if err != nil {
		return envelope{}, err
	}
	// Every link names its workspace, the default's too, so a message
	// opens the workspace it is about whichever one the reader last had
	// open.
	link := func(path string) string { return s.public.Scheme + "://" + s.public.Host + "/w/" + w.Slug + path }
	return envelope{ID: id, Kind: p.Kind(), Message: m, URL: link(m.Path), All: link(NotificationsPath), Settings: link(SettingsPath), Channel: channel, CreatedAt: created}, nil
}

// Alert is one event as the app shows it.
type Alert struct {
	ID        int64           `json:"id"`
	Kind      Kind            `json:"kind"`
	Title     string          `json:"title"`
	Lines     []string        `json:"lines"`
	Path      string          `json:"path" jsonschema:"the page it links to, relative to the app's address"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt time.Time       `json:"created_at"`
	Read      bool            `json:"read" jsonschema:"whether the caller's person has read it"`
}

type AlertList struct {
	Alerts []Alert `json:"alerts" jsonschema:"newest first"`
	Unread int     `json:"unread" jsonschema:"unread notifications the caller may see, in all"`
}

type AlertQuery struct {
	Unread bool `json:"unread,omitempty" jsonschema:"only notifications the caller's person has not read"`
	Limit  int  `json:"limit,omitempty" jsonschema:"at most this many, 1 to 200, default 50"`
}

// visibleKinds is the kinds a caller may read alerts of.
func visibleKinds(a core.Actor) []Kind {
	admin := a.Via == core.ViaSession && a.Role == core.Admin
	out := []Kind{}
	for _, k := range registry {
		if (k.Audience == Readers && a.Has(core.ResultsRead)) || (k.Audience == Admins && admin) {
			out = append(out, k.Kind)
		}
	}
	return out
}

// Alerts lists the alerts a caller may see, newest first, with whether its
// person has read each.
func (s *Service) Alerts(ctx context.Context, a core.Actor, q AlertQuery) (AlertList, error) {
	if q.Limit == 0 {
		q.Limit = 50
	}
	if q.Limit < 1 || q.Limit > 200 {
		return AlertList{}, fmt.Errorf("%w: limit must be 1 to 200", core.ErrInvalid)
	}
	kinds := visibleKinds(a)
	out := AlertList{Alerts: []Alert{}}
	rows, err := s.db.Query(ctx, `SELECT a.id, a.kind, a.payload, a.created_at, r.alert_id IS NOT NULL
		FROM alert a LEFT JOIN alert_read r ON r.alert_id = a.id AND r.user_id = $2
		WHERE a.kind = ANY ($1) AND (NOT $3 OR r.alert_id IS NULL)
		ORDER BY a.id DESC LIMIT $4`, kinds, a.UserID, q.Unread, q.Limit)
	if err != nil {
		return out, err
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Alert, error) {
		var al Alert
		err := r.Scan(&al.ID, &al.Kind, &al.Payload, &al.CreatedAt, &al.Read)
		return al, err
	})
	if err != nil {
		return out, err
	}
	for _, al := range list {
		info, _ := lookup(al.Kind)
		p, err := info.decode(al.Payload)
		if err != nil {
			return out, fmt.Errorf("notification %d: %w", al.ID, err)
		}
		m := p.message()
		al.Title, al.Lines, al.Path = m.Title, m.Lines, m.Path
		if al.Lines == nil {
			al.Lines = []string{}
		}
		out.Alerts = append(out.Alerts, al)
	}
	out.Unread, err = s.unread(ctx, kinds, a.UserID)
	return out, err
}

func (s *Service) unread(ctx context.Context, kinds []Kind, userID int64) (int, error) {
	var n int
	err := s.db.QueryRow(ctx, `SELECT count(*) FROM alert a WHERE a.kind = ANY ($1)
		AND NOT EXISTS (SELECT 1 FROM alert_read r WHERE r.alert_id = a.id AND r.user_id = $2)`, kinds, userID).Scan(&n)
	return n, err
}

type ReadInput struct {
	IDs []int64 `json:"ids,omitempty" jsonschema:"the notifications to mark read"`
	All bool    `json:"all,omitempty" jsonschema:"mark every notification the caller may see read"`
}

type ReadResult struct {
	Unread int `json:"unread" jsonschema:"unread notifications left"`
}

// MarkRead marks alerts read for the caller's person. Marking one twice is
// fine.
func (s *Service) MarkRead(ctx context.Context, a core.Actor, in ReadInput) (ReadResult, error) {
	if a.UserID == 0 {
		return ReadResult{}, fmt.Errorf("%w: this credential acts for nobody, so it has no read state", core.ErrInvalid)
	}
	if !in.All && len(in.IDs) == 0 {
		return ReadResult{}, fmt.Errorf("%w: give ids, or all: true", core.ErrInvalid)
	}
	kinds := visibleKinds(a)
	if _, err := s.db.Exec(ctx, `INSERT INTO alert_read (user_id, alert_id, read_at)
		SELECT $1, id, $5 FROM alert WHERE kind = ANY ($2) AND ($3 OR id = ANY ($4))
		ON CONFLICT DO NOTHING`, a.UserID, kinds, in.All, in.IDs, s.now()); err != nil {
		return ReadResult{}, err
	}
	n, err := s.unread(ctx, kinds, a.UserID)
	return ReadResult{Unread: n}, err
}
