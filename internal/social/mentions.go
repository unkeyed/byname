package social

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/unkeyed/byname/internal/core"
	"github.com/unkeyed/byname/internal/notify"
)

// Call is the Treg search call that first found a post.
type Call struct {
	ID         int64  `json:"id"`
	TregCallID string `json:"treg_call_id"`
	Route      Route  `json:"route" jsonschema:"treg or direct"`
	// CostUSD is the whole call's cost, shared by every post it found: nil
	// when Treg reported no cost, a known 0 for a direct call.
	CostUSD     *float64 `json:"cost_usd" jsonschema:"the whole call's cost, shared by every post it found; null when Treg reported no cost, 0 for a direct call, which is free"`
	CostMissing bool     `json:"cost_missing" jsonschema:"true when Treg reported no cost for the call, which is not the same as free"`
}

// Mention is one stored post, with whether the left-out list hides it now.
type Mention struct {
	ID           int64      `json:"id"`
	Network      Network    `json:"network" jsonschema:"x, reddit, hackernews, linkedin, youtube, threads or bluesky"`
	PostID       string     `json:"post_id" jsonschema:"the network's own id for the post"`
	URL          string     `json:"url"`
	AuthorHandle string     `json:"author_handle" jsonschema:"lowercase, with no @ or u/; empty when the network gives none"`
	AuthorName   string     `json:"author_name"`
	Text         string     `json:"text"`
	PostedAt     time.Time  `json:"posted_at"`
	PostedApprox bool       `json:"posted_approx" jsonschema:"true when the network gave only an age, such as YouTube's 2 years ago, so posted_at is approximate"`
	Engagement   Engagement `json:"engagement"`
	Term         string     `json:"term" jsonschema:"the search term that found it"`
	FirstSeenAt  time.Time  `json:"first_seen_at"`
	Hidden       bool       `json:"hidden" jsonschema:"true when its author is on the left-out list"`
	LeftOutAs    string     `json:"left_out_as" jsonschema:"the left-out label that hides it, empty when shown"`
	Call         *Call      `json:"call" jsonschema:"the Treg call that first found it"`
}

// mentionFrom is every read's FROM: each mention, the left-out entry that
// hides it now if any, and the call that found it. Leaving out is decided
// here, at read time, so the list applies to past mentions as well.
const mentionFrom = ` FROM social_mention m
	LEFT JOIN social_left_out l ON l.network = m.network AND l.handle = m.author_handle
	LEFT JOIN social_call c ON c.id = m.call_id `

const mentionColumns = `m.id, m.network, m.post_id, m.url, m.author_handle, m.author_name, m.text, m.posted_at, m.posted_approx,
	m.likes, m.replies, m.reposts, m.score, m.views, m.engagement, m.term, m.first_seen_at,
	coalesce(l.label, ''), c.id, coalesce(c.route, ''), coalesce(c.treg_call_id, ''), c.cost_usd::float8`

func scanMention(r pgx.CollectableRow) (Mention, error) {
	var m Mention
	var callID *int64
	var route Route
	var tregID string
	var cost *float64
	e := &m.Engagement
	if err := r.Scan(&m.ID, &m.Network, &m.PostID, &m.URL, &m.AuthorHandle, &m.AuthorName, &m.Text, &m.PostedAt, &m.PostedApprox,
		&e.Likes, &e.Replies, &e.Reposts, &e.Score, &e.Views, &e.Total, &m.Term, &m.FirstSeenAt, &m.LeftOutAs, &callID, &route, &tregID, &cost); err != nil {
		return m, err
	}
	m.Hidden = m.LeftOutAs != ""
	if callID != nil {
		m.Call = &Call{ID: *callID, Route: route, TregCallID: tregID, CostUSD: cost, CostMissing: cost == nil}
	}
	return m, nil
}

