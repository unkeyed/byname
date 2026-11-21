package changes

import (
	"testing"
	"time"

	"github.com/unkeyed/byname/internal/gsc"
)

var defaults = Thresholds{Position: 2.5, CTRPoints: 0.8, ImpressionsRatio: 2.7}

// side builds a window's totals from clicks, impressions and position.
func side(clicks, impressions int64, position float64) gsc.Totals {
	t := gsc.Totals{Clicks: clicks, Impressions: impressions, Position: position}
	if impressions > 0 {
		t.CTR = float64(clicks) / float64(impressions)
	}
	return t
}

func sampleOf(pageBefore, pageAfter, ctlBefore, ctlAfter gsc.Totals) sample {
	return sample{
		before:   Window{From: "2026-09-01", To: "2026-09-28", Days: 28, Page: pageBefore, Control: ctlBefore},
		after:    Window{From: "2026-10-05", To: "2026-11-01", Days: 28, Page: pageAfter, Control: ctlAfter},
		firstDay: "2026-08-01",
		controls: 3,
	}
}

func TestAssess(t *testing.T) {
	flat := side(280, 28000, 8)
	for _, tc := range []struct {
		name    string
		s       sample
		want    Outcome
		reason  string
		signals int
		pos     float64
		ctr     float64
		ratio   float64
	}{
		{
			// The page climbs from 12 to 6 and triples its impressions while the controls hold still.
			name: "improved", s: sampleOf(side(56, 2800, 12), side(336, 8400, 6), flat, flat),
			want: Improved, signals: 3, pos: -6, ctr: 2, ratio: 3,
		},
		{
			// The page drops from 5 to 9 while the controls hold; CTR falls 1.5 points.
			name: "worsened", s: sampleOf(side(100, 4000, 5), side(40, 4000, 9), flat, flat),
			want: Worsened, signals: 2, pos: 4, ctr: -1.5, ratio: 1,
		},
		{
			// The page gains 2 places, but so do the controls: nothing is the change's doing.
			name: "no change when the controls moved too", s: sampleOf(side(56, 2800, 12), side(56, 2800, 10), side(280, 28000, 10), side(280, 28000, 8)),
			want: NoChange, signals: 0, pos: 0, ctr: 0, ratio: 1,
		},
		{
			// Position improved past the threshold while CTR worsened past it: they disagree.
			name: "no change when signals disagree", s: sampleOf(side(100, 4000, 9), side(40, 4000, 5), flat, flat),
			want: NoChange, signals: 2, pos: -4, ctr: -1.5, ratio: 1,
		},
		{
			name: "insufficient: under a week", s: func() sample { s := sampleOf(flat, flat, flat, flat); s.after.Days, s.before.Days = 5, 5; return s }(),
			want: InsufficientData, reason: "fewer than 7 final days since the first crawl",
		},
		{
			name: "insufficient: no controls", s: func() sample { s := sampleOf(flat, flat, flat, flat); s.controls = 0; return s }(),
			want: InsufficientData, reason: "no control pages to compare against",
		},
		{
			name: "insufficient: page without impressions", s: sampleOf(side(0, 0, 0), flat, flat, flat),
			want: InsufficientData, reason: "the page had no impressions in one of the windows",
		},
		{
			name: "insufficient: history too short", s: func() sample { s := sampleOf(flat, flat, flat, flat); s.firstDay = "2026-09-10"; return s }(),
			want: InsufficientData, reason: "Search Console history does not reach back to 2026-09-01",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, reason, d, signals := assess(tc.s, defaults)
			if got != tc.want || reason != tc.reason {
				t.Fatalf("assess = %s (%q), want %s (%q)", got, reason, tc.want, tc.reason)
			}
			if tc.want == InsufficientData {
				if d != nil {
					t.Fatalf("an insufficient read carries deltas %+v", d)
				}
				return
			}
			if len(signals) != tc.signals || d.Position != tc.pos || d.CTRPoints != tc.ctr || d.ImpressionsRatio != tc.ratio {
				t.Fatalf("deltas %+v signals %v, want position %v, ctr %v, ratio %v and %d signals", d, signals, tc.pos, tc.ctr, tc.ratio, tc.signals)
			}
		})
	}
}

