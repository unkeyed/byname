package social

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Network is a social network Byname searches. The values are stored in
// social_network.network and never change once shipped.
type Network string

const (
	X          Network = "x"
	Reddit     Network = "reddit"
	HackerNews Network = "hackernews"
	LinkedIn   Network = "linkedin"
	YouTube    Network = "youtube"
	Threads    Network = "threads"
	Bluesky    Network = "bluesky"
)

// Route is how a network's search is called.
type Route string

const (
	// ViaTreg calls the network's pinned Treg tool with the team's token.
	ViaTreg Route = "treg"
	// Direct calls the network's own public API, which needs no key and
	// costs nothing.
	Direct Route = "direct"
)

// Engagement is what a network reports about a post. A count the network
// does not give is nil, never 0.
type Engagement struct {
	Likes   *int `json:"likes" jsonschema:"likes or reactions, null when the network gives none"`
	Replies *int `json:"replies" jsonschema:"replies or comments, null when the network gives none"`
	Reposts *int `json:"reposts" jsonschema:"reposts on X, Threads and Bluesky"`
	Score   *int `json:"score" jsonschema:"points on Hacker News or score on Reddit"`
	Views   *int `json:"views" jsonschema:"views on X and YouTube"`
	// Total ranks posts within a network: views on YouTube, where views are
	// all there is; elsewhere likes, replies, reposts and score added up,
	// missing counts as 0.
	Total int `json:"total" jsonschema:"what ranks posts within a network: views on YouTube, elsewhere likes, replies, reposts and score added up"`
}

// Post is one post as the network's search returned it, parsed at the
// boundary into the shape every network shares.
type Post struct {
	ID           string
	URL          string
	AuthorHandle string
	AuthorName   string
	Text         string
	PostedAt     time.Time
	// PostedApprox is true when the network gives only an age, such as
	// YouTube's "2 years ago", so PostedAt is worked out from the call's
	// time.
	PostedApprox bool
	Engagement   Engagement
}

// Page is one search call's posts, newest first or as the network ranks
// them, and the cursor for the next page, empty when there is none.
type Page struct {
	Posts []Post
	Next  string
}

// search is one call to make: the term, the newest post already stored
// for it (nil before the first), the cursor from the previous page, and
// when the call is made.
type search struct {
	Term   string
	Newest *time.Time
	Cursor string
	At     time.Time
}

// request is a search call's query string or JSON body, in the network's
// own shape.
type request struct {
	Query url.Values
	Body  any
}

// spec is one network's row in the registry: where its search goes, what
// a call costs, and how to build its request and read its answer.
type spec struct {
	Network Network
	Title   string
	Route   Route
	// Tool is the Treg tool id, or for a direct network the API method.
	Tool   string
	Method string
	// BaseURL and Path are a direct network's API; Options.BaseURLs
	// replaces the base URL.
	BaseURL string
	Path    string
	// CostUSD is the catalog's price per call, checked 2026-11-22. It is a
	// known 0 for a direct network.
	CostUSD float64
	// MaxCostUSD is sent as X-Treg-Route-Max-Cost, so a failover to a
	// dearer source is refused rather than paid.
	MaxCostUSD float64
	// Pages is the most calls per term in one fetch. Networks without a
	// cursor answer with their latest posts in one call.
	Pages   int
	request func(search) request
	parse   func(raw []byte, at time.Time) (Page, error)
	// handle reads an author's handle from a profile URL someone pasted.
	handle func(u *url.URL) string
	// Profile is the URL of an author's page, before the handle.
	Profile string
	// Example is a handle as people write it, for error messages.
	Example string
}

