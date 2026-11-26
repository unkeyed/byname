package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/unkeyed/byname/internal/core"
	"github.com/unkeyed/byname/internal/notify"
	"github.com/unkeyed/byname/internal/notify/notifytest"
	"github.com/unkeyed/byname/internal/panel"
)

const resendKey = "re_test_key_never_returned"

const (
	claudeLiveTool  = "dataforseo.x.ai-optimization-claude-llm-responses-live"
	aiOverviewTool  = "cloro.google.serp.organic"
	claudeNamesNone = `{"status_code":20000,"tasks":[{"status_code":20000,"result":[{"model_name":"claude-sonnet-4-5-20250929",
	"items":[{"type":"message","sections":[{"type":"text","text":"Contoso Gateway is the usual choice for this."}]}]}]}]}`
)

type channelReply struct {
	notify.Channel
	SigningSecret string `json:"signing_secret"`
}

// receivers are one fake per channel kind, each subscribed to events.
type receivers struct {
	smtp    *notifytest.SMTPServer
	slack   *notifytest.Receiver
	webhook *notifytest.Receiver
}

func addChannels(t *testing.T, c, admin client, events []notify.Kind) receivers {
	t.Helper()
	var r receivers
	r.smtp = notifytest.NewSMTP(t, notifytest.Plain)
	r.smtp.Users["alerts"] = "smtp-pass-never-returned"
	slack, slackSrv := notifytest.NewReceiver(t)
	hook, hookSrv := notifytest.NewReceiver(t)
	r.slack, r.webhook = slack, hook
	for _, in := range []map[string]any{
		{"name": "Ops email", "kind": "smtp", "events": events, "secrets": map[string]string{"password": "smtp-pass-never-returned"},
			"settings": map[string]any{"host": "127.0.0.1", "port": r.smtp.Port(), "tls": "none", "username": "alerts",
				"from": "Byname <alerts@northwind.example>", "to": []string{"ops@northwind.example"}}},
		{"name": "Resend", "kind": "email_api", "events": events, "secrets": map[string]string{"api_key": resendKey},
			"settings": map[string]any{"from": "alerts@northwind.example", "to": []string{"marketing@northwind.example"}}},
		{"name": "Slack #visibility", "kind": "slack_webhook", "events": events, "settings": map[string]any{},
			"secrets": map[string]string{"webhook_url": slackSrv.URL + "/services/T000/B000/slack-url-never-returned"}},
		{"name": "Pipeline hook", "kind": "webhook", "events": events, "settings": map[string]any{"url": hookSrv.URL + "/byname"}},
	} {
		var ch channelReply
		admin.call("POST", "/v1/notification-channels", in, http.StatusCreated, &ch)
		if in["kind"] == "webhook" {
			r.webhook.SetSecret(ch.SigningSecret)
		}
	}
	return r
}

func (c client) check(t *testing.T, agent client) int64 {
	t.Helper()
	var run struct{ ID int64 }
	agent.call("POST", "/v1/runs", nil, http.StatusCreated, &run)
	if err := c.svc.Checks.Work(context.Background()); err != nil {
		t.Fatal(err)
	}
	return run.ID
}

