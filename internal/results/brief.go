package results

import (
	"cmp"
	"context"
	_ "embed"
	"fmt"
	"slices"
	"strings"
	"text/template"
	"time"
	"unicode/utf8"

	"github.com/unkeyed/byname/internal/core"
	"github.com/unkeyed/byname/internal/engines"
	"github.com/unkeyed/byname/internal/panel"
)

// LineState is how one answer treated you: named and linked, named only,
// linked only, or neither.
type LineState string

const (
	LineMissed      LineState = "missed"
	LineLinked      LineState = "linked"
	LineNamed       LineState = "named"
	LineNamedLinked LineState = "named_linked"
)

// worse ranks states for a brief, the answers that left you out first.
var worse = []LineState{LineMissed, LineLinked, LineNamed, LineNamedLinked}

// BriefLine is one AI app and model's answers to the brief's question. A
// check that asks a question several times gives it several answers; the
// line says what most of them did.
type BriefLine struct {
	Series
	Title     string    `json:"title" jsonschema:"the AI app, and the model where it takes one, such as Claude Code · claude-sonnet-4-5"`
	AnswerID  int64     `json:"answer_id" jsonschema:"the first of answer_ids"`
	AnswerIDs []int64   `json:"answer_ids" jsonschema:"every answer the line sums up, in the order asked"`
	Answered  int       `json:"answered" jsonschema:"answers that did not fail"`
	Named     int       `json:"named" jsonschema:"answers that named you"`
	Linked    int       `json:"linked" jsonschema:"answers that linked to your site"`
	State     LineState `json:"state" jsonschema:"what most answers did: named_linked, named, linked (linked to your site without naming you) or missed"`
	Position  *int      `json:"position" jsonschema:"your best position among the answers that named you; null unless most did"`
	Brands    []string  `json:"brands" jsonschema:"your brand and competitors most answers named, in the order first named"`
	FanOut    []string  `json:"fan_out" jsonschema:"the web searches the AI app ran, in order, without repeats; empty where the endpoint does not expose them"`
	Links     []string  `json:"links" jsonschema:"the pages the answers linked to, in order, without repeats"`
	Excerpt   string    `json:"-"`
}

// BriefPage is one page the brief's answers linked to.
type BriefPage struct {
	URL     string     `json:"url"`
	Domain  string     `json:"domain"`
	IsSelf  bool       `json:"is_self" jsonschema:"on one of your domains"`
	Answers int        `json:"answers" jsonschema:"answers that linked to it"`
	Check   *PageCheck `json:"check"`
	// Outreach marks a page that names a competitor and not you, on a site
	// that is neither yours nor a competitor's.
	Outreach bool `json:"outreach"`
}

// Brief is one question on one check, as a prompt for an agent that writes
// your site's content: how each AI app answered, the competitors named
// instead, what the apps searched for and the pages they read.
type Brief struct {
	RunID    int64       `json:"run_id"`
	Question QuestionRef `json:"question"`
	Answered int         `json:"answered" jsonschema:"answers that did not fail"`
	Named    int         `json:"named" jsonschema:"answers that named you"`
	Linked   int         `json:"linked" jsonschema:"answers that linked to your site"`
	Instead  []string    `json:"instead" jsonschema:"competitors named by the answers that did not name you, most often first"`
	FanOut   []string    `json:"fan_out" jsonschema:"every web search the AI apps ran, those of answers that left you out first"`
	Lines    []BriefLine `json:"lines" jsonschema:"one per AI app and model that answered, those that left you out first"`
	// PerLine is how many answers each line sums up when they all sum up
	// the same number, else 0.
	PerLine int         `json:"-"`
	Pages   []BriefPage `json:"pages" jsonschema:"the 10 pages the most answers linked to"`
	Prompt  string      `json:"prompt" jsonschema:"markdown to paste into an agent working on your site"`
}

const (
	briefPages    = 10
	briefExcerpts = 4
	excerptRunes  = 1500
)

//go:embed brief.md.tmpl
var briefSource string

var briefTemplate = template.Must(template.New("brief").Funcs(template.FuncMap{
	"list": sentenceList,
	"quoted": func(items []string) string {
		quoted := make([]string, len(items))
		for i, s := range items {
			quoted[i] = "“" + s + "”"
		}
		return sentenceList(quoted)
	},
	"blockquote": func(s string) string {
		lines := strings.Split(s, "\n")
		for i, l := range lines {
			lines[i] = strings.TrimRight("> "+l, " ")
		}
		return strings.Join(lines, "\n")
	},
	"deref": func(b *bool) bool { return b != nil && *b },
}).Parse(briefSource))

