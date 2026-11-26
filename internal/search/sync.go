package search

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/unkeyed/byname/internal/db"
)

// syncLock keeps two replicas from syncing at once. Every provider shares
// it, so one recovery step covers every provider's cut-off syncs.
var syncLock = db.WorkspaceLock("byname.search.sync")

// SyncIfDue syncs each connected site once a day, and at once after a site
// is picked or the credential replaced, provider by provider in registry
// order. A failed sync retries an hour later, unless someone picks a site
// or reconnects first, and never holds up another provider. It returns at
// once when another process is syncing.
func (s *Service) SyncIfDue(ctx context.Context) error {
	return s.locked(ctx, func() error {
		rows, err := s.db.Query(ctx, `SELECT provider FROM search_connection c
			WHERE site IS NOT NULL
			AND (last_sync_at IS NULL OR last_sync_at <= $1::timestamptz - interval '1 day' OR connected_at > last_sync_at)
			AND NOT (last_sync_error IS NOT NULL AND EXISTS (SELECT 1 FROM job_run j
				WHERE j.job = 'search_sync' AND j.provider = c.provider AND j.status = 'failed' AND j.started_at > $1::timestamptz - interval '1 hour'))`,
			s.now())
		if err != nil {
			return err
		}
		due, err := pgx.CollectRows(rows, pgx.RowTo[Provider])
		if err != nil {
			return err
		}
		var errs []error
		for _, sp := range registry {
			if !slices.Contains(due, sp.Provider) {
				continue
			}
			if err := s.sync(ctx, sp.Provider); err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", sp.Title, err))
			}
		}
		return errors.Join(errs...)
	})
}

// Sync syncs p's connected site now, whether or not it is due, and waits
// for a sync another process is running to finish first.
func (s *Service) Sync(ctx context.Context, p Provider) error {
	if _, err := s.connection(ctx, p); err != nil {
		return err
	}
	return s.lockedWait(ctx, func() error { return s.sync(ctx, p) })
}

func (s *Service) locked(ctx context.Context, f func() error) error {
	return s.withLock(ctx, `SELECT pg_try_advisory_lock(`+syncLock+`)`, f)
}

func (s *Service) lockedWait(ctx context.Context, f func() error) error {
	return s.withLock(ctx, `SELECT pg_advisory_lock(`+syncLock+`) IS NOT NULL`, f)
}

