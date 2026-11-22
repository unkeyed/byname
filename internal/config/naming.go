package config

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5"

	"github.com/unkeyed/byname/internal/core"
	"github.com/unkeyed/byname/internal/panel"
)

// MarkBrandQuestions works out again which questions name your brand, by
// the rules answers are scored with. Such a question is still asked and
// its answers shown, but by default they count in no score: an answer
// naming you is no news when the question did. The brand, its aliases, the
// match mode and the other brands' names all decide it, so each change to
// them marks the questions again, and so does every start, which marks
// questions stored before the flag existed. Running it again changes
// nothing.
func (s *Service) MarkBrandQuestions(ctx context.Context) error {
	return pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error { return markBrandQuestions(ctx, tx) })
}

func markBrandQuestions(ctx context.Context, q querier) error {
	if _, err := q.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('byname.questions'))`); err != nil {
		return err
	}
	brands, mode, err := matchRules(ctx, q)
	if err != nil {
		return err
	}
	rows, err := q.Query(ctx, `SELECT id, text, names_brand FROM question`)
	if err != nil {
		return err
	}
	var ids []int64
	var flags []bool
	for rows.Next() {
		var id int64
		var text string
		var stored bool
		if err := rows.Scan(&id, &text, &stored); err != nil {
			return err
		}
		if names := namesSelf(text, brands, mode); names != stored {
			ids, flags = append(ids, id), append(flags, names)
		}
	}
	if err := rows.Err(); err != nil || len(ids) == 0 {
		return err
	}
	_, err = q.Exec(ctx, `UPDATE question SET names_brand = x.names
		FROM unnest($1::bigint[], $2::boolean[]) AS x(id, names) WHERE question.id = x.id`, ids, flags)
	return err
}

// matchRules is the brand table and match mode answers are scored with.
func matchRules(ctx context.Context, q querier) ([]panel.Brand, panel.MatchMode, error) {
	var mode panel.MatchMode
	if err := q.QueryRow(ctx, `SELECT match_mode FROM instance_settings`).Scan(&mode); err != nil {
		return nil, mode, err
	}
	rows, err := q.Query(ctx, `SELECT id, name, aliases, domains, kind FROM tracked_brand ORDER BY kind <> 'self', lower(name)`)
	if err != nil {
		return nil, mode, err
	}
	brands, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (panel.Brand, error) {
		var b panel.Brand
		return b, r.Scan(&b.ID, &b.Name, &b.Aliases, &b.Domains, &b.Kind)
	})
	return brands, mode, err
}

func namesSelf(text string, brands []panel.Brand, mode panel.MatchMode) bool {
	for _, m := range panel.Named(text, brands, mode) {
		for _, b := range brands {
			if b.ID == m.Brand && b.Kind == panel.KindSelf {
				return true
			}
		}
	}
	return false
}

// QuestionScoringInput sets whether one question's answers count in
// scores.
type QuestionScoringInput struct {
	CountInScores *bool `json:"count_in_scores" jsonschema:"true to count its answers in scores, false to leave them out, or null for the default: left out when the question names your brand, counted otherwise"`
}

// SetQuestionScoring counts a current question's answers in scores, or
// leaves them out, or goes back to the default. The choice covers the
// earlier versions the question carries on, so a chart never jumps where
// the question set changed, and later versions carry it on.
func (s *Service) SetQuestionScoring(ctx context.Context, id int64, in QuestionScoringInput) (Question, error) {
	var out Question
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var current bool
		err := tx.QueryRow(ctx, `SELECT set_id = (SELECT id FROM question_set ORDER BY version DESC LIMIT 1) FROM question WHERE id = $1`, id).Scan(&current)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return fmt.Errorf("%w: no question %d", core.ErrNotFound, id)
		case err != nil:
			return err
		case !current:
			return fmt.Errorf("%w: question %d is from an earlier question set; set it on the current question that carries it on (GET /v1/questions)", core.ErrInvalid, id)
		}
		if _, err := tx.Exec(ctx, `WITH RECURSIVE lineage (qid) AS (
				SELECT $1::bigint
				UNION
				SELECT m.old_question_id FROM lineage l JOIN question_map m ON m.new_question_id = l.qid
			)
			UPDATE question SET count_in_scores_override = $2 WHERE id IN (SELECT qid FROM lineage)`, id, in.CountInScores); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT `+questionColumns+` FROM question WHERE id = $1`, id)
		if err != nil {
			return err
		}
		out, err = pgx.CollectExactlyOneRow(rows, scanQuestion)
		return err
	})
	return out, err
}

