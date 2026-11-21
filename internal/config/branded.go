package config

import (
	"context"
	"fmt"
	"strings"

	"github.com/unkeyed/byname/internal/core"
)

// BrandedTerms are the search terms that make a Search Console query
// branded, lowercased: the ones set, or the brand's name and aliases.
func (s *Service) BrandedTerms(ctx context.Context) ([]string, error) {
	terms, _, err := brandedTerms(ctx, s.db)
	return terms, err
}

func brandedTerms(ctx context.Context, q querier) (terms []string, isDefault bool, err error) {
	var set, brand []string
	if err := q.QueryRow(ctx, `SELECT branded_terms, (SELECT array_prepend(name, aliases) FROM brand WHERE kind = 'self')
		FROM instance_settings`).Scan(&set, &brand); err != nil {
		return nil, false, err
	}
	if set != nil {
		return set, false, nil
	}
	return lowerWords(brand), true, nil
}

func lowerWords(in []string) []string {
	out := make([]string, 0, len(in))
	for _, w := range in {
		out = append(out, strings.ToLower(w))
	}
	return words(out)
}

// SetBrandedTerms replaces the branded search terms. A query containing
// any of them, ignoring case, is brand. An empty list goes back to the
// default: the brand's name and aliases.
func (s *Service) SetBrandedTerms(ctx context.Context, terms []string) (SettingsView, error) {
	terms = lowerWords(terms)
	if len(terms) > 50 {
		return SettingsView{}, fmt.Errorf("%w: at most 50 branded search terms", core.ErrInvalid)
	}
	for _, t := range terms {
		if len([]rune(t)) < 2 || len([]rune(t)) > 80 {
			return SettingsView{}, fmt.Errorf("%w: each branded search term must be 2 to 80 characters, got %q", core.ErrInvalid, t)
		}
	}
	var stored []string
	if len(terms) > 0 {
		stored = terms
	}
	if _, err := s.db.Exec(ctx, `UPDATE instance_settings SET branded_terms = $1, updated_at = now()`, stored); err != nil {
		return SettingsView{}, err
	}
	return s.Settings(ctx)
}