// MentionQuery filters mentions.
type MentionQuery struct {
	Network       Network `json:"network,omitempty" jsonschema:"only this network: x, reddit, hackernews, linkedin, youtube, threads or bluesky"`
	Since         string  `json:"since,omitempty" jsonschema:"only posts from then on: a day such as 2026-11-01 (midnight in the instance's time zone) or a time such as 2026-11-01T09:00:00Z"`
	IncludeHidden bool    `json:"include_hidden,omitempty" jsonschema:"also return mentions by left-out accounts, marked hidden"`
	Limit         int     `json:"limit,omitempty" jsonschema:"at most this many, 1 to 500, default 100"`
}

// MentionList is mentions newest first, with how many matched.
type MentionList struct {
	Mentions []Mention `json:"mentions" jsonschema:"newest post first"`
	Total    int       `json:"total" jsonschema:"mentions that match and are shown, which can be more than returned"`
	Hidden   int       `json:"hidden" jsonschema:"mentions that match but are by left-out accounts"`
}

// Mentions lists stored mentions newest first, hiding those by left-out
// accounts unless asked.
func (s *Service) Mentions(ctx context.Context, q MentionQuery) (MentionList, error) {
	if q.Limit == 0 {
		q.Limit = 100
	}
	if q.Limit < 1 || q.Limit > 500 {
		return MentionList{}, fmt.Errorf("%w: limit must be 1 to 500", core.ErrInvalid)
	}
	if q.Network != "" {
		if _, err := parseNetwork(string(q.Network)); err != nil {
			return MentionList{}, fmt.Errorf("%w: %v", core.ErrInvalid, err)
		}
	}
	var since *time.Time
	if q.Since != "" {
		st, err := s.settings(ctx)
		if err != nil {
			return MentionList{}, err
		}
		t, err := parseSince(q.Since, st.loc)
		if err != nil {
			return MentionList{}, err
		}
		since = &t
	}
	const where = ` WHERE ($1 = '' OR m.network = $1) AND ($2::timestamptz IS NULL OR m.posted_at >= $2)`
	out := MentionList{Mentions: []Mention{}}
	if err := s.db.QueryRow(ctx, `SELECT count(*) FILTER (WHERE l.id IS NULL), count(*) FILTER (WHERE l.id IS NOT NULL)`+mentionFrom+where,
		q.Network, since).Scan(&out.Total, &out.Hidden); err != nil {
		return out, err
	}
	rows, err := s.db.Query(ctx, `SELECT `+mentionColumns+mentionFrom+where+` AND ($3 OR l.id IS NULL)
		ORDER BY m.posted_at DESC, m.id DESC LIMIT $4`, q.Network, since, q.IncludeHidden, q.Limit)
	if err != nil {
		return out, err
	}
	list, err := pgx.CollectRows(rows, scanMention)
	if len(list) > 0 {
		out.Mentions = list
	}
	return out, err
}

func parseSince(raw string, loc *time.Location) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t, nil
	}
	if d, err := time.ParseInLocation(time.DateOnly, raw, loc); err == nil {
		return d, nil
	}
	return time.Time{}, fmt.Errorf("%w: since must be a day such as 2026-11-01 or a time such as 2026-11-01T09:00:00Z, got %q", core.ErrInvalid, raw)
}

// period is one digest's span, from midnight to midnight in the
// instance's time zone.
type period struct {
	cadence    Cadence
	start, end time.Time
}

