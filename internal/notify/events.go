package notify

import (
	"encoding/json"
	"fmt"
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
)

// Audience is who may read an event's alerts in the app.
type Audience string

const (
	// Readers is anyone with results:read.
	Readers Audience = "results:read"
	// Admins is a signed-in admin only.
	Admins Audience = "admin"
)

// Payload is what one kind of event carries. Each kind has its own payload
// type, and its message is what every channel sends: a title, a few short
// lines and the page it links to.
type Payload interface {
	Kind() Kind
	message() Message
}

// Message is an event as people read it. Path is the page it links to,
// relative to PUBLIC_URL.
type Message struct {
	Title string   `json:"title"`
	Lines []string `json:"lines"`
	Path  string   `json:"path"`
}

// Event is something that happened. Subject names what it happened to, such
// as "run/12"; with the payload's kind it is the event's natural key, so
// emitting the same event twice stores and sends it once.
type Event struct {
	Subject string
	Payload Payload
}

// KindInfo describes one kind of event, as Settings > Notifications lists
// them for a channel to subscribe to.
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
	kind[CheckCompletedPayload](CheckCompleted, "Check completed", "Gains, losses and moves since the previous check, with the questions that changed most.", Readers),
	kind[CheckRefusedPayload](CheckRefused, "Check refused", "Treg refused a call, for example for an empty balance, and the check stopped.", Readers),
	kind[CheckFailedPayload](CheckFailed, "Check failed", "A check could not run or could not store its answers.", Readers),
	kind[SpendCapReachedPayload](SpendCapReached, "Spend cap reached", "A check stopped queueing calls at the per-check or monthly spend cap.", Readers),
	kind[PairPausedPayload](PairPaused, "Model paused", "A model left its surface's live model list, so Byname stopped asking it.", Readers),
	kind[GoalReachedPayload](GoalReached, "Goal reached", "A search goal reached its target.", Readers),
	kind[SearchMovedPayload](SearchMoved, "Search moved", "Clicks, impressions or position moved notably in Search Console.", Readers),
	kind[ChangeVerdictPayload](ChangeVerdict, "Change verdict", "A site change got its interim read or final verdict against the control pages.", Readers),
	kind[TokenExpiringPayload](TokenExpiring, "Token expiring", "The GitHub token for site changes expires soon.", Admins),
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
	m := Message{Path: "/runs", Lines: []string{fmt.Sprintf("Named in %d of %d answers.", p.Named, p.Answered)}}
	if p.PreviousRunID == 0 {
		m.Title = fmt.Sprintf("Check #%d finished, the first to compare against", p.RunID)
		return m
	}
	m.Path = fmt.Sprintf("/compare?before=%d&after=%d", p.PreviousRunID, p.RunID)
	if p.Gained+p.Lost+p.Moved == 0 {
		m.Title = fmt.Sprintf("Check #%d: no change", p.RunID)
	} else {
		m.Title = fmt.Sprintf("Check #%d: %d gained, %d lost", p.RunID, p.Gained, p.Lost)
	}
	m.Lines = append(m.Lines, fmt.Sprintf("Since check #%d: %d gained, %d lost, %d moved, net %+d.", p.PreviousRunID, p.Gained, p.Lost, p.Moved, p.Net))
	for _, q := range p.Top {
		var parts []string
		for _, c := range []struct {
			n    int
			verb string
		}{{q.Gained, "gained"}, {q.Lost, "lost"}, {q.Moved, "moved"}} {
			if c.n > 0 {
				parts = append(parts, fmt.Sprintf("%d %s", c.n, c.verb))
			}
		}
		m.Lines = append(m.Lines, fmt.Sprintf("%s: %s", strings.Join(parts, ", "), q.Question))
	}
	return m
}

type CheckRefusedPayload struct {
	RunID   int64  `json:"run_id"`
	Code    string `json:"code" jsonschema:"Treg's error code, such as insufficient_balance"`
	Message string `json:"message"`
}

