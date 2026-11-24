package changes

import (
	"math"
	"time"

	"github.com/unkeyed/byname/internal/search"
)

// Outcome is what a read concluded.
type Outcome string

const (
	Improved         Outcome = "improved"
	Worsened         Outcome = "worsened"
	NoChange         Outcome = "no_change"
	InsufficientData Outcome = "insufficient_data"
)

// ReadKind says whether a read is the provisional one on interim_on or the
// final one on verdict_on.
type ReadKind string

const (
	Interim ReadKind = "interim"
	Final   ReadKind = "final"
)

// Thresholds are how far a page must move against the control pages for a
// metric to count, from instance_settings.
type Thresholds struct {
	Position         float64 `json:"position" jsonschema:"places of average position, default 2.5"`
	CTRPoints        float64 `json:"ctr_points" jsonschema:"percentage points of CTR, default 0.8"`
	ImpressionsRatio float64 `json:"impressions_ratio" jsonschema:"impressions ratio against the controls, default 2.7; its inverse counts as worse"`
}

// Window is one side of a read: the changed page's numbers and the control
// set's, summed over the same final days.
type Window struct {
	From    string        `json:"from"`
	To      string        `json:"to"`
	Days    int           `json:"days"`
	Page    search.Totals `json:"page"`
	Control search.Totals `json:"control"`
}

// Deltas are the differences in differences: how the page moved minus how
// the control set moved over the same days.
type Deltas struct {
	Position         float64 `json:"position" jsonschema:"change in average position against the controls, in places; negative means the page moved up"`
	CTRPoints        float64 `json:"ctr_points" jsonschema:"change in CTR against the controls, in percentage points"`
	ImpressionsRatio float64 `json:"impressions_ratio" jsonschema:"the page's after over before impressions, divided by the controls'; above 1 means it gained on them"`
}

// Signal is one metric that crossed its threshold.
type Signal struct {
	Metric string  `json:"metric" jsonschema:"position, ctr or impressions"`
	Way    Outcome `json:"way" jsonschema:"improved or worsened"`
}

// Read is one difference-in-differences check of a change.
type Read struct {
	Kind    ReadKind `json:"kind"`
	ReadOn  string   `json:"read_on" jsonschema:"the day the read was taken, in the instance's time zone"`
	Outcome Outcome  `json:"outcome"`
	// Reason says why the data was insufficient; empty otherwise.
	Reason     string     `json:"reason"`
	Before     Window     `json:"before"`
	After      Window     `json:"after"`
	Deltas     *Deltas    `json:"deltas" jsonschema:"null when the data was insufficient"`
	Signals    []Signal   `json:"signals"`
	Controls   []string   `json:"controls" jsonschema:"the control pages the read compared against"`
	Thresholds Thresholds `json:"thresholds"`
}

// minWindowDays is the shortest after window a read accepts: a week, so
// every weekday is in both windows.
const minWindowDays = 7

// pacific is Search Console's day: every date it reports is a Pacific Time
// calendar day, so merges and crawls are placed on Pacific days too.
var pacific = func() *time.Location {
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		return time.FixedZone("PT", -8*3600)
	}
	return loc
}()

