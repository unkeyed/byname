package search

import (
	"context"
	"fmt"
	"time"

	"github.com/unkeyed/byname/internal/core"
)

// HeadlineDays is the window the headline, top pages and top queries
// default to, and the window brand against non-brand is summed over.
const HeadlineDays = 28

// Terms gives the branded search terms in use, lowercased: config's
// setting, or the brand's name and aliases.
type Terms interface {
	BrandedTerms(ctx context.Context) ([]string, error)
}

// branded is the SQL test for a branded query: it contains a branded term,
// ignoring case. $terms names the parameter holding the lowercased terms.
func branded(terms string) string {
	return `EXISTS (SELECT 1 FROM unnest(` + terms + `::text[]) t WHERE strpos(lower(query), t) > 0)`
}

// avgPosition weights position by impressions over the rows that have one.
const avgPosition = `coalesce(sum(position * impressions) FILTER (WHERE position > 0) / nullif(sum(impressions) FILTER (WHERE position > 0), 0), 0)`

// Totals are clicks, impressions, CTR and position over some days.
type Totals struct {
	Clicks      int64   `json:"clicks"`
	Impressions int64   `json:"impressions"`
	CTR         float64 `json:"ctr" jsonschema:"clicks over impressions, 0 to 1"`
	Position    float64 `json:"position" jsonschema:"average position, weighted by impressions; 0 when there is none, as for Bing's site totals"`
}

// Period is the site's numbers over from to to, inclusive.
type Period struct {
	From string `json:"from"`
	To   string `json:"to"`
	Days int    `json:"days" jsonschema:"the window's length in days, a whole number of the provider's step_days"`
	Totals
	BrandClicks    int64 `json:"brand_clicks" jsonschema:"clicks on searches containing a branded search term"`
	NonBrandClicks int64 `json:"non_brand_clicks" jsonschema:"clicks on listed searches without a branded term; unlisted searches are never non-brand"`
	// UnlistedClicks are clicks the provider reports for the site but on no
	// search it lists: Google hides rare searches, Bing lists only its top
	// searches each week.
	UnlistedClicks int64 `json:"unlisted_clicks"`
	DaysWithData   int   `json:"days_with_data"`
}

// Summary is the Search headline: the latest 28 days ending on the latest
// final day, and the 28 days before for the trend.
type Summary struct {
	Provider Provider `json:"provider"`
	Site     string   `json:"site" jsonschema:"empty when nothing is synced yet"`
	Current  *Period  `json:"current" jsonschema:"null when nothing is synced yet"`
	Previous *Period  `json:"previous" jsonschema:"the window before current; null without data there"`
}

// scope is the site the read API reports on for a provider, and the days
// its windows end on.
type scope struct {
	spec  providerSpec
	site  string
	first day // the earliest stored day
	// latest is the latest stored day and final the latest final one, or
	// latest when none is final yet.
	latest, final day
	hasFinal      bool
	// anchor is the day every window ends on: final, or for a provider
	// whose rows span weeks, the end of its latest final week.
	anchor day
}

func (s *Service) scope(ctx context.Context, p Provider) (scope, bool, error) {
	sp, err := lookup(p)
	if err != nil {
		return scope{}, false, err
	}
	sc := scope{spec: sp}
	if sc.site, err = s.dataSite(ctx, p); err != nil || sc.site == "" {
		return sc, false, err
	}
	var first, last, final *time.Time
	if err := s.db.QueryRow(ctx, `SELECT min(day), max(day), max(day) FILTER (WHERE is_final) FROM search_site_daily WHERE provider = $1 AND site = $2`, p, sc.site).
		Scan(&first, &last, &final); err != nil {
		return sc, false, err
	}
	if first == nil {
		return sc, false, nil
	}
	sc.first, sc.latest, sc.final = dayOf(*first), dayOf(*last), dayOf(*last)
	if final != nil {
		sc.final, sc.hasFinal = dayOf(*final), true
	}
	sc.anchor = sc.final
	if sp.StepDays > 1 {
		var week *time.Time
		if err := s.db.QueryRow(ctx, `SELECT max(last_day) FROM (
				SELECT last_day FROM search_query_stats WHERE provider = $1 AND site = $2 AND is_final AND last_day <= $3
				UNION ALL SELECT last_day FROM search_page_stats WHERE provider = $1 AND site = $2 AND is_final AND last_day <= $3) x`,
			p, sc.site, sc.final.date()).Scan(&week); err != nil {
			return sc, false, err
		}
		if week != nil {
			sc.anchor = dayOf(*week)
		}
	}
	return sc, true, nil
}

