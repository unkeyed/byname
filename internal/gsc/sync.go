package gsc

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// tables are the dimension sets the sync pulls, each into its own table,
// the site totals first.
// Every set starts with date, so every row belongs to one day and Google
// reports firstIncompleteDate for it. keys are the remaining dimensions,
// which are also the table's key columns after property and date.
var tables = []struct {
	name string
	keys []string
}{
	{"gsc_site_daily", nil},
	{"gsc_query_daily", []string{"query"}},
	{"gsc_page_daily", []string{"page"}},
	{"gsc_query_page_daily", []string{"query", "page"}},
}

const (
	// chunkDays is how many days one round of requests covers, so a
	// 16-month backfill commits as it goes and resumes where it stopped.
	chunkDays = 30
	// syncLock keeps two replicas from syncing at once.
	syncLock = `hashtext('byname.gsc.sync')`
)

// SyncIfDue syncs the connected property once a day, and at once after a
// property is picked or the credential replaced. A failed sync retries an
// hour later, unless someone picks a property or reconnects first. It
// returns at once when another process is syncing.
func (s *Service) SyncIfDue(ctx context.Context) error {
	return s.locked(ctx, func() error {
		var due bool
		err := s.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM gsc_connection
			WHERE property IS NOT NULL
			AND (last_sync_at IS NULL OR last_sync_at <= $1::timestamptz - interval '1 day' OR connected_at > last_sync_at)
			AND NOT (last_sync_error IS NOT NULL AND EXISTS (SELECT 1 FROM job_run
				WHERE job = 'gsc_sync' AND status = 'failed' AND started_at > $1::timestamptz - interval '1 hour')))`,
			s.now()).Scan(&due)
		if err != nil || !due {
			return err
		}
		return s.sync(ctx)
	})
}

// Sync syncs the connected property now, whether or not it is due, and
// waits for a sync another process is running to finish first.
func (s *Service) Sync(ctx context.Context) error {
	if _, err := s.connection(ctx); err != nil {
		return err
	}
	return s.lockedWait(ctx, func() error { return s.sync(ctx) })
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
		WHERE job = 'gsc_sync' AND status = 'running'`, s.now()); err != nil {
		return err
	}
	return f()
}

// sync pulls every window the stored data is missing and logs a job_run.
// Each chunk replaces its days' partial rows and upserts the rest, so a
// sync that stops halfway, or runs twice, ends in the same rows.
func (s *Service) sync(ctx context.Context) error {
	c, err := s.connection(ctx)
	if err != nil || c.property == "" {
		return err
	}
	var job int64
	if err := s.db.QueryRow(ctx, `INSERT INTO job_run (job, trigger, started_at) VALUES ('gsc_sync', 'scheduled', $1) RETURNING id`, s.now()).Scan(&job); err != nil {
		return err
	}
	err = s.pull(ctx, c)
	status, msg := "completed", (*string)(nil)
	if err != nil {
		status, msg = "failed", new(err.Error())
	}
	_, ferr := s.db.Exec(ctx, `WITH j AS (UPDATE job_run SET status = $2, error = $3, finished_at = $4 WHERE id = $1)
		UPDATE gsc_connection SET last_sync_error = $3,
			last_sync_at = CASE WHEN $3::text IS NULL THEN $4 ELSE last_sync_at END
		WHERE property = $5`, job, status, msg, s.now(), c.property)
	if err == nil {
		err = ferr
	}
	return err
}

func (s *Service) pull(ctx context.Context, c connection) error {
	ranges, err := s.windows(ctx, c, s.today())
	if err != nil {
		return err
	}
	api := s.api(ctx, c.tokens)
	for _, r := range ranges {
		for from := r[0]; !r[1].before(from); from = from.add(chunkDays) {
			to := from.add(chunkDays - 1)
			if r[1].before(to) {
				to = r[1]
			}
			if err := s.pullChunk(ctx, api, c.property, from, to); err != nil {
				return fmt.Errorf("sync %s to %s: %w", from, to, err)
			}
		}
	}
	return nil
}

// windows are the date ranges to pull, oldest first. A first sync after
// connecting, picking a property or turning backfill on pulls every day
// Google keeps (16 months) with backfill, else the last 28 days, plus
// anything stored that is not final. Later syncs pull from the first day
// not stored as final through today. A first sync that stops halfway runs
// again in full, so a backfill never leaves a hole.
func (s *Service) windows(ctx context.Context, c connection, today day) ([][2]day, error) {
	earliest := today.add(-27)
	if c.backfill {
		earliest = dayOf(today.t.AddDate(0, -16, 0))
	}
	var resume *time.Time
	err := s.db.QueryRow(ctx, `SELECT coalesce(
			(SELECT min(date) FROM gsc_site_daily WHERE property = $1 AND NOT is_final),
			(SELECT max(date) + 1 FROM gsc_site_daily WHERE property = $1))`, c.property).Scan(&resume)
	if err != nil {
		return nil, err
	}
	start := earliest
	if resume != nil && (c.lastSync != nil || !c.backfill) {
		start = dayOf(*resume)
	}
	if today.before(start) {
		return nil, nil
	}
	return [][2]day{{start, today}}, nil
}