// searchDay is the Search Console date t falls on, as midnight UTC.
func searchDay(t time.Time) time.Time {
	y, m, d := t.In(pacific).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// windows places a read's before and after windows; see assess for the
// method. days is 0 when no final day follows the crawl yet.
func windows(mergedAt, crawledAt time.Time, length int, latestFinal time.Time) (before, after [2]time.Time, days int) {
	crawl := searchDay(crawledAt)
	afterFrom, afterTo := crawl.AddDate(0, 0, 1), crawl.AddDate(0, 0, length)
	if latestFinal.Before(afterTo) {
		afterTo = latestFinal
	}
	days = int(afterTo.Sub(afterFrom).Hours()/24) + 1
	merge := searchDay(mergedAt)
	beforeTo := merge.AddDate(0, 0, -1)
	return [2]time.Time{beforeTo.AddDate(0, 0, 1-max(days, 1)), beforeTo}, [2]time.Time{afterFrom, afterTo}, max(days, 0)
}

// sum totals the final days among days that fall in w. Partial days never
// count, and a day without a row had no impressions.
func sum(days []search.Day, w [2]time.Time) search.Totals {
	var t search.Totals
	var weighted float64
	from, to := w[0].Format(time.DateOnly), w[1].Format(time.DateOnly)
	for _, d := range days {
		if !d.Final || d.Date < from || d.Date > to {
			continue
		}
		t.Clicks += d.Clicks
		t.Impressions += d.Impressions
		weighted += d.Position * float64(d.Impressions)
	}
	if t.Impressions > 0 {
		t.CTR = float64(t.Clicks) / float64(t.Impressions)
		t.Position = weighted / float64(t.Impressions)
	}
	return t
}

// sample is the numbers a read compares: the page and the control set,
// before and after.
type sample struct {
	before, after Window
	// firstDay is the earliest day Search Console data is stored for.
	firstDay string
	controls int
}

// assess is the verdict method, and the one place it is defined.
//
// Windows. Search Console reports Pacific Time days, so merges and crawls
// are placed on Pacific days. Let crawl be the day of the first crawl
// after the merge, merge the day of the merge, and L the read's length:
// the interim days (default 17) for the interim read, the verdict days
// (default 31) for the final one. The after window runs from crawl+1 to
// crawl+L, cut short at the latest day Search Console has finalized; n is
// its length in days. The before window is the n days that end on
// merge-1. Days from the merge to the crawl are in neither window: the
// page had changed, but Google had not seen it.
//
// Totals. Only days Search Console marks final count; a day the page has
// no row for had no impressions. For the changed page and for the control
// set (every control page whose hold date is on or after the read day,
// the changed page left out) clicks and impressions are summed over each
// window, position is the impression-weighted mean of the daily average
// positions, and CTR is clicks over impressions.
//
// Insufficient data. The read is insufficient_data, with a reason, when n
// is under a week, when the stored history does not reach back to the
// start of the before window, when there are no control pages, or when the
// page or the control set had no impressions in either window.
//
// Deltas, each the page's change minus the control set's:
//   - position: (page after - page before) - (control after - control
//     before), in places; negative means the page moved up;
//   - CTR: the same over CTR, in percentage points;
//   - impressions: (page after / page before) / (control after / control
//     before), a ratio; above 1 means the page gained on the controls.
//
// Signals. Position improved at -T or less and worsened at +T or more (T
// default 2.5). CTR improved at +T points or more and worsened at -T or
// less (default 0.8). Impressions improved at a ratio of T or more and
// worsened at 1/T or less (default 2.7).
//
// Outcome. improved when at least one signal improved and none worsened,
// worsened when at least one worsened and none improved, and no_change
// when no signal fired or they disagree.
func assess(s sample, t Thresholds) (Outcome, string, *Deltas, []Signal) {
	b, a := s.before, s.after
	switch {
	case a.Days < minWindowDays:
		return InsufficientData, "fewer than 7 days of final Google data since Google saw the change", nil, []Signal{}
	case s.firstDay == "" || b.From < s.firstDay:
		return InsufficientData, "Search Console history does not reach back to " + b.From, nil, []Signal{}
	case s.controls == 0:
		return InsufficientData, "no comparison pages to measure against", nil, []Signal{}
	case b.Page.Impressions == 0 || a.Page.Impressions == 0:
		return InsufficientData, "the page did not appear in Google results before or after the change", nil, []Signal{}
	case b.Control.Impressions == 0 || a.Control.Impressions == 0:
		return InsufficientData, "the comparison pages did not appear in Google results before or after the change", nil, []Signal{}
	}
	d := &Deltas{
		Position:  round((a.Page.Position - b.Page.Position) - (a.Control.Position - b.Control.Position)),
		CTRPoints: round(100 * ((a.Page.CTR - b.Page.CTR) - (a.Control.CTR - b.Control.CTR))),
		ImpressionsRatio: round((float64(a.Page.Impressions) / float64(b.Page.Impressions)) /
			(float64(a.Control.Impressions) / float64(b.Control.Impressions))),
	}
	signals := []Signal{}
	add := func(metric string, better, worse bool) {
		switch {
		case better:
			signals = append(signals, Signal{metric, Improved})
		case worse:
			signals = append(signals, Signal{metric, Worsened})
		}
	}
	add("position", d.Position <= -t.Position, d.Position >= t.Position)
	add("ctr", d.CTRPoints >= t.CTRPoints, d.CTRPoints <= -t.CTRPoints)
	add("impressions", d.ImpressionsRatio >= t.ImpressionsRatio, d.ImpressionsRatio <= 1/t.ImpressionsRatio)
	ways := map[Outcome]int{}
	for _, sg := range signals {
		ways[sg.Way]++
	}
	switch {
	case ways[Improved] > 0 && ways[Worsened] == 0:
		return Improved, "", d, signals
	case ways[Worsened] > 0 && ways[Improved] == 0:
		return Worsened, "", d, signals
	}
	return NoChange, "", d, signals
}

// round keeps four decimal places, so stored deltas compare exactly with
// the thresholds people read.
func round(v float64) float64 { return math.Round(v*1e4) / 1e4 }
