package discovery

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/unkeyed/byname/internal/config"
	"github.com/unkeyed/byname/internal/core"
	"github.com/unkeyed/byname/internal/engines"
	"github.com/unkeyed/byname/internal/logos"
)

// Settings is what finding other brands does and costs: reading each new
// answer for the brands it names, and looking up brands' logos.
type Settings struct {
	HasToken  bool          `json:"has_token" jsonschema:"a Treg token is stored; both go through Treg and need one"`
	Onboarded bool          `json:"onboarded" jsonschema:"onboarding is finished; nothing runs before"`
	Find      FindSettings  `json:"find"`
	Logos     LogosSettings `json:"logos"`
}

// FindSettings is reading each new answer for other brands.
type FindSettings struct {
	On               bool    `json:"on" jsonschema:"new answers are read for other brands while a Treg token is stored"`
	CostPerAnswerUSD float64 `json:"cost_per_answer_usd" jsonschema:"what reading one answer costs: the mean of past reads, or the sampled price before any"`
	Model            string  `json:"model" jsonschema:"the model that reads answers, through Treg"`
	MinQuestions     int     `json:"min_questions" jsonschema:"a brand is listed once answers to at least this many different questions offered it as a choice"`
}

// LogosSettings is looking up brands' logos and colours on Context.dev.
type LogosSettings struct {
	On              bool    `json:"on" jsonschema:"logos are looked up while a Treg token is stored"`
	CostPerBrandUSD float64 `json:"cost_per_brand_usd" jsonschema:"what looking up one new brand costs: the mean of past lookups, or Context.dev's price before any; a brand Context.dev has nothing for costs nothing"`
	MonthLimit      int     `json:"month_limit" jsonschema:"the most new brands looked up in a calendar month"`
	MonthMaxUSD     float64 `json:"month_max_usd" jsonschema:"month_limit times cost_per_brand_usd: the most lookups can cost in a month"`
	ThisMonth       int     `json:"this_month" jsonschema:"brands looked up so far this month"`
	Logos           int     `json:"logos" jsonschema:"brands with a logo"`
}

func (s *Service) Settings(ctx context.Context) (Settings, error) {
	out := Settings{Find: FindSettings{Model: engines.BrandFinder.Model}}
	err := s.db.QueryRow(ctx, `SELECT find_brands, other_brands_min_questions, brand_logos, brand_logo_month_limit, onboarded_at IS NOT NULL,
			(SELECT count(*) FROM brand_logo) FROM instance_settings`).
		Scan(&out.Find.On, &out.Find.MinQuestions, &out.Logos.On, &out.Logos.MonthLimit, &out.Onboarded, &out.Logos.Logos)
	if err != nil {
		return out, err
	}
	if _, out.HasToken, err = s.config.Treg(ctx); err != nil {
		return out, err
	}
	if out.Find.CostPerAnswerUSD, err = config.FindCostPerAnswer(ctx, s.db); err != nil {
		return out, err
	}
	if out.Logos.CostPerBrandUSD, err = logos.CostPerBrand(ctx, s.db); err != nil {
		return out, err
	}
	out.Logos.MonthMaxUSD = out.Logos.CostPerBrandUSD * float64(out.Logos.MonthLimit)
	st, err := s.config.Settings(ctx)
	if err != nil {
		return out, err
	}
	loc := st.Schedule().Location
	local := s.now().In(loc)
	out.Logos.ThisMonth, err = logos.MonthLookups(ctx, s.db, time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, loc))
	return out, err
}

// SettingsInput changes what finding other brands does. A field left out
// keeps its setting.
type SettingsInput struct {
	Find           *bool `json:"find,omitempty" jsonschema:"true to read each new answer for other brands, which costs a fraction of a cent an answer"`
	Logos          *bool `json:"logos,omitempty" jsonschema:"true to look up the logos of your brand, your competitors and the brands answers name"`
	LogoMonthLimit *int  `json:"logo_month_limit,omitempty" jsonschema:"the most new brands looked up a month, 0 to 1000"`
	MinQuestions   *int  `json:"min_questions,omitempty" jsonschema:"list a brand once answers to at least this many different questions offered it, 1 to 50"`
}

func (s *Service) PutSettings(ctx context.Context, in SettingsInput) (Settings, error) {
	if in.LogoMonthLimit != nil && (*in.LogoMonthLimit < 0 || *in.LogoMonthLimit > 1000) {
		return Settings{}, fmt.Errorf("%w: logo_month_limit must be 0 to 1000", core.ErrInvalid)
	}
	if in.MinQuestions != nil && (*in.MinQuestions < 1 || *in.MinQuestions > 50) {
		return Settings{}, fmt.Errorf("%w: min_questions must be 1 to 50", core.ErrInvalid)
	}
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE instance_settings SET find_brands = coalesce($1, find_brands), brand_logos = coalesce($2, brand_logos),
			brand_logo_month_limit = coalesce($3, brand_logo_month_limit), other_brands_min_questions = coalesce($4, other_brands_min_questions),
			updated_at = now()`, in.Find, in.Logos, in.LogoMonthLimit, in.MinQuestions)
		return err
	})
	if err != nil {
		return Settings{}, err
	}
	return s.Settings(ctx)
}