// withLock runs f holding the session advisory lock on a dedicated
// connection, so the lock dies with the process.
func (s *Service) withLock(ctx context.Context, take string, f func() error) error {
	conn, err := s.db.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	var locked bool
	if err := conn.QueryRow(ctx, take).Scan(&locked); err != nil || !locked {
		return err
	}
	defer func() {
		if _, err := conn.Exec(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock(`+syncLock+`)`); err != nil {
			_ = conn.Conn().Close(context.WithoutCancel(ctx))
		}
	}()
	// With the lock held no sync runs anywhere, so one still marked running
	// was cut off by a crash or a redeploy.
	if _, err := conn.Exec(ctx, `UPDATE job_run SET status = 'failed', finished_at = $1,
			error = 'the process stopped before this sync finished; the next sync carries on from the last stored day'
		WHERE job = 'search_sync' AND status = 'running'`, s.now()); err != nil {
		return err
	}
	return f()
}

// sync pulls what p's stored data is missing and logs a job_run. Each
// report replaces its span's partial rows and upserts the rest, so a sync
// that stops halfway, or runs twice, ends in the same rows.
func (s *Service) sync(ctx context.Context, p Provider) error {
	sp, err := lookup(p)
	if err != nil {
		return err
	}
	c, err := s.connection(ctx, p)
	if err != nil || c.site == "" {
		return err
	}
	var job int64
	if err := s.db.QueryRow(ctx, `INSERT INTO job_run (job, provider, trigger, started_at) VALUES ('search_sync', $1, 'scheduled', $2) RETURNING id`, p, s.now()).Scan(&job); err != nil {
		return err
	}
	want, err := s.missing(ctx, sp, c)
	if err == nil {
		err = s.adapters[p].pull(ctx, c.cred, c.site, want, func(r report) error { return s.put(ctx, p, c.site, r) })
	}
	status, msg := "completed", (*string)(nil)
	if err != nil {
		status, msg = "failed", new(err.Error())
	}
	_, ferr := s.db.Exec(ctx, `WITH j AS (UPDATE job_run SET status = $2, error = $3, finished_at = $4 WHERE id = $1)
		UPDATE search_connection SET last_sync_error = $3,
			last_sync_at = CASE WHEN $3::text IS NULL THEN $4 ELSE last_sync_at END
		WHERE provider = $5 AND site = $6`, job, status, msg, s.now(), p, c.site)
	if err == nil {
		err = ferr
	}
	if err == nil {
		if err := s.searchAlerts(ctx, p); err != nil {
			return fmt.Errorf("search notifications after the sync: %w", err)
		}
	}
	return err
}

// missing is the days to pull, through today in the provider's calendar.
// A first sync after connecting, picking a site or turning backfill on
// pulls BackfillMonths back with backfill, else the last 28 days, plus
// anything stored that is not final. Later syncs pull from the first day
// not stored as final. A first sync that stops halfway runs again in full,
// so a backfill never leaves a hole. The span is empty when nothing is
// missing.
func (s *Service) missing(ctx context.Context, sp providerSpec, c conn) (span, error) {
	today := s.today(sp)
	earliest := today.add(-27)
	if c.backfill && sp.BackfillMonths > 0 {
		earliest = dayOf(today.t.AddDate(0, -sp.BackfillMonths, 0))
	}
	var resume *time.Time
	err := s.db.QueryRow(ctx, `SELECT coalesce(
			(SELECT min(day) FROM search_site_daily WHERE provider = $1 AND site = $2 AND NOT is_final),
			(SELECT max(day) + 1 FROM search_site_daily WHERE provider = $1 AND site = $2))`, sp.Provider, c.site).Scan(&resume)
	if err != nil {
		return span{}, err
	}
	start := earliest
	if resume != nil && (c.lastSync != nil || !c.backfill) {
		start = dayOf(*resume)
	}
	return span{start, today}, nil
}

// stats are where each dimension's rows go. Site totals are one day a row;
// searches and pages span first_day to last_day.
var stats = map[dim]struct{ table, key string }{
	queries: {"search_query_stats", "query"},
	pages:   {"search_page_stats", "page"},
}

// put stores one report in one transaction: the partial rows stored
// wholly inside its span go, then every row is upserted, rowLimit at a
// time. A final row is never replaced by a partial one. A search that is
// blank would be an anonymized one, which never counts as a search, so it
// is dropped; so is a repeat of a key, which paging over fresh data can
// return, and a key too long for a Postgres index entry.
func (s *Service) put(ctx context.Context, p Provider, site string, r report) error {
	del := `DELETE FROM search_site_daily WHERE provider = $1 AND site = $2 AND day BETWEEN $3 AND $4 AND NOT is_final`
	ins := `INSERT INTO search_site_daily (provider, site, day, clicks, impressions, position, is_final)
		SELECT $1, $2, r.first_day, r.clicks, r.impressions, r.position, r.is_final
		FROM unnest($3::date[], $4::date[], $5::text[], $6::bigint[], $7::bigint[], $8::float8[], $9::bool[])
			AS r (first_day, last_day, key, clicks, impressions, position, is_final)
		ON CONFLICT (provider, site, day) DO UPDATE SET
			clicks = excluded.clicks, impressions = excluded.impressions, position = excluded.position, is_final = excluded.is_final
		WHERE NOT search_site_daily.is_final OR excluded.is_final`
	if t, ok := stats[r.dim]; ok {
		del = `DELETE FROM ` + t.table + ` WHERE provider = $1 AND site = $2 AND first_day >= $3 AND last_day <= $4 AND NOT is_final`
		ins = `INSERT INTO ` + t.table + ` (provider, site, first_day, last_day, ` + t.key + `, clicks, impressions, position, is_final)
			SELECT $1, $2, r.* FROM unnest($3::date[], $4::date[], $5::text[], $6::bigint[], $7::bigint[], $8::float8[], $9::bool[]) AS r
			ON CONFLICT (provider, site, first_day, ` + t.key + `) DO UPDATE SET last_day = excluded.last_day,
				clicks = excluded.clicks, impressions = excluded.impressions, position = excluded.position, is_final = excluded.is_final
			WHERE NOT ` + t.table + `.is_final OR excluded.is_final`
	}
	return pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, del, p, site, r.span.from.date(), r.span.to.date()); err != nil {
			return err
		}
		seen := map[string]bool{}
		for start := 0; start < len(r.rows); start += rowLimit {
			var firsts, lasts []time.Time
			var keys []string
			var clicks, impressions []int64
			var positions []float64
			var final []bool
			for _, w := range r.rows[start:min(start+rowLimit, len(r.rows))] {
				id := w.first.String() + "\x00" + w.key
				if (r.dim == queries && strings.TrimSpace(w.key) == "") || len(w.key) > maxKeyBytes || seen[id] {
					continue
				}
				seen[id] = true
				firsts, lasts, keys = append(firsts, w.first.date()), append(lasts, w.last.date()), append(keys, w.key)
				clicks, impressions = append(clicks, w.clicks), append(impressions, w.impressions)
				positions, final = append(positions, w.position), append(final, w.final)
			}
			if _, err := tx.Exec(ctx, ins, p, site, firsts, lasts, keys, clicks, impressions, positions, final); err != nil {
				return err
			}
		}
		return nil
	})
}

// maxKeyBytes keeps a row's key inside a btree index entry (about 2.7 kB).
const maxKeyBytes = 2000
