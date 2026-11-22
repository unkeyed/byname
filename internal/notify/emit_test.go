package notify_test

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/unkeyed/byname/internal/checks"
	"github.com/unkeyed/byname/internal/checks/checkstest"
	"github.com/unkeyed/byname/internal/config"
	"github.com/unkeyed/byname/internal/core"
	"github.com/unkeyed/byname/internal/engines/tregtest"
	"github.com/unkeyed/byname/internal/notify"
	"github.com/unkeyed/byname/internal/results"
	"github.com/unkeyed/byname/internal/secretbox"
	"github.com/unkeyed/byname/internal/testdb"
)

var reader = core.Actor{Via: core.ViaAPIKey, UserID: 0, Role: core.Viewer, Scopes: []core.Scope{core.ResultsRead}}

// A finished run raises the event its status names, and a model leaving its
// list raises pair_paused, each once however often they are reported.
func TestRunsAndPausesRaiseTheirEvents(t *testing.T) {
	ctx := context.Background()
	db := testdb.New(t)
	treg := tregtest.New(t)
	alerts := notify.New(db, results.New(db), notify.Options{Secrets: secretbox.ForTest(), PublicURL: &url.URL{Scheme: "https", Host: "byname.example"}})
	cfg := config.New(db, config.Options{Secrets: secretbox.ForTest(), TregBaseURL: treg.URL, Alerts: alerts})
	checkstest.Seed(t, cfg, treg, func(s *config.Settings) { s.SpendCapRunUSD = 0.025 })
	runs := checks.New(db, cfg, checks.Options{Alerts: alerts})
	check := func() {
		t.Helper()
		if _, _, err := runs.RunNow(ctx); err != nil {
			t.Fatal(err)
		}
		if err := runs.Work(ctx); err != nil {
			t.Fatal(err)
		}
	}

	check()
	treg.Before(func(tregtest.Call) *tregtest.Refusal {
		return &tregtest.Refusal{Status: 402, Code: "insufficient_balance"}
	})
	check()
	treg.Before(nil)
	treg.SetBody("dataforseo.x.ai-optimization-claude-llm-responses-models",
		[]byte(`{"status_code":20000,"tasks":[{"status_code":20000,"result":[{"model_name":"claude-sonnet-6","web_search_supported":true}]}]}`))
	for range 2 {
		if err := cfg.RefreshModels(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if err := alerts.RunFinished(ctx, 1); err != nil {
		t.Fatal(err)
	}

	list, err := alerts.Alerts(ctx, reader, notify.AlertQuery{})
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, a := range list.Alerts {
		kinds = append(kinds, string(a.Kind))
	}
	if strings.Join(kinds, " ") != "pair_paused check_refused spend_cap_reached" {
		t.Fatalf("alerts newest first = %v, want the pause, the refused check and the capped one, once each", kinds)
	}
	if a := list.Alerts[1]; a.Title != "Check #2 stopped: Treg refused a request" || !strings.Contains(strings.Join(a.Lines, " "), "insufficient_balance") {
		t.Fatalf("refused alert = %+v", a)
	}
	if a := list.Alerts[0]; a.Title != "Paused claude-sonnet-5-5 on Claude Code" {
		t.Fatalf("pause alert = %+v", a)
	}
	if list.Unread != 3 {
		t.Fatalf("unread = %d, want 3 for a credential that acts for nobody", list.Unread)
	}

	if err := alerts.Emit(ctx, notify.Event{Subject: "github/northwind/site", Payload: notify.TokenExpiringPayload{Provider: "GitHub", Repo: "northwind/site"}}); err != nil {
		t.Fatal(err)
	}
	admin := core.Actor{Via: core.ViaSession, Role: core.Admin, Scopes: core.Scopes}
	if l, _ := alerts.Alerts(ctx, reader, notify.AlertQuery{}); len(l.Alerts) != 3 {
		t.Fatalf("a reader sees %d alerts, want the admin-only token alert hidden", len(l.Alerts))
	}
	if l, _ := alerts.Alerts(ctx, admin, notify.AlertQuery{}); len(l.Alerts) != 4 || l.Alerts[0].Kind != notify.TokenExpiring {
		t.Fatalf("an admin sees %+v, want the token alert too", l.Alerts)
	}
}

// Alerts read as plain sentences in the app's words and link to the page
// that shows the event.
func TestAlertsReadAsSentences(t *testing.T) {
	ctx := context.Background()
	alerts := notify.New(testdb.New(t), nil, notify.Options{Secrets: secretbox.ForTest()})
	digest := notify.SocialDigestPayload{Cadence: "weekly", From: "2026-11-16", To: "2026-11-22", Mentions: 12, Hidden: 2,
		Networks: []notify.SocialNetworkDigest{
			{Network: "x", Title: "X", Mentions: 5, Top: []notify.SocialPost{{Author: "@dana", Text: "Northwind sorted our invoices", URL: "https://x.example/1", Engagement: 40}}},
			{Network: "reddit", Title: "Reddit", Mentions: 4},
			{Network: "hackernews", Title: "Hacker News", Mentions: 3},
			{Network: "linkedin", Title: "LinkedIn"},
		},
		NewAuthors: []string{"@dana", "u/ledgerfan"}, NewAuthorsCount: 3, SpendUSD: 0.0406}
	for _, e := range []notify.Event{
		{Subject: "week/2026-11-16", Payload: digest},
		{Subject: "change/7/final", Payload: notify.ChangeVerdictPayload{ChangeID: 7, URL: "https://northwind.example/pricing", Verdict: "improved"}},
		{Subject: "search/non_brand_clicks", Payload: notify.SearchMovedPayload{Metric: "non_brand_clicks", WindowDays: 28, Before: 120, After: 80}},
	} {
		if err := alerts.Emit(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	list, err := alerts.Alerts(ctx, reader, notify.AlertQuery{})
	if err != nil || len(list.Alerts) != 3 {
		t.Fatalf("alerts = %+v, %v", list, err)
	}
	moved, change, social := list.Alerts[0], list.Alerts[1], list.Alerts[2]

	if social.Title != "Social digest: 12 new posts named you in the week of Nov 16" || social.Path != "/social" {
		t.Errorf("digest title %q, path %q", social.Title, social.Path)
	}
	want := []string{
		"5 were on X, 4 on Reddit and 3 on Hacker News.",
		"3 people who had not mentioned you before posted about you: @dana, u/ledgerfan and 1 other.",
		"2 more posts came from accounts you left out, so they are not counted.",
		"The posts people engaged with most:",
		"@dana on X, with 40 likes, replies and reposts: “Northwind sorted our invoices” https://x.example/1",
		"Searching for posts cost $0.04.",
	}
	if strings.Join(social.Lines, "\n") != strings.Join(want, "\n") {
		t.Errorf("digest lines:\n%s\nwant:\n%s", strings.Join(social.Lines, "\n"), strings.Join(want, "\n"))
	}

	if change.Title != "Change result: helped" || change.Path != "/google/site-changes?chg=7" {
		t.Errorf("change alert %q at %q", change.Title, change.Path)
	}
	if moved.Title != "Clicks from searches without your name fell over the last 28 days" || moved.Path != "/google/traffic" {
		t.Errorf("search alert %q at %q", moved.Title, moved.Path)
	}
}
