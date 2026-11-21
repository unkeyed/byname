package config

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"

	"github.com/unkeyed/byname/internal/core"
)

type Question struct {
	ID    int64  `json:"id"`
	Group string `json:"group"`
	Text  string `json:"text"`
}

// QuestionSet is one version of the questions. Carried says which question
// of the previous version each of this version's questions carries on, and
// Retired lists the previous version's questions that ended there.
type QuestionSet struct {
	ID        int64          `json:"id"`
	Version   int            `json:"version"`
	Notes     string         `json:"notes"`
	CreatedAt time.Time      `json:"created_at"`
	Questions []Question     `json:"questions"`
	Carried   []QuestionLink `json:"carried"`
	Retired   []int64        `json:"retired"`
}

type QuestionLink struct {
	From int64 `json:"from" jsonschema:"a question of the previous version"`
	To   int64 `json:"to" jsonschema:"the question of this version that carries it on"`
}

type QuestionSetSummary struct {
	ID        int64     `json:"id"`
	Version   int       `json:"version"`
	Notes     string    `json:"notes"`
	CreatedAt time.Time `json:"created_at"`
	Questions int       `json:"questions"`
}

// QuestionsInput is the whole new set. A question whose text matches a
// current question (ignoring case and punctuation) carries that one on by
// itself. Every other current question must be named in some question's
// replaces, or in retired, so history never breaks silently.
type QuestionsInput struct {
	Notes     string          `json:"notes"`
	Questions []QuestionInput `json:"questions"`
	Retired   []int64         `json:"retired" jsonschema:"current question ids that end with this version"`
}

type QuestionInput struct {
	Group    string  `json:"group"`
	Text     string  `json:"text" jsonschema:"10 to 300 characters, sent to every surface as written"`
	Replaces []int64 `json:"replaces" jsonschema:"current question ids this question carries on, when its text changed"`
}

// normalized is a question's identity for deduplication and carrying over:
// lowercase letters and digits only.
func normalized(text string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(text) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func (in *QuestionsInput) validate() error {
	if len(in.Questions) == 0 {
		return fmt.Errorf("%w: questions needs at least one question", core.ErrInvalid)
	}
	seen := map[string]int{}
	for i := range in.Questions {
		q := &in.Questions[i]
		q.Group, q.Text = oneLine(q.Group), oneLine(q.Text)
		switch n := len([]rune(q.Text)); {
		case q.Group == "" || len([]rune(q.Group)) > 80:
			return fmt.Errorf("%w: questions[%d].group must be 1 to 80 characters; a question can be a group of one", core.ErrInvalid, i)
		case n < 10 || n > 300:
			return fmt.Errorf("%w: questions[%d].text must be 10 to 300 characters, got %d", core.ErrInvalid, i, n)
		}
		key := normalized(q.Text)
		if j, dup := seen[key]; dup {
			return fmt.Errorf("%w: questions[%d] repeats questions[%d] (ignoring case and punctuation)", core.ErrInvalid, i, j)
		}
		seen[key] = i
	}
	return nil
}

// CurrentQuestions is the newest question set. ErrNotFound before the first.
func (s *Service) CurrentQuestions(ctx context.Context) (QuestionSet, error) {
	return questionSet(ctx, s.db, `(SELECT max(version) FROM question_set)`)
}

// QuestionSet is one version by id, with how it carries on the one before.
func (s *Service) QuestionSet(ctx context.Context, id int64) (QuestionSet, error) {
	return questionSet(ctx, s.db, `(SELECT version FROM question_set WHERE id = $1)`, id)
}

func questionSet(ctx context.Context, q querier, version string, args ...any) (QuestionSet, error) {
	set := QuestionSet{Questions: []Question{}, Carried: []QuestionLink{}, Retired: []int64{}}
	err := q.QueryRow(ctx, `SELECT id, version, notes, created_at FROM question_set WHERE version = `+version, args...).
		Scan(&set.ID, &set.Version, &set.Notes, &set.CreatedAt)
	if err == pgx.ErrNoRows {
		return set, fmt.Errorf("%w: no question set; create one with PUT /v1/questions", core.ErrNotFound)
	}
	if err != nil {
		return set, err
	}
	if set.Questions, err = questions(ctx, q, set.ID); err != nil {
		return set, err
	}
	rows, err := q.Query(ctx, `SELECT p.id, m.new_question_id FROM question p
		JOIN question_set ps ON ps.id = p.set_id AND ps.version = $1 - 1
		LEFT JOIN question_map m ON m.old_question_id = p.id
		ORDER BY p.ord`, set.Version)
	if err != nil {
		return set, err
	}
	for rows.Next() {
		var from int64
		var to *int64
		if err := rows.Scan(&from, &to); err != nil {
			return set, err
		}
		if to == nil {
			set.Retired = append(set.Retired, from)
		} else {
			set.Carried = append(set.Carried, QuestionLink{From: from, To: *to})
		}
	}
	return set, rows.Err()
}

// Questions are one set's questions in order.
func (s *Service) Questions(ctx context.Context, setID int64) ([]Question, error) {
	return questions(ctx, s.db, setID)
}

func questions(ctx context.Context, q querier, setID int64) ([]Question, error) {
	rows, err := q.Query(ctx, `SELECT id, group_name, text FROM question WHERE set_id = $1 ORDER BY ord`, setID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Question, error) {
		var qu Question
		return qu, r.Scan(&qu.ID, &qu.Group, &qu.Text)
	})
}

