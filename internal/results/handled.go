package results

import (
	"cmp"
	"context"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/unkeyed/byname/internal/panel"
)

// MarkedPage is a page marked as answering a question, as the question's
// row in Missed questions shows it.
type MarkedPage struct {
	ID      int64     `json:"id"`
	URL     string    `json:"url"`
	AddedAt time.Time `json:"added_at"`
	Linked  int       `json:"linked" jsonschema:"answers since it was marked that link to it, to any question"`
}

// PageLinks is every answer since a page was marked that links to it, to
// any question, from any AI app.
type PageLinks struct {
	Answers     int        `json:"answers"`
	Questions   int        `json:"questions" jsonschema:"distinct questions those answers were to"`
	Pairs       []PairRef  `json:"pairs" jsonschema:"the AI apps and models those answers came from, in pair order"`
	FirstRunID  *int64     `json:"first_run_id" jsonschema:"the check that first linked to it; null until one does"`
	FirstAt     *time.Time `json:"first_at" jsonschema:"when an answer first linked to it; null until one does"`
	LatestRunID *int64     `json:"latest_run_id" jsonschema:"the latest check that linked to it; null until one does"`
}

// Naming is how one check's answers to a question named you.
type Naming struct {
	RunID        int64     `json:"run_id"`
	RunStartedAt time.Time `json:"run_started_at"`
	Answered     int       `json:"answered" jsonschema:"answers that did not fail"`
	Named        int       `json:"named" jsonschema:"answers that named you"`
}

// HandledPage is a marked page and what happened since it was marked.
type HandledPage struct {
	ID       int64       `json:"id"`
	URL      string      `json:"url"`
	Question QuestionRef `json:"question" jsonschema:"the question it answers, as it reads now"`
	AddedAt  time.Time   `json:"added_at"`
	AddedBy  string      `json:"added_by" jsonschema:"who marked it; empty for an API key or a person since removed"`
	Linked   PageLinks   `json:"linked"`
	Before   *Naming     `json:"before" jsonschema:"the question's last check before the page was marked; null when there was none"`
	Now      *Naming     `json:"now" jsonschema:"the question's latest check since the page was marked; null until one runs"`
}

type HandledPages struct {
	Pages []HandledPage `json:"pages" jsonschema:"most recently marked first"`
}

// pageFamily selects, per marked page, current: its question as it reads
// now, following question_map to the newest version, and family: that
// question and every earlier version it carries on, so a mark survives a
// change to the question set.
const pageFamily = `WITH RECURSIVE ahead (page_id, qid, depth) AS (
		SELECT id, question_id, 0 FROM question_page
		UNION ALL
		SELECT a.page_id, m.new_question_id, a.depth + 1 FROM ahead a JOIN question_map m ON m.old_question_id = a.qid
	), current AS (
		SELECT DISTINCT ON (page_id) page_id, qid FROM ahead ORDER BY page_id, depth DESC
	), family (page_id, qid) AS (
		SELECT page_id, qid FROM current
		UNION
		SELECT f.page_id, m.old_question_id FROM family f JOIN question_map m ON m.new_question_id = f.qid
	) `

// pageLink is one answer that links to a marked page.
type pageLink struct {
	answer, run, question int64
	at                    time.Time
	pair                  PairRef
}

type markedPage struct {
	HandledPage
	family []panel.QuestionID
	// links are the answers since the page was marked that link to it,
	// oldest first.
	links []pageLink
}

