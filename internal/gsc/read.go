package gsc

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

// Totals are clicks, impressions, CTR and position over some days.
type Totals struct {
	Clicks      int64   `json:"clicks"`
	Impressions int64   `json:"impressions"`
	CTR         float64 `json:"ctr" jsonschema:"clicks over impressions, 0 to 1"`
	Position    float64 `json:"position" jsonschema:"average position, weighted by impressions; 0 with no impressions"`
}

// Period is the site's numbers over from to to, inclusive.
type Period struct {
	From string `json:"from"`
	To   string `json:"to"`
	Totals
	BrandClicks    int64 `json:"brand_clicks" jsonschema:"clicks on queries containing a branded search term"`
	NonBrandClicks int64 `json:"non_brand_clicks" jsonschema:"clicks on named queries without a branded term; anonymized queries are never non-brand"`
	// AnonymizedClicks are clicks Google reports for the site but on no
	// query, because it hides rare queries.
	AnonymizedClicks int64 `json:"anonymized_clicks"`
	DaysWithData     int   `json:"days_with_data"`
}

// Summary is the Search headline: the latest 28 days ending on the latest
// final day, and the 28 days before for the trend.
type Summary struct {
	Property string  `json:"property" jsonschema:"empty when nothing is synced yet"`
	Current  *Period `json:"current" jsonschema:"null when nothing is synced yet"`
	Previous *Period `json:"previous" jsonschema:"the 28 days before current; null without data there"`
}

// scope is the property the read API reports on and its latest final day.
type scope struct {
	property    string
	latestFinal day
	latestDay   day
	firstDay    day
}

func (s *Service) scope(ctx context.Context) (scope, bool, error) {
	prop, err := s.dataProperty(ctx)
	if err != nil || prop == "" {
		return scope{}, false, err
	}
	var first, last, final *time.Time
	if err := s.db.QueryRow(ctx, `SELECT min(date), max(date), max(date) FILTER (WHERE is_final) FROM gsc_site_daily WHERE property = $1`, prop).
		Scan(&first, &last, &final); err != nil {
		return scope{}, false, err
	}
	if first == nil {
		return scope{property: prop}, false, nil
	}
	sc := scope{property: prop, firstDay: dayOf(*first), latestDay: dayOf(*last), latestFinal: dayOf(*last)}
	if final != nil {
		sc.latestFinal = dayOf(*final)
	}
	return sc, true, nil
}

// Summary is the latest 28 days and the 28 before.
func (s *Service) Summary(ctx context.Context) (Summary, error) {
	sc, ok, err := s.scope(ctx)
	if err != nil || !ok {
		return Summary{Property: sc.property}, err
	}
	out := Summary{Property: sc.property}
	to := sc.latestFinal
	cur, err := s.period(ctx, sc.property, to.add(1-HeadlineDays), to)
	if err != nil {
		return out, err
	}
	out.Current = &cur
	prevTo := to.add(-HeadlineDays)
	if !prevTo.before(sc.firstDay) {
		prev, err := s.period(ctx, sc.property, prevTo.add(1-HeadlineDays), prevTo)
		if err != nil {
			return out, err
		}
		out.Previous = &prev
	}
	return out, nil
}

