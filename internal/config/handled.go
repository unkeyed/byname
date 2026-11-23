package config

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/unkeyed/byname/internal/core"
)

// QuestionPageInput is a page published to answer a question.
type QuestionPageInput struct {
	URL string `json:"url" jsonschema:"the page's address, such as https://northwind.example/blog/zero-downtime; https:// is assumed when left out"`
}

// QuestionPage is a page marked as answering a question. Results tracks
// every later answer that links to it (GET /v1/handled).
type QuestionPage struct {
	ID         int64     `json:"id"`
	QuestionID int64     `json:"question_id"`
	URL        string    `json:"url"`
	AddedAt    time.Time `json:"added_at" jsonschema:"when it was first marked; answers from then on count as linking to it"`
}

var hasScheme = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*:`)

// MarkHandled marks a question as handled by the page at in.URL. Marking
// the same page again, in any spelling page_key treats as the same, keeps
// the first mark and when it started.
func (s *Service) MarkHandled(ctx context.Context, actor core.Actor, questionID int64, in QuestionPageInput) (QuestionPage, error) {
	raw := strings.TrimSpace(in.URL)
	if raw != "" && !hasScheme.MatchString(raw) {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil || len(raw) > 2000 {
		return QuestionPage{}, fmt.Errorf("%w: url must be a page's address such as https://northwind.example/blog/zero-downtime, got %q", core.ErrInvalid, in.URL)
	}
	var by *int64
	if actor.UserID != 0 {
		by = &actor.UserID
	}
	p := QuestionPage{QuestionID: questionID}
	err = s.db.QueryRow(ctx, `INSERT INTO question_page (question_id, url, added_by)
		SELECT id, $2, $3 FROM question WHERE id = $1
		ON CONFLICT (question_id, url_key) DO UPDATE SET url = question_page.url
		RETURNING id, url, added_at`, questionID, raw, by).Scan(&p.ID, &p.URL, &p.AddedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return QuestionPage{}, fmt.Errorf("%w: no question %d", core.ErrNotFound, questionID)
	}
	return p, err
}

// UnmarkPage removes a page mark. Its tracking goes with it.
func (s *Service) UnmarkPage(ctx context.Context, id int64) error {
	tag, err := s.db.Exec(ctx, `DELETE FROM question_page WHERE id = $1`, id)
	if err == nil && tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: no marked page %d", core.ErrNotFound, id)
	}
	return err
}
