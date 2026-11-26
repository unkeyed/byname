package notify_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/unkeyed/byname/internal/db"
	"github.com/unkeyed/byname/internal/notify"
	"github.com/unkeyed/byname/internal/notify/notifytest"
	"github.com/unkeyed/byname/internal/results"
	"github.com/unkeyed/byname/internal/secretbox"
	"github.com/unkeyed/byname/internal/testdb"
)

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

type env struct {
	db     *pgxpool.Pool
	notify *notify.Service
	clock  *clock
}

func setup(t *testing.T, o notify.Options) *env {
	t.Helper()
	// Postgres keeps microseconds, so a clock with nanoseconds would not survive a round trip on Linux.
	e := &env{db: testdb.New(t), clock: &clock{t: time.Now().Truncate(time.Microsecond)}}
	o.Secrets = secretbox.ForTest()
	o.PublicURL = &url.URL{Scheme: "https", Host: "byname.example"}
	o.Now = e.clock.now
	e.notify = notify.New(e.db, results.New(e.db), o)
	return e
}

func (e *env) channel(t *testing.T, in notify.ChannelInput) notify.CreatedChannel {
	t.Helper()
	c, err := e.notify.CreateChannel(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func webhook(name, url string, events ...notify.Kind) notify.ChannelInput {
	return notify.ChannelInput{Name: name, Kind: notify.Webhook, Settings: obj(map[string]any{"url": url}), Events: events}
}

func obj(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func (e *env) deliveries(t *testing.T) []notify.Delivery {
	t.Helper()
	d, err := e.notify.Deliveries(context.Background(), notify.DeliveryQuery{})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func failed(run int64) notify.Event {
	return notify.Event{Subject: fmt.Sprintf("run/%d", run), Payload: notify.CheckFailedPayload{RunID: run, Message: "no surface and model pair is enabled"}}
}

func TestEmitIsIdempotent(t *testing.T) {
	ctx := context.Background()
	e := setup(t, notify.Options{})
	rec, srv := notifytest.NewReceiver(t)
	e.channel(t, webhook("Ops hook", srv.URL, notify.CheckFailed))

	for range 3 {
		if err := e.notify.Emit(ctx, failed(7)); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := e.notify.DeliverDue(ctx); err != nil || n != 1 {
		t.Fatalf("DeliverDue = %d, %v; want one delivery for one event emitted three times", n, err)
	}
	if err := e.notify.Emit(ctx, failed(7)); err != nil {
		t.Fatal(err)
	}
	if n, _ := e.notify.DeliverDue(ctx); n != 0 || len(rec.Requests()) != 1 {
		t.Fatalf("after emitting again: %d more deliveries, %d requests; want 0 and 1", n, len(rec.Requests()))
	}
	var alerts int
	if err := e.db.QueryRow(ctx, `SELECT count(*) FROM alert`).Scan(&alerts); err != nil || alerts != 1 {
		t.Fatalf("alerts = %d, %v; want 1", alerts, err)
	}
}

func TestOnlySubscribedEnabledChannelsGetAnEvent(t *testing.T) {
	ctx := context.Background()
	e := setup(t, notify.Options{})
	wants, wantsSrv := notifytest.NewReceiver(t)
	other, otherSrv := notifytest.NewReceiver(t)
	off, offSrv := notifytest.NewReceiver(t)
	e.channel(t, webhook("Failures", wantsSrv.URL, notify.CheckFailed, notify.CheckCompleted))
	e.channel(t, webhook("Spend only", otherSrv.URL, notify.SpendCapReached))
	disabled := webhook("Turned off", offSrv.URL, notify.CheckFailed)
	disabled.Enabled = new(false)
	e.channel(t, disabled)

	if err := e.notify.Emit(ctx, failed(3)); err != nil {
		t.Fatal(err)
	}
	if _, err := e.notify.DeliverDue(ctx); err != nil {
		t.Fatal(err)
	}
	if len(wants.Requests()) != 1 || len(other.Requests()) != 0 || len(off.Requests()) != 0 {
		t.Fatalf("requests: subscribed %d, other kind %d, disabled %d; want 1, 0, 0", len(wants.Requests()), len(other.Requests()), len(off.Requests()))
	}
	d := e.deliveries(t)
	if len(d) != 1 || d[0].ChannelName != "Failures" || d[0].Status != "sent" || d[0].Attempts != 1 {
		t.Fatalf("delivery log = %+v, want one sent delivery to Failures", d)
	}
}

func TestWebhookSignatureVerifiesAndATamperedBodyFails(t *testing.T) {
	ctx := context.Background()
	e := setup(t, notify.Options{})
	rec, srv := notifytest.NewReceiver(t)
	c := e.channel(t, webhook("Signed", srv.URL, notify.CheckFailed))
	if !strings.HasPrefix(c.SigningSecret, "whsec_") {
		t.Fatalf("signing secret = %q, want one shown at creation", c.SigningSecret)
	}
	rec.SetSecret(c.SigningSecret)
	if err := e.notify.Emit(ctx, failed(4)); err != nil {
		t.Fatal(err)
	}
	if _, err := e.notify.DeliverDue(ctx); err != nil {
		t.Fatal(err)
	}
	reqs := rec.Requests()
	if len(reqs) != 1 || reqs[0].SignatureErr != nil {
		t.Fatalf("requests = %+v, want one whose signature verifies", reqs)
	}
	var body notify.WebhookBody
	if err := json.Unmarshal(reqs[0].Body, &body); err != nil || body.Kind != notify.CheckFailed || body.URL != "https://byname.example/w/default/settings/checks" {
		t.Fatalf("body = %+v, %v", body, err)
	}

	header := reqs[0].Header.Get(notify.SignatureHeader)
	tampered := []byte(strings.Replace(string(reqs[0].Body), "check_failed", "check_complete", 1))
	if err := notify.Verify(c.SigningSecret, header, tampered, time.Now(), 5*time.Minute); err == nil {
		t.Fatal("a tampered body verified")
	}
	if err := notify.Verify("whsec_other", header, reqs[0].Body, time.Now(), 5*time.Minute); err == nil {
		t.Fatal("another channel's secret verified")
	}
	if err := notify.Verify(c.SigningSecret, header, reqs[0].Body, time.Now().Add(time.Hour), 5*time.Minute); err == nil {
		t.Fatal("a replay an hour later verified")
	}
	if d := e.deliveries(t); d[0].Status != "sent" {
		t.Fatalf("delivery = %+v, want sent", d[0])
	}
}

func TestRetriesAreBoundedWithBackoff(t *testing.T) {
	ctx := context.Background()
	e := setup(t, notify.Options{})
	rec, srv := notifytest.NewReceiver(t)
	rec.Fail(func(int) int { return http.StatusBadGateway })
	e.channel(t, webhook("Down", srv.URL, notify.CheckFailed))
	if err := e.notify.Emit(ctx, failed(5)); err != nil {
		t.Fatal(err)
	}
	for i := range 20 {
		if _, err := e.notify.DeliverDue(ctx); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			if n, _ := e.notify.DeliverDue(ctx); n != 0 {
				t.Fatal("a failed delivery was retried at once, without backoff")
			}
			d := e.deliveries(t)[0]
			if d.Status != "pending" || d.NextAttemptAt == nil || d.NextAttemptAt.Sub(e.clock.now()) != 30*time.Second || !strings.Contains(d.Error, "502") {
				t.Fatalf("after one failure: %+v, want pending, due in 30s, with the 502", d)
			}
		}
		e.clock.add(time.Hour)
	}
	if n := len(rec.Requests()); n != notify.MaxAttempts {
		t.Fatalf("receiver got %d requests, want exactly %d", n, notify.MaxAttempts)
	}
	d := e.deliveries(t)[0]
	if d.Status != "failed" || d.Attempts != notify.MaxAttempts || d.NextAttemptAt != nil {
		t.Fatalf("delivery = %+v, want failed after %d attempts", d, notify.MaxAttempts)
	}
}

func TestARecoveredReceiverGetsTheRetry(t *testing.T) {
	ctx := context.Background()
	e := setup(t, notify.Options{})
	rec, srv := notifytest.NewReceiver(t)
	rec.Fail(func(n int) int {
		if n < 3 {
			return http.StatusServiceUnavailable
		}
		return 0
	})
	e.channel(t, webhook("Flaky", srv.URL, notify.CheckFailed))
	if err := e.notify.Emit(ctx, failed(6)); err != nil {
		t.Fatal(err)
	}
	for range 5 {
		if _, err := e.notify.DeliverDue(ctx); err != nil {
			t.Fatal(err)
		}
		e.clock.add(time.Hour)
	}
	d := e.deliveries(t)[0]
	if d.Status != "sent" || d.Attempts != 3 || d.Error != "" || len(rec.Requests()) != 3 {
		t.Fatalf("delivery = %+v after %d requests, want sent on the third", d, len(rec.Requests()))
	}
}

func TestEmailChannels(t *testing.T) {
	ctx := context.Background()
	for _, mode := range []notifytest.SMTPMode{notifytest.StartTLS, notifytest.Implicit, notifytest.Plain} {
		t.Run(string(mode), func(t *testing.T) {
			smtp := notifytest.NewSMTP(t, mode)
			smtp.Users["alerts"] = "smtp-secret"
			resend, resendSrv := notifytest.NewResend(t, "re_secret")
			e := setup(t, notify.Options{SMTPRootCAs: smtp.RootCAs(), ResendBaseURL: resendSrv.URL})
			e.channel(t, notify.ChannelInput{Name: "SMTP", Kind: notify.SMTP, Events: []notify.Kind{notify.SpendCapReached},
				Settings: obj(map[string]any{"host": "localhost", "port": smtp.Port(), "tls": string(mode), "username": "alerts",
					"from": "Byname <alerts@northwind.example>", "to": []string{"ops@northwind.example", "Dana <dana@northwind.example>"}}),
				Secrets: obj(map[string]string{"password": "smtp-secret"})})
			e.channel(t, notify.ChannelInput{Name: "Resend", Kind: notify.EmailAPI, Events: []notify.Kind{notify.SpendCapReached},
				Settings: obj(map[string]any{"from": "alerts@northwind.example", "to": []string{"ops@northwind.example"}}),
				Secrets:  obj(map[string]string{"api_key": "re_secret"})})
			ev := notify.Event{Subject: "run/9", Payload: notify.SpendCapReachedPayload{RunID: 9, SpendUSD: 5.01, Message: "stopped asking questions at $5.01; the spend cap for one check is $5.00"}}
			if err := e.notify.Emit(ctx, ev); err != nil {
				t.Fatal(err)
			}
			if _, err := e.notify.DeliverDue(ctx); err != nil {
				t.Fatal(err)
			}
			for _, d := range e.deliveries(t) {
				if d.Status != "sent" {
					t.Fatalf("%s delivery = %+v", d.ChannelName, d)
				}
			}
			mail := smtp.Mail()
			if len(mail) != 1 {
				t.Fatalf("SMTP got %d messages, want 1", len(mail))
			}
			m := mail[0]
			if m.User != "alerts" || m.TLS != (mode != notifytest.Plain) || m.From != "alerts@northwind.example" || len(m.To) != 2 ||
				m.Subject != "Check #9 reached its spend cap" || !strings.Contains(m.Body, "the spend cap for one check is $5.00") ||
				!strings.Contains(m.Body, "Open schedule and limits: https://byname.example/w/default/settings/schedule\nEvery notification: https://byname.example/w/default/notifications\n") {
				t.Fatalf("mail = %+v", m)
			}
			emails := resend.Emails()
			if len(emails) != 1 || emails[0].Subject != m.Subject || emails[0].To[0] != "ops@northwind.example" || emails[0].IdempotencyKey == "" ||
				!strings.Contains(emails[0].Text, "https://byname.example/w/default/settings/schedule") {
				t.Fatalf("Resend got %+v", emails)
			}
		})
	}
}

func TestSlackGetsBlockKit(t *testing.T) {
	ctx := context.Background()
	e := setup(t, notify.Options{})
	rec, srv := notifytest.NewReceiver(t)
	e.channel(t, notify.ChannelInput{Name: "Slack", Kind: notify.SlackWebhook, Events: []notify.Kind{notify.PairPaused},
		Secrets: obj(map[string]string{"webhook_url": srv.URL + "/services/T0/B0/x"})})
	if err := e.notify.PairPaused(ctx, 4, "claude_code", "claude-sonnet-5-5", "claude-sonnet-5-5 left Claude Code's model list on 2026-11-21 <today>"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.notify.DeliverDue(ctx); err != nil {
		t.Fatal(err)
	}
	reqs := rec.Requests()
	if len(reqs) != 1 {
		t.Fatalf("Slack got %d requests", len(reqs))
	}
	var msg struct {
		Text   string `json:"text"`
		Blocks []struct {
			Type string `json:"type"`
			Text struct {
				Text string `json:"text"`
			} `json:"text"`
			Elements []struct {
				Text string `json:"text"`
			} `json:"elements"`
		} `json:"blocks"`
	}
	if err := json.Unmarshal(reqs[0].Body, &msg); err != nil || msg.Text != "Paused claude-sonnet-5-5 on Claude Code" || len(msg.Blocks) != 2 ||
		msg.Blocks[0].Type != "section" || !strings.Contains(msg.Blocks[0].Text.Text, "&lt;today&gt;") ||
		len(msg.Blocks[1].Elements) != 1 || msg.Blocks[1].Elements[0].Text != "<https://byname.example/w/default/settings/ai-apps|Open AI apps settings> · <https://byname.example/w/default/notifications|Every notification>" {
		t.Fatalf("Slack body = %s, %v", reqs[0].Body, err)
	}
}

func TestChannelInputIsValidated(t *testing.T) {
	ctx := context.Background()
	e := setup(t, notify.Options{})
	for name, in := range map[string]notify.ChannelInput{
		"unknown kind":          {Name: "x", Kind: "pager", Settings: obj(map[string]any{})},
		"unknown event":         {Name: "x", Kind: notify.Webhook, Settings: obj(map[string]any{"url": "https://hooks.example/x"}), Events: []notify.Kind{"lunch"}},
		"http to a public host": webhook("x", "http://hooks.example/x"),
		"unknown setting":       {Name: "x", Kind: notify.Webhook, Settings: obj(map[string]any{"url": "https://hooks.example/x", "method": "GET"})},
		"webhook secret given":  {Name: "x", Kind: notify.Webhook, Settings: obj(map[string]any{"url": "https://hooks.example/x"}), Secrets: obj(map[string]string{"signing_secret": "mine"})},
		"smtp without to":       {Name: "x", Kind: notify.SMTP, Settings: obj(map[string]any{"host": "smtp.example.com", "port": 587, "tls": "starttls", "from": "a@example.com"})},
		"smtp user without password": {Name: "x", Kind: notify.SMTP,
			Settings: obj(map[string]any{"host": "smtp.example.com", "port": 587, "tls": "starttls", "username": "u", "from": "a@example.com", "to": []string{"b@example.com"}})},
		"resend without key": {Name: "x", Kind: notify.EmailAPI, Settings: obj(map[string]any{"from": "a@example.com", "to": []string{"b@example.com"}})},
	} {
		if _, err := e.notify.CreateChannel(ctx, in); err == nil {
			t.Errorf("%s: created, want refused", name)
		}
	}
}

func TestAnEditKeepsSecretsItLeavesOut(t *testing.T) {
	ctx := context.Background()
	resend, resendSrv := notifytest.NewResend(t, "re_first")
	e := setup(t, notify.Options{ResendBaseURL: resendSrv.URL})
	in := notify.ChannelInput{Name: "Resend", Kind: notify.EmailAPI, Events: []notify.Kind{notify.CheckFailed},
		Settings: obj(map[string]any{"from": "alerts@northwind.example", "to": []string{"ops@northwind.example"}}),
		Secrets:  obj(map[string]string{"api_key": "re_first"})}
	c := e.channel(t, in)
	in.Secrets, in.Name = nil, "Resend email"
	in.Settings = obj(map[string]any{"from": "alerts@northwind.example", "to": []string{"ops@northwind.example", "dana@northwind.example"}})
	if _, err := e.notify.UpdateChannel(ctx, c.ID, in); err != nil {
		t.Fatal(err)
	}
	tested, err := e.notify.TestChannel(ctx, c.ID)
	if err != nil || tested.LastTestStatus != "ok" || tested.LastTestAt == nil || tested.Name != "Resend email" {
		t.Fatalf("test after an edit without secrets = %+v, %v; want ok with the stored key", tested, err)
	}
	if got := resend.Emails(); len(got) != 1 || got[0].APIKey != "re_first" || len(got[0].To) != 2 {
		t.Fatalf("Resend got %+v", got)
	}
	resend.SetAPIKey("re_second")
	in.Secrets = obj(map[string]string{"api_key": "re_second"})
	if _, err := e.notify.UpdateChannel(ctx, c.ID, in); err != nil {
		t.Fatal(err)
	}
	if tested, _ := e.notify.TestChannel(ctx, c.ID); tested.LastTestStatus != "ok" {
		t.Fatalf("test after replacing the key = %q", tested.LastTestStatus)
	}
	resend.SetAPIKey("re_third")
	if tested, _ := e.notify.TestChannel(ctx, c.ID); !strings.Contains(tested.LastTestStatus, "401") {
		t.Fatalf("test with a revoked key = %q, want the 401 recorded", tested.LastTestStatus)
	}
}

// Each workspace's messages link into that workspace.
func TestLinksNameTheWorkspace(t *testing.T) {
	ctx := context.Background()
	e := setup(t, notify.Options{})
	w, err := db.CreateWorkspace(ctx, e.db, "Kyring", "kyring")
	if err != nil {
		t.Fatal(err)
	}
	pool := db.In(e.db, w.Schema)
	kyring := notify.New(pool, results.New(pool), notify.Options{Secrets: secretbox.ForTest(), PublicURL: &url.URL{Scheme: "https", Host: "byname.example"}})
	rec, srv := notifytest.NewReceiver(t)
	if _, err := kyring.CreateChannel(ctx, webhook("Kyring hook", srv.URL, notify.CheckFailed)); err != nil {
		t.Fatal(err)
	}
	if err := kyring.Emit(ctx, failed(4)); err != nil {
		t.Fatal(err)
	}
	if _, err := kyring.DeliverDue(ctx); err != nil {
		t.Fatal(err)
	}
	reqs := rec.Requests()
	var body notify.WebhookBody
	if len(reqs) != 1 || json.Unmarshal(reqs[0].Body, &body) != nil || body.URL != "https://byname.example/w/kyring/settings/checks" {
		t.Fatalf("Kyring's webhook got %+v, want a link into /w/kyring", body)
	}
	if want := w.Schema.String() + "/alert-1/channel-1"; body.ID != want {
		t.Fatalf("Kyring's first message has id %q, want %q, apart from the first workspace's alert-1/channel-1", body.ID, want)
	}
	if n, err := e.notify.DeliverDue(ctx); err != nil || n != 0 {
		t.Fatalf("the default workspace delivered %d, %v; want nothing of Kyring's", n, err)
	}
}
