package config

import (
	"context"
	"maps"

	"github.com/jackc/pgx/v5"

	"github.com/unkeyed/byname/internal/engines"
	"github.com/unkeyed/byname/internal/panel"
)

// rederive brings everything the brand table decides up to date after a
// change to it: which questions name your brand, which found brands are
// now tracked under another spelling, and every stored answer's score,
// so a brand tracked today counts in past answers too. Scoring is local
// and free. Running it again changes nothing.
func rederive(ctx context.Context, q querier) error {
	if err := markBrandQuestions(ctx, q); err != nil {
		return err
	}
	brands, mode, err := matchRules(ctx, q)
	if err != nil {
		return err
	}
	if err := dropCoveredBrands(ctx, q, brands); err != nil {
		return err
	}
	return rescoreAnswers(ctx, q, brands, mode)
}

// dropCoveredBrands deletes the untracked and hidden brands a tracked
// brand now covers, with their found mentions: tracking "Litware Cloud"
// makes a found "Litware Cloud GmbH" the same brand, which brand_mention
// now records.
func dropCoveredBrands(ctx context.Context, q querier, tracked []panel.Brand) error {
	rows, err := q.Query(ctx, `SELECT id, name, coalesce(domains[1], '') FROM brand WHERE key IS NOT NULL`)
	if err != nil {
		return err
	}
	type found struct {
		id           int64
		name, domain string
	}
	all, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (found, error) {
		var f found
		return f, r.Scan(&f.id, &f.name, &f.domain)
	})
	if err != nil {
		return err
	}
	var covered []int64
	for _, f := range all {
		if panel.Covers(tracked, f.name, f.domain) {
			covered = append(covered, f.id)
		}
	}
	if len(covered) == 0 {
		return nil
	}
	_, err = q.Exec(ctx, `DELETE FROM brand WHERE id = ANY($1) AND key IS NOT NULL`, covered)
	return err
}

// freeName deletes an untracked or hidden brand spelled name, so a
// tracked brand can take the name.
func freeName(ctx context.Context, q querier, name string) error {
	_, err := q.Exec(ctx, `DELETE FROM brand WHERE lower(name) = lower($1) AND key IS NOT NULL`, name)
	return err
}

// rescoreAnswers scores every stored answer again against brands and
// rewrites the ones whose score changed: whether and where they named
// you, whether they linked to you, which tracked brands they named, and
// which citations are yours.
func rescoreAnswers(ctx context.Context, q querier, brands []panel.Brand, mode panel.MatchMode) error {
	rows, err := q.Query(ctx, `SELECT a.id, a.status, a.text, a.mentioned, a.cited, coalesce(a.position, 0),
			coalesce((SELECT array_agg(c.url ORDER BY c.ord) FROM citation c WHERE c.answer_id = a.id), '{}'),
			coalesce((SELECT array_agg(c.ord ORDER BY c.ord) FROM citation c WHERE c.answer_id = a.id), '{}'),
			coalesce((SELECT array_agg(c.is_self ORDER BY c.ord) FROM citation c WHERE c.answer_id = a.id), '{}'),
			coalesce((SELECT array_agg(bm.brand_id) FROM brand_mention bm WHERE bm.answer_id = a.id), '{}'),
			coalesce((SELECT array_agg(coalesce(bm.position, 0)) FROM brand_mention bm WHERE bm.answer_id = a.id), '{}')
		FROM answer a`)
	if err != nil {
		return err
	}
	var (
		ids, mentionAnswers, mentionBrands, citeAnswers []int64
		mentioned, cited, citeSelf                      []bool
		positions                                       []*int
		mentionPositions                                []*int
		citeOrds                                        []int
	)
	for rows.Next() {
		var id int64
		var res engines.Result
		var was panel.Scored
		var ords []int
		var selfs []bool
		var brandIDs []int64
		var brandPositions []int
		if err := rows.Scan(&id, &res.Status, &res.Text, &was.Mentioned, &was.Cited, &was.Position, &res.Citations, &ords, &selfs, &brandIDs, &brandPositions); err != nil {
			rows.Close()
			return err
		}
		now := panel.Score(res, brands, panel.Settings{MatchMode: mode})
		stored := map[panel.BrandID]int{}
		for i, b := range brandIDs {
			stored[panel.BrandID(b)] = brandPositions[i]
		}
		scored := map[panel.BrandID]int{}
		for _, m := range now.Mentions {
			scored[m.Brand] = m.Position
		}
		if now.Mentioned != was.Mentioned || now.Cited != was.Cited || now.Position != was.Position || !maps.Equal(stored, scored) {
			ids = append(ids, id)
			mentioned, cited, positions = append(mentioned, now.Mentioned), append(cited, now.Cited), append(positions, nullable(now.Position))
			for _, m := range now.Mentions {
				mentionAnswers, mentionBrands = append(mentionAnswers, id), append(mentionBrands, int64(m.Brand))
				mentionPositions = append(mentionPositions, nullable(m.Position))
			}
		}
		for i, c := range now.Citations {
			if i < len(selfs) && c.IsSelf != selfs[i] {
				citeAnswers, citeOrds, citeSelf = append(citeAnswers, id), append(citeOrds, ords[i]), append(citeSelf, c.IsSelf)
			}
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(citeAnswers) > 0 {
		if _, err := q.Exec(ctx, `UPDATE citation SET is_self = x.is_self
			FROM unnest($1::bigint[], $2::int[], $3::boolean[]) AS x(answer_id, ord, is_self)
			WHERE citation.answer_id = x.answer_id AND citation.ord = x.ord`, citeAnswers, citeOrds, citeSelf); err != nil {
			return err
		}
	}
	if len(ids) == 0 {
		return nil
	}
	if _, err := q.Exec(ctx, `UPDATE answer SET mentioned = x.mentioned, cited = x.cited, position = x.position
		FROM unnest($1::bigint[], $2::boolean[], $3::boolean[], $4::int[]) AS x(id, mentioned, cited, position)
		WHERE answer.id = x.id`, ids, mentioned, cited, positions); err != nil {
		return err
	}
	if _, err := q.Exec(ctx, `DELETE FROM brand_mention WHERE answer_id = ANY($1)`, ids); err != nil {
		return err
	}
	_, err = q.Exec(ctx, `INSERT INTO brand_mention (answer_id, brand_id, position)
		SELECT * FROM unnest($1::bigint[], $2::bigint[], $3::int[])`, mentionAnswers, mentionBrands, mentionPositions)
	return err
}

func nullable(p int) *int {
	if p == 0 {
		return nil
	}
	return &p
}