func midnight(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// periodAt is the period, under the current cadence, that t falls in.
func (st settings) periodAt(t time.Time) period {
	day := midnight(t.In(st.loc))
	if st.cadence == Daily {
		return period{Daily, day, day.AddDate(0, 0, 1)}
	}
	start := day.AddDate(0, 0, -((int(day.Weekday()) - st.weekday + 7) % 7))
	return period{Weekly, start, start.AddDate(0, 0, 7)}
}

// latestFinished is the most recent period that has ended by now.
func (st settings) latestFinished(now time.Time) period {
	return st.periodAt(st.periodAt(now).start.AddDate(0, 0, -1))
}

func (p period) from() string { return p.start.Format(time.DateOnly) }
func (p period) to() string   { return p.end.AddDate(0, 0, -1).Format(time.DateOnly) }

// subject is the period's event subject, so each period's digest is
// stored and sent once: week/2026-11-16 or day/2026-11-21.
func (p period) subject() string {
	if p.cadence == Daily {
		return "day/" + p.from()
	}
	return "week/" + p.from()
}

// DigestQuery picks a digest's period.
type DigestQuery struct {
	Day string `json:"day,omitempty" jsonschema:"any day in the period, such as 2026-11-18; empty for the latest period that has ended"`
}

// NetworkDigest is one network's mentions in a period.
type NetworkDigest struct {
	Network  Network   `json:"network"`
	Title    string    `json:"title"`
	Mentions int       `json:"mentions"`
	Top      []Mention `json:"top" jsonschema:"the most engaged mentions, at most five"`
}

// Author is someone whose first shown mention fell in the period.
type Author struct {
	Network  Network `json:"network"`
	Handle   string  `json:"handle"`
	Name     string  `json:"name"`
	Profile  string  `json:"profile"`
	Mentions int     `json:"mentions" jsonschema:"their mentions in the period"`
}

// Digest is one period's mentions first seen in it, without the accounts
// left out now. It is data only: a written summary, when one comes, reads
// this.
type Digest struct {
	Cadence  Cadence   `json:"cadence" jsonschema:"daily or weekly"`
	From     string    `json:"from" jsonschema:"the period's first day, in the instance's time zone"`
	To       string    `json:"to" jsonschema:"the period's last day"`
	StartsAt time.Time `json:"starts_at"`
	EndsAt   time.Time `json:"ends_at"`
	Finished bool      `json:"finished" jsonschema:"false while the period is still running"`
	Mentions int       `json:"mentions" jsonschema:"mentions first seen in the period, shown"`
	Hidden   int       `json:"hidden" jsonschema:"mentions first seen in the period by left-out accounts"`
	// Networks has each network with a mention, most mentions first.
	Networks          []NetworkDigest `json:"networks"`
	NewAuthors        []Author        `json:"new_authors" jsonschema:"accounts whose first shown mention fell in the period, most mentions first"`
	Calls             int             `json:"calls" jsonschema:"Treg search calls made in the period"`
	SpendUSD          float64         `json:"spend_usd"`
	SpendMissingCalls int             `json:"spend_missing_calls" jsonschema:"Treg calls in the period that reported no cost, left out of spend_usd rather than counted as $0.00; direct calls are a known 0 and never counted here"`
}

const topPerNetwork = 5

// Digest is the digest for the period q names, or the latest one that has
// ended.
func (s *Service) Digest(ctx context.Context, q DigestQuery) (Digest, error) {
	st, err := s.settings(ctx)
	if err != nil {
		return Digest{}, err
	}
	p := st.latestFinished(s.now())
	if q.Day != "" {
		day, err := time.ParseInLocation(time.DateOnly, q.Day, st.loc)
		if err != nil {
			return Digest{}, fmt.Errorf("%w: day must be a day such as 2026-11-18, got %q", core.ErrInvalid, q.Day)
		}
		p = st.periodAt(day)
	}
	return s.digest(ctx, p)
}

func (s *Service) digest(ctx context.Context, p period) (Digest, error) {
	d := Digest{Cadence: p.cadence, From: p.from(), To: p.to(), StartsAt: p.start, EndsAt: p.end, Finished: !p.end.After(s.now()),
		Networks: []NetworkDigest{}, NewAuthors: []Author{}}
	rows, err := s.db.Query(ctx, `SELECT `+mentionColumns+mentionFrom+`WHERE m.first_seen_at >= $1 AND m.first_seen_at < $2
		ORDER BY m.engagement DESC, m.posted_at DESC, m.id DESC`, p.start, p.end)
	if err != nil {
		return d, err
	}
	mentions, err := pgx.CollectRows(rows, scanMention)
	if err != nil {
		return d, err
	}
	rows, err = s.db.Query(ctx, `SELECT m.network, m.author_handle, max(m.author_name), count(*)
		FROM social_mention m LEFT JOIN social_left_out l ON l.network = m.network AND l.handle = m.author_handle
		WHERE l.id IS NULL AND m.author_handle <> '' AND m.first_seen_at >= $1 AND m.first_seen_at < $2
			AND NOT EXISTS (SELECT 1 FROM social_mention e WHERE e.network = m.network AND e.author_handle = m.author_handle AND e.first_seen_at < $1)
		GROUP BY m.network, m.author_handle ORDER BY count(*) DESC, m.author_handle`, p.start, p.end)
	if err != nil {
		return d, err
	}
	authors, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Author, error) {
		var a Author
		err := r.Scan(&a.Network, &a.Handle, &a.Name, &a.Mentions)
		if sp, ok := lookup(a.Network); ok {
			a.Profile = sp.Profile + a.Handle
		}
		return a, err
	})
	if err != nil {
		return d, err
	}
	if len(authors) > 0 {
		d.NewAuthors = authors
	}
	if err := s.db.QueryRow(ctx, `SELECT count(*), coalesce(sum(cost_usd), 0)::float8, count(*) FILTER (WHERE status = 'ok' AND cost_usd IS NULL)
		FROM social_call WHERE called_at >= $1 AND called_at < $2`, p.start, p.end).Scan(&d.Calls, &d.SpendUSD, &d.SpendMissingCalls); err != nil {
		return d, err
	}
	group(&d, mentions)
	return d, nil
}