// Summary is p's latest 28 days and the 28 before.
func (s *Service) Summary(ctx context.Context, p Provider) (Summary, error) {
	sc, ok, err := s.scope(ctx, p)
	out := Summary{Provider: p, Site: sc.site}
	if err != nil || !ok {
		return out, err
	}
	cur := window(sc.anchor, HeadlineDays, sc.spec.StepDays)
	c, err := s.period(ctx, sc, cur)
	if err != nil {
		return out, err
	}
	out.Current = &c
	if prev := window(cur.from.add(-1), HeadlineDays, sc.spec.StepDays); !prev.to.before(sc.first) {
		p, err := s.period(ctx, sc, prev)
		if err != nil {
			return out, err
		}
		out.Previous = &p
	}
	return out, nil
}

// period sums the site totals inside w, and the searches whose rows lie
// wholly inside it.
func (s *Service) period(ctx context.Context, sc scope, w span) (Period, error) {
	terms, err := s.terms.BrandedTerms(ctx)
	if err != nil {
		return Period{}, err
	}
	p := Period{From: w.from.String(), To: w.to.String(), Days: w.days()}
	err = s.db.QueryRow(ctx, `SELECT coalesce(sum(clicks), 0), coalesce(sum(impressions), 0), `+avgPosition+`, count(*),
			(SELECT coalesce(sum(clicks) FILTER (WHERE `+branded("$5")+`), 0) FROM search_query_stats
				WHERE provider = $1 AND site = $2 AND first_day >= $3 AND last_day <= $4),
			(SELECT coalesce(sum(clicks) FILTER (WHERE NOT `+branded("$5")+`), 0) FROM search_query_stats
				WHERE provider = $1 AND site = $2 AND first_day >= $3 AND last_day <= $4)
		FROM search_site_daily WHERE provider = $1 AND site = $2 AND day BETWEEN $3 AND $4`,
		sc.spec.Provider, sc.site, w.from.date(), w.to.date(), terms).Scan(&p.Clicks, &p.Impressions, &p.Position, &p.DaysWithData, &p.BrandClicks, &p.NonBrandClicks)
	p.CTR = ctr(p.Clicks, p.Impressions)
	p.UnlistedClicks = max(0, p.Clicks-p.BrandClicks-p.NonBrandClicks)
	return p, err
}

func ctr(clicks, impressions int64) float64 {
	if impressions == 0 {
		return 0
	}
	return float64(clicks) / float64(impressions)
}

// Range asks for days from From to To, both optional dates.
type Range struct {
	From string `json:"from,omitempty" jsonschema:"first day, such as 2026-08-01; defaults to 90 days before to"`
	To   string `json:"to,omitempty" jsonschema:"last day; defaults to the latest day stored"`
}

// resolve parses the range, defaulting to the 90 days ending on last.
func (r Range) resolve(last day) (day, day, error) {
	to, from := last, last.add(-89)
	var err error
	if r.To != "" {
		if to, err = parseDay("to", r.To); err != nil {
			return day{}, day{}, err
		}
		from = to.add(-89)
	}
	if r.From != "" {
		if from, err = parseDay("from", r.From); err != nil {
			return day{}, day{}, err
		}
	}
	if to.before(from) {
		return day{}, day{}, fmt.Errorf("%w: from %s is after to %s", core.ErrInvalid, from, to)
	}
	if to.t.Sub(from.t) > 500*24*time.Hour {
		return day{}, day{}, fmt.Errorf("%w: a range is at most 500 days", core.ErrInvalid)
	}
	return from, to, nil
}

// Day is one day's numbers. Final is false while the provider still
// revises it.
type Day struct {
	Date string `json:"date"`
	Totals
	Final bool `json:"final"`
}

// DailySeries is the site's clicks and impressions per day.
type DailySeries struct {
	Provider    Provider `json:"provider"`
	Site        string   `json:"site"`
	LatestFinal string   `json:"latest_final_day" jsonschema:"the latest day the provider has finalized"`
	Days        []Day    `json:"days" jsonschema:"days the provider reported, oldest first; a day with no row had no impressions"`
}

// Daily is p's site numbers per day over r, partial days included and
// marked. Every provider reports site totals by day.
func (s *Service) Daily(ctx context.Context, p Provider, r Range) (DailySeries, error) {
	out := DailySeries{Provider: p, Days: []Day{}}
	sc, ok, err := s.scope(ctx, p)
	out.Site = sc.site
	if err != nil || !ok {
		return out, err
	}
	out.LatestFinal = sc.final.String()
	from, to, err := r.resolve(sc.latest)
	if err != nil {
		return out, err
	}
	out.Days, err = s.days(ctx, `SELECT to_char(day, 'YYYY-MM-DD'), clicks, impressions, position, is_final
		FROM search_site_daily WHERE provider = $1 AND site = $2 AND day BETWEEN $3 AND $4 ORDER BY day`, p, sc.site, from.date(), to.date())
	return out, err
}

