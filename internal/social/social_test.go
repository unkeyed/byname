package social_test

import (
	"context"
	"github.com/unkeyed/byname/internal/social/socialtest"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/unkeyed/byname/internal/checks/checkstest"
	"github.com/unkeyed/byname/internal/config"
	"github.com/unkeyed/byname/internal/engines/tregtest"
	"github.com/unkeyed/byname/internal/notify"
	"github.com/unkeyed/byname/internal/results"
	"github.com/unkeyed/byname/internal/secretbox"
	"github.com/unkeyed/byname/internal/social"
	"github.com/unkeyed/byname/internal/testdb"
)

// tregFixtures is the body the fake Treg answers each social tool with.
var tregFixtures = map[string]string{
	"anyapi.x.search.posts":              "anyapi.x.search.posts.json",
	"crawl4ai.reddit.search.posts":       "crawl4ai.reddit.search.posts.json",
	"crawl4ai.hackernews.search.stories": "crawl4ai.hackernews.search.stories.json",
	"anyapi.linkedin.search.posts":       "anyapi.linkedin.search.posts.json",
	"crawl4ai.youtube.search.videos":     "crawl4ai.youtube.search.videos.json",
	"tikhub.x.threads-web-search-recent": "tikhub.x.threads-web-search-recent.synthetic.json",
}

// fakeBluesky answers app.bsky.feed.searchPosts with the recorded fixture,
// or with status when it is set.
type fakeBluesky struct {
	*httptest.Server
	mu     sync.Mutex
	status int
	calls  []url.Values
}

