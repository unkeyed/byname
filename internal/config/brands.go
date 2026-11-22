package config

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/unkeyed/byname/internal/core"
	"github.com/unkeyed/byname/internal/panel"
)

// Brand is one row of the brand table. Kind self is the instance's own
// brand, edited through Settings.
type Brand struct {
	ID      int64           `json:"id"`
	Name    string          `json:"name"`
	Aliases []string        `json:"aliases"`
	Domains []string        `json:"domains"`
	Kind    panel.BrandKind `json:"kind"`
}

// BrandInput is a competitor or technology. Names are unique ignoring case.
type BrandInput struct {
	Name    string          `json:"name"`
	Aliases []string        `json:"aliases"`
	Domains []string        `json:"domains"`
	Kind    panel.BrandKind `json:"kind" jsonschema:"competitor or technology"`
}

func (in *BrandInput) validate() error {
	in.Name = oneLine(in.Name)
	in.Aliases = words(in.Aliases)
	if in.Kind == "" {
		in.Kind = panel.KindCompetitor
	}
	var err error
	if in.Domains, err = hostnames("domains", in.Domains); err != nil {
		return err
	}
	switch {
	case len([]rune(in.Name)) < 1 || len([]rune(in.Name)) > 80:
		return fmt.Errorf("%w: name must be 1 to 80 characters", core.ErrInvalid)
	case in.Kind == panel.KindSelf:
		return fmt.Errorf("%w: your own brand is set with PUT /v1/settings; kind must be competitor or technology", core.ErrInvalid)
	case in.Kind != panel.KindCompetitor && in.Kind != panel.KindTechnology:
		return fmt.Errorf("%w: kind must be competitor or technology, got %q", core.ErrInvalid, in.Kind)
	}
	return nil
}

const brandColumns = `id, name, aliases, domains, kind`

func scanBrand(row pgx.Row) (Brand, error) {
	var b Brand
	err := row.Scan(&b.ID, &b.Name, &b.Aliases, &b.Domains, &b.Kind)
	if err == pgx.ErrNoRows {
		err = fmt.Errorf("%w: no such brand", core.ErrNotFound)
	}
	return b, err
}

// Brands lists the brand table, your own brand first, then by name.
func (s *Service) Brands(ctx context.Context) ([]Brand, error) {
	rows, err := s.db.Query(ctx, `SELECT `+brandColumns+` FROM brand ORDER BY kind <> 'self', lower(name)`)
	if err != nil {
		return nil, err
	}
	out := []Brand{}
	for rows.Next() {
		b, err := scanBrand(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// PanelBrands is the brand table as panel.Score reads it.
func (s *Service) PanelBrands(ctx context.Context) ([]panel.Brand, error) {
	brands, err := s.Brands(ctx)
	out := make([]panel.Brand, len(brands))
	for i, b := range brands {
		out[i] = panel.Brand{ID: panel.BrandID(b.ID), Name: b.Name, Aliases: b.Aliases, Domains: b.Domains, Kind: b.Kind}
	}
	return out, err
}

// UpsertBrand adds a competitor or technology, or replaces the one with the
// same name ignoring case.
func (s *Service) UpsertBrand(ctx context.Context, in BrandInput) (Brand, error) {
	if err := in.validate(); err != nil {
		return Brand{}, err
	}
	var b Brand
	err := s.brandChange(ctx, func(tx pgx.Tx) (err error) {
		b, err = scanBrand(tx.QueryRow(ctx, `INSERT INTO brand (name, aliases, domains, kind) VALUES ($1, $2, $3, $4)
			ON CONFLICT (lower(name)) DO UPDATE SET name = excluded.name, aliases = excluded.aliases,
				domains = excluded.domains, kind = excluded.kind, updated_at = now()
			WHERE brand.kind <> 'self'
			RETURNING `+brandColumns, in.Name, in.Aliases, in.Domains, in.Kind))
		return err
	})
	if errors.Is(err, core.ErrNotFound) {
		return b, fmt.Errorf("%w: %q is your own brand; edit it with PUT /v1/settings", core.ErrConflict, in.Name)
	}
	return b, err
}

// brandChange runs a change to the brand table and marks the questions
// again, since another brand's longer name can hide yours in a question.
func (s *Service) brandChange(ctx context.Context, f func(pgx.Tx) error) error {
	return pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if err := f(tx); err != nil {
			return err
		}
		return markBrandQuestions(ctx, tx)
	})
}

// UpdateBrand replaces a competitor or technology by id, which can rename it.
func (s *Service) UpdateBrand(ctx context.Context, id int64, in BrandInput) (Brand, error) {
	if err := in.validate(); err != nil {
		return Brand{}, err
	}
	var b Brand
	err := s.brandChange(ctx, func(tx pgx.Tx) (err error) {
		b, err = scanBrand(tx.QueryRow(ctx, `UPDATE brand SET name = $2, aliases = $3, domains = $4, kind = $5, updated_at = now()
			WHERE id = $1 AND kind <> 'self' RETURNING `+brandColumns, id, in.Name, in.Aliases, in.Domains, in.Kind))
		return err
	})
	if isUniqueViolation(err, "brand_name") {
		return b, fmt.Errorf("%w: another brand is already named %q", core.ErrConflict, in.Name)
	}
	return b, err
}

// DeleteBrand removes a competitor or technology and its recorded mentions.
func (s *Service) DeleteBrand(ctx context.Context, id int64) error {
	return s.brandChange(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `DELETE FROM brand WHERE id = $1 AND kind <> 'self'`, id)
		if err == nil && tag.RowsAffected() == 0 {
			return fmt.Errorf("%w: no such competitor or technology; your own brand is edited with PUT /v1/settings", core.ErrNotFound)
		}
		return err
	})
}