// pullChunk fetches and writes each dimension set for from to to in turn,
// so a sync holds one set in memory at a time. Days on or after the
// earliest firstIncompleteDate a response reported are stored as partial.
// The site totals are written last: windows resumes from them, so a chunk
// that fails partway is pulled again in full.
func (s *Service) pullChunk(ctx context.Context, api googleAPI, property string, from, to day) error {
	finalBefore := to.add(1).String()
	fetch := func(keys []string) ([]queryRow, error) {
		rows, first, err := api.queryAll(ctx, property, from, to, append([]string{"date"}, keys...))
		if first != "" && first < finalBefore {
			finalBefore = first
		}
		return rows, err
	}
	write := func(name string, keys []string, rows []queryRow) error {
		return pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `DELETE FROM `+name+` WHERE property = $1 AND date BETWEEN $2 AND $3 AND NOT is_final`,
				property, from.date(), to.date()); err != nil {
				return err
			}
			return upsert(ctx, tx, name, keys, property, rows, finalBefore)
		})
	}
	site, err := fetch(nil)
	if err != nil {
		return err
	}
	for _, t := range tables[1:] {
		rows, err := fetch(t.keys)
		if err != nil {
			return err
		}
		if err := write(t.name, t.keys, rows); err != nil {
			return err
		}
	}
	return write(tables[0].name, nil, site)
}

// upsert writes rows into table, rowLimit at a time. A final row is never
// replaced by a partial one. Rows whose query is empty would be anonymized
// queries, which never count as a query, so they are dropped; so is a
// repeat of a key, which paging over fresh data can return, and a key too
// long for a Postgres index entry.
func upsert(ctx context.Context, tx pgx.Tx, table string, keys []string, property string, rows []queryRow, finalBefore string) error {
	cols := append([]string{"date"}, keys...)
	var b strings.Builder
	fmt.Fprintf(&b, `INSERT INTO %s (property, %s, clicks, impressions, position, is_final) SELECT $1, r.* FROM unnest($2::date[]`, table, strings.Join(cols, ", "))
	for i := range keys {
		fmt.Fprintf(&b, `, $%d::text[]`, i+3)
	}
	n := len(keys) + 3
	fmt.Fprintf(&b, `, $%d::bigint[], $%d::bigint[], $%d::float8[], $%d::bool[]) AS r ON CONFLICT (property, %s) DO UPDATE SET
		clicks = excluded.clicks, impressions = excluded.impressions, position = excluded.position, is_final = excluded.is_final
		WHERE NOT %s.is_final OR excluded.is_final`, n, n+1, n+2, n+3, strings.Join(cols, ", "), table)
	sql := b.String()

	seen := map[string]bool{}
	for start := 0; start < len(rows); start += rowLimit {
		batch := rows[start:min(start+rowLimit, len(rows))]
		dates := make([]string, 0, len(batch))
		keyCols := make([][]string, len(keys))
		var clicks, impressions []int64
		var positions []float64
		var final []bool
		for _, r := range batch {
			if len(r.Keys) != len(cols) || anonymized(keys, r.Keys[1:]) || oversize(r.Keys) {
				continue
			}
			id := strings.Join(r.Keys, "\x00")
			if seen[id] {
				continue
			}
			seen[id] = true
			dates = append(dates, r.Keys[0])
			for i := range keys {
				keyCols[i] = append(keyCols[i], r.Keys[i+1])
			}
			clicks = append(clicks, int64(math.Round(r.Clicks)))
			impressions = append(impressions, int64(math.Round(r.Impressions)))
			positions = append(positions, r.Position)
			final = append(final, r.Keys[0] < finalBefore)
		}
		args := []any{property, dates}
		for _, k := range keyCols {
			args = append(args, k)
		}
		args = append(args, clicks, impressions, positions, final)
		if _, err := tx.Exec(ctx, sql, args...); err != nil {
			return err
		}
	}
	return nil
}

func anonymized(keys, values []string) bool {
	for i, k := range keys {
		if k == "query" && strings.TrimSpace(values[i]) == "" {
			return true
		}
	}
	return false
}

// maxKeyBytes keeps a row's key inside a btree index entry (about 2.7 kB).
const maxKeyBytes = 2000

func oversize(keys []string) bool {
	n := 0
	for _, k := range keys {
		n += len(k)
	}
	return n > maxKeyBytes
}
