package social

import (
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func ptr(n int) *int { return &n }

var callAt = time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Each network's parser reads its fixture into posts: the example Treg's
// catalog captured for its tool (<tool>.json, saved verbatim), a synthetic
// body for Threads, whose capture cuts every post, and a recorded Bluesky
// answer with invented authors. Expected values are read off the bodies by
// hand.
func TestParsersReadTheirFixtures(t *testing.T) {
	cases := []struct {
		network Network
		fixture string
		posts   int
		next    string
		first   Post
	}{
		{X, "anyapi.x.search.posts.json", 2, "RamvY3F9", Post{ID: "2101246259504021713", URL: "https://x.com/i/web/status/2101246259504021713", AuthorHandle: "_kidrah_24", AuthorName: "Hardik (🔮)",
			Text: "@AroraBhavyam @miramurati @OpenAI Those who are deep into AI trenches only know who she is", PostedAt: time.Unix(1789811122, 0).UTC(),
			Engagement: Engagement{Likes: ptr(0), Replies: ptr(0), Reposts: ptr(0), Views: ptr(0)}}},
		{Reddit, "crawl4ai.reddit.search.posts.json", 2, "", Post{ID: "1uzotgx", URL: "https://www.reddit.com/r/dotnet/comments/1uzotgx/simple_net_11_preview_6_runtime_async_benchmark/", AuthorHandle: "iso3200",
			Text: "Simple .NET 11 preview 6 runtime async benchmark", PostedAt: time.Date(2026, 7, 18, 7, 2, 18, 0, time.UTC),
			Engagement: Engagement{Score: ptr(126), Replies: ptr(47)}}},
		{HackerNews, "crawl4ai.hackernews.search.stories.json", 2, "", Post{ID: "26406989", URL: "https://news.ycombinator.com/item?id=26406989", AuthorHandle: "tazjin",
			Text: "Why asynchronous Rust doesn't work", PostedAt: time.Date(2021, 3, 10, 2, 16, 55, 0, time.UTC),
			Engagement: Engagement{Score: ptr(603), Replies: ptr(482)}}},
		{LinkedIn, "anyapi.linkedin.search.posts.json", 2, "", Post{ID: "7505637791545020416", URL: "https://www.linkedin.com/posts/jakevalles_my-best-placements-this-year-didnt-start-activity-7505637791545020416-0J5g",
			AuthorHandle: "jakevalles", AuthorName: "Jake Valles", PostedAt: time.Unix(1789483497, 0).UTC(), Engagement: Engagement{Likes: ptr(13), Replies: ptr(2)}}},
		{YouTube, "crawl4ai.youtube.search.videos.json", 2, "", Post{ID: "rQ_J9WH6CGk", URL: "https://www.youtube.com/watch?v=rQ_J9WH6CGk", AuthorHandle: "BekBrace", AuthorName: "BekBrace",
			Text:     "Rust Programming Full Course  | Learn ⚙️ in 2024 | #rustprogramming #rust\n\nDuration: 3 hours and 5 minutes Instructor: Amir Bekhit Support My Channel Through Patreon: ...",
			PostedAt: callAt.Add(-2 * 365 * 24 * time.Hour), PostedApprox: true, Engagement: Engagement{Views: ptr(640565)}}},
		{Threads, "tikhub.x.threads-web-search-recent.synthetic.json", 2, "QVFIUmN6dE5vcnRod2luZA", Post{ID: "3762810465238117345", URL: "https://www.threads.com/@priya.builds/post/DQ4nW1vEk2x",
			AuthorHandle: "priya.builds", AuthorName: "Priya Raman", Text: "Northwind Cloud's new edge regions cut our p95 in half. Worth a look if you run APIs in Europe.",
			PostedAt: time.Unix(1795301400, 0).UTC(), Engagement: Engagement{Likes: ptr(42), Replies: ptr(6), Reposts: ptr(3)}}},
		{Threads, "tikhub.x.threads-web-search-recent.json", 0, "", Post{}},
		{Bluesky, "bluesky.searchPosts.json", 2, "eyJzIjpbMTc5NTM0MzkyMDAwMCwiZGlkOnBsYzpleDRtcGxlIiwiM24ydmdlZHZndmMyZCJdfQ", Post{ID: "at://did:plc:ex4mple0000000000000ada/app.bsky.feed.post/3n2vjanlqwz22",
			URL: "https://bsky.app/profile/ada.bsky.social/post/3n2vjanlqwz22", AuthorHandle: "ada.bsky.social", AuthorName: "Ada Lindqvist",
			Text:     "Moved our staging API to Northwind Cloud this week. Deploys went from 9 minutes to under 2.",
			PostedAt: time.Date(2026, 11, 22, 11, 30, 17, 336091950, time.UTC), Engagement: Engagement{Likes: ptr(14), Replies: ptr(3), Reposts: ptr(2)}}},
	}
	for _, c := range cases {
		sp, _ := lookup(c.network)
		page, err := sp.parse(readFixture(t, c.fixture), callAt)
		if err != nil {
			t.Fatalf("%s: %v", c.fixture, err)
		}
		if len(page.Posts) != c.posts || len(page.Next) < len(c.next) || page.Next[:len(c.next)] != c.next || (c.next == "") != (page.Next == "") {
			t.Fatalf("%s: %d posts, next %q; want %d posts and a next cursor starting %q", c.fixture, len(page.Posts), page.Next, c.posts, c.next)
		}
		if c.posts == 0 {
			continue
		}
		got := page.Posts[0]
		if c.network == LinkedIn {
			got.Text = "" // the capture cuts the text mid-sentence; the other fields are the point
		}
		if !reflect.DeepEqual(got, c.first) {
			t.Errorf("%s first post =\n%+v\nwant\n%+v", c.fixture, got, c.first)
		}
	}
}

// Reddit's catalog note says its public feed carries no score or comment
// count; a row without them parses to nil counts, never 0.
func TestMissingEngagementIsNil(t *testing.T) {
	sp, _ := lookup(Reddit)
	page, err := sp.parse([]byte(`{"rows":[{"id":"a1","author":"Someone","created":"2026-11-20T10:00:00Z","title":"Northwind Cloud is fast","text":"","url":"https://www.reddit.com/r/x/comments/a1/","score":null,"comments":null}]}`), callAt)
	if err != nil {
		t.Fatal(err)
	}
	if e := page.Posts[0].Engagement; e.Score != nil || e.Replies != nil || e.Likes != nil {
		t.Fatalf("engagement = %+v, want every count nil", e)
	}
}

func TestParsersRefuseBodiesTheyCannotRead(t *testing.T) {
	for _, n := range registry {
		for _, body := range []string{`not json`, `{}`, `{"error":"bad_request","message":"query is required"}`} {
			if _, err := n.parse([]byte(body), callAt); err == nil {
				t.Errorf("%s parsed %s without an error", n.Network, body)
			}
		}
	}
}

func TestYouTubeAges(t *testing.T) {
	for in, want := range map[string]time.Duration{
		"2 years ago": 2 * 365 * 24 * time.Hour, "Streamed 5 hours ago": 5 * time.Hour, "1 week ago": 7 * 24 * time.Hour, "3 months ago": 90 * 24 * time.Hour, "": 0,
	} {
		if got := callAt.Sub(ago(in, callAt)); got != want {
			t.Errorf("ago(%q) = %v before the call, want %v", in, got, want)
		}
	}
}

func TestNormalizeHandle(t *testing.T) {
	for _, c := range []struct {
		network Network
		in      string
		want    string
	}{
		{X, "@Northwind_HQ", "northwind_hq"},
		{X, "https://x.com/Northwind_HQ/status/1", "northwind_hq"},
		{Reddit, "u/Ada_Builds", "ada_builds"},
		{Reddit, "/u/ada_builds", "ada_builds"},
		{Reddit, "https://www.reddit.com/user/Ada_Builds/", "ada_builds"},
		{HackerNews, "https://news.ycombinator.com/user?id=adab", "adab"},
		{HackerNews, " adab ", "adab"},
		{LinkedIn, "https://www.linkedin.com/in/ada-byron-42/", "ada-byron-42"},
		{LinkedIn, "www.linkedin.com/company/northwind-cloud", "northwind-cloud"},
		{YouTube, "Northwind Cloud", "northwindcloud"},
		{YouTube, "https://www.youtube.com/@NorthwindCloud", "northwindcloud"},
		{YouTube, "https://www.youtube.com/channel/UCnorthwind000000000000", "ucnorthwind000000000000"},
		{Threads, "https://www.threads.com/@priya.builds", "priya.builds"},
		{Threads, "@Priya.Builds", "priya.builds"},
		{Bluesky, "https://bsky.app/profile/ada.bsky.social", "ada.bsky.social"},
		{Bluesky, "@ada.bsky.social", "ada.bsky.social"},
	} {
		got, err := normalizeHandle(c.network, c.in)
		if err != nil || got != c.want {
			t.Errorf("normalizeHandle(%s, %q) = %q, %v; want %q", c.network, c.in, got, err, c.want)
		}
	}
	for _, bad := range []string{"", "two words", "https://x.com/", "@"} {
		if got, err := normalizeHandle(X, bad); err == nil {
			t.Errorf("normalizeHandle(x, %q) = %q, want an error", bad, got)
		}
	}
}

// X ANDs bare words, so a term of several words is a phrase; retweets are
// left out, and a later fetch reads back only to the newest day stored.
func TestRequests(t *testing.T) {
	newest := time.Date(2026, 11, 18, 22, 0, 0, 0, time.UTC)
	got := xRequest(search{Term: "Northwind Cloud", Newest: &newest, Cursor: "c2"}).Body
	want := map[string]any{"query": `"Northwind Cloud" -is:retweet since:2026-11-18`, "queryType": "Latest", "limit": 50, "cursor": "c2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("x body = %v, want %v", got, want)
	}
	if q := xRequest(search{Term: "northwind.example"}).Body.(map[string]any)["query"]; q != "northwind.example -is:retweet" {
		t.Fatalf("x query = %q", q)
	}
	if q := blueskyRequest(search{Term: "Northwind Cloud", Newest: &newest, Cursor: "c9"}).Query; !reflect.DeepEqual(q, url.Values{
		"q": {"Northwind Cloud"}, "sort": {"latest"}, "limit": {"50"}, "since": {"2026-11-18T22:00:00Z"}, "cursor": {"c9"}}) {
		t.Fatalf("bluesky query = %v", q)
	}
	if q := threadsRequest(search{Term: "Northwind Cloud", Cursor: "e1"}).Query; !reflect.DeepEqual(q, url.Values{"query": {"Northwind Cloud"}, "end_cursor": {"e1"}}) {
		t.Fatalf("threads query = %v", q)
	}
}