func newFakeBluesky(t *testing.T) *fakeBluesky {
	body := socialtest.Naming(t, fixture(t, "bluesky.searchPosts.json"))
	f := &fakeBluesky{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.calls = append(f.calls, r.URL.Query())
		status := f.status
		f.mu.Unlock()
		if r.URL.Path != "/xrpc/app.bsky.feed.searchPosts" {
			http.NotFound(w, r)
			return
		}
		if status != 0 {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":"AuthMissing","message":"Authentication Required"}`))
			return
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeBluesky) set(status int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status = status
}

func (f *fakeBluesky) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

type env struct {
	t    *testing.T
	ctx  context.Context
	db   *pgxpool.Pool
	treg *tregtest.Server
	bsky *fakeBluesky
	cfg  *config.Service
	svc  *social.Service
	now  time.Time
}

// newEnv is an onboarded instance whose brand is Northwind, with social
// turned on, searching for one term, with a Treg token unless token is
// false, against a fake Treg and a fake Bluesky. The clock starts on
// Wednesday 2026-11-18 at noon UTC.
func newEnv(t *testing.T, token bool) *env {
	t.Helper()
	e := &env{t: t, ctx: context.Background(), db: testdb.New(t), treg: tregtest.New(t), bsky: newFakeBluesky(t), now: time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)}
	for tool, file := range tregFixtures {
		e.treg.SetBody(tool, socialtest.Naming(t, fixture(t, file)))
	}
	e.cfg = config.New(e.db, config.Options{Secrets: secretbox.ForTest(), TregBaseURL: e.treg.URL})
	if _, err := e.cfg.PutSettings(e.ctx, checkstest.Settings()); err != nil {
		t.Fatal(err)
	}
	if token {
		if _, err := e.cfg.SetCredential(e.ctx, "treg", "treg_test_token", ""); err != nil {
			t.Fatal(err)
		}
	}
	alerts := notify.New(e.db, results.New(e.db), notify.Options{Secrets: secretbox.ForTest()})
	e.svc = social.New(e.db, e.cfg, social.Options{Alerts: alerts, BaseURLs: map[social.Network]string{social.Bluesky: e.bsky.URL}, Now: func() time.Time { return e.now }})
	if _, err := e.db.Exec(e.ctx, `UPDATE instance_settings SET onboarded_at = $1`, e.now); err != nil {
		t.Fatal(err)
	}
	e.put(social.SettingsInput{Enabled: new(true), Terms: []string{"Northwind Cloud"}})
	return e
}

func (e *env) put(in social.SettingsInput) social.Settings {
	e.t.Helper()
	if in.FetchEveryHours == 0 {
		in.FetchEveryHours = 6
	}
	if in.DigestCadence == "" {
		in.DigestCadence = social.Weekly
		in.DigestWeekday = 1
	}
	st, err := e.svc.PutSettings(e.ctx, in)
	if err != nil {
		e.t.Fatal(err)
	}
	return st
}

func (e *env) fetch() {
	e.t.Helper()
	if err := e.svc.Fetch(e.ctx); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) mentions(q social.MentionQuery) social.MentionList {
	e.t.Helper()
	l, err := e.svc.Mentions(e.ctx, q)
	if err != nil {
		e.t.Fatal(err)
	}
	return l
}

func (e *env) count(sql string, args ...any) int {
	e.t.Helper()
	var n int
	if err := e.db.QueryRow(e.ctx, sql, args...).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

func (e *env) toolCalls(tool string) int {
	n := 0
	for _, c := range e.treg.Calls() {
		if c.Tool == tool {
			n++
		}
	}
	return n
}

func (e *env) lastJob() (status, msg string) {
	e.t.Helper()
	if err := e.db.QueryRow(e.ctx, `SELECT status, coalesce(error, '') FROM job_run WHERE job = 'social_fetch' ORDER BY id DESC LIMIT 1`).Scan(&status, &msg); err != nil {
		e.t.Fatal(err)
	}
	return status, msg
}

// Social searches cost money, so none runs until someone turns social on,
// and none before onboarding is finished even then. Saving the Treg token
// starts nothing.
func TestSocialWaitsUntilTurnedOnAndOnboarded(t *testing.T) {
	e := newEnv(t, true)
	e.put(social.SettingsInput{Enabled: new(false), Terms: []string{"Northwind Cloud"}})
	tick := func() {
		t.Helper()
		if err := e.svc.Tick(e.ctx); err != nil {
			t.Fatal(err)
		}
		e.now = e.now.Add(24 * time.Hour)
	}
	tick()
	if err := e.svc.Fetch(e.ctx); err != nil {
		t.Fatal(err)
	}
	if n := len(e.treg.Calls()) + e.bsky.count() + e.count(`SELECT count(*) FROM social_call`); n != 0 {
		t.Fatalf("%d calls with social off, want none", n)
	}
	if st := e.put(social.SettingsInput{Terms: []string{"Northwind Cloud"}}); st.Enabled || st.NextFetchAt != nil {
		t.Fatalf("saving without enabled turned social on (%v) or scheduled a fetch (%v); leaving it out keeps it off", st.Enabled, st.NextFetchAt)
	}

	if _, err := e.db.Exec(e.ctx, `UPDATE instance_settings SET onboarded_at = NULL`); err != nil {
		t.Fatal(err)
	}
	if st := e.put(social.SettingsInput{Enabled: new(true), Terms: []string{"Northwind Cloud"}}); !st.Enabled || st.NextFetchAt != nil {
		t.Fatalf("on before onboarding: enabled %v next fetch %v, want on with nothing scheduled", st.Enabled, st.NextFetchAt)
	}
	tick()
	if n := len(e.treg.Calls()) + e.bsky.count(); n != 0 {
		t.Fatalf("%d calls before onboarding was finished, want none", n)
	}

	if _, err := e.db.Exec(e.ctx, `UPDATE instance_settings SET onboarded_at = $1`, e.now); err != nil {
		t.Fatal(err)
	}
	tick()
	if e.toolCalls("anyapi.x.search.posts") == 0 || e.bsky.count() == 0 {
		t.Fatal("social on and onboarding finished, but the tick searched nothing")
	}
}

// A first fetch pages each network back as far as its page cap allows and
// stores every post once; fetching again stores no post twice, pages only
// until it reaches what it has, refreshes engagement and keeps when each
// post was first seen.
func TestFetchStoresEachPostOnce(t *testing.T) {
	e := newEnv(t, true)
	e.fetch()
	if n := e.count(`SELECT count(*) FROM social_mention`); n != 14 {
		t.Fatalf("%d mentions after the first fetch, want 2 on each of 7 networks", n)
	}
	for tool, want := range map[string]int{"anyapi.x.search.posts": 3, "tikhub.x.threads-web-search-recent": 2, "crawl4ai.reddit.search.posts": 1, "crawl4ai.youtube.search.videos": 1} {
		if got := e.toolCalls(tool); got != want {
			t.Errorf("%s: %d calls on the first fetch, want %d (its page cap, or one for a network without a cursor)", tool, got, want)
		}
	}
	if got := e.bsky.count(); got != 3 {
		t.Errorf("bluesky: %d calls on the first fetch, want its page cap of 3", got)
	}
	var firstSeen time.Time
	if err := e.db.QueryRow(e.ctx, `SELECT first_seen_at FROM social_mention WHERE post_id = '2101246259504021713'`).Scan(&firstSeen); err != nil {
		t.Fatal(err)
	}

	e.treg.Reset()
	e.now = e.now.Add(6 * time.Hour)
	x := strings.Replace(string(fixture(t, "anyapi.x.search.posts.json")), `"likeCount": 0,`, `"likeCount": 31,`, 1)
	x = string(socialtest.Naming(t, []byte(x)))
	e.treg.SetBody("anyapi.x.search.posts", []byte(x))
	e.fetch()
	if n := e.count(`SELECT count(*) FROM social_mention`); n != 14 {
		t.Fatalf("%d mentions after fetching the same posts again, want 14", n)
	}
	if got := e.toolCalls("anyapi.x.search.posts"); got != 1 {
		t.Errorf("X: %d calls on the second fetch, want 1: its first page reaches the newest post stored", got)
	}
	var likes int
	var seen time.Time
	if err := e.db.QueryRow(e.ctx, `SELECT likes, first_seen_at FROM social_mention WHERE post_id = '2101246259504021713'`).Scan(&likes, &seen); err != nil {
		t.Fatal(err)
	}
	if likes != 31 || !seen.Equal(firstSeen) {
		t.Fatalf("after a refetch likes = %d and first seen %v, want 31 and still %v", likes, seen, firstSeen)
	}
	l := e.mentions(social.MentionQuery{Network: social.YouTube})
	if len(l.Mentions) != 2 || !l.Mentions[0].PostedApprox || l.Mentions[0].Engagement.Total != *l.Mentions[0].Engagement.Views {
		t.Fatalf("youtube mentions = %+v, want approximate times ranked by views", l.Mentions)
	}
}

// Leaving an account out hides its mentions, past ones included, at read
// time; taking it off the list brings them back. Nothing is deleted.
func TestLeftOutAccountsAreHiddenAtReadTime(t *testing.T) {
	e := newEnv(t, true)
	e.fetch()
	if l := e.mentions(social.MentionQuery{}); l.Total != 14 || l.Hidden != 0 || len(l.Mentions) != 14 {
		t.Fatalf("before leaving anyone out: total %d hidden %d, want 14 and 0", l.Total, l.Hidden)
	}
	hardik, err := e.svc.LeaveOut(e.ctx, social.LeftOutInput{Network: social.X, Handle: "@_Kidrah_24", Label: "Hardik"})
	if err != nil || hardik.Handle != "_kidrah_24" || hardik.Mentions != 1 {
		t.Fatalf("leave out @_Kidrah_24 = %+v, %v", hardik, err)
	}
	if _, err := e.svc.LeaveOut(e.ctx, social.LeftOutInput{Network: social.Bluesky, Handle: "https://bsky.app/profile/ada.bsky.social", Label: "Ada (team)"}); err != nil {
		t.Fatal(err)
	}
	l := e.mentions(social.MentionQuery{})
	if l.Total != 12 || l.Hidden != 2 || len(l.Mentions) != 12 {
		t.Fatalf("with two accounts left out: total %d hidden %d listed %d, want 12, 2, 12", l.Total, l.Hidden, len(l.Mentions))
	}
	for _, m := range l.Mentions {
		if m.AuthorHandle == "_kidrah_24" || m.AuthorHandle == "ada.bsky.social" {
			t.Fatalf("left-out author %s is listed", m.AuthorHandle)
		}
	}
	all := e.mentions(social.MentionQuery{IncludeHidden: true, Network: social.X})
	i := slices.IndexFunc(all.Mentions, func(m social.Mention) bool { return m.AuthorHandle == "_kidrah_24" })
	if len(all.Mentions) != 2 || i < 0 || !all.Mentions[i].Hidden || all.Mentions[i].LeftOutAs != "Hardik" {
		t.Fatalf("include_hidden on X = %+v, want both posts with Hardik's marked hidden", all.Mentions)
	}
	if err := e.svc.RemoveLeftOut(e.ctx, hardik.ID); err != nil {
		t.Fatal(err)
	}
	if l := e.mentions(social.MentionQuery{}); l.Total != 13 || l.Hidden != 1 {
		t.Fatalf("after taking Hardik off the list: total %d hidden %d, want 13 and 1", l.Total, l.Hidden)
	}
	if n := e.count(`SELECT count(*) FROM social_mention`); n != 14 {
		t.Fatalf("%d mentions stored, want all 14 kept", n)
	}
}

// A Treg call without a cost header is a missing cost, never $0; a
// Bluesky call is a known $0, which is not missing.
func TestMissingCostIsFlagged(t *testing.T) {
	e := newEnv(t, true)
	e.treg.SetCost("crawl4ai.reddit.search.posts", nil)
	e.fetch()
	for _, c := range []struct {
		network social.Network
		missing bool
		cost    *float64
	}{{social.Reddit, true, nil}, {social.Bluesky, false, new(0.0)}, {social.X, false, new(0.01)}} {
		l := e.mentions(social.MentionQuery{Network: c.network})
		got := l.Mentions[0].Call
		if got == nil || got.CostMissing != c.missing || (got.CostUSD == nil) != (c.cost == nil) || got.CostUSD != nil && *got.CostUSD != *c.cost {
			t.Errorf("%s call = %+v, want missing %v and cost %v", c.network, got, c.missing, c.cost)
		}
	}
	d, err := e.svc.Digest(e.ctx, social.DigestQuery{Day: "2026-11-18"})
	if err != nil {
		t.Fatal(err)
	}
	if d.SpendMissingCalls != 1 || d.Calls != 12 {
		t.Fatalf("digest calls %d, missing %d; want 12 calls and the one Reddit call missing", d.Calls, d.SpendMissingCalls)
	}
}

// Each finished period's digest is emitted once, however often the
// scheduler ticks, and even when the marker that it was sent is lost.
func TestDigestIsEmittedOncePerPeriod(t *testing.T) {
	e := newEnv(t, true)
	if _, err := e.svc.LeaveOut(e.ctx, social.LeftOutInput{Network: social.Threads, Handle: "@Northwind.Cloud", Label: "Our accounts"}); err != nil {
		t.Fatal(err)
	}
	e.fetch()
	e.now = time.Date(2026, 11, 23, 0, 30, 0, 0, time.UTC)
	for range 3 {
		if err := e.svc.Tick(e.ctx); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.db.Exec(e.ctx, `UPDATE instance_settings SET social_digest_sent_through = NULL`); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Tick(e.ctx); err != nil {
		t.Fatal(err)
	}
	if n := e.count(`SELECT count(*) FROM alert WHERE kind = 'social_digest'`); n != 1 {
		t.Fatalf("%d social_digest alerts after four ticks, want 1", n)
	}
	var subject string
	var mentions, hidden int
	if err := e.db.QueryRow(e.ctx, `SELECT subject, (payload->>'mentions')::int, (payload->>'hidden')::int FROM alert WHERE kind = 'social_digest'`).Scan(&subject, &mentions, &hidden); err != nil {
		t.Fatal(err)
	}
	if subject != "week/2026-11-16" || mentions != 13 || hidden != 1 {
		t.Fatalf("digest %s with %d mentions and %d hidden, want week/2026-11-16 with 13 and 1", subject, mentions, hidden)
	}
}

// The week after, only accounts never seen before count as new voices.
func TestDigestNamesNewAuthors(t *testing.T) {
	e := newEnv(t, true)
	e.fetch()
	e.now = time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	at := e.now.Add(-time.Hour).Unix()
	e.treg.SetBody("anyapi.x.search.posts", []byte(`{"output":{"data":{"items":[
		{"id":"9001","url":"https://x.com/i/web/status/9001","authorUsername":"Fresh_Voice","authorName":"Fresh Voice","text":"Trying Northwind Cloud today","createdUtc":`+itoa(at)+`,"likeCount":4,"replyCount":1,"retweetCount":0,"viewCount":80},
		{"id":"9002","url":"https://x.com/i/web/status/9002","authorUsername":"_kidrah_24","authorName":"Hardik","text":"Northwind Cloud again","createdUtc":`+itoa(at)+`,"likeCount":2,"replyCount":0,"retweetCount":0,"viewCount":10}
	],"nextCursor":null},"found":true}}`))
	e.fetch()
	d, err := e.svc.Digest(e.ctx, social.DigestQuery{Day: "2026-11-25"})
	if err != nil {
		t.Fatal(err)
	}
	if d.From != "2026-11-23" || d.To != "2026-11-29" || d.Mentions != 2 || len(d.Networks) != 1 || d.Networks[0].Network != social.X {
		t.Fatalf("week of Nov 23: %+v, want the two new X posts", d)
	}
	if len(d.NewAuthors) != 1 || d.NewAuthors[0].Handle != "fresh_voice" {
		t.Fatalf("new authors = %+v, want only fresh_voice", d.NewAuthors)
	}
}

// Bluesky asking for sign-in marks it unavailable for that fetch, on the
// network and on the job, without failing the fetch or retrying each term.
func TestBlueskySignInIsUnavailableNotACrash(t *testing.T) {
	e := newEnv(t, true)
	e.put(social.SettingsInput{Terms: []string{"Northwind Cloud", "northwind.example"}})
	e.bsky.set(http.StatusForbidden)
	e.fetch()
	if got := e.bsky.count(); got != 1 {
		t.Fatalf("%d Bluesky calls, want 1: one sign-in answer skips its other term", got)
	}
	if status, msg := e.lastJob(); status != "completed" || !strings.Contains(msg, "Bluesky search is unavailable") {
		t.Fatalf("job %s %q, want completed with the note that Bluesky search is unavailable", status, msg)
	}
	st, err := e.svc.Settings(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(st.Networks, func(n social.NetworkStatus) bool { return n.Network == social.Bluesky })
	if !st.Networks[i].Unavailable || st.Networks[i].Route != social.Direct {
		t.Fatalf("bluesky status = %+v, want unavailable", st.Networks[i])
	}
	if n := e.count(`SELECT count(*) FROM social_mention WHERE network <> 'bluesky'`); n != 12 {
		t.Fatalf("%d mentions from the other networks, want 12", n)
	}
}

// Without a Treg token only Bluesky, which needs none, is searched.
func TestWithoutATokenOnlyBlueskyRuns(t *testing.T) {
	e := newEnv(t, false)
	if err := e.svc.Tick(e.ctx); err != nil {
		t.Fatal(err)
	}
	if len(e.treg.Calls()) != 0 || e.bsky.count() != 3 {
		t.Fatalf("%d Treg calls and %d Bluesky calls, want 0 and 3", len(e.treg.Calls()), e.bsky.count())
	}
	st, err := e.svc.Settings(e.ctx)
	if err != nil || st.TokenSet || st.NextFetchAt == nil || len(st.Networks) != 7 {
		t.Fatalf("settings = %+v, %v", st, err)
	}
	e.put(social.SettingsInput{Terms: []string{"Northwind Cloud"}, Networks: []social.NetworkToggle{{Network: social.Bluesky, Enabled: false}}})
	e.now = e.now.Add(7 * time.Hour)
	if err := e.svc.Tick(e.ctx); err != nil {
		t.Fatal(err)
	}
	if n := e.count(`SELECT count(*) FROM job_run WHERE job = 'social_fetch'`); n != 1 {
		t.Fatalf("%d fetch jobs, want 1: with Bluesky off and no token there is nothing to search", n)
	}
}

// The monthly spend cap, shared with AI checks, stops the Treg searches;
// a Treg refusal does the same. Bluesky, which is free, still runs.
func TestCapAndRefusalStopTregSearches(t *testing.T) {
	e := newEnv(t, true)
	s := checkstest.Settings()
	s.SpendCapMonthUSD = new(0.005)
	if _, err := e.cfg.PutSettings(e.ctx, s); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Fetch(e.ctx); err == nil {
		t.Fatal("a capped fetch returned no error")
	}
	if status, _ := e.lastJob(); status != "capped" || len(e.treg.Calls()) != 1 || e.bsky.count() != 3 {
		t.Fatalf("job %s after %d Treg and %d Bluesky calls, want capped after 1 and 3", status, len(e.treg.Calls()), e.bsky.count())
	}

	r := newEnv(t, true)
	r.treg.Before(func(tregtest.Call) *tregtest.Refusal {
		return &tregtest.Refusal{Status: 402, Code: "insufficient_balance"}
	})
	_ = r.svc.Fetch(r.ctx)
	if status, msg := r.lastJob(); status != "refused" || !strings.Contains(msg, "insufficient_balance") || len(r.treg.Calls()) != 1 {
		t.Fatalf("job %s %q after %d Treg calls, want refused after 1", status, msg, len(r.treg.Calls()))
	}
	if n := r.count(`SELECT count(*) FROM social_call WHERE status = 'refused' AND cost_usd IS NULL`); n != 1 {
		t.Fatalf("%d refused calls recorded, want 1", n)
	}
}

func TestSettingsValidate(t *testing.T) {
	e := newEnv(t, true)
	st := e.put(social.SettingsInput{Terms: []string{}})
	if st.CustomTerms || !slices.Equal(st.Terms, []string{"Northwind", "northwind.example"}) {
		t.Fatalf("default terms = %v, want the brand's name and domain", st.Terms)
	}
	for _, bad := range []social.SettingsInput{
		{Terms: []string{"a", "b", "c", "d", "e", "f"}, FetchEveryHours: 6, DigestCadence: social.Weekly},
		{Terms: []string{"x"}, FetchEveryHours: 6, DigestCadence: social.Weekly},
		{FetchEveryHours: 5, DigestCadence: social.Weekly},
		{FetchEveryHours: 6, DigestCadence: "monthly"},
		{FetchEveryHours: 6, DigestCadence: social.Weekly, DigestWeekday: 7},
		{FetchEveryHours: 6, DigestCadence: social.Weekly, Networks: []social.NetworkToggle{{Network: "mastodon"}}},
	} {
		if _, err := e.svc.PutSettings(e.ctx, bad); err == nil {
			t.Errorf("PutSettings(%+v) succeeded", bad)
		}
	}
	if _, err := e.svc.LeaveOut(e.ctx, social.LeftOutInput{Network: social.X, Handle: "two words", Label: "Team"}); err == nil {
		t.Error("left out a handle with a space")
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// A network's search matches loosely, so a post that does not name the
// term as a word, in its text or its link, is never kept.
func TestPostsThatDoNotNameTheTermAreLeftOut(t *testing.T) {
	e := newEnv(t, true)
	at := itoa(e.now.Add(-time.Hour).Unix())
	post := func(id, text string) string {
		return `{"id":"` + id + `","url":"https://x.com/i/web/status/` + id + `","authorUsername":"someone","authorName":"Someone","text":"` + text +
			`","createdUtc":` + at + `,"likeCount":1,"replyCount":0,"retweetCount":0,"viewCount":1}`
	}
	e.treg.SetBody("anyapi.x.search.posts", []byte(`{"output":{"data":{"items":[`+
		post("1", "Moved our API keys to northwind cloud last week")+","+
		post("2", "Thanks @Northwind Cloud team!")+","+
		post("3", "Northwind Cloudy skies all weekend")+","+
		post("4", "The pharaoh had one rule: keep the pig out of the vault")+
		`],"nextCursor":null},"found":true}}`))
	e.fetch()
	var ids []string
	rows, err := e.db.Query(e.ctx, `SELECT post_id FROM social_mention WHERE network = 'x' ORDER BY post_id`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if !slices.Equal(ids, []string{"1", "2"}) {
		t.Fatalf("kept X posts %v, want 1 and 2: 3 says Cloudy, not Cloud, and 4 never names Northwind Cloud", ids)
	}
	var raw int
	if err := e.db.QueryRow(e.ctx, `SELECT count(*) FROM social_mention WHERE network = 'linkedin'`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if raw == 0 {
		t.Fatal("the LinkedIn recording, which names Northwind Cloud in every post here, kept nothing")
	}
}