func (s *Service) period(ctx context.Context, property string, from, to day) (Period, error) {
	terms, err := s.terms.BrandedTerms(ctx)
	if err != nil {
		return Period{}, err
	}
	p := Period{From: from.String(), To: to.String()}
	err = s.db.QueryRow(ctx, `SELECT coalesce(sum(clicks), 0), coalesce(sum(impressions), 0),
			coalesce(sum(position * impressions) / nullif(sum(impressions), 0), 0), count(*),
			(SELECT coalesce(sum(clicks) FILTER (WHERE `+branded("$4")+`), 0) FROM gsc_query_daily WHERE property = $1 AND date BETWEEN $2 AND $3),
			(SELECT coalesce(sum(clicks) FILTER (WHERE NOT `+branded("$4")+`), 0) FROM gsc_query_daily WHERE property = $1 AND date BETWEEN $2 AND $3)
		FROM gsc_site_daily WHERE property = $1 AND date BETWEEN $2 AND $3`,
		property, from.date(), to.date(), terms).Scan(&p.Clicks, &p.Impressions, &p.Position, &p.DaysWithData, &p.BrandClicks, &p.NonBrandClicks)
	p.CTR = ctr(p.Clicks, p.Impressions)
	p.AnonymizedClicks = max(0, p.Clicks-p.BrandClicks-p.NonBrandClicks)
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

// Day is one day's numbers. Final is false while Google still revises it.
type Day struct {
	Date string `json:"date"`
	Totals
	Final bool `json:"final"`
}

// DailySeries is the site's clicks and impressions per day.
type DailySeries struct {
	Property    string `json:"property"`
	LatestFinal string `json:"latest_final_day"`
	Days        []Day  `json:"days" jsonschema:"days Google reported, oldest first; a day with no row had no impressions"`
}

// Daily is the site's numbers per day over r, partial days included and
// marked.
func (s *Service) Daily(ctx context.Context, r Range) (DailySeries, error) {
	out := DailySeries{Days: []Day{}}
	sc, ok, err := s.scope(ctx)
	out.Property = sc.property
	if err != nil || !ok {
		return out, err
	}
	out.LatestFinal = sc.latestFinal.String()
	from, to, err := r.resolve(sc.latestDay)
	if err != nil {
		return out, err
	}
	out.Days, err = s.days(ctx, `SELECT to_char(date, 'YYYY-MM-DD'), clicks, impressions, position, is_final
		FROM gsc_site_daily WHERE property = $1 AND date BETWEEN $2 AND $3 ORDER BY date`, sc.property, from.date(), to.date())
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

// PageDaily is one page's clicks, impressions, CTR and position per day
// from from to to (calendar dates, inclusive), for the property the Search
// page shows, partial days marked. page is the full URL as Search Console
// reports it, such as https://www.example.com/pricing. A page with no
// impressions on a day has no entry for it. Changes and recrawl watch read
// before and after periods through it.
func (s *Service) PageDaily(ctx context.Context, page string, from, to time.Time) ([]Day, error) {
	prop, err := s.dataProperty(ctx)
	if err != nil || prop == "" {
		return []Day{}, err
	}
	return s.days(ctx, `SELECT to_char(date, 'YYYY-MM-DD'), clicks, impressions, position, is_final
		FROM gsc_page_daily WHERE property = $1 AND page = $2 AND date BETWEEN $3 AND $4 ORDER BY date`,
		prop, page, dayOf(from).date(), dayOf(to).date())
}

// BrandPoint is the trailing 28 days of clicks ending on Date, split by
// brand. Anonymized clicks are never non-brand.
type BrandPoint struct {
	Date             string `json:"date" jsonschema:"the last day of the 28-day window"`
	Clicks           int64  `json:"clicks"`
	BrandClicks      int64  `json:"brand_clicks"`
	NonBrandClicks   int64  `json:"non_brand_clicks"`
	AnonymizedClicks int64  `json:"anonymized_clicks"`
}

// BrandSeries is brand against non-brand clicks over trailing 28-day
// windows, one per day.
type BrandSeries struct {
	Property string       `json:"property"`
	Terms    []string     `json:"terms" jsonschema:"the branded search terms that decided brand against non-brand"`
	Points   []BrandPoint `json:"points"`
}

// Brand is brand against non-brand clicks over trailing 28-day windows
// ending on each day of r. Windows end on final days only, and none starts
// before the first stored day.
func (s *Service) Brand(ctx context.Context, r Range) (BrandSeries, error) {
	out := BrandSeries{Terms: []string{}, Points: []BrandPoint{}}
	terms, err := s.terms.BrandedTerms(ctx)
	if err != nil {
		return out, err
	}
	out.Terms = terms
	sc, ok, err := s.scope(ctx)
	out.Property = sc.property
	if err != nil || !ok {
		return out, err
	}
	from, to, err := r.resolve(sc.latestFinal)
	if err != nil {
		return out, err
	}
	if sc.latestFinal.before(to) {
		to = sc.latestFinal
	}
	if first := sc.firstDay.add(HeadlineDays - 1); from.before(first) {
		from = first
	}
	if to.before(from) {
		return out, nil
	}
	rows, err := s.db.Query(ctx, `WITH days AS (
			SELECT d::date AS date FROM generate_series($2::date - 27, $3::date, interval '1 day') d),
		site AS (SELECT date, clicks FROM gsc_site_daily WHERE property = $1 AND date BETWEEN $2::date - 27 AND $3),
		q AS (SELECT date, sum(clicks) FILTER (WHERE `+branded("$4")+`) AS brand,
				sum(clicks) FILTER (WHERE NOT `+branded("$4")+`) AS non_brand
			FROM gsc_query_daily WHERE property = $1 AND date BETWEEN $2::date - 27 AND $3 GROUP BY date),
		daily AS (SELECT days.date, coalesce(site.clicks, 0) AS clicks, coalesce(q.brand, 0) AS brand, coalesce(q.non_brand, 0) AS non_brand
			FROM days LEFT JOIN site USING (date) LEFT JOIN q USING (date)),
		rolled AS (SELECT date, sum(clicks) OVER w AS clicks, sum(brand) OVER w AS brand, sum(non_brand) OVER w AS non_brand
			FROM daily WINDOW w AS (ORDER BY date ROWS BETWEEN 27 PRECEDING AND CURRENT ROW))
		SELECT to_char(date, 'YYYY-MM-DD'), clicks, brand, non_brand FROM rolled WHERE date >= $2 ORDER BY date`,
		sc.property, from.date(), to.date(), terms)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var p BrandPoint
		if err := rows.Scan(&p.Date, &p.Clicks, &p.BrandClicks, &p.NonBrandClicks); err != nil {
			return out, err
		}
		p.AnonymizedClicks = max(0, p.Clicks-p.BrandClicks-p.NonBrandClicks)
		out.Points = append(out.Points, p)
	}
	return out, rows.Err()
}

// Top asks for the top pages or queries over the window ending on the
// latest final day.
type Top struct {
	Days     int  `json:"days,omitempty" jsonschema:"window length in days, 1 to 480, default 28"`
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
	Property string    `json:"property"`
	From     string    `json:"from"`
	To       string    `json:"to"`
	Pages    []PageRow `json:"pages"`
}

// TopPages ranks pages by clicks over the window ending on the latest
// final day.
func (s *Service) TopPages(ctx context.Context, t Top) (PageList, error) {
	out := PageList{Pages: []PageRow{}}
	days, limit, err := t.resolve()
	if err != nil {
		return out, err
	}
	sc, ok, err := s.scope(ctx)
	out.Property = sc.property
	if err != nil || !ok {
		return out, err
	}
	from, to := sc.latestFinal.add(1-days), sc.latestFinal
	out.From, out.To = from.String(), to.String()
	rows, err := s.db.Query(ctx, `SELECT page, sum(clicks), sum(impressions), coalesce(sum(position * impressions) / nullif(sum(impressions), 0), 0)
		FROM gsc_page_daily WHERE property = $1 AND date BETWEEN $2 AND $3
		GROUP BY page ORDER BY 2 DESC, 3 DESC, page LIMIT $4`, sc.property, from.date(), to.date(), limit)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var p PageRow
		if err := rows.Scan(&p.Page, &p.Clicks, &p.Impressions, &p.Position); err != nil {
			return out, err
		}
		p.CTR = ctr(p.Clicks, p.Impressions)
		out.Pages = append(out.Pages, p)
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
	Property string     `json:"property"`
	From     string     `json:"from"`
	To       string     `json:"to"`
	NonBrand bool       `json:"non_brand"`
	Queries  []QueryRow `json:"queries"`
}

// TopQueries ranks queries by clicks over the window ending on the latest
// final day, optionally non-brand only. Anonymized queries never appear.
func (s *Service) TopQueries(ctx context.Context, t Top) (QueryList, error) {
	out := QueryList{NonBrand: t.NonBrand, Queries: []QueryRow{}}
	days, limit, err := t.resolve()
	if err != nil {
		return out, err
	}
	terms, err := s.terms.BrandedTerms(ctx)
	if err != nil {
		return out, err
	}
	sc, ok, err := s.scope(ctx)
	out.Property = sc.property
	if err != nil || !ok {
		return out, err
	}
	from, to := sc.latestFinal.add(1-days), sc.latestFinal
	out.From, out.To = from.String(), to.String()
	rows, err := s.db.Query(ctx, `SELECT query, `+branded("$4")+`, sum(clicks), sum(impressions),
			coalesce(sum(position * impressions) / nullif(sum(impressions), 0), 0)
		FROM gsc_query_daily WHERE property = $1 AND date BETWEEN $2 AND $3 AND NOT ($6 AND `+branded("$4")+`)
		GROUP BY query ORDER BY 3 DESC, 4 DESC, query LIMIT $5`, sc.property, from.date(), to.date(), terms, limit, t.NonBrand)
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

// InspectURL asks Google's URL Inspection (urlInspection.index.inspect)
// how the connected property's index sees pageURL: the coverage state,
// when Google last crawled it, and the canonical Google chose against the
// one the page declares. pageURL must be inside the property. Each call
// spends one of the property's 2,000 daily inspections, so recrawl watch
// calls it for changed pages only.
func (s *Service) InspectURL(ctx context.Context, pageURL string) (Inspection, error) {
	c, err := s.connection(ctx)
	if err != nil {
		return Inspection{}, err
	}
	if c.property == "" {
		return Inspection{}, fmt.Errorf("%w: pick a Search Console property before inspecting URLs", core.ErrInvalid)
	}
	return s.api(ctx, c.tokens).inspect(ctx, c.property, pageURL)
}
