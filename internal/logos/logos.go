// Package logos looks up each brand's logo and colour once, from
// Context.dev through Treg, and serves the logo from Byname, so the
// browser never calls a third party. It looks up your brand, your
// competitors and the brands answers name that you don't track, most
// named first, at most a set number of new brands a month, and only
// while it is on, a Treg token is stored and onboarding is finished.
package logos

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/unkeyed/byname/internal/db"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/unkeyed/byname/internal/config"
	"github.com/unkeyed/byname/internal/core"
	"github.com/unkeyed/byname/internal/engines"
	"github.com/unkeyed/byname/internal/money"
)

// Config is what logos reads from internal/config.
type Config interface {
	Treg(ctx context.Context) (engines.Treg, bool, error)
	Onboarded(ctx context.Context) (bool, error)
	Settings(ctx context.Context) (config.SettingsView, error)
}

type Service struct {
	db     db.Pool
	config Config
	client *http.Client
	now    func() time.Time
	log    *slog.Logger
}

type Options struct {
	// Client downloads logo images; it defaults to one with a 10 second
	// timeout.
	Client *http.Client
	Now    func() time.Time
	Log    *slog.Logger
}

func New(db db.Pool, cfg Config, o Options) *Service {
	if o.Client == nil {
		o.Client = &http.Client{Timeout: 10 * time.Second}
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Log == nil {
		o.Log = slog.Default()
	}
	return &Service{db: db, config: cfg, client: o.Client, now: o.Now, log: o.Log}
}

// maxImage is the largest logo stored, which 011_other_brands.sql checks.
const maxImage = 512 << 10

// maxAttempts is how many times a brand whose lookup failed is tried; a
// brand Context.dev has nothing for is never tried again.
const maxAttempts = 3

// retryAfter is how long a failed lookup waits before the next try.
const retryAfter = 24 * time.Hour

// mimes are the image types stored, as 011_other_brands.sql lists them.
var mimes = []string{"image/png", "image/jpeg", "image/webp", "image/gif", "image/svg+xml", "image/x-icon"}

// Logo is what the brand lists draw: whether a logo is stored, and the
// brand's colour.
type Logo struct {
	BrandID int64  `json:"brand_id"`
	Domain  string `json:"domain"`
	Color   string `json:"color" jsonschema:"the brand's main colour as #rrggbb, or empty"`
	// Version changes when the logo does, so a URL with ?v= can be cached.
	Version int64 `json:"version" jsonschema:"changes when the logo changes; add it to the logo URL as ?v= to cache it"`
}

// Logos lists every brand that has a logo.
func (s *Service) Logos(ctx context.Context) ([]Logo, error) {
	rows, err := s.db.Query(ctx, `SELECT brand_id, domain, coalesce(color, ''), extract(epoch FROM looked_up_at)::bigint FROM brand_logo ORDER BY brand_id`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Logo, error) {
		var l Logo
		return l, r.Scan(&l.BrandID, &l.Domain, &l.Color, &l.Version)
	})
}

// Image is one brand's logo file.
type Image struct {
	Bytes []byte
	MIME  string
}

// Image returns a brand's logo, or core.ErrNotFound when it has none.
func (s *Service) Image(ctx context.Context, brandID int64) (Image, error) {
	var img Image
	err := s.db.QueryRow(ctx, `SELECT image, mime FROM brand_logo WHERE brand_id = $1`, brandID).Scan(&img.Bytes, &img.MIME)
	if errors.Is(err, pgx.ErrNoRows) {
		return img, fmt.Errorf("%w: brand %d has no logo", core.ErrNotFound, brandID)
	}
	return img, err
}

// CostPerBrand is what looking up one brand costs: the mean of past
// lookups that reported a cost, or the tool's sample price before any did.
func CostPerBrand(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}) (float64, error) {
	var mean *float64
	err := q.QueryRow(ctx, `SELECT avg(cost_usd)::float8 FROM brand_call WHERE purpose = 'lookup' AND status = 'ok' AND cost_usd IS NOT NULL`).Scan(&mean)
	if mean == nil {
		return engines.BrandByDomain.SampleCostUSD, err
	}
	return *mean, err
}

