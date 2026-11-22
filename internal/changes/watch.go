package changes

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/unkeyed/byname/internal/gsc"
	"github.com/unkeyed/byname/internal/notify"
)

// recrawl inspects each page with a change Google has not crawled since,
// the oldest change first, up to what is left of the day's inspections.
// When Google's last crawl is after a change's merge, that crawl is the
// change's first crawl, and its interim and verdict days follow from its
// date in the instance's time zone. Google reports only the latest crawl,
// so a page inspected daily gets its first crawl to within a day.
func (s *Service) recrawl(ctx context.Context, st instanceSettings) error {
	status, err := s.search.Status(ctx)
	if err != nil || status.Property == "" {
		return err
	}
	var used int
	if err := s.db.QueryRow(ctx, `SELECT count(*) FROM url_inspection WHERE checked_at >= $1`, dayStart(s.now())).Scan(&used); err != nil {
		return err
	}
	rows, err := s.db.Query(ctx, `SELECT url FROM change WHERE first_crawl_at IS NULL GROUP BY url ORDER BY min(merged_at), url LIMIT $1`, max(s.cap-used, 0))
	if err != nil {
		return err
	}
	urls, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return err
	}
	var failed []error
	for _, u := range urls {
		in, err := s.search.InspectURL(ctx, u)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var msg *string
		if err != nil {
			msg = new(err.Error())
			failed = append(failed, err)
		}
		if _, err := s.db.Exec(ctx, `INSERT INTO url_inspection (url, checked_at, verdict, coverage_state, last_crawl_at, google_canonical, user_canonical, error)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`, u, s.now(), in.Verdict, in.CoverageState, in.LastCrawl, in.GoogleCanonical, in.UserCanonical, msg); err != nil {
			return err
		}
		if in.LastCrawl == nil {
			continue
		}
		y, m, d := in.LastCrawl.In(st.loc).Date()
		crawlDay := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
		if _, err := s.db.Exec(ctx, `UPDATE change SET first_crawl_at = $2, interim_on = $3, verdict_on = $4
			WHERE url = $1 AND first_crawl_at IS NULL AND merged_at < $2`,
			u, *in.LastCrawl, crawlDay.AddDate(0, 0, st.InterimDays), crawlDay.AddDate(0, 0, st.VerdictDays)); err != nil {
			return err
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("%d of %d URL inspections failed, the first with: %w", len(failed), len(urls), failed[0])
	}
	return nil
}

// dayStart is midnight Pacific Time on t's Search Console day, when the
// URL Inspection quota starts over.
func dayStart(t time.Time) time.Time {
	y, m, d := t.In(pacific).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, pacific)
}

// searchData is the stored Search Console data's extent.
type searchData struct {
	firstDay    string
	latestFinal time.Time
}

func (s *Service) searchData(ctx context.Context) (searchData, bool, error) {
	status, err := s.search.Status(ctx)
	if err != nil || status.LatestFinal == "" {
		return searchData{}, false, err
	}
	latest, err := time.Parse(time.DateOnly, status.LatestFinal)
	return searchData{firstDay: status.FirstDay, latestFinal: latest}, err == nil, err
}

// verdicts fills in each change's before numbers once those days are
// final, then takes each read that has come due: the interim read on and
// after interim_on, the final one on and after verdict_on, as days in the
// instance's time zone. Each read is stored once; a final read raises
// change_verdict.
func (s *Service) verdicts(ctx context.Context, st instanceSettings) error {
	data, ok, err := s.searchData(ctx)
	if err != nil || !ok {
		return err
	}
	if err := s.fillBefore(ctx, st, data); err != nil {
		return err
	}
	today := st.today(s.now())
	rows, err := s.db.Query(ctx, `SELECT `+changeColumns+` FROM change WHERE first_crawl_at IS NOT NULL
		AND ((interim IS NULL AND interim_on <= $1::date) OR (final IS NULL AND verdict_on <= $1::date)) ORDER BY id`, today)
	if err != nil {
		return err
	}
	due, err := pgx.CollectRows(rows, scanChange)
	if err != nil {
		return err
	}
	if len(due) == 0 {
		return nil
	}
	rows, err = s.db.Query(ctx, `SELECT url FROM control_page WHERE hold_until >= $1::date ORDER BY url`, today)
	if err != nil {
		return err
	}
	controls, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return err
	}
	for _, c := range due {
		for _, r := range []struct {
			kind   ReadKind
			on     *string
			done   bool
			length int
		}{{Interim, c.InterimOn, c.Interim != nil, st.InterimDays}, {Final, c.VerdictOn, c.Final != nil, st.VerdictDays}} {
			if r.done || *r.on > today {
				continue
			}
			read, err := s.read(ctx, c, r.kind, r.length, controls, st, data, today)
			if err != nil {
				return err
			}
			if err := s.storeRead(ctx, c, read); err != nil {
				return err
			}
		}
	}
	return nil
}

