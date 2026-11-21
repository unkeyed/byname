package results

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/unkeyed/byname/internal/core"
	"github.com/unkeyed/byname/internal/engines"
	"github.com/unkeyed/byname/internal/panel"
)

// Line names one chart line: a surface, or one of its pairs, on one
// endpoint at one fidelity. A surface line combines its models' answers,
// which all share the fidelity, so the engine view never mixes fidelity
// labels.
type Line struct {
	Surface  engines.Surface `json:"surface" jsonschema:"a surface key, such as claude_code"`
	PairID   int64           `json:"pair_id,omitempty" jsonschema:"one pair of the surface; omit for the surface line, which combines its models"`
	Endpoint string          `json:"endpoint" jsonschema:"the line's endpoint, as a series reports it"`
	Fidelity string          `json:"fidelity" jsonschema:"app, model_match or approximation"`
}

var fidelities = []string{"app", "model_match", "approximation"}

func (l Line) check() error {
	if _, ok := engines.Lookup(l.Surface); !ok {
		return fmt.Errorf("%w: surface %q is not a surface key; list them with GET /v1/surfaces", core.ErrInvalid, l.Surface)
	}
	if l.Endpoint == "" || !slices.Contains(fidelities, l.Fidelity) {
		return fmt.Errorf("%w: a line needs its endpoint and its fidelity (app, model_match or approximation), as a series reports them", core.ErrInvalid)
	}
	return nil
}

// EnginePrompt is one question on the line in the latest run.
type EnginePrompt struct {
	Question QuestionRef `json:"question"`
	// AnswerID is the answer to open: the best placed one when the line
	// combines models.
	AnswerID int64    `json:"answer_id"`
	Position *int     `json:"position" jsonschema:"your best position on the line; null when not named"`
	Cited    bool     `json:"cited" jsonschema:"an answer cited your domains"`
	Instead  []string `json:"instead" jsonschema:"competitors named by answers that did not name you, most often first"`
}

// Engine is the engine drawer: one line's standing on its latest run in
// range, its visibility per run, and the brands it names.
type Engine struct {
	Series
	Models      []string `json:"models" jsonschema:"the models the line combines; none on an app surface"`
	Runs        int      `json:"runs" jsonschema:"runs in range with answers on the line"`
	LatestRunID int64    `json:"latest_run_id" jsonschema:"0 when the line has no answers in range"`
	Answered    int      `json:"answered" jsonschema:"answers on the latest run that did not fail"`
	Named       int      `json:"named" jsonschema:"of those, answers that named you"`
	// Visibility is named over answered on the latest run, the line's
	// mention rate; null when none answered.
	Visibility      *float64        `json:"visibility"`
	Prompts         int             `json:"prompts" jsonschema:"questions the line answered on the latest run"`
	VisiblePrompts  int             `json:"visible_prompts" jsonschema:"of those, questions it named you on"`
	AveragePosition *float64        `json:"average_position" jsonschema:"your mean position over the latest run's answers that named you; null when none did"`
	Points          []RatePoint     `json:"points" jsonschema:"visibility per run, oldest first"`
	Ranking         []BrandStanding `json:"ranking" jsonschema:"you and every competitor the line named over the range, most mentioned first; technologies left out"`
	NotMentioned    []EnginePrompt  `json:"not_mentioned" jsonschema:"latest run questions it did not name you on"`
	Visible         []EnginePrompt  `json:"visible" jsonschema:"latest run questions it named you on, best position first"`
}