// MonthLookups is how many lookups this month the monthly limit counts:
// every one that found the brand or was charged, since a miss is free.
func MonthLookups(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, start time.Time) (int, error) {
	var n int
	err := q.QueryRow(ctx, `SELECT count(*) FROM brand_call WHERE purpose = 'lookup' AND (status = 'ok' OR cost_usd > 0) AND called_at >= $1`, start).Scan(&n)
	return n, err
}

// lock keeps two replicas from looking up the same brands at once.
var lock = db.EntityLock("byname.logos")

// due selects the brands to look up, most wanted first: yours, then your
// competitors, then the brands answers name most that you don't track. A
// brand is due until it has a logo, Context.dev had nothing for it, or
// maxAttempts lookups failed; a failed or refused one waits retryAfter
// ($1 is that moment), and a refused one is no attempt. $2 is how many may
// still be looked up this month.
var due = fmt.Sprintf(`SELECT b.id, b.name, coalesce(b.domains[1], '') FROM brand b
	WHERE b.kind IN ('self', 'competitor', 'untracked')
	AND NOT EXISTS (SELECT 1 FROM brand_logo l WHERE l.brand_id = b.id)
	AND NOT EXISTS (SELECT 1 FROM brand_call c WHERE c.purpose = 'lookup' AND c.brand_id = b.id
		AND (c.status IN ('ok', 'not_found') OR c.called_at >= $1))
	AND (SELECT count(*) FROM brand_call c WHERE c.purpose = 'lookup' AND c.brand_id = b.id AND c.status = 'failed') < %d
	AND (b.domains[1] IS NOT NULL OR char_length(b.name) >= 3)
	ORDER BY b.kind = 'self' DESC, b.kind = 'competitor' DESC,
		(SELECT count(*) FROM discovered_mention d WHERE d.brand_id = b.id) DESC, b.id
	LIMIT $2`, maxAttempts)

type brand struct {
	id           int64
	name, domain string
}

// Tick is the scheduler's call each minute: it looks up the brands that
// are due, as a brand_logos job logged only when there is one. It holds an
// advisory lock and returns at once when another process holds it.
func (s *Service) Tick(ctx context.Context) error {
	unlock, ok, err := db.TryLock(ctx, s.db, lock)
	if err != nil || !ok {
		return err
	}
	defer unlock()
	if _, err := s.db.Exec(ctx, `UPDATE job_run SET status = 'failed', finished_at = $1,
			error = 'the process stopped before these lookups finished; the logos it stored stay'
		WHERE job = 'brand_logos' AND status = 'running'`, s.now()); err != nil {
		return err
	}
	var on bool
	var limit int
	if err := s.db.QueryRow(ctx, `SELECT brand_logos, brand_logo_month_limit FROM instance_settings`).Scan(&on, &limit); err != nil || !on {
		return err
	}
	if done, err := s.config.Onboarded(ctx); err != nil || !done {
		return err
	}
	treg, ok, err := s.config.Treg(ctx)
	if err != nil || !ok {
		return err
	}
	st, err := s.config.Settings(ctx)
	if err != nil {
		return err
	}
	loc := st.Schedule().Location
	local := s.now().In(loc)
	month := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, loc)
	used, err := MonthLookups(ctx, s.db, month)
	if err != nil || used >= limit {
		return err
	}
	rows, err := s.db.Query(ctx, due, s.now().Add(-retryAfter), limit-used)
	if err != nil {
		return err
	}
	brands, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (brand, error) {
		var b brand
		return b, r.Scan(&b.id, &b.name, &b.domain)
	})
	if err != nil || len(brands) == 0 {
		return err
	}
	var job int64
	if err := s.db.QueryRow(ctx, `INSERT INTO job_run (job, trigger, status, started_at) VALUES ('brand_logos', 'scheduled', 'running', $1) RETURNING id`, s.now()).Scan(&job); err != nil {
		return err
	}
	status, msg := "completed", (*string)(nil)
	for _, b := range brands {
		if over, err := s.overCap(ctx, st, month); err != nil || over != "" {
			status, msg = "capped", &over
			if err != nil {
				status, msg = "failed", new(err.Error())
			}
			break
		}
		refused, err := s.lookUp(ctx, treg, job, b)
		if err != nil {
			status, msg = "failed", new(err.Error())
			break
		}
		if refused != nil {
			status, msg = "refused", new(refused.Error())
			break
		}
	}
	_, err = s.db.Exec(context.WithoutCancel(ctx), `UPDATE job_run SET status = $2, error = $3, finished_at = $4 WHERE id = $1`, job, status, msg, s.now())
	return err
}

