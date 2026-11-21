package config

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/unkeyed/byname/internal/core"
	"github.com/unkeyed/byname/internal/panel"
)

// Settings is what PUT /v1/settings replaces. Every field is required
// except the monthly cap and raw retention, where null means none.
type Settings struct {
	Brand          SelfBrand       `json:"brand"`
	MatchMode      panel.MatchMode `json:"match_mode"`
	Market         string          `json:"market" jsonschema:"ISO 3166-1 alpha-2 country, such as US"`
	Language       string          `json:"language" jsonschema:"ISO 639-1 language, such as en"`
	TimeZone       string          `json:"time_zone" jsonschema:"IANA time zone, such as Europe/Berlin"`
	CheckEveryDays int             `json:"check_every_days" jsonschema:"1 to 30"`
	CheckTime      string          `json:"check_time" jsonschema:"HH:MM in time_zone"`
	SpendCapRunUSD float64         `json:"spend_cap_run_usd" jsonschema:"stop queueing calls once a check has spent this much"`
	// SpendCapMonthUSD stops a check from starting, and from queueing more
	// calls, once the calendar month's checks have spent this much.
	SpendCapMonthUSD *float64 `json:"spend_cap_month_usd"`
	Concurrency      int      `json:"concurrency" jsonschema:"1 to 32"`
	Retries          int      `json:"retries" jsonschema:"0 to 3"`
	TimeoutS         int      `json:"timeout_s" jsonschema:"30 to 600 seconds per call"`
	RawRetentionDays *int     `json:"raw_retention_days" jsonschema:"days to keep raw responses, null keeps them forever"`
}

// SelfBrand is the instance's own brand. Domains are hostnames, the first
// the primary domain; a link to any of them or a subdomain is a citation.
type SelfBrand struct {
	Name    string   `json:"name"`
	Aliases []string `json:"aliases"`
	Domains []string `json:"domains"`
}

// SettingsView is the settings with the schedule's state.
type SettingsView struct {
	Settings
	ChecksPaused bool       `json:"checks_paused"`
	NextCheckAt  *time.Time `json:"next_check_at"`
	LastCheckAt  *time.Time `json:"last_check_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

const settingsColumns = `match_mode, market, language, time_zone, check_every_days,
	to_char(check_time, 'HH24:MI'), spend_cap_run_usd::float8, spend_cap_month_usd::float8, concurrency, retries,
	timeout_s, raw_retention_days, checks_paused, next_check_at, last_check_at, updated_at`

func (s *Service) Settings(ctx context.Context) (SettingsView, error) {
	return getSettings(ctx, s.db)
}

func getSettings(ctx context.Context, q querier) (SettingsView, error) {
	var v SettingsView
	err := q.QueryRow(ctx, `SELECT `+settingsColumns+` FROM instance_settings`).Scan(
		&v.MatchMode, &v.Market, &v.Language, &v.TimeZone, &v.CheckEveryDays, &v.CheckTime,
		&v.SpendCapRunUSD, &v.SpendCapMonthUSD, &v.Concurrency, &v.Retries, &v.TimeoutS,
		&v.RawRetentionDays, &v.ChecksPaused, &v.NextCheckAt, &v.LastCheckAt, &v.UpdatedAt)
	if err != nil {
		return v, err
	}
	v.Brand = SelfBrand{Aliases: []string{}, Domains: []string{}}
	err = q.QueryRow(ctx, `SELECT name, aliases, domains FROM brand WHERE kind = 'self'`).Scan(&v.Brand.Name, &v.Brand.Aliases, &v.Brand.Domains)
	if err == pgx.ErrNoRows {
		err = nil
	}
	return v, err
}

var (
	countryCode  = regexp.MustCompile(`^[A-Z]{2}$`)
	languageCode = regexp.MustCompile(`^[a-z]{2}$`)
)

func (in *Settings) validate() error {
	in.Brand.Name = oneLine(in.Brand.Name)
	in.Brand.Aliases = words(in.Brand.Aliases)
	var err error
	if in.Brand.Domains, err = hostnames("brand.domains", in.Brand.Domains); err != nil {
		return err
	}
	if in.MatchMode == "" {
		in.MatchMode = panel.MatchWholeWord
	}
	_, clockErr := time.Parse("15:04", in.CheckTime)
	_, zoneErr := time.LoadLocation(in.TimeZone)
	for _, c := range []struct {
		bad bool
		msg string
	}{
		{len([]rune(in.Brand.Name)) < 2 || len([]rune(in.Brand.Name)) > 80, "brand.name must be 2 to 80 characters"},
		{len(in.Brand.Domains) == 0, "brand.domains needs your primary domain, such as example.com"},
		{in.MatchMode != panel.MatchWholeWord && in.MatchMode != panel.MatchSelfAnywhere, "match_mode must be whole_word or self_anywhere"},
		{!countryCode.MatchString(in.Market), "market must be an ISO 3166-1 alpha-2 country code such as US"},
		{!languageCode.MatchString(in.Language), "language must be an ISO 639-1 code such as en"},
		{in.TimeZone == "" || zoneErr != nil, "time_zone must be an IANA name such as Europe/Berlin"},
		{in.CheckEveryDays < 1 || in.CheckEveryDays > 30, "check_every_days must be 1 to 30"},
		{clockErr != nil, "check_time must be HH:MM, such as 06:00"},
		{!(in.SpendCapRunUSD > 0), "spend_cap_run_usd must be more than 0"},
		{in.SpendCapMonthUSD != nil && !(*in.SpendCapMonthUSD > 0), "spend_cap_month_usd must be more than 0, or null for none"},
		{in.Concurrency < 1 || in.Concurrency > 32, "concurrency must be 1 to 32"},
		{in.Retries < 0 || in.Retries > 3, "retries must be 0 to 3"},
		{in.TimeoutS < 30 || in.TimeoutS > 600, "timeout_s must be 30 to 600"},
		{in.RawRetentionDays != nil && *in.RawRetentionDays < 1, "raw_retention_days must be at least 1, or null to keep raw responses forever"},
	} {
		if c.bad {
			return fmt.Errorf("%w: %s", core.ErrInvalid, c.msg)
		}
	}
	return nil
}

// PutSettings replaces the settings and the instance's own brand. When the
// schedule changes, or has never been set, it recomputes the next check.
func (s *Service) PutSettings(ctx context.Context, in Settings) (SettingsView, error) {
	if err := in.validate(); err != nil {
		return SettingsView{}, err
	}
	var out SettingsView
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		old, err := getSettings(ctx, tx)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO brand (name, aliases, domains, kind) VALUES ($1, $2, $3, 'self')
			ON CONFLICT (kind) WHERE kind = 'self'
			DO UPDATE SET name = excluded.name, aliases = excluded.aliases, domains = excluded.domains, updated_at = now()`,
			in.Brand.Name, in.Brand.Aliases, in.Brand.Domains)
		if isUniqueViolation(err, "brand_name") {
			return fmt.Errorf("%w: a competitor or technology is already named %q; rename or delete it first", core.ErrConflict, in.Brand.Name)
		}
		if err != nil {
			return err
		}
		next := old.NextCheckAt
		if next == nil || old.CheckEveryDays != in.CheckEveryDays || old.CheckTime != in.CheckTime || old.TimeZone != in.TimeZone {
			n := NextCheck(s.now(), old.LastCheckAt, in.Schedule())
			next = &n
		}
		_, err = tx.Exec(ctx, `UPDATE instance_settings SET match_mode = $1, market = $2, language = $3, time_zone = $4,
			check_every_days = $5, check_time = $6::time, spend_cap_run_usd = $7, spend_cap_month_usd = $8,
			concurrency = $9, retries = $10, timeout_s = $11, raw_retention_days = $12, next_check_at = $13, updated_at = now()`,
			in.MatchMode, in.Market, in.Language, in.TimeZone, in.CheckEveryDays, in.CheckTime, in.SpendCapRunUSD,
			in.SpendCapMonthUSD, in.Concurrency, in.Retries, in.TimeoutS, in.RawRetentionDays, next)
		if err != nil {
			return err
		}
		out, err = getSettings(ctx, tx)
		return err
	})
	return out, err
}