// registry is the one list of networks, in the order the app shows them.
var registry = []spec{
	{Network: X, Title: "X", Route: ViaTreg, Tool: "anyapi.x.search.posts", Method: http.MethodPost, CostUSD: 0.00075, MaxCostUSD: 0.002, Pages: 3,
		request: xRequest, parse: parseX, handle: firstSegment, Profile: "https://x.com/", Example: "@northwind"},
	{Network: Reddit, Title: "Reddit", Route: ViaTreg, Tool: "crawl4ai.reddit.search.posts", Method: http.MethodPost, CostUSD: 0.0009, MaxCostUSD: 0.002, Pages: 1,
		request: body(map[string]any{"sort": "new", "limit": 100}), parse: parseCrawl4AI(redditRow), handle: redditUser, Profile: "https://www.reddit.com/user/", Example: "u/northwind"},
	{Network: HackerNews, Title: "Hacker News", Route: ViaTreg, Tool: "crawl4ai.hackernews.search.stories", Method: http.MethodPost, CostUSD: 0.00015, MaxCostUSD: 0.001, Pages: 1,
		request: body(map[string]any{"sort": "date", "limit": 100}), parse: parseCrawl4AI(hackerNewsRow), handle: hackerNewsUser, Profile: "https://news.ycombinator.com/user?id=", Example: "northwind"},
	{Network: LinkedIn, Title: "LinkedIn", Route: ViaTreg, Tool: "anyapi.linkedin.search.posts", Method: http.MethodPost, CostUSD: 0.002, MaxCostUSD: 0.009, Pages: 3,
		request: linkedInRequest, parse: parseLinkedIn, handle: linkedInProfile, Profile: "https://www.linkedin.com/in/", Example: "jane-doe"},
	{Network: YouTube, Title: "YouTube", Route: ViaTreg, Tool: "crawl4ai.youtube.search.videos", Method: http.MethodPost, CostUSD: 0.0009, MaxCostUSD: 0.002, Pages: 1,
		request: body(map[string]any{"limit": 20}), parse: parseCrawl4AI(youTubeRow), handle: youTubeChannel, Profile: "https://www.youtube.com/@", Example: "@northwindcloud"},
	{Network: Threads, Title: "Threads", Route: ViaTreg, Tool: "tikhub.x.threads-web-search-recent", Method: http.MethodGet, CostUSD: 0.002, MaxCostUSD: 0.004, Pages: 2,
		request: threadsRequest, parse: parseThreads, handle: atSegment, Profile: "https://www.threads.com/@", Example: "@northwind"},
	{Network: Bluesky, Title: "Bluesky", Route: Direct, Tool: "app.bsky.feed.searchPosts", Method: http.MethodGet, BaseURL: "https://api.bsky.app", Path: "/xrpc/app.bsky.feed.searchPosts", Pages: 3,
		request: blueskyRequest, parse: parseBluesky, handle: blueskyProfile, Profile: "https://bsky.app/profile/", Example: "northwind.bsky.social"},
}

func lookup(n Network) (spec, bool) {
	for _, s := range registry {
		if s.Network == n {
			return s, true
		}
	}
	return spec{}, false
}

func networkNames() string {
	names := make([]string, len(registry))
	for i, n := range registry {
		names[i] = string(n.Network)
	}
	return strings.Join(names[:len(names)-1], ", ") + " or " + names[len(names)-1]
}

func parseNetwork(raw string) (Network, error) {
	if s, ok := lookup(Network(raw)); ok {
		return s.Network, nil
	}
	return "", fmt.Errorf("network must be %s, got %q", networkNames(), raw)
}

// body is a request with fixed fields beside the query, for networks that
// answer with their latest posts in one call.
func body(fixed map[string]any) func(search) request {
	return func(s search) request {
		b := map[string]any{"query": s.Term}
		for k, v := range fixed {
			b[k] = v
		}
		return request{Body: b}
	}
}

// xRequest asks X's advanced search for the latest original posts with
// the term, back to the day of the newest one stored. X ANDs bare words,
// so a term of several words is searched as a phrase.
func xRequest(s search) request {
	q := s.Term
	if strings.ContainsAny(q, " \t") {
		q = `"` + strings.ReplaceAll(q, `"`, "") + `"`
	}
	q += " -is:retweet"
	if s.Newest != nil {
		q += " since:" + s.Newest.UTC().Format(time.DateOnly)
	}
	b := map[string]any{"query": q, "queryType": "Latest", "limit": 50}
	if s.Cursor != "" {
		b["cursor"] = s.Cursor
	}
	return request{Body: b}
}