// overCap is why lookups must stop for the month's spend cap, or "".
func (s *Service) overCap(ctx context.Context, st config.SettingsView, month time.Time) (string, error) {
	if st.SpendCapMonthUSD == nil {
		return "", nil
	}
	var spent float64
	if err := s.db.QueryRow(ctx, `SELECT coalesce(sum(usd), 0)::float8 FROM spend WHERE at >= $1`, month).Scan(&spent); err != nil {
		return "", err
	}
	if spent >= *st.SpendCapMonthUSD {
		return fmt.Sprintf("stopped looking up logos: this month has spent %s; the monthly spend cap is %s", money.Format(spent), money.Format(*st.SpendCapMonthUSD)), nil
	}
	return "", nil
}

// record is Context.dev's brand record, the fields Byname keeps.
type record struct {
	Status string `json:"status"`
	Brand  struct {
		Domain string `json:"domain"`
		Colors []struct {
			Hex string `json:"hex"`
		} `json:"colors"`
		Logos []logo `json:"logos"`
	} `json:"brand"`
}

type logo struct {
	URL        string `json:"url"`
	Mode       string `json:"mode"`
	Type       string `json:"type"`
	Resolution struct {
		Width  int `json:"width"`
		Height int `json:"height"`
	} `json:"resolution"`
}

// lookUp asks Context.dev for one brand, by its domain when it has one,
// otherwise by name, and stores the logo it finds. It returns Treg's
// refusal when Treg declined the call.
func (s *Service) lookUp(ctx context.Context, treg engines.Treg, job int64, b brand) (*engines.Refusal, error) {
	tool, q := engines.BrandByName, url.Values{"name": {b.name}}
	if b.domain != "" {
		tool, q = engines.BrandByDomain, url.Values{"domain": {b.domain}}
	}
	resp, err := treg.CallTool(ctx, engines.ToolCall{Method: http.MethodGet, Tool: tool.ID, Query: q})
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	refusal, _ := errors.AsType[*engines.Refusal](err)
	status, reason := "ok", ""
	var img Image
	var rec record
	switch {
	case refusal != nil:
		status, reason = "refused", err.Error()
	case err != nil:
		status, reason = "failed", err.Error()
	case resp.StatusCode >= 500:
		status, reason = "failed", fmt.Sprintf("Context.dev answered HTTP %d", resp.StatusCode)
	case resp.StatusCode/100 != 2:
		status = "not_found"
	default:
		if jerr := json.Unmarshal(resp.Body, &rec); jerr != nil {
			status, reason = "failed", "Context.dev's answer did not parse: "+jerr.Error()
		} else if rec.Status != "ok" || len(rec.Brand.Logos) == 0 {
			status = "not_found"
		} else if img, err = s.download(ctx, pickLogo(rec)); err != nil {
			status, reason = "failed", err.Error()
		}
	}
	return refusal, pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var msg *string
		if status == "failed" || status == "refused" {
			msg = &reason
		}
		if _, err := tx.Exec(ctx, `INSERT INTO brand_call (purpose, brand_id, job_run_id, tool, status, cost_usd, treg_call_id, error, called_at)
			VALUES ('lookup', $1, $2, $3, $4, $5, $6, $7, $8)`, b.id, job, tool.ID, status, resp.CostUSD, resp.CallID, msg, s.now()); err != nil {
			return err
		}
		if status != "ok" {
			return nil
		}
		domain := hostOnly(rec.Brand.Domain)
		if domain == "" {
			domain = b.domain
		}
		var color *string
		if len(rec.Brand.Colors) > 0 {
			if c := strings.ToLower(rec.Brand.Colors[0].Hex); hexColor(c) {
				color = &c
			}
		}
		_, err := tx.Exec(ctx, `INSERT INTO brand_logo (brand_id, domain, color, image, mime, looked_up_at) VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (brand_id) DO UPDATE SET domain = excluded.domain, color = excluded.color, image = excluded.image,
				mime = excluded.mime, looked_up_at = excluded.looked_up_at`, b.id, domain, color, img.Bytes, img.MIME, s.now())
		if err != nil {
			return err
		}
		// Only a found brand takes Context.dev's domain: a name can match
		// the wrong company, and a tracked brand's domains decide scores.
		_, err = tx.Exec(ctx, `UPDATE brand SET domains = ARRAY[$2::text] WHERE id = $1 AND key IS NOT NULL AND cardinality(domains) = 0 AND $2 <> ''`, b.id, domain)
		return err
	})
}