// group counts a period's mentions by network and keeps each network's
// most engaged ones; mentions arrive most engaged first.
func group(d *Digest, mentions []Mention) {
	byNetwork := map[Network]*NetworkDigest{}
	for _, m := range mentions {
		if m.Hidden {
			d.Hidden++
			continue
		}
		d.Mentions++
		nd := byNetwork[m.Network]
		if nd == nil {
			sp, _ := lookup(m.Network)
			nd = &NetworkDigest{Network: m.Network, Title: sp.Title, Top: []Mention{}}
			byNetwork[m.Network] = nd
		}
		nd.Mentions++
		if len(nd.Top) < topPerNetwork {
			nd.Top = append(nd.Top, m)
		}
	}
	for _, n := range registry {
		if nd := byNetwork[n.Network]; nd != nil {
			d.Networks = append(d.Networks, *nd)
		}
	}
	slices.SortStableFunc(d.Networks, func(a, b NetworkDigest) int { return cmp.Compare(b.Mentions, a.Mentions) })
}

// payload is the digest as the social_digest event carries it.
func (d Digest) payload() notify.SocialDigestPayload {
	p := notify.SocialDigestPayload{Cadence: string(d.Cadence), From: d.From, To: d.To, Mentions: d.Mentions, Hidden: d.Hidden,
		Networks: []notify.SocialNetworkDigest{}, NewAuthors: []string{}, NewAuthorsCount: len(d.NewAuthors), SpendUSD: d.SpendUSD, SpendMissingCalls: d.SpendMissingCalls}
	for _, n := range d.Networks {
		nd := notify.SocialNetworkDigest{Network: string(n.Network), Title: n.Title, Mentions: n.Mentions, Top: []notify.SocialPost{}}
		for _, m := range n.Top[:min(3, len(n.Top))] {
			nd.Top = append(nd.Top, notify.SocialPost{Author: authorLabel(m.Network, m.AuthorHandle, m.AuthorName), Text: excerpt(m.Text, 140), URL: m.URL, Engagement: m.Engagement.Total})
		}
		p.Networks = append(p.Networks, nd)
	}
	for _, a := range d.NewAuthors[:min(10, len(d.NewAuthors))] {
		p.NewAuthors = append(p.NewAuthors, authorLabel(a.Network, a.Handle, a.Name))
	}
	return p
}

// authorLabel is how a message names an author: @handle on X, u/handle on
// Reddit, the name on LinkedIn, whose handles are profile slugs.
func authorLabel(n Network, handle, name string) string {
	switch {
	case n == LinkedIn && name != "":
		return name
	case handle == "":
		return name
	case n == YouTube && name != "":
		return name
	case n == X, n == Threads, n == Bluesky:
		return "@" + handle
	case n == Reddit:
		return "u/" + handle
	}
	return handle
}

func excerpt(s string, n int) string {
	r := []rune(s)
	for i, c := range r {
		if c == '\n' {
			r = r[:i]
			break
		}
	}
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n-1]) + "…"
}