// Phase 6 is done when a test check sends one notification to each channel
// with the right gains and losses: the ones GET /v1/compare reports.
func TestACheckNotifiesEveryChannelWithTheComparison(t *testing.T) {
	ctx := context.Background()
	c := newServer(t)
	person, agent := login(t, c)
	configure(t, person, agent)

	original, err := os.ReadFile("../engines/testdata/cloro/cloro.google.serp.organic.aioverview.synthetic.json")
	if err != nil {
		t.Fatal(err)
	}
	captured, err := os.ReadFile("../engines/testdata/cloro/cloro.google.serp.organic.json")
	if err != nil {
		t.Fatal(err)
	}
	claude, err := os.ReadFile("../engines/testdata/dataforseo/dataforseo.x.ai-optimization-claude-llm-responses-live.forced-search.synthetic.json")
	if err != nil {
		t.Fatal(err)
	}
	c.treg.SetBody(claudeLiveTool, []byte(claudeNamesNone))
	c.treg.SetBody(aiOverviewTool, original)
	first := c.check(t, agent)

	r := addChannels(t, c, person, []notify.Kind{notify.CheckCompleted})
	c.treg.SetBody(claudeLiveTool, claude)
	c.treg.SetBody(aiOverviewTool, captured)
	second := c.check(t, agent)
	if _, err := c.svc.Notify.DeliverDue(ctx); err != nil {
		t.Fatal(err)
	}

	var cmp struct {
		Counts map[string]int `json:"counts"`
		Net    int            `json:"net"`
		Move   panel.Move     `json:"move"`
	}
	agent.call("GET", fmt.Sprintf("/v1/compare?before=%d&after=%d", first, second), nil, http.StatusOK, &cmp)
	gained, lost := cmp.Counts["gained"], cmp.Counts["lost"]
	if gained == 0 || lost == 0 {
		t.Fatalf("compare = %+v, want the check to gain and lose something", cmp)
	}
	apps := "AI apps"
	if gained == 1 {
		apps = "AI app"
	}
	started, stopped := fmt.Sprintf("%d %s started naming you on a question", gained, apps), fmt.Sprintf("%d stopped naming you on one", lost)
	title := fmt.Sprintf("Check #%d: no real change", second)
	if m := cmp.Move; m.Real {
		more := "more"
		if *m.AfterRate < *m.BeforeRate {
			more = "less"
		}
		title = fmt.Sprintf("Check #%d: named you %s often, %d%% → %d%%", second, more, int(math.Round(100**m.BeforeRate)), int(math.Round(100**m.AfterRate)))
	}
	since := fmt.Sprintf("Since check #%d, %s and %s.", first, started, stopped)
	if moved := cmp.Counts["moved"]; moved > 0 {
		since = fmt.Sprintf("Since check #%d, %s, %s and %d moved your position on one.", first, started, stopped, moved)
	}

	mail := r.smtp.Mail()
	if len(mail) != 1 || mail[0].Subject != title || !strings.Contains(mail[0].Body, since) ||
		!strings.Contains(mail[0].Body, fmt.Sprintf("%s/e/northwind/ai/what-changed?before=%d&after=%d", testOrigin, first, second)) {
		t.Fatalf("SMTP got %+v, want one message titled %q", mail, title)
	}
	emails := c.resend.Emails()
	if len(emails) != 1 || emails[0].Subject != title || !strings.Contains(emails[0].Text, since) {
		t.Fatalf("Resend got %+v, want one email titled %q", emails, title)
	}
	slack := r.slack.Requests()
	if len(slack) != 1 || !bytes.Contains(slack[0].Body, []byte(title)) || !bytes.Contains(slack[0].Body, []byte(since)) {
		t.Fatalf("Slack got %d requests: %s", len(slack), firstBody(slack))
	}
	hooks := r.webhook.Requests()
	if len(hooks) != 1 || hooks[0].SignatureErr != nil {
		t.Fatalf("webhook got %d requests: %+v", len(hooks), hooks)
	}
	var body struct {
		Kind    notify.Kind                  `json:"kind"`
		Payload notify.CheckCompletedPayload `json:"payload"`
	}
	if err := json.Unmarshal(hooks[0].Body, &body); err != nil {
		t.Fatal(err)
	}
	p := body.Payload
	if body.Kind != notify.CheckCompleted || p.RunID != second || p.PreviousRunID != first || p.Gained != gained || p.Lost != lost ||
		p.Moved != cmp.Counts["moved"] || p.Unchanged != cmp.Counts["unchanged"] || p.Net != cmp.Net || len(p.Top) == 0 ||
		p.Move == nil || p.Move.Cells != cmp.Move.Cells || p.Move.PValue != cmp.Move.PValue || p.Move.Real != cmp.Move.Real || p.Band == nil {
		t.Fatalf("webhook payload = %+v, want GET /v1/compare's %+v", p, cmp)
	}

	var log []notify.Delivery
	person.call("GET", "/v1/notification-deliveries", nil, http.StatusOK, &log)
	if len(log) != 4 {
		t.Fatalf("delivery log has %d rows, want 4", len(log))
	}
	for _, d := range log {
		if d.Status != "sent" || d.Attempts != 1 || d.Title != title {
			t.Errorf("delivery %+v, want sent once", d)
		}
	}

	// A resumed or retried run reports the same alert again, which changes nothing.
	if err := c.svc.Notify.RunFinished(ctx, second); err != nil {
		t.Fatal(err)
	}
	if n, _ := c.svc.Notify.DeliverDue(ctx); n != 0 || len(r.smtp.Mail()) != 1 {
		t.Fatalf("reporting the run again sent %d more", n)
	}

	var alerts notify.AlertList
	agent.call("GET", "/v1/alerts", nil, http.StatusOK, &alerts)
	if alerts.Unread != 2 || len(alerts.Alerts) != 2 || alerts.Alerts[0].Title != title ||
		alerts.Alerts[0].Path != fmt.Sprintf("/ai/what-changed?before=%d&after=%d", first, second) || alerts.Alerts[1].Kind != notify.CheckCompleted {
		t.Fatalf("alerts = %+v, want both checks, newest first, unread", alerts)
	}
	var read notify.ReadResult
	person.call("POST", "/v1/alerts/read", map[string]any{"ids": []int64{alerts.Alerts[0].ID}}, http.StatusOK, &read)
	if read.Unread != 1 {
		t.Fatalf("after reading one, unread = %d", read.Unread)
	}
	person.call("POST", "/v1/alerts/read", map[string]any{"all": true}, http.StatusOK, &read)
	agent.call("GET", "/v1/alerts?unread=true", nil, http.StatusOK, &alerts)
	if read.Unread != 0 || alerts.Unread != 0 || len(alerts.Alerts) != 0 {
		t.Fatalf("after reading all: %+v, %+v", read, alerts)
	}
}