func (s *Service) days(ctx context.Context, sql string, args ...any) ([]Day, error) {
	rows, err := s.db.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Day{}
	for rows.Next() {
		var d Day
		if err := rows.Scan(&d.Date, &d.Clicks, &d.Impressions, &d.Position, &d.Final); err != nil {
			return nil, err
		}
		d.CTR = ctr(d.Clicks, d.Impressions)
		out = append(out, d)
	}
	return out, rows.Err()
}

// GooglePageDaily is one page's Google clicks, impressions, CTR and
// position per day from from to to (calendar dates, inclusive), for the
// site the Search page shows, partial days marked. page is the full URL as
// Search Console reports it, such as https://www.example.com/pricing. A
// page with no impressions on a day has no entry for it. Changes and
// recrawl watch read before and after periods through it; only Google
// reports pages by day.
func (s *Service) GooglePageDaily(ctx context.Context, page string, from, to time.Time) ([]Day, error) {
	site, err := s.dataSite(ctx, Google)
	if err != nil || site == "" {
		return []Day{}, err
	}
	return s.days(ctx, `SELECT to_char(first_day, 'YYYY-MM-DD'), clicks, impressions, position, is_final
		FROM search_page_stats WHERE provider = $1 AND site = $2 AND page = $3 AND first_day BETWEEN $4 AND $5 ORDER BY first_day`,
		Google, site, page, dayOf(from).date(), dayOf(to).date())
}

// BrandPoint is the trailing 28 days of clicks ending on Date, split by
// brand. Unlisted clicks are never non-brand.
type BrandPoint struct {
	Date           string `json:"date" jsonschema:"the last day of the 28-day window"`
	Clicks         int64  `json:"clicks"`
	BrandClicks    int64  `json:"brand_clicks"`
	NonBrandClicks int64  `json:"non_brand_clicks"`
	UnlistedClicks int64  `json:"unlisted_clicks"`
}

// BrandSeries is brand against non-brand clicks over trailing 28-day
// windows, one per step: a day for Google, a week for Bing.
type BrandSeries struct {
	Provider Provider     `json:"provider"`
	Site     string       `json:"site"`
	Terms    []string     `json:"terms" jsonschema:"the branded search terms that decided brand against non-brand"`
	Points   []BrandPoint `json:"points"`
}

// Brand is brand against non-brand clicks over trailing 28-day windows
// ending in r, one every step_days back from the latest final day. None
// starts before the first stored day.
func (s *Service) Brand(ctx context.Context, p Provider, r Range) (BrandSeries, error) {
	out := BrandSeries{Provider: p, Terms: []string{}, Points: []BrandPoint{}}
	terms, err := s.terms.BrandedTerms(ctx)
	if err != nil {
		return out, err
	}
	out.Terms = terms
	sc, ok, err := s.scope(ctx, p)
	out.Site = sc.site
	if err != nil || !ok {
		return out, err
	}
	from, to, err := r.resolve(sc.anchor)
	if err != nil {
		return out, err
	}
	step := sc.spec.StepDays
	length := window(sc.anchor, HeadlineDays, step).days()
	if first := sc.first.add(length - 1); from.before(first) {
		from = first
	}
	var ends []time.Time
	for e := sc.anchor; !e.before(from); e = e.add(-step) {
		if !to.before(e) {
			ends = append(ends, e.date())
		}
	}
	if len(ends) == 0 {
		return out, nil
	}
	rows, err := s.db.Query(ctx, `WITH ends AS (SELECT unnest($3::date[]) AS e),
		site AS (SELECT day, clicks FROM search_site_daily WHERE provider = $1 AND site = $2 AND day BETWEEN $4 AND $5),
		q AS (SELECT first_day, last_day, sum(clicks) FILTER (WHERE `+branded("$7")+`) AS brand,
				sum(clicks) FILTER (WHERE NOT `+branded("$7")+`) AS non_brand
			FROM search_query_stats WHERE provider = $1 AND site = $2 AND first_day >= $4 AND last_day <= $5
			GROUP BY first_day, last_day)
		SELECT to_char(e, 'YYYY-MM-DD'),
			(SELECT coalesce(sum(clicks), 0) FROM site WHERE day BETWEEN e - $6::int AND e),
			(SELECT coalesce(sum(brand), 0) FROM q WHERE first_day >= e - $6::int AND last_day <= e),
			(SELECT coalesce(sum(non_brand), 0) FROM q WHERE first_day >= e - $6::int AND last_day <= e)
		FROM ends ORDER BY e`,
		p, sc.site, ends, dayOf(ends[len(ends)-1]).add(1-length).date(), ends[0], length-1, terms)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var b BrandPoint
		if err := rows.Scan(&b.Date, &b.Clicks, &b.BrandClicks, &b.NonBrandClicks); err != nil {
			return out, err
		}
		b.UnlistedClicks = max(0, b.Clicks-b.BrandClicks-b.NonBrandClicks)
		out.Points = append(out.Points, b)
	}
	return out, rows.Err()
}

