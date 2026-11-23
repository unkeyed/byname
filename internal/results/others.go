package results

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
)

// OtherBrands is every brand the answers in range named that you don't
// track, most named first. None of it counts in any score.
type OtherBrands struct {
	Answered int `json:"answered" jsonschema:"answers in range that did not fail, the N in named in M of N answers"`
	Read     int `json:"read" jsonschema:"of those, answers read for other brands; an unread answer names none here yet"`
	// LatestRun is the newest check in range, and PreviousRun the check
	// before it, which each brand's change is against.
	LatestRun   int64 `json:"latest_run" jsonschema:"the newest check in range, 0 when none"`
	PreviousRun int64 `json:"previous_run" jsonschema:"the check before latest_run on these AI apps, 0 when latest_run is the first"`
	Hidden      int   `json:"hidden" jsonschema:"brands someone marked as not a competitor; list them with GET /v1/other-brands/hidden"`
	// MinQuestions is how many different questions' answers must offer a
	// brand as a choice before it is listed.
	MinQuestions int          `json:"min_questions" jsonschema:"a brand is listed once answers to at least this many different questions offered it as a choice"`
	Brands       []OtherBrand `json:"brands"`
}

// OtherBrand is one brand you don't track and how often answers named it.
type OtherBrand struct {
	BrandID int64  `json:"brand_id"`
	Name    string `json:"name"`
	Domain  string `json:"domain"`
	Named   int    `json:"named" jsonschema:"answers in range that named it"`
	// Latest and Previous are the answers that named it in the latest
	// check and the one before; Change is their difference, null when
	// there is no check before.
	Latest    int             `json:"latest" jsonschema:"answers in the latest check that named it"`
	Previous  int             `json:"previous" jsonschema:"answers in the check before that named it"`
	Change    *int            `json:"change" jsonschema:"latest minus previous, null when the latest check is the first"`
	Questions []QuestionNamed `json:"questions" jsonschema:"the questions whose answers named it most, at most 3"`
}

// QuestionNamed is a question and how many of its answers named a brand.
type QuestionNamed struct {
	QuestionID int64  `json:"question_id"`
	Text       string `json:"text"`
	Named      int    `json:"named"`
}

