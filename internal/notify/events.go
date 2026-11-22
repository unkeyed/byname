package notify

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/unkeyed/byname/internal/engines"
)

// Kind is one kind of event. The registry below is the only list of them.
type Kind string

const (
	CheckCompleted  Kind = "check_completed"
	CheckRefused    Kind = "check_refused"
	CheckFailed     Kind = "check_failed"
	SpendCapReached Kind = "spend_cap_reached"
	PairPaused      Kind = "pair_paused"
	GoalReached     Kind = "goal_reached"
	SearchMoved     Kind = "search_moved"
	ChangeVerdict   Kind = "change_verdict"
	TokenExpiring   Kind = "token_expiring"
	SocialDigest    Kind = "social_digest"
)

// Audience is who may read an event's notifications in the app.
type Audience string

const (
	// Readers is anyone with results:read.
	Readers Audience = "results:read"
	// Admins is a signed-in admin only.
	Admins Audience = "admin"
)

// Payload is what one kind of event carries. Each kind has its own payload
// type, and its message is what every channel sends: a title, a few short
// lines, and the page it links to with the words for that link.
type Payload interface {
	Kind() Kind
	message() Message
}

// Message is an event as people read it. Path is the page it links to,
// relative to PUBLIC_URL, and Action is that link's words, such as "See
// what changed".
type Message struct {
	Title  string
	Lines  []string
	Path   string
	Action string
}

// NotificationsPath and SettingsPath are the app's notifications page and
// the settings page that chooses where notifications go.
const (
	NotificationsPath = "/notifications"
	SettingsPath      = "/settings/notifications"
)

// Event is something that happened. Subject names what it happened to, such
// as "run/12"; with the payload's kind it is the event's natural key, so
// emitting the same event twice stores and sends it once.
type Event struct {
	Subject string
	Payload Payload
}