// QuestionSets lists every version, newest first.
func (s *Service) QuestionSets(ctx context.Context) ([]QuestionSetSummary, error) {
	rows, err := s.db.Query(ctx, `SELECT s.id, s.version, s.notes, s.created_at, count(q.id)
		FROM question_set s LEFT JOIN question q ON q.set_id = s.id
		GROUP BY s.id ORDER BY s.version DESC`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (QuestionSetSummary, error) {
		var v QuestionSetSummary
		return v, r.Scan(&v.ID, &v.Version, &v.Notes, &v.CreatedAt, &v.Questions)
	})
}

// PutQuestions makes in the current question set. Sending the current set
// again changes nothing; any other change creates the next version and
// records how it carries on the current one.
func (s *Service) PutQuestions(ctx context.Context, in QuestionsInput) (QuestionSet, error) {
	if err := in.validate(); err != nil {
		return QuestionSet{}, err
	}
	var out QuestionSet
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('byname.questions'))`); err != nil {
			return err
		}
		cur, err := questionSet(ctx, tx, `(SELECT max(version) FROM question_set)`)
		first := errors.Is(err, core.ErrNotFound)
		if err != nil && !first {
			return err
		}
		if !first && sameQuestions(cur.Questions, in) {
			out = cur
			return nil
		}
		links, err := carry(cur.Questions, in)
		if err != nil {
			return err
		}
		var setID int64
		if err := tx.QueryRow(ctx, `INSERT INTO question_set (version, notes) VALUES ($1, $2) RETURNING id`,
			cur.Version+1, strings.TrimSpace(in.Notes)).Scan(&setID); err != nil {
			return err
		}
		ids := make([]int64, len(in.Questions))
		for i, q := range in.Questions {
			if err := tx.QueryRow(ctx, `INSERT INTO question (set_id, ord, group_name, text) VALUES ($1, $2, $3, $4) RETURNING id`,
				setID, i, q.Group, q.Text).Scan(&ids[i]); err != nil {
				return err
			}
		}
		for old, i := range links {
			if _, err := tx.Exec(ctx, `INSERT INTO question_map (old_question_id, new_question_id) VALUES ($1, $2)`, old, ids[i]); err != nil {
				return err
			}
		}
		out, err = questionSet(ctx, tx, `$1`, cur.Version+1)
		return err
	})
	return out, err
}

func sameQuestions(cur []Question, in QuestionsInput) bool {
	if len(cur) != len(in.Questions) || len(in.Retired) > 0 {
		return false
	}
	for i, q := range in.Questions {
		if q.Group != cur[i].Group || q.Text != cur[i].Text || len(q.Replaces) > 0 {
			return false
		}
	}
	return true
}

// carry maps each current question id to the index of the new question
// that carries it on, and checks that every current question is carried
// on or retired.
func carry(cur []Question, in QuestionsInput) (map[int64]int, error) {
	byText := map[string]int64{}
	current := map[int64]Question{}
	for _, q := range cur {
		byText[normalized(q.Text)] = q.ID
		current[q.ID] = q
	}
	links := map[int64]int{}
	for i, q := range in.Questions {
		if id, ok := byText[normalized(q.Text)]; ok {
			links[id] = i
		}
	}
	for i, q := range in.Questions {
		for _, id := range q.Replaces {
			if _, ok := current[id]; !ok {
				return nil, fmt.Errorf("%w: questions[%d].replaces names %d, which is not a current question", core.ErrInvalid, i, id)
			}
			if j, taken := links[id]; taken && j != i {
				return nil, fmt.Errorf("%w: question %d is carried on by both questions[%d] and questions[%d]; it can carry on into one", core.ErrInvalid, id, j, i)
			}
			links[id] = i
		}
	}
	for _, id := range in.Retired {
		if _, ok := current[id]; !ok {
			return nil, fmt.Errorf("%w: retired names %d, which is not a current question", core.ErrInvalid, id)
		}
		if i, taken := links[id]; taken {
			return nil, fmt.Errorf("%w: question %d is both retired and carried on by questions[%d]", core.ErrInvalid, id, i)
		}
	}
	var unmapped []string
	for _, q := range cur {
		if _, ok := links[q.ID]; !ok && !slices.Contains(in.Retired, q.ID) {
			unmapped = append(unmapped, fmt.Sprintf("%d (%q)", q.ID, q.Text))
		}
	}
	if len(unmapped) > 0 {
		return nil, fmt.Errorf("%w: these current questions changed or were removed: %s; name each in the replaces of the question that carries it on, or in retired",
			core.ErrInvalid, strings.Join(unmapped, ", "))
	}
	return links, nil
}