// fillBefore stores the before numbers of each change whose before window
// (the verdict days that end the day before the merge) is final.
func (s *Service) fillBefore(ctx context.Context, st instanceSettings, data searchData) error {
	rows, err := s.db.Query(ctx, `SELECT id, url, merged_at FROM change WHERE before IS NULL`)
	if err != nil {
		return err
	}
	type pending struct {
		ID       int64
		URL      string
		MergedAt time.Time
	}
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[pending])
	if err != nil {
		return err
	}
	for _, p := range list {
		to := searchDay(p.MergedAt).AddDate(0, 0, -1)
		if data.latestFinal.Before(to) {
			continue
		}
		from := to.AddDate(0, 0, 1-st.VerdictDays)
		days, err := s.search.PageDaily(ctx, p.URL, from, to)
		if err != nil {
			return err
		}
		raw, err := json.Marshal(Before{From: from.Format(time.DateOnly), To: to.Format(time.DateOnly), Totals: sum(days, [2]time.Time{from, to})})
		if err != nil {
			return err
		}
		if _, err := s.db.Exec(ctx, `UPDATE change SET before = $2 WHERE id = $1 AND before IS NULL`, p.ID, raw); err != nil {
			return err
		}
	}
	return nil
}

// read takes one difference-in-differences read of c (see assess).
func (s *Service) read(ctx context.Context, c Change, kind ReadKind, length int, controls []string, st instanceSettings, data searchData, today string) (Read, error) {
	before, after, n := windows(c.MergedAt, *c.FirstCrawlAt, length, data.latestFinal)
	pageDays, err := s.search.PageDaily(ctx, c.URL, before[0], after[1])
	if err != nil {
		return Read{}, err
	}
	used := []string{}
	var controlDays []gsc.Day
	for _, u := range controls {
		if u == c.URL {
			continue
		}
		days, err := s.search.PageDaily(ctx, u, before[0], after[1])
		if err != nil {
			return Read{}, err
		}
		used = append(used, u)
		controlDays = append(controlDays, days...)
	}
	window := func(w [2]time.Time) Window {
		return Window{From: w[0].Format(time.DateOnly), To: w[1].Format(time.DateOnly), Days: n, Page: sum(pageDays, w), Control: sum(controlDays, w)}
	}
	r := Read{Kind: kind, ReadOn: today, Before: window(before), After: window(after), Controls: used, Thresholds: st.Thresholds}
	r.Outcome, r.Reason, r.Deltas, r.Signals = assess(sample{before: r.Before, after: r.After, firstDay: data.firstDay, controls: len(used)}, st.Thresholds)
	return r, nil
}

// storeRead stores a read once. The final read sets the verdict and
// raises change_verdict, which notify stores once per change.
func (s *Service) storeRead(ctx context.Context, c Change, r Read) error {
	raw, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if r.Kind == Interim {
		_, err := s.db.Exec(ctx, `UPDATE change SET interim = $2 WHERE id = $1 AND interim IS NULL`, c.ID, raw)
		return err
	}
	if _, err := s.db.Exec(ctx, `UPDATE change SET final = $2, verdict = $3 WHERE id = $1 AND final IS NULL`, c.ID, raw, r.Outcome); err != nil {
		return err
	}
	if s.alerts == nil {
		return nil
	}
	return s.alerts.Emit(ctx, notify.Event{
		Subject: fmt.Sprintf("change/%d/final", c.ID),
		Payload: notify.ChangeVerdictPayload{ChangeID: c.ID, URL: c.URL, Verdict: string(r.Outcome), Detail: verdictDetail(r)},
	})
}

// verdictDetail is the read in one line, for the alert, with the figures
// named as the change drawer names them.
func verdictDetail(r Read) string {
	if r.Deltas == nil {
		return "Not enough data: " + r.Reason + "."
	}
	d := r.Deltas
	return fmt.Sprintf("Compared with %d comparison pages over %d days: average position %+.1f places (negative is better), click rate %+.2f points, appearances ×%.2f (above 1 means the page gained on them).",
		len(r.Controls), r.After.Days, d.Position, d.CTRPoints, d.ImpressionsRatio)
}
