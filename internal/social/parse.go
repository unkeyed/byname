package social

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Parsers read one network's answer, in its provider's own shape, into a
// Page. They do no I/O. at is when the call was made, for networks that
// give only a post's age.

// objects keeps the JSON objects of a list. Treg's catalog captures stand
// a "… N more item(s) truncated" string in for the rows they cut.
func objects(items []json.RawMessage) []json.RawMessage {
	var out []json.RawMessage
	for _, it := range items {
		if t := strings.TrimSpace(string(it)); strings.HasPrefix(t, "{") {
			out = append(out, it)
		}
	}
	return out
}

// anyAPIData is the envelope AnyAPI answers in.
func anyAPIData(raw []byte, into any) error {
	var env struct {
		Output *struct {
			Data  json.RawMessage `json:"data"`
			Found *bool           `json:"found"`
		} `json:"output"`
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("not JSON: %w", err)
	}
	switch {
	case env.Error != "" || env.Message != "" && env.Output == nil:
		return fmt.Errorf("provider error: %s %s", env.Error, env.Message)
	case env.Output == nil:
		return errors.New("no output in the answer")
	case env.Output.Found != nil && !*env.Output.Found, len(env.Output.Data) == 0, string(env.Output.Data) == "null":
		return nil
	}
	return json.Unmarshal(env.Output.Data, into)
}

func unixTime(s int64) time.Time { return time.Unix(s, 0).UTC() }

func parseX(raw []byte, _ time.Time) (Page, error) {
	var data struct {
		Items      []json.RawMessage `json:"items"`
		NextCursor *string           `json:"nextCursor"`
	}
	if err := anyAPIData(raw, &data); err != nil {
		return Page{}, err
	}
	var p Page
	for _, it := range objects(data.Items) {
		var r struct {
			ID           string `json:"id"`
			URL          string `json:"url"`
			Username     string `json:"authorUsername"`
			Name         string `json:"authorName"`
			Text         string `json:"text"`
			CreatedUTC   int64  `json:"createdUtc"`
			LikeCount    *int   `json:"likeCount"`
			ReplyCount   *int   `json:"replyCount"`
			RetweetCount *int   `json:"retweetCount"`
			ViewCount    *int   `json:"viewCount"`
		}
		if err := json.Unmarshal(it, &r); err != nil {
			return Page{}, fmt.Errorf("an X post: %w", err)
		}
		if r.URL == "" && r.ID != "" {
			r.URL = "https://x.com/i/web/status/" + r.ID
		}
		p.Posts = append(p.Posts, Post{ID: r.ID, URL: r.URL, AuthorHandle: r.Username, AuthorName: r.Name, Text: r.Text, PostedAt: unixTime(r.CreatedUTC),
			Engagement: Engagement{Likes: r.LikeCount, Replies: r.ReplyCount, Reposts: r.RetweetCount, Views: r.ViewCount}})
	}
	if data.NextCursor != nil {
		p.Next = *data.NextCursor
	}
	return p, nil
}

func parseLinkedIn(raw []byte, _ time.Time) (Page, error) {
	var data struct {
		Posts      []json.RawMessage `json:"posts"`
		NextCursor *string           `json:"nextCursor"`
	}
	if err := anyAPIData(raw, &data); err != nil {
		return Page{}, err
	}
	var p Page
	for _, it := range objects(data.Posts) {
		var r struct {
			ID            string `json:"id"`
			URL           string `json:"url"`
			AuthorName    string `json:"authorName"`
			AuthorURL     string `json:"authorUrl"`
			Text          string `json:"text"`
			CreatedUTC    int64  `json:"createdUtc"`
			ReactionCount *int   `json:"reactionCount"`
			CommentCount  *int   `json:"commentCount"`
		}
		if err := json.Unmarshal(it, &r); err != nil {
			return Page{}, fmt.Errorf("a LinkedIn post: %w", err)
		}
		handle := ""
		if u, err := url.Parse(r.AuthorURL); err == nil && r.AuthorURL != "" {
			handle = linkedInProfile(u)
		}
		p.Posts = append(p.Posts, Post{ID: r.ID, URL: r.URL, AuthorHandle: handle, AuthorName: r.AuthorName, Text: r.Text, PostedAt: unixTime(r.CreatedUTC),
			Engagement: Engagement{Likes: r.ReactionCount, Replies: r.CommentCount}})
	}
	if data.NextCursor != nil {
		p.Next = *data.NextCursor
	}
	return p, nil
}