func TestSumCountsFinalDaysInTheWindowOnly(t *testing.T) {
	days := []gsc.Day{
		{Date: "2026-10-31", Totals: side(99, 9900, 1), Final: true}, // before the window
		{Date: "2026-11-01", Totals: side(10, 100, 4), Final: true},
		{Date: "2026-11-02", Totals: side(30, 300, 8), Final: true},
		{Date: "2026-11-03", Totals: side(500, 50000, 1), Final: false}, // partial: Google may still revise it
	}
	got := sum(days, [2]time.Time{time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)})
	if got.Clicks != 40 || got.Impressions != 400 || got.Position != 7 || got.CTR != 0.1 {
		t.Fatalf("sum = %+v, want 40 clicks, 400 impressions, position 7 weighted by impressions, CTR 0.1", got)
	}
}

func TestWindowsAreEqualAndSkipTheDaysBeforeTheCrawl(t *testing.T) {
	// Merged Nov 1, 10:00 Pacific; crawled Nov 4, 23:30 Pacific (Nov 5 UTC).
	merged := time.Date(2026, 11, 1, 17, 0, 0, 0, time.UTC)
	crawled := time.Date(2026, 11, 5, 7, 30, 0, 0, time.UTC)
	day := func(s string) time.Time { t, _ := time.Parse(time.DateOnly, s); return t }
	before, after, n := windows(merged, crawled, 17, day("2026-11-18"))
	if n != 14 || after[0] != day("2026-11-05") || after[1] != day("2026-11-18") || before[0] != day("2026-10-18") || before[1] != day("2026-10-31") {
		t.Fatalf("windows = before %v, after %v, %d days; want Oct 18 to 31 and Nov 5 to 18, 14 days each", before, after, n)
	}
	if _, after, n := windows(merged, crawled, 17, day("2026-12-31")); n != 17 || after[1] != day("2026-11-21") {
		t.Fatalf("with every day final, the after window ends %v after %d days, want Nov 21 after 17", after[1], n)
	}
}

func TestPathRules(t *testing.T) {
	rules, err := compileRules([]Rule{
		{Glob: "content/blog/*.mdx", URL: "https://www.northwind.example/blog/{1}"},
		{Glob: "app/(marketing)/**/page.tsx", URL: "https://www.northwind.example/{1}"},
		{Glob: "docs/**", URL: "https://docs.northwind.example/{1}"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{
		"content/blog/rate-limits.mdx":        "https://www.northwind.example/blog/rate-limits",
		"content/blog/2026/nested.mdx":        "",
		"app/(marketing)/page.tsx":            "https://www.northwind.example/",
		"app/(marketing)/pricing/page.tsx":    "https://www.northwind.example/pricing",
		"app/(marketing)/docs/intro/page.tsx": "https://www.northwind.example/docs/intro",
		"app/(marketing)/pricing/layout.tsx":  "",
		"docs/guides/start.md":                "https://docs.northwind.example/guides/start.md",
		"README.md":                           "",
	} {
		if got := pageFor(rules, path); got != want {
			t.Errorf("%s maps to %q, want %q", path, got, want)
		}
	}
	for _, bad := range [][]Rule{
		{{Glob: "", URL: "https://x.example/"}},
		{{Glob: "/abs/*.md", URL: "https://x.example/{1}"}},
		{{Glob: "a/*.md", URL: "https://x.example/{2}"}},
		{{Glob: "a/*.md", URL: "/relative/{1}"}},
	} {
		if _, err := compileRules(bad); err == nil {
			t.Errorf("rules %+v were accepted", bad)
		}
	}
}