// briefPrompt is what the template reads beyond the Brief itself.
type briefPrompt struct {
	Brief
	Brand            string
	Domains          []string
	Market, Language string
	Day              string
	Excerpts         []BriefLine
	SelfUnnamed      []string
	Outreach         []BriefPage
}

// Brief builds the content brief for one question on one check, or on the
// latest check with answers to it on the pairs in scope when runID is 0.
// Failed answers are left out.
func (s *Service) Brief(ctx context.Context, questionID, runID int64, sc Scope) (Brief, error) {
	if err := sc.check(); err != nil {
		return Brief{}, err
	}
	err := s.db.QueryRow(ctx, `SELECT coalesce(max(a.run_id), 0) FROM answer a JOIN surface_model m ON m.id = a.surface_model_id
		WHERE a.question_id = $1 AND a.status <> 'failed' AND ($2 = 0 OR a.run_id = $2)
		AND ($3::text = '' OR m.surface_key = $3) AND ($4::text = '' OR m.model = $4)`, questionID, runID, sc.Surface, sc.Model).Scan(&runID)
	if err != nil {
		return Brief{}, err
	}
	if runID == 0 {
		return Brief{}, fmt.Errorf("%w: no answers to question %d from the AI apps and models in scope; GET /v1/runs/{id}/questions lists the questions each check answered", core.ErrNotFound, questionID)
	}
	answers, err := s.scoredAnswers(ctx, `a.run_id = $1 AND a.question_id = $2`, runID, questionID)
	if err != nil {
		return Brief{}, err
	}
	answers = slices.DeleteFunc(answers, func(a scoredAnswer) bool { return a.scored.Status == engines.StatusFailed || !sc.holds(a.series.Pair) })
	refs, err := s.questionRefs(ctx, []panel.QuestionID{panel.QuestionID(questionID)})
	if err != nil {
		return Brief{}, err
	}
	brands, err := s.brands(ctx)
	if err != nil {
		return Brief{}, err
	}
	kinds, err := s.brandKinds(ctx)
	if err != nil {
		return Brief{}, err
	}
	p := briefPrompt{Brief: Brief{RunID: runID, Question: refs[panel.QuestionID(questionID)], Instead: namedInstead(answers, brands),
		FanOut: []string{}, Lines: []BriefLine{}, Pages: []BriefPage{}}}
	if err := s.db.QueryRow(ctx, `SELECT market, language, to_char(coalesce(r.started_at, r.queued_at) AT TIME ZONE time_zone, 'YYYY-MM-DD')
		FROM instance_settings, run r WHERE r.id = $1`, runID).Scan(&p.Market, &p.Language, &p.Day); err != nil {
		return Brief{}, err
	}
	byID := map[panel.BrandID]panel.Brand{}
	for _, b := range brands {
		byID[b.ID] = b
		if b.Kind == panel.KindSelf {
			p.Brand, p.Domains = b.Name, b.Domains
		}
	}

	ids := make([]int64, len(answers))
	for i, a := range answers {
		ids[i] = a.id
	}
	rows, err := s.db.Query(ctx, `SELECT a.id, a.text, c.url, c.domain, c.is_self,
			p.status, p.fetched_at, p.self_present, coalesce(p.brands_present, '{}'), coalesce(p.error, '')
		FROM answer a LEFT JOIN citation c ON c.answer_id = a.id LEFT JOIN source_page p ON p.url = c.url
		WHERE a.id = ANY($1) ORDER BY a.id, c.ord`, ids)
	if err != nil {
		return Brief{}, err
	}
	texts := map[int64]string{}
	links := map[int64][]string{}
	pages := map[string]*BriefPage{}
	citations := map[string]int{}
	rivals := rivalDomains(brands)
	for rows.Next() {
		var id int64
		var text string
		var url, domain, status *string
		var isSelf *bool
		var fetched *time.Time
		var check PageCheck
		if err := rows.Scan(&id, &text, &url, &domain, &isSelf, &status, &fetched, &check.SelfPresent, &check.Brands, &check.Error); err != nil {
			return Brief{}, err
		}
		texts[id] = text
		if url == nil {
			continue
		}
		citations[*url]++
		if slices.Contains(links[id], *url) {
			continue
		}
		links[id] = append(links[id], *url)
		page := pages[*url]
		if page == nil {
			page = &BriefPage{URL: *url, Domain: *domain, IsSelf: *isSelf}
			if status != nil {
				check.Status, check.FetchedAt, check.Competitors = *status, *fetched, competitorsIn(check.Brands, kinds)
				page.Check = &check
			}
			page.Outreach = outreach(page.Domain, page.IsSelf, page.Check, rivals)
			pages[*url] = page
		}
		page.Answers++
	}
	if err := rows.Err(); err != nil {
		return Brief{}, err
	}

	for i, cell := range cellsBy(answers, bySeries) {
		t := panel.TallyOf(scoredOf(cell))
		l := BriefLine{Series: cell[0].series, Title: lineTitle(cell[0].series.Pair), AnswerID: cell[0].id, AnswerIDs: idsOf(cell),
			Answered: t.Answered, Named: t.Named, Linked: t.Cited, State: lineState(t.Mentioned(), t.MostlyCited()),
			Brands: []string{}, FanOut: []string{}, Links: []string{}}
		if t.Mentioned() {
			l.Position = nullPosition(t.Position)
		}
		for _, a := range cell {
			l.FanOut = appendNew(l.FanOut, a.fanOut...)
			l.Links = appendNew(l.Links, links[a.id]...)
		}
		// The excerpt is an answer that did what the line says, so a missed
		// line never quotes the one answer that named you.
		typical := max(0, slices.IndexFunc(cell, func(a scoredAnswer) bool { return lineState(a.scored.Mentioned, a.scored.Cited) == l.State }))
		l.Excerpt = opening(texts[cell[typical].id], excerptRunes)
		for _, m := range t.Mentions {
			if b := byID[m.Brand]; b.Kind != panel.KindTechnology {
				l.Brands = append(l.Brands, b.Name)
			}
		}
		p.Lines = append(p.Lines, l)
		p.Answered, p.Named, p.Linked = p.Answered+t.Answered, p.Named+t.Named, p.Linked+t.Cited
		if i == 0 {
			p.PerLine = t.Answered
		} else if p.PerLine != t.Answered {
			p.PerLine = 0
		}
	}
	slices.SortFunc(p.Lines, func(a, b BriefLine) int {
		return cmp.Or(cmp.Compare(slices.Index(worse, a.State), slices.Index(worse, b.State)), cmp.Compare(a.Pair.ID, b.Pair.ID))
	})
	for _, l := range p.Lines {
		for _, q := range l.FanOut {
			if !slices.Contains(p.FanOut, q) {
				p.FanOut = append(p.FanOut, q)
			}
		}
		if l.State != LineNamedLinked && l.Excerpt != "" && len(p.Excerpts) < briefExcerpts {
			p.Excerpts = append(p.Excerpts, l)
		}
	}
	for _, page := range pages {
		p.Pages = append(p.Pages, *page)
	}
	slices.SortFunc(p.Pages, func(a, b BriefPage) int {
		return cmp.Or(cmp.Compare(b.Answers, a.Answers), cmp.Compare(citations[b.URL], citations[a.URL]), cmp.Compare(a.URL, b.URL))
	})
	p.Pages = p.Pages[:min(len(p.Pages), briefPages)]
	for _, pg := range p.Pages {
		if pg.IsSelf && pg.Check != nil && pg.Check.Status == "ok" && pg.Check.SelfPresent != nil && !*pg.Check.SelfPresent {
			p.SelfUnnamed = append(p.SelfUnnamed, pg.URL)
		}
		if pg.Outreach {
			p.Outreach = append(p.Outreach, pg)
		}
	}
	var out strings.Builder
	if err := briefTemplate.Execute(&out, p); err != nil {
		return Brief{}, err
	}
	p.Prompt = out.String()
	return p.Brief, nil
}

// lineState is how answers treated you, from whether they named you and
// linked to your site.
func lineState(named, linked bool) LineState {
	switch {
	case named && linked:
		return LineNamedLinked
	case named:
		return LineNamed
	case linked:
		return LineLinked
	default:
		return LineMissed
	}
}

// lineTitle names a pair as the screens do: "Claude Code · claude-sonnet-4-5",
// or "ChatGPT app" for a surface that takes no model.
func lineTitle(p PairRef) string {
	title := string(p.Surface)
	if spec, ok := engines.Lookup(p.Surface); ok {
		title = spec.Title
	}
	if p.Model != "" {
		title += " · " + p.Model
	}
	return title
}

// sentenceList joins items as a sentence does: "a", "a and b", "a, b and c".
func sentenceList(items []string) string {
	if len(items) < 2 {
		return strings.Join(items, "")
	}
	return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
}

// opening cuts text to about n characters at a word boundary.
func opening(text string, n int) string {
	text = strings.TrimSpace(text)
	if utf8.RuneCountInString(text) <= n {
		return text
	}
	cut := string([]rune(text)[:n])
	if i := strings.LastIndexAny(cut, " \n\t"); i > 0 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " \n\t,;:") + "…"
}
