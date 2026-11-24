package social

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/unkeyed/byname/internal/core"
)

// FilterKind is what a filter matches.
type FilterKind string

const (
	// Topic is a word or phrase in a post's text.
	Topic FilterKind = "topic"
	// Subreddit is the subreddit a Reddit post is in.
	Subreddit FilterKind = "subreddit"
)

// FilterMode is what a filter does with the posts it matches.
type FilterMode string

const (
	// LeaveOut hides the posts it matches.
	LeaveOut FilterMode = "leave_out"
	// Only hides the posts of its kind that match no Only filter of that
	// kind: with an Only subreddit, Reddit posts in other subreddits are
	// hidden; with an Only topic, posts that name none of them are.
	Only FilterMode = "only"
)

// Filter is a topic or subreddit that reads hide, or keep to.
type Filter struct {
	ID    int64      `json:"id"`
	Kind  FilterKind `json:"kind" jsonschema:"topic (a word or phrase in a post) or subreddit"`
	Mode  FilterMode `json:"mode" jsonschema:"leave_out hides the posts it matches; only hides the posts of its kind that match no only filter of that kind"`
	Value string     `json:"value" jsonschema:"the topic, lowercase, or the subreddit's name without r/"`
	// Mentions is how many stored mentions it matches.
	Mentions int `json:"mentions" jsonschema:"stored mentions it matches"`
}

// FilterInput adds a filter, or changes the mode of the one already set
// for that topic or subreddit.
type FilterInput struct {
	Kind  FilterKind `json:"kind" jsonschema:"topic or subreddit"`
	Mode  FilterMode `json:"mode" jsonschema:"leave_out to hide what it matches, only to show only what it matches"`
	Value string     `json:"value" jsonschema:"a topic: a word or phrase, matched as a whole word in any case; a subreddit: its name, r/name or a link to it"`
}

// filterReason is the SQL, over a mention m, for why a filter hides it:
// the first topic or subreddit left out that it matches, or that it falls
// outside the only filters of a kind. It is empty when no filter hides it.
const filterReason = `coalesce(
	(SELECT 'topic ' || f.value FROM social_filter f WHERE f.kind = 'topic' AND f.mode = 'leave_out' AND m.text ~* f.pattern ORDER BY f.id LIMIT 1),
	(SELECT 'r/' || f.value FROM social_filter f WHERE f.kind = 'subreddit' AND f.mode = 'leave_out' AND f.value = m.subreddit),
	CASE WHEN m.network = 'reddit'
		AND EXISTS (SELECT 1 FROM social_filter f WHERE f.kind = 'subreddit' AND f.mode = 'only')
		AND NOT EXISTS (SELECT 1 FROM social_filter f WHERE f.kind = 'subreddit' AND f.mode = 'only' AND f.value = m.subreddit)
		THEN 'not in your subreddits' END,
	CASE WHEN EXISTS (SELECT 1 FROM social_filter f WHERE f.kind = 'topic' AND f.mode = 'only')
		AND NOT EXISTS (SELECT 1 FROM social_filter f WHERE f.kind = 'topic' AND f.mode = 'only' AND m.text ~* f.pattern)
		THEN 'none of your topics' END,
	'')`

// filterMatches is the SQL, over a filter f, counting the stored mentions
// it matches.
const filterMatches = `(SELECT count(*) FROM social_mention m WHERE CASE WHEN f.kind = 'topic' THEN m.text ~* f.pattern ELSE m.subreddit = f.value END)`

var subredditName = regexp.MustCompile(`^[a-z0-9_]{2,21}$`)

// normalizeFilter is the stored value of a topic or subreddit, and the
// pattern that matches a topic.
func normalizeFilter(kind FilterKind, raw string) (value, pattern string, err error) {
	raw = strings.Join(strings.Fields(raw), " ")
	switch kind {
	case Topic:
		value = strings.ToLower(raw)
		if n := len([]rune(value)); n < 2 || n > 100 {
			return "", "", fmt.Errorf("a topic is 2 to 100 characters, got %q", raw)
		}
		// A whole word: what is around it is no letter, digit or _, so
		// weather matches "Weather," and "#weather" but not "weatherproof".
		return value, `(^|\W)` + regexp.QuoteMeta(value) + `($|\W)`, nil
	case Subreddit:
		value = strings.ToLower(raw)
		if u, err := url.Parse(value); err == nil && u.Host != "" {
			value = u.Path
		}
		value = strings.Trim(value, "/")
		value = strings.TrimPrefix(value, "r/")
		value, _, _ = strings.Cut(value, "/")
		if !subredditName.MatchString(value) {
			return "", "", fmt.Errorf("give a subreddit's name, r/name or a link to it, got %q", raw)
		}
		return value, "", nil
	}
	return "", "", fmt.Errorf("kind must be topic or subreddit, got %q", kind)
}

func filterRow(r pgx.CollectableRow) (Filter, error) {
	var f Filter
	return f, r.Scan(&f.ID, &f.Kind, &f.Mode, &f.Value, &f.Mentions)
}

// Filters lists the topics, then the subreddits, each kind by mode and
// name.
func (s *Service) Filters(ctx context.Context) ([]Filter, error) {
	rows, err := s.db.Query(ctx, `SELECT f.id, f.kind, f.mode, f.value, `+filterMatches+`
		FROM social_filter f ORDER BY f.kind DESC, f.mode, f.value`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, filterRow)
}

// AddFilter adds a topic or subreddit filter, or changes the mode of the
// one already set for it. It applies to past mentions as well.
func (s *Service) AddFilter(ctx context.Context, in FilterInput) (Filter, error) {
	if in.Mode != LeaveOut && in.Mode != Only {
		return Filter{}, fmt.Errorf("%w: mode must be leave_out or only, got %q", core.ErrInvalid, in.Mode)
	}
	value, pattern, err := normalizeFilter(in.Kind, in.Value)
	if err != nil {
		return Filter{}, fmt.Errorf("%w: %v", core.ErrInvalid, err)
	}
	rows, err := s.db.Query(ctx, `WITH f AS (INSERT INTO social_filter (kind, mode, value, pattern) VALUES ($1, $2, $3, $4)
			ON CONFLICT (kind, value) DO UPDATE SET mode = excluded.mode
			RETURNING id, kind, mode, value, pattern)
		SELECT f.id, f.kind, f.mode, f.value, `+filterMatches+` FROM f`, in.Kind, in.Mode, value, pattern)
	if err != nil {
		return Filter{}, err
	}
	return pgx.CollectExactlyOneRow(rows, filterRow)
}

// RemoveFilter takes a filter off, so the posts it hid show again.
func (s *Service) RemoveFilter(ctx context.Context, id int64) error {
	tag, err := s.db.Exec(ctx, `DELETE FROM social_filter WHERE id = $1`, id)
	if err == nil && tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: no social filter %d", core.ErrNotFound, id)
	}
	return err
}