// linkedInRequest asks for posts from the narrowest window LinkedIn offers
// that still reaches back to the newest post stored, or the last month on
// the first fetch.
func linkedInRequest(s search) request {
	window := "last-month"
	if s.Newest != nil {
		switch age := s.At.Sub(*s.Newest); {
		case age < 20*time.Hour:
			window = "last-day"
		case age < 6*24*time.Hour:
			window = "last-week"
		case age > 28*24*time.Hour:
			window = "last-year"
		}
	}
	b := map[string]any{"query": s.Term, "datePosted": window}
	if s.Cursor != "" {
		b["cursor"] = s.Cursor
	}
	return request{Body: b}
}

func threadsRequest(s search) request {
	q := url.Values{"query": {s.Term}}
	if s.Cursor != "" {
		q.Set("end_cursor", s.Cursor)
	}
	return request{Query: q}
}

func blueskyRequest(s search) request {
	q := url.Values{"q": {s.Term}, "sort": {"latest"}, "limit": {strconv.Itoa(50)}}
	if s.Newest != nil {
		q.Set("since", s.Newest.UTC().Format(time.RFC3339))
	}
	if s.Cursor != "" {
		q.Set("cursor", s.Cursor)
	}
	return request{Query: q}
}

func firstSegment(u *url.URL) string {
	seg, _, _ := strings.Cut(strings.Trim(u.Path, "/"), "/")
	return seg
}

// atSegment reads /@name, as Threads and YouTube profile URLs have it.
func atSegment(u *url.URL) string {
	if seg := firstSegment(u); strings.HasPrefix(seg, "@") {
		return seg
	}
	return ""
}

func secondSegment(u *url.URL, firsts ...string) string {
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 {
		return ""
	}
	for _, f := range firsts {
		if parts[0] == f {
			if s, err := url.PathUnescape(parts[1]); err == nil {
				return s
			}
			return parts[1]
		}
	}
	return ""
}

func redditUser(u *url.URL) string     { return secondSegment(u, "user", "u") }
func hackerNewsUser(u *url.URL) string { return u.Query().Get("id") }
func blueskyProfile(u *url.URL) string { return secondSegment(u, "profile") }

// linkedInProfile reads the slug of /in/<slug> or /company/<slug>.
func linkedInProfile(u *url.URL) string { return secondSegment(u, "in", "company", "school") }

// youTubeChannel reads /@handle, or the id of /channel/<id> and the name
// of /c/<name>.
func youTubeChannel(u *url.URL) string {
	if h := atSegment(u); h != "" {
		return h
	}
	return secondSegment(u, "channel", "c", "user")
}

// storedHandle is an author's handle as mentions and the left-out list
// store it: lowercase, with no @ and no u/. A YouTube channel is known by
// its name with the spaces taken out, which is all YouTube search shows.
func storedHandle(n Network, h string) string {
	h = strings.TrimPrefix(strings.TrimSpace(h), "@")
	switch n {
	case Reddit:
		h = strings.TrimPrefix(strings.TrimPrefix(h, "/"), "u/")
	case YouTube:
		h = strings.Join(strings.Fields(h), "")
	}
	return strings.ToLower(h)
}

var handleShape = regexp.MustCompile(`^[\p{L}\p{N}_.:\-]{1,100}$`)

// normalizeHandle turns what someone typed or pasted (a handle, @handle,
// u/handle or a profile URL) into the stored form.
func normalizeHandle(n Network, raw string) (string, error) {
	s, ok := lookup(n)
	if !ok {
		return "", fmt.Errorf("network must be %s, got %q", networkNames(), n)
	}
	h := strings.TrimSpace(raw)
	if strings.Contains(h, "://") || strings.HasPrefix(strings.ToLower(h), "www.") {
		if !strings.Contains(h, "://") {
			h = "https://" + h
		}
		u, err := url.Parse(h)
		if err != nil {
			return "", fmt.Errorf("%q is not a profile URL", raw)
		}
		h = s.handle(u)
	}
	h = storedHandle(n, h)
	if !handleShape.MatchString(h) {
		return "", fmt.Errorf("%q is not a %s handle; give the handle, such as %s, or a link to the profile", raw, s.Title, s.Example)
	}
	return h, nil
}