// OtherBrands adds up the answers in range (since, nil for all time) on the
// AI apps in scope that named each untracked brand, counting only answers
// that count in scores, like Competitors.
func (s *Service) OtherBrands(ctx context.Context, since *time.Time, sc Scope) (OtherBrands, error) {
	if err := sc.check(); err != nil {
		return OtherBrands{}, err
	}
	out := OtherBrands{Brands: []OtherBrand{}}
	args := scopeArgs(since, sc, ByPair)[:3]
	err := s.db.QueryRow(ctx, `SELECT count(*) FILTER (WHERE a.status <> 'failed'),
			count(*) FILTER (WHERE a.status <> 'failed' AND EXISTS (SELECT 1 FROM brand_call c WHERE c.purpose = 'find' AND c.answer_id = a.id AND c.status = 'ok')),
			coalesce(max(a.run_id), 0), (SELECT count(*) FROM brand WHERE kind = 'hidden'),
			(SELECT other_brands_min_questions FROM instance_settings)`+scopedFrom+scopedWhere+counted, args...).
		Scan(&out.Answered, &out.Read, &out.LatestRun, &out.Hidden, &out.MinQuestions)
	if err != nil || out.LatestRun == 0 {
		return out, err
	}
	err = s.db.QueryRow(ctx, `SELECT coalesce(max(a.run_id), 0) FROM answer a JOIN surface_model m ON m.id = a.surface_model_id
		WHERE a.run_id < $1 AND ($2::text = '' OR m.surface_key = $2) AND ($3::text = '' OR m.model = $3)`,
		out.LatestRun, string(sc.Surface), sc.Model).Scan(&out.PreviousRun)
	if err != nil {
		return out, err
	}
	// A brand is listed once answers to enough different questions offered
	// it as a choice; one named only as a tool or an example never is.
	rows, err := s.db.Query(ctx, `SELECT b.id, b.name, coalesce(b.domains[1], ''), count(*),
			count(*) FILTER (WHERE a.run_id = $4), (SELECT count(*) FROM discovered_mention d2 JOIN answer a2 ON a2.id = d2.answer_id
				JOIN surface_model m2 ON m2.id = a2.surface_model_id JOIN question q2 ON q2.id = a2.question_id
				WHERE d2.brand_id = b.id AND a2.run_id = $5 AND a2.status <> 'failed' AND q2.counts_in_scores AND d2.offered IS NOT FALSE
				AND ($2::text = '' OR m2.surface_key = $2) AND ($3::text = '' OR m2.model = $3))`+scopedFrom+`
			JOIN discovered_mention d ON d.answer_id = a.id AND d.offered IS NOT FALSE JOIN brand b ON b.id = d.brand_id AND b.kind = 'untracked'`+
		scopedWhere+counted+`AND a.status <> 'failed' GROUP BY b.id
			HAVING count(DISTINCT a.question_id) >= (SELECT other_brands_min_questions FROM instance_settings)
			ORDER BY count(*) DESC, lower(b.name)`,
		append(args, out.LatestRun, out.PreviousRun)...)
	if err != nil {
		return out, err
	}
	byID := map[int64]int{}
	for rows.Next() {
		var b OtherBrand
		if err := rows.Scan(&b.BrandID, &b.Name, &b.Domain, &b.Named, &b.Latest, &b.Previous); err != nil {
			return out, err
		}
		if out.PreviousRun != 0 {
			b.Change = new(b.Latest - b.Previous)
		}
		b.Questions = []QuestionNamed{}
		byID[b.BrandID] = len(out.Brands)
		out.Brands = append(out.Brands, b)
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	rows, err = s.db.Query(ctx, `SELECT d.brand_id, q.id, q.text, count(*)`+scopedFrom+`
			JOIN discovered_mention d ON d.answer_id = a.id AND d.offered IS NOT FALSE JOIN brand b ON b.id = d.brand_id AND b.kind = 'untracked'`+
		scopedWhere+counted+`AND a.status <> 'failed' GROUP BY 1, 2, 3`, args...)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var brand int64
		var q QuestionNamed
		if err := rows.Scan(&brand, &q.QuestionID, &q.Text, &q.Named); err != nil {
			return out, err
		}
		if i, ok := byID[brand]; ok {
			out.Brands[i].Questions = append(out.Brands[i].Questions, q)
		}
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	for i := range out.Brands {
		qs := out.Brands[i].Questions
		slices.SortFunc(qs, func(a, b QuestionNamed) int {
			return cmp.Or(cmp.Compare(b.Named, a.Named), cmp.Compare(a.Text, b.Text))
		})
		out.Brands[i].Questions = qs[:min(len(qs), 3)]
	}
	return out, nil
}

// HiddenBrand is a found brand someone marked as not a competitor.
type HiddenBrand struct {
	BrandID int64  `json:"brand_id"`
	Name    string `json:"name"`
	Domain  string `json:"domain"`
	Named   int    `json:"named" jsonschema:"answers that named it, over all time"`
}

// HiddenBrands lists the brands marked as not a competitor, by name.
func (s *Service) HiddenBrands(ctx context.Context) ([]HiddenBrand, error) {
	rows, err := s.db.Query(ctx, `SELECT b.id, b.name, coalesce(b.domains[1], ''),
			(SELECT count(*) FROM discovered_mention d WHERE d.brand_id = b.id)
		FROM brand b WHERE b.kind = 'hidden' ORDER BY lower(b.name)`)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (HiddenBrand, error) {
		var h HiddenBrand
		return h, r.Scan(&h.BrandID, &h.Name, &h.Domain, &h.Named)
	})
	if errors.Is(err, pgx.ErrNoRows) || out == nil {
		out = []HiddenBrand{}
	}
	return out, err
}
