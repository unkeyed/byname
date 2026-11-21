package results

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/unkeyed/byname/internal/panel"
)

// BrandStanding is one brand's mentions over every run and line in range.
type BrandStanding struct {
	BrandID  int64           `json:"brand_id"`
	Name     string          `json:"name"`
	Kind     panel.BrandKind `json:"kind" jsonschema:"self or competitor; technologies never count"`
	Mentions int             `json:"mentions" jsonschema:"answers that named the brand"`
	Share    float64         `json:"share" jsonschema:"mentions over every brand's mentions"`
	// Rank is 1 for the most mentioned, shared by brands with equal
	// mentions, and 0 for a brand never named.
	Rank int `json:"rank"`
}

// HeatCell is how often one line's answers named one brand.
type HeatCell struct {
	BrandID int64   `json:"brand_id"`
	Named   int     `json:"named" jsonschema:"answers on this line that named the brand"`
	Rate    float64 `json:"rate" jsonschema:"named over the line's answered"`
}

// HeatColumn is one line of the brand by surface heatmap.
type HeatColumn struct {
	Series
	Models   []string   `json:"models" jsonschema:"the models the line combines; none on an app surface"`
	Answered int        `json:"answered"`
	Cells    []HeatCell `json:"cells" jsonschema:"one per brand the line's answers named; a brand missing here was named 0 times"`
}

// Competitors is share of voice and the brand by surface heatmap over the
// runs in range and the pairs in scope.
type Competitors struct {
	Runs     int `json:"runs" jsonschema:"runs counted"`
	Answered int `json:"answered" jsonschema:"question and pair answers that did not fail"`
	Mentions int `json:"mentions" jsonschema:"mentions of you and your competitors, the share's denominator"`
	// BrandsNamed is how many brands were named at least once: the M in
	// "your rank #N of M".
	BrandsNamed int             `json:"brands_named"`
	Brands      []BrandStanding `json:"brands" jsonschema:"you and every competitor named, most mentioned first; you are listed even when never named"`
	Columns     []HeatColumn    `json:"columns" jsonschema:"one per line, in pair order"`
}

// Competitors adds up brand mentions over every run started since (nil for
// all time) on the pairs in scope, with one heatmap column per line (by).
// Technologies are left out.
func (s *Service) Competitors(ctx context.Context, since *time.Time, sc Scope, by Grouping) (Competitors, error) {
	if by == "" {
		by = ByPair
	}
	if err := errors.Join(sc.check(), by.check()); err != nil {
		return Competitors{}, err
	}
	args := scopeArgs(since, sc, by)
	rows, err := s.db.Query(ctx, `SELECT `+lineKey+`, min(m.id), `+lineModels+`, count(*) FILTER (WHERE a.status <> 'failed'),
			array_agg(DISTINCT a.run_id)`+scopedFrom+scopedWhere+`GROUP BY 1, 2, 3, 4, 5`, args...)
	if err != nil {
		return Competitors{}, err
	}
	out := Competitors{Brands: []BrandStanding{}, Columns: []HeatColumn{}}
	first := map[Series]int64{}
	col := map[Series]int{}
	runs := map[int64]bool{}
	for rows.Next() {
		var c HeatColumn
		var pair int64
		var ids []int64
		if err := rows.Scan(&c.Pair.ID, &c.Pair.Surface, &c.Pair.Model, &c.Endpoint, &c.Fidelity, &pair, &c.Models, &c.Answered, &ids); err != nil {
			return Competitors{}, err
		}
		for _, id := range ids {
			runs[id] = true
		}
		first[c.Series] = pair
		c.Cells = []HeatCell{}
		out.Answered += c.Answered
		out.Columns = append(out.Columns, c)
	}
	if err := rows.Err(); err != nil {
		return Competitors{}, err
	}
	out.Runs = len(runs)
	slices.SortFunc(out.Columns, func(a, b HeatColumn) int { return cmp.Compare(first[a.Series], first[b.Series]) })
	for i, c := range out.Columns {
		col[c.Series] = i
	}

	byBrand := map[int64]*BrandStanding{}
	var self BrandStanding
	switch err := s.db.QueryRow(ctx, `SELECT id, name, kind FROM brand WHERE kind = 'self'`).Scan(&self.BrandID, &self.Name, &self.Kind); {
	case err == nil:
		byBrand[self.BrandID] = &self
	case !errors.Is(err, pgx.ErrNoRows):
		return Competitors{}, err
	}
	rows, err = s.db.Query(ctx, `SELECT `+lineKey+`, b.id, b.name, b.kind, count(*)`+scopedFrom+`
			JOIN brand_mention bm ON bm.answer_id = a.id JOIN brand b ON b.id = bm.brand_id AND b.kind <> 'technology'`+
		scopedWhere+`AND a.status <> 'failed' GROUP BY 1, 2, 3, 4, 5, 6, 7, 8`, args...)
	if err != nil {
		return Competitors{}, err
	}
	for rows.Next() {
		var sr Series
		var b BrandStanding
		var named int
		if err := rows.Scan(&sr.Pair.ID, &sr.Pair.Surface, &sr.Pair.Model, &sr.Endpoint, &sr.Fidelity, &b.BrandID, &b.Name, &b.Kind, &named); err != nil {
			return Competitors{}, err
		}
		if byBrand[b.BrandID] == nil {
			byBrand[b.BrandID] = &b
		}
		byBrand[b.BrandID].Mentions += named
		out.Mentions += named
		c := &out.Columns[col[sr]]
		c.Cells = append(c.Cells, HeatCell{BrandID: b.BrandID, Named: named, Rate: float64(named) / float64(c.Answered)})
	}
	if err := rows.Err(); err != nil {
		return Competitors{}, err
	}

	for _, b := range byBrand {
		if out.Mentions > 0 {
			b.Share = float64(b.Mentions) / float64(out.Mentions)
		}
		out.Brands = append(out.Brands, *b)
	}
	rankBrands(out.Brands)
	for _, b := range out.Brands {
		if b.Mentions > 0 {
			out.BrandsNamed++
		}
	}
	return out, nil
}