// SetPaused pauses or resumes scheduled checks. Resuming moves a next
// check that passed while paused to the next one due, so resuming never
// starts a check by itself. Run now works either way.
func (s *Service) SetPaused(ctx context.Context, paused bool) (SettingsView, error) {
	var out SettingsView
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		cur, err := getSettings(ctx, tx)
		if err != nil {
			return err
		}
		next := cur.NextCheckAt
		if !paused && next != nil && !next.After(s.now()) {
			n := NextCheck(s.now(), cur.LastCheckAt, cur.Schedule())
			next = &n
		}
		if _, err := tx.Exec(ctx, `UPDATE instance_settings SET checks_paused = $1, next_check_at = $2, updated_at = now()`, paused, next); err != nil {
			return err
		}
		out, err = getSettings(ctx, tx)
		return err
	})
	return out, err
}

// Schedule is when checks run: every Days days at Clock in Location.
type Schedule struct {
	Days     int
	Hour     int
	Minute   int
	Location *time.Location
}

// Schedule parses the validated schedule fields.
func (in Settings) Schedule() Schedule {
	loc, err := time.LoadLocation(in.TimeZone)
	if err != nil {
		loc = time.UTC
	}
	t, _ := time.Parse("15:04", in.CheckTime)
	return Schedule{Days: in.CheckEveryDays, Hour: t.Hour(), Minute: t.Minute(), Location: loc}
}

// NextCheck is when the next check is due: Days after the last one's day,
// at the scheduled time. With no last check, or when that time has already
// passed, it is the next scheduled time from now, so a schedule change
// never makes a check due at once.
func NextCheck(now time.Time, last *time.Time, s Schedule) time.Time {
	local := now.In(s.Location)
	first := time.Date(local.Year(), local.Month(), local.Day(), s.Hour, s.Minute, 0, 0, s.Location)
	if first.Before(now) {
		first = time.Date(local.Year(), local.Month(), local.Day()+1, s.Hour, s.Minute, 0, 0, s.Location)
	}
	if last == nil {
		return first
	}
	l := last.In(s.Location)
	due := time.Date(l.Year(), l.Month(), l.Day()+s.Days, s.Hour, s.Minute, 0, 0, s.Location)
	if due.Before(now) {
		return first
	}
	return due
}