// pickLogo is the logo that reads best in a small square on either
// theme: an icon over a wordmark, one on its own background over a light
// one over a dark one, and the squarest.
func pickLogo(r record) string {
	modes := map[string]int{"has_opaque_background": 0, "light": 1, "dark": 3}
	rank := func(l logo) []int {
		typ := 1
		if l.Type == "icon" || l.Type == "symbol" {
			typ = 0
		}
		mode, ok := modes[l.Mode]
		if !ok {
			mode = 2
		}
		w, h := max(l.Resolution.Width, 1), max(l.Resolution.Height, 1)
		return []int{typ, mode, max(w, h) * 100 / min(w, h)}
	}
	return slices.MinFunc(r.Brand.Logos, func(a, b logo) int { return slices.Compare(rank(a), rank(b)) }).URL
}

// download fetches a logo image, at most maxImage bytes of one of mimes.
func (s *Service) download(ctx context.Context, link string) (Image, error) {
	u, err := url.Parse(link)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return Image{}, fmt.Errorf("the logo link %q is not a web address", link)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Image{}, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return Image{}, fmt.Errorf("download the logo: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Image{}, fmt.Errorf("download the logo: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxImage+1))
	if err != nil {
		return Image{}, fmt.Errorf("download the logo: %w", err)
	}
	if len(body) == 0 || len(body) > maxImage {
		return Image{}, fmt.Errorf("the logo is empty or over %d KB", maxImage>>10)
	}
	typ, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if !slices.Contains(mimes, typ) {
		typ, _, _ = mime.ParseMediaType(http.DetectContentType(body))
	}
	if !slices.Contains(mimes, typ) {
		return Image{}, fmt.Errorf("the logo is %s, not an image Byname keeps", typ)
	}
	return Image{Bytes: body, MIME: typ}, nil
}

func hostOnly(d string) string {
	d = strings.ToLower(strings.TrimSpace(d))
	d = strings.TrimPrefix(d, "www.")
	if !strings.Contains(d, ".") || strings.ContainsAny(d, " /:") {
		return ""
	}
	return d
}

func hexColor(c string) bool {
	if len(c) != 7 || c[0] != '#' {
		return false
	}
	for _, r := range c[1:] {
		if !strings.ContainsRune("0123456789abcdef", r) {
			return false
		}
	}
	return true
}