// markedPages loads every marked page with its question's versions and
// the answers that link to it, most recently marked first. It is the one
// place that decides which answers link to a page.
func (s *Service) markedPages(ctx context.Context) ([]*markedPage, error) {
	rows, err := s.db.Query(ctx, pageFamily+`SELECT p.id, p.url, p.added_at, coalesce(nullif(u.name, ''), u.email, ''),
			q.id, q.group_name, q.text, q.counts_in_scores, (SELECT array_agg(f.qid) FROM family f WHERE f.page_id = p.id)
		FROM question_page p JOIN current c ON c.page_id = p.id JOIN question q ON q.id = c.qid LEFT JOIN users u ON u.id = p.added_by
		ORDER BY p.added_at DESC, p.id DESC`)
	if err != nil {
		return nil, err
	}
	pages, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (*markedPage, error) {
		p := &markedPage{}
		p.Linked.Pairs = []PairRef{}
		return p, r.Scan(&p.ID, &p.URL, &p.AddedAt, &p.AddedBy, &p.Question.ID, &p.Question.Group, &p.Question.Text, &p.Question.CountsInScores, &p.family)
	})
	if err != nil || len(pages) == 0 {
		return pages, err
	}
	byID := map[int64]*markedPage{}
	for _, p := range pages {
		byID[p.ID] = p
	}
	rows, err = s.db.Query(ctx, `SELECT DISTINCT p.id, a.answered_at, a.id, a.run_id, a.question_id, m.id, m.surface_key, coalesce(m.model, '')
		FROM question_page p JOIN citation c ON page_key(c.url) = p.url_key
		JOIN answer a ON a.id = c.answer_id AND a.answered_at >= p.added_at JOIN surface_model m ON m.id = a.surface_model_id
		ORDER BY p.id, a.answered_at, a.id`)
	if err != nil {
		return nil, err
	}
	var page int64
	var l pageLink
	_, err = pgx.ForEachRow(rows, []any{&page, &l.at, &l.answer, &l.run, &l.question, &l.pair.ID, &l.pair.Surface, &l.pair.Model}, func() error {
		if p, ok := byID[page]; ok {
			p.links = append(p.links, l)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, p := range pages {
		p.Linked = summarizeLinks(p.links)
	}
	return pages, nil
}

func summarizeLinks(links []pageLink) PageLinks {
	out := PageLinks{Answers: len(links), Pairs: []PairRef{}}
	if len(links) == 0 {
		return out
	}
	questions := map[int64]bool{}
	latest := links[0].run
	for _, l := range links {
		questions[l.question] = true
		if !slices.Contains(out.Pairs, l.pair) {
			out.Pairs = append(out.Pairs, l.pair)
		}
		latest = max(latest, l.run)
	}
	slices.SortFunc(out.Pairs, func(a, b PairRef) int { return cmp.Compare(a.ID, b.ID) })
	out.Questions = len(questions)
	out.FirstRunID, out.FirstAt, out.LatestRunID = &links[0].run, &links[0].at, &latest
	return out
}

// Handled is every marked page: the answers since it was marked that link
// to it, to any question and from any AI app, and how its question's
// answers named you on the last check before it was marked and on the
// latest check since.
func (s *Service) Handled(ctx context.Context) (HandledPages, error) {
	pages, err := s.markedPages(ctx)
	if err != nil {
		return HandledPages{}, err
	}
	out := HandledPages{Pages: make([]HandledPage, len(pages))}
	if len(pages) == 0 {
		return out, nil
	}
	byID := map[int64]*markedPage{}
	for _, p := range pages {
		byID[p.ID] = p
	}
	rows, err := s.db.Query(ctx, pageFamily+`SELECT f.page_id, a.run_id, coalesce(r.started_at, r.queued_at),
			count(*) FILTER (WHERE a.status <> 'failed'), count(*) FILTER (WHERE a.mentioned)
		FROM family f JOIN answer a ON a.question_id = f.qid JOIN run r ON r.id = a.run_id
		GROUP BY 1, 2, 3 ORDER BY 1, 2`)
	if err != nil {
		return HandledPages{}, err
	}
	var page int64
	var n Naming
	_, err = pgx.ForEachRow(rows, []any{&page, &n.RunID, &n.RunStartedAt, &n.Answered, &n.Named}, func() error {
		p, ok := byID[page]
		if !ok {
			return nil
		}
		if n.RunStartedAt.Before(p.AddedAt) {
			p.Before = new(n)
		} else {
			p.Now = new(n)
		}
		return nil
	})
	for i, p := range pages {
		out.Pages[i] = p.HandledPage
	}
	return out, err
}

// markedFor is the pages marked on each question, under every version of
// it, most recently marked first.
func (s *Service) markedFor(ctx context.Context) (map[panel.QuestionID][]MarkedPage, error) {
	pages, err := s.markedPages(ctx)
	out := map[panel.QuestionID][]MarkedPage{}
	for _, p := range pages {
		for _, q := range p.family {
			out[q] = append(out[q], MarkedPage{ID: p.ID, URL: p.URL, AddedAt: p.AddedAt, Linked: p.Linked.Answers})
		}
	}
	return out, err
}

// FirstLink is a marked page that one check's answers were the first to
// link to.
type FirstLink struct {
	PageID   int64
	URL      string
	Question string
	// Answers and Pairs are that check's answers that link to it, and the
	// pairs they came from, in pair order.
	Answers int
	Pairs   []PairRef
}

// LinkedFirstTime is the marked pages whose first linking answer since
// they were marked is in run runID.
func (s *Service) LinkedFirstTime(ctx context.Context, runID int64) ([]FirstLink, error) {
	pages, err := s.markedPages(ctx)
	if err != nil {
		return nil, err
	}
	var out []FirstLink
	for _, p := range pages {
		if p.Linked.FirstRunID == nil || *p.Linked.FirstRunID != runID {
			continue
		}
		var inRun []pageLink
		for _, l := range p.links {
			if l.run == runID {
				inRun = append(inRun, l)
			}
		}
		out = append(out, FirstLink{PageID: p.ID, URL: p.URL, Question: p.Question.Text, Answers: len(inRun), Pairs: summarizeLinks(inRun).Pairs})
	}
	return out, nil
}