// NamingCheckInput is text someone is typing: question texts, and names
// or aliases for a brand.
type NamingCheckInput struct {
	Questions []string `json:"questions,omitempty" jsonschema:"question texts to check against the brand table, at most 100"`
	Aliases   []string `json:"aliases,omitempty" jsonschema:"brand names or aliases to check for common words, at most 50"`
}

// NamingCheck says, while someone types, which brands a question names
// and which aliases are everyday words.
type NamingCheck struct {
	Brand     string           `json:"brand" jsonschema:"your brand's name; empty before it is set"`
	Questions []QuestionNaming `json:"questions"`
	Aliases   []AliasCheck     `json:"aliases"`
}

type QuestionNaming struct {
	Text string `json:"text"`
	// NamesBrand means the question would be asked but left out of scores
	// by default.
	NamesBrand bool     `json:"names_brand" jsonschema:"the text names your brand, so its answers are left out of scores by default"`
	Brands     []string `json:"brands" jsonschema:"every brand the text names, yours and competitors', in order of first mention"`
}

type AliasCheck struct {
	Alias      string `json:"alias"`
	CommonWord bool   `json:"common_word" jsonschema:"a common English word, so everyday text would count as naming the brand"`
}

// CheckNaming checks text against the stored brand table, by the rules
// answers are scored with, and aliases against a list of common English
// words. It stores nothing.
func (s *Service) CheckNaming(ctx context.Context, in NamingCheckInput) (NamingCheck, error) {
	out := NamingCheck{Questions: []QuestionNaming{}, Aliases: []AliasCheck{}}
	if len(in.Questions) > 100 || len(in.Aliases) > 50 {
		return out, fmt.Errorf("%w: check at most 100 questions and 50 aliases at once", core.ErrInvalid)
	}
	brands, mode, err := matchRules(ctx, s.db)
	if err != nil {
		return out, err
	}
	names := map[panel.BrandID]panel.Brand{}
	for _, b := range brands {
		names[b.ID] = b
		if b.Kind == panel.KindSelf {
			out.Brand = b.Name
		}
	}
	for _, text := range in.Questions {
		q := QuestionNaming{Text: text, Brands: []string{}}
		for _, m := range panel.Named(text, brands, mode) {
			b := names[m.Brand]
			q.Brands = append(q.Brands, b.Name)
			q.NamesBrand = q.NamesBrand || b.Kind == panel.KindSelf
		}
		out.Questions = append(out.Questions, q)
	}
	for _, a := range in.Aliases {
		out.Aliases = append(out.Aliases, AliasCheck{Alias: a, CommonWord: CommonWord(a)})
	}
	return out, nil
}

//go:embed common_words.txt
var commonWordList string

var commonWords = sync.OnceValue(func() map[string]bool {
	out := map[string]bool{}
	for line := range strings.Lines(commonWordList) {
		if w := strings.TrimSpace(line); w != "" && !strings.HasPrefix(w, "#") {
			out[w] = true
		}
	}
	return out
})

// CommonWord reports whether name, trimmed and ignoring case, is a common
// English word. A name of several words is not.
func CommonWord(name string) bool {
	return commonWords()[strings.ToLower(strings.TrimSpace(name))]
}