// Engine reads one line over the runs started since (nil for all time).
func (s *Service) Engine(ctx context.Context, since *time.Time, l Line) (Engine, error) {
	out := Engine{Series: Series{Pair: PairRef{ID: l.PairID, Surface: l.Surface}, Endpoint: l.Endpoint, Fidelity: l.Fidelity},
		Models: []string{}, Points: []RatePoint{}, Ranking: []BrandStanding{}, NotMentioned: []EnginePrompt{}, Visible: []EnginePrompt{}}
	if err := l.check(); err != nil {
		return out, err
	}
	answers, err := s.scoredAnswers(ctx, `m.surface_key = $1 AND ($2 = 0 OR m.id = $2) AND a.endpoint = $3 AND a.fidelity = $4
		AND ($5::timestamptz IS NULL OR coalesce(r.started_at, r.queued_at) >= $5)`, l.Surface, l.PairID, l.Endpoint, l.Fidelity, since)
	if err != nil {
		return out, err
	}
	if l.PairID != 0 && len(answers) > 0 {
		out.Pair = answers[0].series.Pair
	}
	brands, err := s.brands(ctx)
	if err != nil {
		return out, err
	}
	byID := map[panel.BrandID]panel.Brand{}
	standings := map[panel.BrandID]*BrandStanding{}
	for _, b := range brands {
		byID[b.ID] = b
		if b.Kind == panel.KindSelf {
			standings[b.ID] = &BrandStanding{BrandID: int64(b.ID), Name: b.Name, Kind: b.Kind}
		}
	}
	mentions := 0
	for _, a := range answers {
		if m := a.series.Pair.Model; m != "" && !slices.Contains(out.Models, m) {
			out.Models = append(out.Models, m)
		}
		if n := len(out.Points); n == 0 || out.Points[n-1].RunID != a.runID {
			out.Points = append(out.Points, RatePoint{RunID: a.runID, RunStartedAt: a.started})
		}
		p := &out.Points[len(out.Points)-1]
		p.Calls++
		if a.scored.Status == engines.StatusFailed {
			continue
		}
		p.Answered++
		if a.scored.Mentioned {
			p.Mentioned++
		}
		if a.scored.Cited {
			p.Cited++
		}
		for _, m := range a.scored.Mentions {
			b := byID[m.Brand]
			if b.Kind == panel.KindTechnology {
				continue
			}
			if standings[b.ID] == nil {
				standings[b.ID] = &BrandStanding{BrandID: int64(b.ID), Name: b.Name, Kind: b.Kind}
			}
			standings[b.ID].Mentions++
			mentions++
		}
	}
	slices.Sort(out.Models)
	for i := range out.Points {
		if p := &out.Points[i]; p.Answered > 0 {
			p.MentionRate = new(float64(p.Mentioned) / float64(p.Answered))
			p.CitationRate = new(float64(p.Cited) / float64(p.Answered))
		}
	}
	for _, b := range standings {
		if mentions > 0 {
			b.Share = float64(b.Mentions) / float64(mentions)
		}
		out.Ranking = append(out.Ranking, *b)
	}
	rankBrands(out.Ranking)
	out.Runs = len(out.Points)
	if out.Runs == 0 {
		return out, nil
	}

	latest := out.Points[len(out.Points)-1]
	out.LatestRunID, out.Answered, out.Named, out.Visibility = latest.RunID, latest.Answered, latest.Mentioned, latest.MentionRate
	byQuestion := map[panel.QuestionID][]scoredAnswer{}
	var order []panel.QuestionID
	positions, sum := 0, 0
	for _, a := range answers {
		if a.runID != latest.RunID || a.scored.Status == engines.StatusFailed {
			continue
		}
		if _, ok := byQuestion[a.question]; !ok {
			order = append(order, a.question)
		}
		byQuestion[a.question] = append(byQuestion[a.question], a)
		if a.scored.Position > 0 {
			positions++
			sum += a.scored.Position
		}
	}
	if positions > 0 {
		out.AveragePosition = new(float64(sum) / float64(positions))
	}
	refs, err := s.questionRefs(ctx, order)
	if err != nil {
		return out, err
	}
	out.Prompts = len(order)
	for _, q := range order {
		group := byQuestion[q]
		ep := EnginePrompt{Question: refs[q], AnswerID: group[0].id, Instead: namedInstead(group, brands)}
		for _, a := range group {
			ep.Cited = ep.Cited || a.scored.Cited
			if p := a.scored.Position; p > 0 && (ep.Position == nil || p < *ep.Position) {
				ep.Position, ep.AnswerID = &p, a.id
			}
		}
		if ep.Position != nil {
			out.VisiblePrompts++
			out.Visible = append(out.Visible, ep)
		} else {
			out.NotMentioned = append(out.NotMentioned, ep)
		}
	}
	slices.SortStableFunc(out.Visible, func(a, b EnginePrompt) int { return cmp.Compare(*a.Position, *b.Position) })
	slices.SortStableFunc(out.NotMentioned, func(a, b EnginePrompt) int { return cmp.Compare(len(b.Instead), len(a.Instead)) })
	return out, nil
}

// rankBrands sorts brands most mentioned first and ranks them: 1 for the
// most mentioned, shared by brands with equal mentions, 0 for one never
// named.
func rankBrands(brands []BrandStanding) {
	slices.SortFunc(brands, func(a, b BrandStanding) int {
		return cmp.Or(cmp.Compare(b.Mentions, a.Mentions), cmp.Compare(a.Name, b.Name))
	})
	for i := range brands {
		switch b := &brands[i]; {
		case b.Mentions == 0:
		case i > 0 && brands[i-1].Mentions == b.Mentions:
			b.Rank = brands[i-1].Rank
		default:
			b.Rank = i + 1
		}
	}
}
