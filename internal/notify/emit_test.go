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

// Notifications read as plain sentences in the app's words and link to the
// page that shows the event. The digest says counts, at most three new
// people by name and the five posts people engaged with most, and never the
// cost.
func TestNotificationsReadAsSentences(t *testing.T) {
	ctx := context.Background()
	alerts := notify.New(testdb.New(t), nil, notify.Options{Secrets: secretbox.ForTest()})
	digest := notify.SocialDigestPayload{Cadence: "weekly", From: "2026-11-09", To: "2026-11-15", Mentions: 12, Hidden: 10,
		Networks: []notify.SocialNetworkDigest{
			{Network: "x", Title: "X", Mentions: 5, Top: []notify.SocialPost{
				{Author: "@dana", Text: "Northwind sorted our invoices", URL: "https://x.example/1", Engagement: 40},
				{Author: "@lee", Text: "Trying Northwind", URL: "https://x.example/2", Engagement: 3},
				{Author: "@kim", Text: "Northwind or Ledgerly?", URL: "https://x.example/3", Engagement: 1},
			}},
			{Network: "reddit", Title: "Reddit", Mentions: 4, Top: []notify.SocialPost{
				{Author: "u/ledgerfan", Text: "Northwind is the one", URL: "https://reddit.example/1", Engagement: 120},
				{Author: "u/sam", Text: "We moved to Northwind", URL: "https://reddit.example/2", Engagement: 2},
			}},
			{Network: "hackernews", Title: "Hacker News", Mentions: 3, Top: []notify.SocialPost{
				{Author: "pg", Text: "Show HN: an invoice tool", URL: "https://news.example/1", Engagement: 0},
			}},
			{Network: "linkedin", Title: "LinkedIn"},
		},
		NewAuthors: []string{"@dana", "u/ledgerfan", "@lee", "@kim"}, NewAuthorsCount: 9, SpendUSD: 0.0138}
	for _, e := range []notify.Event{
		{Subject: "week/2026-11-09", Payload: digest},
		{Subject: "change/7/final", Payload: notify.ChangeVerdictPayload{ChangeID: 7, URL: "https://northwind.example/pricing", Verdict: "improved"}},
		{Subject: "search/non_brand_clicks", Payload: notify.SearchMovedPayload{Metric: "non_brand_clicks", WindowDays: 28, Before: 120, After: 80}},
	} {
		if err := alerts.Emit(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	list, err := alerts.Alerts(ctx, reader, notify.AlertQuery{})
	if err != nil || len(list.Alerts) != 3 {
		t.Fatalf("notifications = %+v, %v", list, err)
	}
	moved, change, social := list.Alerts[0], list.Alerts[1], list.Alerts[2]

	if social.Title != "12 new posts named you, Nov 9 to 15" || social.Path != "/social?digest=2026-11-09" {
		t.Errorf("digest title %q, path %q", social.Title, social.Path)
	}
	want := []string{
		"5 were on X, 4 on Reddit and 3 on Hacker News.",
		"9 people posted about you for the first time, among them @dana, u/ledgerfan and @lee.",
		"10 posts from accounts you left out are not counted.",
		"The posts people engaged with most:",
		"u/ledgerfan on Reddit, 120 upvotes and comments: “Northwind is the one” https://reddit.example/1",
		"@dana on X, 40 likes, replies and reposts: “Northwind sorted our invoices” https://x.example/1",
		"@lee on X, 3 likes, replies and reposts: “Trying Northwind” https://x.example/2",
		"u/sam on Reddit, 2 upvotes and comments: “We moved to Northwind” https://reddit.example/2",
		"@kim on X, 1 like, reply or repost: “Northwind or Ledgerly?” https://x.example/3",
	}
	if strings.Join(social.Lines, "\n") != strings.Join(want, "\n") {
		t.Errorf("digest lines:\n%s\nwant:\n%s", strings.Join(social.Lines, "\n"), strings.Join(want, "\n"))
	}

	if change.Title != "Change result: helped" || change.Path != "/google/site-changes?chg=7" {
		t.Errorf("change notification %q at %q", change.Title, change.Path)
	}
	if moved.Title != "Clicks from searches without your name fell over the last 28 days" || moved.Path != "/google/traffic" {
		t.Errorf("search notification %q at %q", moved.Title, moved.Path)
	}
}

// A check whose answers are the first to link to a marked page raises
// page_linked once, however many later checks link to it again.
func TestFirstLinkToAMarkedPageRaisesPageLinkedOnce(t *testing.T) {
	ctx := context.Background()
	db := testdb.New(t)
	treg := tregtest.New(t)
	alerts := notify.New(db, results.New(db), notify.Options{Secrets: secretbox.ForTest(), PublicURL: &url.URL{Scheme: "https", Host: "byname.example"}})
	cfg := config.New(db, config.Options{Secrets: secretbox.ForTest(), TregBaseURL: treg.URL, Alerts: alerts})
	checkstest.Seed(t, cfg, treg, nil)
	treg.SetBody("dataforseo.x.ai-optimization-claude-llm-responses-live", []byte(`{"status_code":20000,"tasks":[{"status_code":20000,"result":[{"model_name":"claude-sonnet-4-5-20250929",
		"items":[{"type":"message","sections":[{"type":"text","text":"Northwind Cloud does this well.",
		"annotations":[{"title":"Zero downtime deploys","url":"https://www.northwind.example/blog/zero-downtime?utm_source=claude"}]}]}]}]}]}`))
	runs := checks.New(db, cfg, checks.Options{Alerts: alerts})
	check := func() int64 {
		t.Helper()
		run, _, err := runs.RunNow(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := runs.Work(ctx); err != nil {
			t.Fatal(err)
		}
		return run.ID
	}
	linked := func() []notify.Alert {
		t.Helper()
		list, err := alerts.Alerts(ctx, reader, notify.AlertQuery{})
		if err != nil {
			t.Fatal(err)
		}
		var out []notify.Alert
		for _, a := range list.Alerts {
			if a.Kind == notify.PageLinked {
				out = append(out, a)
			}
		}
		return out
	}

	check()
	set, err := cfg.CurrentQuestions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.MarkHandled(ctx, core.Actor{}, set.Questions[0].ID, config.QuestionPageInput{URL: "https://northwind.example/blog/zero-downtime"}); err != nil {
		t.Fatal(err)
	}
	if got := linked(); len(got) != 0 {
		t.Fatalf("page_linked before a check since the mark = %+v", got)
	}
	second := check()
	check()
	if err := alerts.RunFinished(ctx, second); err != nil {
		t.Fatal(err)
	}
	got := linked()
	if len(got) != 1 {
		t.Fatalf("page_linked alerts = %+v, want one", got)
	}
	a := got[0]
	want := "2 answers linked to it, the first since you marked it as answering “" + set.Questions[0].Text + "”."
	if a.Title != "Claude Code linked to northwind.example/blog/zero-downtime" || len(a.Lines) != 1 || a.Lines[0] != want || a.Path != "/ai/missed?tab=handled" {
		t.Errorf("page_linked = %q %q at %q", a.Title, a.Lines, a.Path)
	}
}