// Top asks for the top pages or queries over the window ending on the
// latest final day.
type Top struct {
	Days     int  `json:"days,omitempty" jsonschema:"window length in days, 1 to 480, default 28; rounded to whole weeks for Bing"`
	Limit    int  `json:"limit,omitempty" jsonschema:"rows, 1 to 200, default 25"`
	NonBrand bool `json:"non_brand,omitempty" jsonschema:"queries only: leave out branded queries"`
}

func (t Top) resolve() (int, int, error) {
	days, limit := t.Days, t.Limit
	if days == 0 {
		days = HeadlineDays
	}
	if limit == 0 {
		limit = 25
	}
	if days < 1 || days > 480 {
		return 0, 0, fmt.Errorf("%w: days must be 1 to 480", core.ErrInvalid)
	}
	if limit < 1 || limit > 200 {
		return 0, 0, fmt.Errorf("%w: limit must be 1 to 200", core.ErrInvalid)
	}
	return days, limit, nil
}

// PageRow is one page over a window.
type PageRow struct {
	Page string `json:"page"`
	Totals
}

// PageList is the top pages by clicks, then impressions.
type PageList struct {
	Provider Provider  `json:"provider"`
	Site     string    `json:"site"`
	From     string    `json:"from"`
	To       string    `json:"to"`
	Pages    []PageRow `json:"pages"`
}

// TopPages ranks p's pages by clicks over the window ending on the latest
// final day.
func (s *Service) TopPages(ctx context.Context, p Provider, t Top) (PageList, error) {
	out := PageList{Provider: p, Pages: []PageRow{}}
	days, limit, err := t.resolve()
	if err != nil {
		return out, err
	}
	sc, ok, err := s.scope(ctx, p)
	out.Site = sc.site
	if err != nil || !ok {
		return out, err
	}
	w := window(sc.anchor, days, sc.spec.StepDays)
	out.From, out.To = w.from.String(), w.to.String()
	rows, err := s.db.Query(ctx, `SELECT page, sum(clicks), sum(impressions), `+avgPosition+`
		FROM search_page_stats WHERE provider = $1 AND site = $2 AND first_day >= $3 AND last_day <= $4
		GROUP BY page ORDER BY 2 DESC, 3 DESC, page LIMIT $5`, p, sc.site, w.from.date(), w.to.date(), limit)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var r PageRow
		if err := rows.Scan(&r.Page, &r.Clicks, &r.Impressions, &r.Position); err != nil {
			return out, err
		}
		r.CTR = ctr(r.Clicks, r.Impressions)
		out.Pages = append(out.Pages, r)
	}
	return out, rows.Err()
}

// QueryRow is one search query over a window.
type QueryRow struct {
	Query   string `json:"query"`
	Branded bool   `json:"branded"`
	Totals
}

// QueryList is the top queries by clicks, then impressions.
type QueryList struct {
	Provider Provider   `json:"provider"`
	Site     string     `json:"site"`
	From     string     `json:"from"`
	To       string     `json:"to"`
	NonBrand bool       `json:"non_brand"`
	Queries  []QueryRow `json:"queries"`
}

// TopQueries ranks p's queries by clicks over the window ending on the
// latest final day, optionally non-brand only. Unlisted searches never
// appear.
func (s *Service) TopQueries(ctx context.Context, p Provider, t Top) (QueryList, error) {
	out := QueryList{Provider: p, NonBrand: t.NonBrand, Queries: []QueryRow{}}
	days, limit, err := t.resolve()
	if err != nil {
		return out, err
	}
	terms, err := s.terms.BrandedTerms(ctx)
	if err != nil {
		return out, err
	}
	sc, ok, err := s.scope(ctx, p)
	out.Site = sc.site
	if err != nil || !ok {
		return out, err
	}
	w := window(sc.anchor, days, sc.spec.StepDays)
	out.From, out.To = w.from.String(), w.to.String()
	rows, err := s.db.Query(ctx, `SELECT query, `+branded("$5")+`, sum(clicks), sum(impressions), `+avgPosition+`
		FROM search_query_stats WHERE provider = $1 AND site = $2 AND first_day >= $3 AND last_day <= $4 AND NOT ($7 AND `+branded("$5")+`)
		GROUP BY query ORDER BY 3 DESC, 4 DESC, query LIMIT $6`, p, sc.site, w.from.date(), w.to.date(), terms, limit, t.NonBrand)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var q QueryRow
		if err := rows.Scan(&q.Query, &q.Branded, &q.Clicks, &q.Impressions, &q.Position); err != nil {
			return out, err
		}
		q.CTR = ctr(q.Clicks, q.Impressions)
		out.Queries = append(out.Queries, q)
	}
	return out, rows.Err()
}
