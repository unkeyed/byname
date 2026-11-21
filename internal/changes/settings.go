package changes

import (
	"context"
	"fmt"
	"time"

	"github.com/unkeyed/byname/internal/core"
)

// Settings are when a change's reads fall and what counts as a move.
type Settings struct {
	InterimDays int `json:"interim_days" jsonschema:"days after the first crawl for the interim read, 7 to 90, default 17"`
	VerdictDays int `json:"verdict_days" jsonschema:"days after the first crawl for the final read, 14 to 180 and more than interim_days, default 31"`
	Thresholds
}

func (in Settings) validate() error {
	for _, c := range []struct {
		bad bool
		msg string
	}{
		{in.InterimDays < 7 || in.InterimDays > 90, "interim_days must be 7 to 90"},
		{in.VerdictDays < 14 || in.VerdictDays > 180, "verdict_days must be 14 to 180"},
		{in.VerdictDays <= in.InterimDays, "verdict_days must be more than interim_days"},
		{in.Position <= 0 || in.Position > 50, "position must be more than 0 and at most 50 places"},
		{in.CTRPoints <= 0 || in.CTRPoints > 50, "ctr_points must be more than 0 and at most 50 points"},
		{in.ImpressionsRatio <= 1 || in.ImpressionsRatio > 100, "impressions_ratio must be more than 1 and at most 100"},
	} {
		if c.bad {
			return fmt.Errorf("%w: %s", core.ErrInvalid, c.msg)
		}
	}
	return nil
}

// instanceSettings is Settings plus the instance's time zone, which
// places interim and verdict days and decides which controls still hold.
type instanceSettings struct {
	Settings
	loc *time.Location
}

func (st instanceSettings) today(now time.Time) string { return now.In(st.loc).Format(time.DateOnly) }

func (s *Service) settings(ctx context.Context) (instanceSettings, error) {
	var st instanceSettings
	var zone string
	err := s.db.QueryRow(ctx, `SELECT time_zone, change_interim_days, change_verdict_days, change_position_threshold,
			change_ctr_threshold, change_impressions_ratio FROM instance_settings`).
		Scan(&zone, &st.InterimDays, &st.VerdictDays, &st.Position, &st.CTRPoints, &st.ImpressionsRatio)
	if err != nil {
		return st, err
	}
	if st.loc, err = time.LoadLocation(zone); err != nil {
		st.loc = time.UTC
	}
	return st, nil
}

// Settings reads the read days and thresholds.
func (s *Service) Settings(ctx context.Context) (Settings, error) {
	st, err := s.settings(ctx)
	return st.Settings, err
}

// PutSettings replaces the read days and thresholds. Changes already
// crawled keep the days they were given; new reads use the new
// thresholds.
func (s *Service) PutSettings(ctx context.Context, in Settings) (Settings, error) {
	if err := in.validate(); err != nil {
		return Settings{}, err
	}
	if _, err := s.db.Exec(ctx, `UPDATE instance_settings SET change_interim_days = $1, change_verdict_days = $2,
			change_position_threshold = $3, change_ctr_threshold = $4, change_impressions_ratio = $5, updated_at = now()`,
		in.InterimDays, in.VerdictDays, in.Position, in.CTRPoints, in.ImpressionsRatio); err != nil {
		return Settings{}, err
	}
	return s.Settings(ctx)
}