// parseCrawl4AI reads a Crawl4AI recipe's rows, each with row.
func parseCrawl4AI(row func(raw json.RawMessage, at time.Time) (Post, error)) func([]byte, time.Time) (Page, error) {
	return func(raw []byte, at time.Time) (Page, error) {
		var body struct {
			Rows  *[]json.RawMessage `json:"rows"`
			Error any                `json:"error"`
		}
		if err := json.Unmarshal(raw, &body); err != nil {
			return Page{}, fmt.Errorf("not JSON: %w", err)
		}
		if body.Rows == nil {
			if body.Error != nil {
				return Page{}, fmt.Errorf("provider error: %v", body.Error)
			}
			return Page{}, errors.New("no rows in the answer")
		}
		var p Page
		for _, it := range objects(*body.Rows) {
			post, err := row(it, at)
			if err != nil {
				return Page{}, err
			}
			p.Posts = append(p.Posts, post)
		}
		return p, nil
	}
}

// postRow is a Reddit or Hacker News row.
type postRow struct {
	ID       string  `json:"id"`
	Author   string  `json:"author"`
	Created  string  `json:"created"`
	Title    *string `json:"title"`
	Text     *string `json:"text"`
	URL      string  `json:"url"`
	HNURL    string  `json:"hn_url"`
	Score    *int    `json:"score"`
	Points   *int    `json:"points"`
	Comments *int    `json:"comments"`
}

func readPostRow(raw json.RawMessage) (postRow, time.Time, error) {
	var r postRow
	if err := json.Unmarshal(raw, &r); err != nil {
		return r, time.Time{}, fmt.Errorf("a row: %w", err)
	}
	at, err := time.Parse(time.RFC3339, r.Created)
	if err != nil {
		return r, at, fmt.Errorf("row %s: created %q is not a time", r.ID, r.Created)
	}
	return r, at.UTC(), nil
}

// joinText is a post's title, then its body when it has one.
func joinText(parts ...*string) string {
	var out []string
	for _, s := range parts {
		if s != nil && strings.TrimSpace(*s) != "" {
			out = append(out, strings.TrimSpace(*s))
		}
	}
	return strings.Join(out, "\n\n")
}

func redditRow(raw json.RawMessage, _ time.Time) (Post, error) {
	r, at, err := readPostRow(raw)
	return Post{ID: r.ID, URL: r.URL, AuthorHandle: r.Author, Text: joinText(r.Title, r.Text), PostedAt: at,
		Engagement: Engagement{Score: r.Score, Replies: r.Comments}}, err
}

// hackerNewsRow links to the story's Hacker News page, where the mention
// is, not to the page the story links to.
func hackerNewsRow(raw json.RawMessage, _ time.Time) (Post, error) {
	r, at, err := readPostRow(raw)
	link := r.HNURL
	if link == "" {
		link = "https://news.ycombinator.com/item?id=" + r.ID
	}
	return Post{ID: r.ID, URL: link, AuthorHandle: r.Author, Text: joinText(r.Title, r.Text), PostedAt: at,
		Engagement: Engagement{Score: r.Points, Replies: r.Comments}}, err
}