// KindInfo describes one kind of event, as Settings > Notifications lists
// them to choose which notifications go where.
type KindInfo struct {
	Kind        Kind     `json:"kind"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Audience    Audience `json:"audience" jsonschema:"results:read when anyone who reads results sees it in the app, admin when only admins do"`
	decode      func(json.RawMessage) (Payload, error)
}

func kind[P Payload](k Kind, title, description string, audience Audience) KindInfo {
	return KindInfo{Kind: k, Title: title, Description: description, Audience: audience, decode: func(raw json.RawMessage) (Payload, error) {
		var p P
		err := json.Unmarshal(raw, &p)
		return p, err
	}}
}

// registry is every event kind, in the order Settings lists them. A kind
// another package emits later is declared here with its payload type first.
var registry = []KindInfo{
	kind[CheckCompletedPayload](CheckCompleted, "Check completed", "Which answers started or stopped naming you since the previous check, and the questions that changed most.", Readers),
	kind[CheckRefusedPayload](CheckRefused, "Check refused", "Treg, the service that runs each question and bills you per answer, refused a request, for example for an empty balance, and the check stopped.", Readers),
	kind[CheckFailedPayload](CheckFailed, "Check failed", "A check could not start, or could not save its answers.", Readers),
	kind[SpendCapReachedPayload](SpendCapReached, "Spend cap reached", "A check stopped asking questions when it reached the spend cap for one check or for the month.", Readers),
	kind[PairPausedPayload](PairPaused, "Model paused", "An AI app stopped offering a model you picked, so Byname stopped asking it.", Readers),
	kind[GoalReachedPayload](GoalReached, "Goal reached", "A Google search goal reached its target.", Readers),
	kind[SearchMovedPayload](SearchMoved, "Google clicks moved", "Google clicks, or clicks from searches without your name, over the last 28 days moved by at least a quarter and by at least 20 clicks against the 28 days before.", Readers),
	kind[ChangeVerdictPayload](ChangeVerdict, "Change result", "A site change got its final result, compared with your comparison pages.", Readers),
	kind[TokenExpiringPayload](TokenExpiring, "Token expiring", "The GitHub token for site changes expires soon.", Admins),
	kind[SocialDigestPayload](SocialDigest, "Social digest", "The day's or week's posts that name you on social networks, without the accounts you left out.", Readers),
}

// Kinds is every event kind Byname can raise, in display order.
func Kinds() []KindInfo { return append([]KindInfo(nil), registry...) }

func lookup(k Kind) (KindInfo, bool) {
	for _, info := range registry {
		if info.Kind == k {
			return info, true
		}
	}
	return KindInfo{}, false
}

// QuestionChange is how one question moved across its pairs.
type QuestionChange struct {
	QuestionID int64  `json:"question_id"`
	Question   string `json:"question"`
	Gained     int    `json:"gained"`
	Lost       int    `json:"lost"`
	Moved      int    `json:"moved"`
}

// CheckCompletedPayload is a finished check compared with the previous one,
// the same comparison GET /v1/compare returns for the two runs.
type CheckCompletedPayload struct {
	RunID int64 `json:"run_id"`
	// PreviousRunID is 0 for the first check with answers.
	PreviousRunID int64 `json:"previous_run_id"`
	Gained        int   `json:"gained"`
	Lost          int   `json:"lost"`
	Moved         int   `json:"moved"`
	Unchanged     int   `json:"unchanged"`
	NotComparable int   `json:"not_comparable"`
	Net           int   `json:"net"`
	// Named of Answered is the headline: question and pair answers that
	// named you, of those that did not fail.
	Named    int     `json:"named"`
	Answered int     `json:"answered"`
	SpendUSD float64 `json:"spend_usd"`
	// Top is the questions that changed most, at most topQuestions.
	Top []QuestionChange `json:"top"`
}

func (CheckCompletedPayload) Kind() Kind { return CheckCompleted }

func (p CheckCompletedPayload) message() Message {
	m := Message{Path: "/", Action: "Open This week", Lines: []string{fmt.Sprintf("AI apps named you in %d of %d answers.", p.Named, p.Answered)}}
	if p.PreviousRunID == 0 {
		m.Title = fmt.Sprintf("Check #%d finished, the first to compare against", p.RunID)
		return m
	}
	m.Path, m.Action = fmt.Sprintf("/ai/what-changed?before=%d&after=%d", p.PreviousRunID, p.RunID), "See what changed"
	changes := changeParts(p.Gained, p.Lost, p.Moved, "answer", "answers")
	if len(changes) == 0 {
		m.Title = fmt.Sprintf("Check #%d: no change", p.RunID)
		m.Lines = append(m.Lines, fmt.Sprintf("No answer started or stopped naming you since check #%d.", p.PreviousRunID))
		return m
	}
	m.Title = fmt.Sprintf("Check #%d: %s", p.RunID, strings.Join(changes[:min(2, len(changes))], ", "))
	m.Lines = append(m.Lines, fmt.Sprintf("Since check #%d, %s.", p.PreviousRunID, sentenceList(changes)))
	for _, q := range p.Top {
		m.Lines = append(m.Lines, fmt.Sprintf("“%s”: %s.", q.Question, sentenceList(changeParts(q.Gained, q.Lost, q.Moved, "AI app", "AI apps"))))
	}
	return m
}

// changeParts says how many answers, or AI apps, started naming you,
// stopped, or put you in a different position, leaving out the zeros.
func changeParts(gained, lost, moved int, one, many string) []string {
	var parts []string
	for _, c := range []struct {
		n    int
		what string
	}{{gained, "started naming you"}, {lost, "stopped naming you"}, {moved, "moved your position"}} {
		if c.n > 0 {
			parts = append(parts, fmt.Sprintf("%s %s", count(c.n, one, many), c.what))
		}
	}
	return parts
}

func count(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// sentenceList joins items as a sentence does: "a", "a and b", "a, b and c".
func sentenceList(items []string) string {
	if len(items) < 2 {
		return strings.Join(items, "")
	}
	return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
}

type CheckRefusedPayload struct {
	RunID   int64  `json:"run_id"`
	Code    string `json:"code" jsonschema:"Treg's error code, such as insufficient_balance"`
	Message string `json:"message"`
}

func (CheckRefusedPayload) Kind() Kind { return CheckRefused }

func (p CheckRefusedPayload) message() Message {
	return Message{Title: fmt.Sprintf("Check #%d stopped: Treg refused a request", p.RunID), Lines: []string{p.Message, "Treg is the service that runs each question and bills you per answer. Fix what it reports, such as an empty balance, then press Check now."}, Path: "/settings/checks", Action: "Open check history"}
}

type CheckFailedPayload struct {
	RunID   int64  `json:"run_id"`
	Message string `json:"message"`
}

func (CheckFailedPayload) Kind() Kind { return CheckFailed }

func (p CheckFailedPayload) message() Message {
	return Message{Title: fmt.Sprintf("Check #%d failed", p.RunID), Lines: []string{p.Message}, Path: "/settings/checks", Action: "Open check history"}
}

type SpendCapReachedPayload struct {
	RunID    int64   `json:"run_id"`
	SpendUSD float64 `json:"spend_usd"`
	Message  string  `json:"message"`
}

func (SpendCapReachedPayload) Kind() Kind { return SpendCapReached }

func (p SpendCapReachedPayload) message() Message {
	return Message{Title: fmt.Sprintf("Check #%d reached its spend cap", p.RunID), Lines: []string{p.Message}, Path: "/settings/schedule", Action: "Open schedule and limits"}
}

type PairPausedPayload struct {
	PairID  int64           `json:"pair_id"`
	Surface engines.Surface `json:"surface"`
	Model   string          `json:"model"`
	Reason  string          `json:"reason"`
}

func (PairPausedPayload) Kind() Kind { return PairPaused }

func (p PairPausedPayload) message() Message {
	title := string(p.Surface)
	if spec, ok := engines.Lookup(p.Surface); ok {
		title = spec.Title
	}
	return Message{Title: fmt.Sprintf("Paused %s on %s", p.Model, title), Lines: []string{p.Reason}, Path: "/settings/ai-apps", Action: "Open AI apps settings"}
}

// metricLabels names Search Console metrics in messages.
var metricLabels = map[string]string{"clicks": "Google clicks", "impressions": "Appearances in Google results", "non_brand_clicks": "Clicks from searches without your name"}

func metricLabel(m string) string {
	if l, ok := metricLabels[m]; ok {
		return l
	}
	return m
}

// GoalReachedPayload is a search goal whose progress first met its target
// (internal/gsc).
type GoalReachedPayload struct {
	GoalID     int64   `json:"goal_id"`
	Metric     string  `json:"metric"`
	WindowDays int     `json:"window_days"`
	Target     float64 `json:"target"`
	Value      float64 `json:"value"`
	DueOn      string  `json:"due_on"`
}

func (GoalReachedPayload) Kind() Kind { return GoalReached }

func (p GoalReachedPayload) message() Message {
	return Message{
		Title:  fmt.Sprintf("Goal reached: %s", metricLabel(p.Metric)),
		Lines:  []string{fmt.Sprintf("%s reached %g over the last %d days, against a target of %g by %s.", metricLabel(p.Metric), p.Value, p.WindowDays, p.Target, p.DueOn)},
		Path:   "/google/traffic",
		Action: "Open Google traffic",
	}
}

// SearchMovedPayload is a 28-day Search Console metric that moved against
// the 28 days before (internal/gsc).
type SearchMovedPayload struct {
	Metric     string  `json:"metric"`
	WindowDays int     `json:"window_days"`
	Before     float64 `json:"before"`
	After      float64 `json:"after"`
}

func (SearchMovedPayload) Kind() Kind { return SearchMoved }

func (p SearchMovedPayload) message() Message {
	way := "rose"
	if p.After < p.Before {
		way = "fell"
	}
	return Message{
		Title:  fmt.Sprintf("%s %s over the last %d days", metricLabel(p.Metric), way, p.WindowDays),
		Lines:  []string{fmt.Sprintf("%g, against %g in the %d days before.", p.After, p.Before, p.WindowDays)},
		Path:   "/google/traffic",
		Action: "Open Google traffic",
	}
}

// ChangeVerdictPayload is a change's final result against the comparison
// pages (internal/changes). Detail is the result in one line.
type ChangeVerdictPayload struct {
	ChangeID int64  `json:"change_id"`
	URL      string `json:"url"`
	Verdict  string `json:"verdict"`
	Detail   string `json:"detail"`
}

func (ChangeVerdictPayload) Kind() Kind { return ChangeVerdict }

func (p ChangeVerdictPayload) message() Message {
	lines := []string{p.URL}
	if p.Detail != "" {
		lines = append(lines, p.Detail)
	}
	label, ok := verdictLabels[p.Verdict]
	if !ok {
		label = strings.ReplaceAll(p.Verdict, "_", " ")
	}
	return Message{Title: "Change result: " + label, Lines: lines, Path: fmt.Sprintf("/google/site-changes?chg=%d", p.ChangeID), Action: "Open the change"}
}

// verdictLabels says each final result (internal/changes' Outcome) in plain words.
var verdictLabels = map[string]string{"improved": "helped", "worsened": "hurt", "no_change": "no clear change", "insufficient_data": "not enough data"}

// TokenExpiringPayload is the GitHub token for site changes nearing its
// expiry (internal/changes).
type TokenExpiringPayload struct {
	Provider  string    `json:"provider"`
	Repo      string    `json:"repo"`
	ExpiresAt time.Time `json:"expires_at"`
}

func (TokenExpiringPayload) Kind() Kind { return TokenExpiring }

func (p TokenExpiringPayload) message() Message {
	return Message{
		Title:  fmt.Sprintf("The %s token for %s expires %s", p.Provider, p.Repo, p.ExpiresAt.UTC().Format(time.DateOnly)),
		Lines:  []string{"Paste a new token under Settings > Site changes before it expires, or site changes stop updating."},
		Path:   "/settings/site-changes",
		Action: "Open site change settings",
	}
}

// SocialPost is one post in a social digest.
type SocialPost struct {
	Author     string `json:"author" jsonschema:"the author's handle"`
	Text       string `json:"text" jsonschema:"the start of the post"`
	URL        string `json:"url"`
	Engagement int    `json:"engagement"`
}

// SocialNetworkDigest is one network's share of a social digest.
type SocialNetworkDigest struct {
	Network  string       `json:"network"`
	Title    string       `json:"title"`
	Mentions int          `json:"mentions"`
	Top      []SocialPost `json:"top" jsonschema:"the most engaged posts, at most three"`
}

// SocialDigestPayload is one finished period's social mentions
// (internal/social), without the accounts left out when it was sent.
type SocialDigestPayload struct {
	Cadence  string                `json:"cadence" jsonschema:"daily or weekly"`
	From     string                `json:"from" jsonschema:"the period's first day"`
	To       string                `json:"to" jsonschema:"the period's last day"`
	Mentions int                   `json:"mentions"`
	Hidden   int                   `json:"hidden" jsonschema:"mentions by left-out accounts, not counted in mentions"`
	Networks []SocialNetworkDigest `json:"networks"`
	// NewAuthors names, at most ten, the accounts whose first mention fell in the period.
	NewAuthors      []string `json:"new_authors"`
	NewAuthorsCount int      `json:"new_authors_count"`
	SpendUSD        float64  `json:"spend_usd"`
	// SpendMissingCalls is calls that reported no cost, which SpendUSD
	// leaves out rather than counting as $0.
	SpendMissingCalls int `json:"spend_missing_calls"`
}

func (SocialDigestPayload) Kind() Kind { return SocialDigest }

func (p SocialDigestPayload) message() Message {
	m := Message{Path: "/social?digest=" + p.From, Action: "See all posts on Social"}
	when := ", " + periodRange(p.From, p.To)
	if p.Cadence == "daily" {
		when = " on " + shortDay(p.From)
	}
	if p.Mentions == 0 {
		m.Title = "No new posts named you" + when
	} else {
		m.Title = count(p.Mentions, "new post", "new posts") + " named you" + when
	}
	if where := p.where(); where != "" {
		m.Lines = append(m.Lines, where)
	}
	if p.NewAuthorsCount > 0 {
		who := count(p.NewAuthorsCount, "person", "people") + " posted about you for the first time"
		names := p.NewAuthors[:min(digestNames, len(p.NewAuthors))]
		switch {
		case len(names) == 0:
		case p.NewAuthorsCount > len(names):
			who += ", among them " + sentenceList(names)
		default:
			who += ": " + sentenceList(names)
		}
		m.Lines = append(m.Lines, who+".")
	}
	if p.Hidden == 1 {
		m.Lines = append(m.Lines, "1 post from an account you left out is not counted.")
	} else if p.Hidden > 1 {
		m.Lines = append(m.Lines, fmt.Sprintf("%d posts from accounts you left out are not counted.", p.Hidden))
	}
	if top := p.top(digestPosts); len(top) > 0 {
		m.Lines = append(m.Lines, "The posts people engaged with most:")
		for _, t := range top {
			by := fmt.Sprintf("%s on %s", t.Author, t.network.Title)
			if t.Engagement > 0 {
				by += ", " + count(t.Engagement, engagementNoun(t.network.Network, true), engagementNoun(t.network.Network, false))
			}
			m.Lines = append(m.Lines, fmt.Sprintf("%s: “%s” %s", by, t.Text, t.URL))
		}
	}
	return m
}

const (
	// digestNames is how many new people a digest names; the rest are a count.
	digestNames = 3
	// digestPosts is how many posts a digest quotes.
	digestPosts = 5
)

// where says, in one sentence, how the period's posts split across networks.
func (p SocialDigestPayload) where() string {
	var active []SocialNetworkDigest
	for _, n := range p.Networks {
		if n.Mentions > 0 {
			active = append(active, n)
		}
	}
	switch {
	case len(active) == 1 && p.Mentions == 1:
		return fmt.Sprintf("It was on %s.", active[0].Title)
	case len(active) == 1:
		return fmt.Sprintf("All %d were on %s.", p.Mentions, active[0].Title)
	case len(active) > 1:
		where := make([]string, len(active))
		for i, n := range active {
			verb := " "
			if i == 0 && n.Mentions == 1 {
				verb = " was "
			} else if i == 0 {
				verb = " were "
			}
			where[i] = fmt.Sprintf("%d%son %s", n.Mentions, verb, n.Title)
		}
		return sentenceList(where) + "."
	}
	return ""
}

type rankedPost struct {
	SocialPost
	network SocialNetworkDigest
}

// top is the period's most engaged posts across every network, at most n.
func (p SocialDigestPayload) top(n int) []rankedPost {
	var all []rankedPost
	for _, net := range p.Networks {
		for _, post := range net.Top {
			all = append(all, rankedPost{post, net})
		}
	}
	slices.SortStableFunc(all, func(a, b rankedPost) int { return b.Engagement - a.Engagement })
	return all[:min(n, len(all))]
}

// periodRange says a period's days as "Nov 9 to 15", or "Nov 28 to Dec 4"
// across a month's end.
func periodRange(from, to string) string {
	f, err1 := time.Parse(time.DateOnly, from)
	t, err2 := time.Parse(time.DateOnly, to)
	if err1 != nil || err2 != nil {
		return from + " to " + to
	}
	if f.Month() == t.Month() {
		return fmt.Sprintf("%s to %d", f.Format("Jan 2"), t.Day())
	}
	return f.Format("Jan 2") + " to " + t.Format("Jan 2")
}

// engagementNouns says what a post's engagement count adds up on each
// network, as internal/social's Engagement.Total does.
var engagementNouns = map[string][2]string{
	"youtube":    {"view", "views"},
	"hackernews": {"point or comment", "points and comments"},
	"reddit":     {"upvote or comment", "upvotes and comments"},
	"linkedin":   {"reaction or comment", "reactions and comments"},
}

func engagementNoun(network string, one bool) string {
	n, ok := engagementNouns[network]
	if !ok {
		n = [2]string{"like, reply or repost", "likes, replies and reposts"}
	}
	if one {
		return n[0]
	}
	return n[1]
}

func shortDay(day string) string {
	t, err := time.Parse(time.DateOnly, day)
	if err != nil {
		return day
	}
	return t.Format("Jan 2")
}

// testPayload is what Send test delivers. It is never stored as a
// notification.
type testPayload struct{}

func (testPayload) Kind() Kind { return "test" }

func (testPayload) message() Message {
	return Message{
		Title:  "Test notification from Byname",
		Lines:  []string{"Notifications reach this place. The ones you chose for it will arrive here."},
		Path:   SettingsPath,
		Action: "Open notification settings",
	}
}