func (CheckRefusedPayload) Kind() Kind { return CheckRefused }

func (p CheckRefusedPayload) message() Message {
	return Message{Title: fmt.Sprintf("Check #%d stopped: Treg refused a call", p.RunID), Lines: []string{p.Message}, Path: "/runs"}
}

type CheckFailedPayload struct {
	RunID   int64  `json:"run_id"`
	Message string `json:"message"`
}

func (CheckFailedPayload) Kind() Kind { return CheckFailed }

func (p CheckFailedPayload) message() Message {
	return Message{Title: fmt.Sprintf("Check #%d failed", p.RunID), Lines: []string{p.Message}, Path: "/runs"}
}

type SpendCapReachedPayload struct {
	RunID    int64   `json:"run_id"`
	SpendUSD float64 `json:"spend_usd"`
	Message  string  `json:"message"`
}

func (SpendCapReachedPayload) Kind() Kind { return SpendCapReached }

func (p SpendCapReachedPayload) message() Message {
	return Message{Title: fmt.Sprintf("Check #%d reached its spend cap", p.RunID), Lines: []string{p.Message}, Path: "/runs"}
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
	return Message{Title: fmt.Sprintf("Paused %s on %s", p.Model, title), Lines: []string{p.Reason}, Path: "/settings/surfaces"}
}

// GoalReachedPayload is declared for the Search Console workstream to emit.
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
		Title: fmt.Sprintf("Goal reached: %s", p.Metric),
		Lines: []string{fmt.Sprintf("%g over %d days, against a target of %g due %s.", p.Value, p.WindowDays, p.Target, p.DueOn)},
		Path:  "/search",
	}
}

// SearchMovedPayload is declared for the Search Console workstream to emit.
type SearchMovedPayload struct {
	Metric     string  `json:"metric"`
	WindowDays int     `json:"window_days"`
	Before     float64 `json:"before"`
	After      float64 `json:"after"`
}

func (SearchMovedPayload) Kind() Kind { return SearchMoved }

func (p SearchMovedPayload) message() Message {
	return Message{
		Title: fmt.Sprintf("Search %s moved", p.Metric),
		Lines: []string{fmt.Sprintf("%g to %g over the last %d days.", p.Before, p.After, p.WindowDays)},
		Path:  "/search",
	}
}

// ChangeVerdictPayload is declared for the site changes workstream to emit.
type ChangeVerdictPayload struct {
	ChangeID int64  `json:"change_id"`
	URL      string `json:"url"`
	Verdict  string `json:"verdict"`
}

func (ChangeVerdictPayload) Kind() Kind { return ChangeVerdict }

func (p ChangeVerdictPayload) message() Message {
	return Message{Title: fmt.Sprintf("Change verdict: %s", p.Verdict), Lines: []string{p.URL}, Path: "/changes"}
}

// TokenExpiringPayload is declared for the site changes workstream to emit.
type TokenExpiringPayload struct {
	Provider  string    `json:"provider"`
	Repo      string    `json:"repo"`
	ExpiresAt time.Time `json:"expires_at"`
}

func (TokenExpiringPayload) Kind() Kind { return TokenExpiring }

func (p TokenExpiringPayload) message() Message {
	return Message{
		Title: fmt.Sprintf("The %s token for %s expires %s", p.Provider, p.Repo, p.ExpiresAt.UTC().Format(time.DateOnly)),
		Lines: []string{"Paste a new token under Settings before it expires, or site changes stop updating."},
		Path:  "/settings",
	}
}

// testPayload is what Send test delivers. It is never stored as an alert.
type testPayload struct{}

func (testPayload) Kind() Kind { return "test" }

func (testPayload) message() Message {
	return Message{
		Title: "Test notification from Byname",
		Lines: []string{"This channel works. Alerts for the events it subscribes to will arrive here."},
		Path:  "/settings/notifications",
	}
}