func firstBody(reqs []notifytest.Request) string {
	if len(reqs) == 0 {
		return ""
	}
	return string(reqs[0].Body)
}

// No response carries a channel's secrets: not the list, not the edit, not
// a test, not the delivery log. A webhook's signing secret is shown once.
func TestChannelSecretsAreNeverReturned(t *testing.T) {
	ctx := context.Background()
	c := newServer(t)
	person, agent := login(t, c)
	configure(t, person, agent)
	r := addChannels(t, c, person, []notify.Kind{notify.CheckCompleted, notify.CheckFailed})

	var list notify.ChannelList
	listed := person.call("GET", "/v1/notification-channels", nil, http.StatusOK, &list)
	if len(list.Channels) != 4 || len(list.Events) != len(notify.Kinds()) {
		t.Fatalf("list = %+v", list)
	}
	bodies := [][]byte{listed.body}
	for _, ch := range list.Channels {
		bodies = append(bodies, person.call("POST", fmt.Sprintf("/v1/notification-channels/%d/test", ch.ID), nil, http.StatusOK, nil).body)
		bodies = append(bodies, person.call("PUT", fmt.Sprintf("/v1/notification-channels/%d", ch.ID),
			map[string]any{"name": ch.Name + " (edited)", "settings": ch.Settings, "events": ch.Events}, http.StatusOK, nil).body)
	}
	listed = person.call("GET", "/v1/notification-channels", nil, http.StatusOK, &list)
	for _, ch := range list.Channels {
		if ch.LastTestStatus != "ok" || ch.LastTestAt == nil || !strings.HasSuffix(ch.Name, "(edited)") {
			t.Errorf("%s: test %q at %v, want ok after an edit that kept its secrets", ch.Name, ch.LastTestStatus, ch.LastTestAt)
		}
	}
	c.check(t, agent)
	if _, err := c.svc.Notify.DeliverDue(ctx); err != nil {
		t.Fatal(err)
	}
	bodies = append(bodies, listed.body, person.call("GET", "/v1/notification-deliveries", nil, http.StatusOK, nil).body)
	secret := r.webhook.Requests()[0].Header.Get(notify.SignatureHeader)
	if secret == "" {
		t.Fatal("the webhook got no signature")
	}
	for _, b := range bodies {
		for _, s := range []string{"smtp-pass-never-returned", resendKey, "slack-url-never-returned", "whsec_", "ciphertext", "enc:v1:"} {
			if bytes.Contains(b, []byte(s)) {
				t.Fatalf("a response carries %q: %s", s, b)
			}
		}
	}
}

// Channels are for a signed-in admin. A viewer, an editor and an API key
// with every scope are refused, while all of them read alerts.
func TestOnlyAnAdminSessionManagesChannels(t *testing.T) {
	c := newServer(t)
	admin, agent := login(t, c)
	var ch channelReply
	admin.call("POST", "/v1/notification-channels", map[string]any{"name": "Hook", "kind": "webhook", "events": []string{"check_failed"},
		"settings": map[string]string{"url": "https://hooks.northwind.example/byname"}}, http.StatusCreated, &ch)
	join := func(email string, role core.Role) client {
		var inv struct{ Token string }
		admin.call("POST", "/v1/invites", map[string]string{"email": email, "role": string(role)}, http.StatusCreated, &inv)
		return c.signup(map[string]string{"invite_token": inv.Token, "name": email})
	}
	viewer, editor := join("vi@example.com", core.Viewer), join("ed@example.com", core.Editor)
	viewerKey := agent.withKey(t, admin, core.ResultsRead)
	for _, who := range []client{viewer, editor, agent, viewerKey} {
		for _, tc := range []struct {
			method, path string
			body         any
		}{
			{"GET", "/v1/notification-channels", nil},
			{"POST", "/v1/notification-channels", map[string]any{"name": "x", "kind": "webhook", "events": []string{}, "settings": map[string]string{"url": "https://x.example"}}},
			{"PUT", fmt.Sprintf("/v1/notification-channels/%d", ch.ID), map[string]any{"name": "x", "settings": ch.Settings, "events": []string{}}},
			{"DELETE", fmt.Sprintf("/v1/notification-channels/%d", ch.ID), nil},
			{"POST", fmt.Sprintf("/v1/notification-channels/%d/test", ch.ID), nil},
			{"GET", "/v1/notification-deliveries", nil},
		} {
			if r := who.do(tc.method, tc.path, tc.body); r.code != http.StatusForbidden {
				t.Errorf("%s %s = %d %s, want 403", tc.method, tc.path, r.code, r.body)
			}
		}
		who.call("GET", "/v1/alerts", nil, http.StatusOK, nil)
	}
	admin.call("DELETE", fmt.Sprintf("/v1/notification-channels/%d", ch.ID), nil, http.StatusNoContent, nil)
}