// youTubeRow is a video whose title or description names the brand. The
// channel is the author, known by its name, and views are its engagement.
// YouTube gives only the video's age, so the time is worked out from at.
func youTubeRow(raw json.RawMessage, at time.Time) (Post, error) {
	var r struct {
		VideoID     string  `json:"video_id"`
		URL         string  `json:"url"`
		Channel     string  `json:"channel"`
		Title       *string `json:"title"`
		Description *string `json:"description"`
		Published   string  `json:"published"`
		Views       *int    `json:"views"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return Post{}, fmt.Errorf("a video: %w", err)
	}
	if r.URL == "" && r.VideoID != "" {
		r.URL = "https://www.youtube.com/watch?v=" + r.VideoID
	}
	return Post{ID: r.VideoID, URL: r.URL, AuthorHandle: r.Channel, AuthorName: r.Channel, Text: joinText(r.Title, r.Description),
		PostedAt: ago(r.Published, at), PostedApprox: true, Engagement: Engagement{Views: r.Views}}, nil
}

var age = regexp.MustCompile(`(\d+)\s+(second|minute|hour|day|week|month|year)s?\s+ago`)

var ageUnit = map[string]time.Duration{
	"second": time.Second, "minute": time.Minute, "hour": time.Hour, "day": 24 * time.Hour,
	"week": 7 * 24 * time.Hour, "month": 30 * 24 * time.Hour, "year": 365 * 24 * time.Hour,
}

// ago reads an age such as "2 years ago" or "Streamed 5 hours ago" back
// from at. An age it cannot read is taken as at.
func ago(s string, at time.Time) time.Time {
	m := age.FindStringSubmatch(strings.ToLower(s))
	if m == nil {
		return at.UTC()
	}
	n, _ := strconv.Atoi(m[1])
	return at.Add(-time.Duration(n) * ageUnit[m[2]]).UTC()
}

// parseThreads reads TikHub's pass-through of the Threads web search: each
// edge's thread holds the post as its first item.
func parseThreads(raw []byte, _ time.Time) (Page, error) {
	var env struct {
		Code    *int   `json:"code"`
		Message string `json:"message"`
		Detail  any    `json:"detail"`
		Data    *struct {
			SearchResults *struct {
				Edges    []json.RawMessage `json:"edges"`
				PageInfo struct {
					EndCursor   *string `json:"end_cursor"`
					HasNextPage bool    `json:"has_next_page"`
				} `json:"page_info"`
			} `json:"searchResults"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return Page{}, fmt.Errorf("not JSON: %w", err)
	}
	if env.Code != nil && *env.Code != 200 || env.Detail != nil {
		return Page{}, fmt.Errorf("provider error: %s %v", env.Message, env.Detail)
	}
	if env.Data == nil || env.Data.SearchResults == nil {
		return Page{}, errors.New("no search results in the answer")
	}
	res := env.Data.SearchResults
	var p Page
	for _, edge := range objects(res.Edges) {
		var e struct {
			Node struct {
				Thread json.RawMessage `json:"thread"`
			} `json:"node"`
		}
		if err := json.Unmarshal(edge, &e); err != nil {
			return Page{}, fmt.Errorf("a Threads result: %w", err)
		}
		if len(objects([]json.RawMessage{e.Node.Thread})) == 0 {
			continue
		}
		var th struct {
			Items []struct {
				Post struct {
					PK      string `json:"pk"`
					Code    string `json:"code"`
					TakenAt int64  `json:"taken_at"`
					Likes   *int   `json:"like_count"`
					Caption *struct {
						Text string `json:"text"`
					} `json:"caption"`
					User struct {
						Username string `json:"username"`
						FullName string `json:"full_name"`
					} `json:"user"`
					Info *struct {
						Replies *int `json:"direct_reply_count"`
						Reposts *int `json:"repost_count"`
					} `json:"text_post_app_info"`
				} `json:"post"`
			} `json:"thread_items"`
		}
		if err := json.Unmarshal(e.Node.Thread, &th); err != nil {
			return Page{}, fmt.Errorf("a Threads post: %w", err)
		}
		if len(th.Items) == 0 {
			continue
		}
		post := th.Items[0].Post
		out := Post{ID: post.PK, URL: "https://www.threads.com/@" + post.User.Username + "/post/" + post.Code, AuthorHandle: post.User.Username,
			AuthorName: post.User.FullName, PostedAt: unixTime(post.TakenAt), Engagement: Engagement{Likes: post.Likes}}
		if post.Caption != nil {
			out.Text = post.Caption.Text
		}
		if post.Info != nil {
			out.Engagement.Replies, out.Engagement.Reposts = post.Info.Replies, post.Info.Reposts
		}
		p.Posts = append(p.Posts, out)
	}
	if res.PageInfo.HasNextPage && res.PageInfo.EndCursor != nil {
		p.Next = *res.PageInfo.EndCursor
	}
	return p, nil
}

// parseBluesky reads app.bsky.feed.searchPosts. A post's page is
// bsky.app/profile/<handle>/post/<rkey>, the rkey being the last segment
// of its at:// uri.
func parseBluesky(raw []byte, _ time.Time) (Page, error) {
	var body struct {
		Posts *[]struct {
			URI    string `json:"uri"`
			Author struct {
				Handle      string `json:"handle"`
				DisplayName string `json:"displayName"`
			} `json:"author"`
			Record struct {
				Text      string `json:"text"`
				CreatedAt string `json:"createdAt"`
			} `json:"record"`
			IndexedAt string `json:"indexedAt"`
			Likes     *int   `json:"likeCount"`
			Replies   *int   `json:"replyCount"`
			Reposts   *int   `json:"repostCount"`
		} `json:"posts"`
		Cursor  string `json:"cursor"`
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return Page{}, fmt.Errorf("not JSON: %w", err)
	}
	if body.Posts == nil {
		if body.Error != "" {
			return Page{}, fmt.Errorf("provider error: %s %s", body.Error, body.Message)
		}
		return Page{}, errors.New("no posts in the answer")
	}
	var p Page
	for _, r := range *body.Posts {
		at, err := time.Parse(time.RFC3339Nano, r.Record.CreatedAt)
		if err != nil {
			if at, err = time.Parse(time.RFC3339Nano, r.IndexedAt); err != nil {
				return Page{}, fmt.Errorf("post %s: createdAt %q is not a time", r.URI, r.Record.CreatedAt)
			}
		}
		rkey := r.URI[strings.LastIndex(r.URI, "/")+1:]
		p.Posts = append(p.Posts, Post{ID: r.URI, URL: "https://bsky.app/profile/" + r.Author.Handle + "/post/" + rkey, AuthorHandle: r.Author.Handle,
			AuthorName: r.Author.DisplayName, Text: r.Record.Text, PostedAt: at.UTC(), Engagement: Engagement{Likes: r.Likes, Replies: r.Replies, Reposts: r.Reposts}})
	}
	p.Next = body.Cursor
	return p, nil
}
