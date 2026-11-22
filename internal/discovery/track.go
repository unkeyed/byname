package discovery

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/unkeyed/byname/internal/config"
)

// Track makes a found brand a competitor, with every spelling answers used
// as an alias. From then on it counts like any competitor, and every past
// answer is scored again, locally and for free, so its history is there
// at once.
func (s *Service) Track(ctx context.Context, id int64) (config.Brand, error) {
	var b config.Brand
	err := s.config.BrandChange(ctx, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `UPDATE brand SET kind = 'competitor', key = NULL, updated_at = now()
			WHERE id = $1 AND key IS NOT NULL RETURNING id, name, aliases, domains, kind`, id).
			Scan(&b.ID, &b.Name, &b.Aliases, &b.Domains, &b.Kind)
		if errors.Is(err, pgx.ErrNoRows) {
			return errNotFound(id)
		}
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `DELETE FROM discovered_mention WHERE brand_id = $1`, id)
		return err
	})
	return b, err
}

// Hide marks a found brand as not a competitor, so it leaves the list of
// brands you don't track. Its mentions are kept, so Unhide brings it back
// as it was.
func (s *Service) Hide(ctx context.Context, id int64) error {
	return s.setKind(ctx, id, "untracked", "hidden")
}

// Unhide puts a hidden brand back on the list of brands you don't track.
func (s *Service) Unhide(ctx context.Context, id int64) error {
	return s.setKind(ctx, id, "hidden", "untracked")
}

func (s *Service) setKind(ctx context.Context, id int64, from, to string) error {
	tag, err := s.db.Exec(ctx, `UPDATE brand SET kind = $3, updated_at = now() WHERE id = $1 AND kind IN ($2, $3)`, id, from, to)
	if err == nil && tag.RowsAffected() == 0 {
		return errNotFound(id)
	}
	return err
}
