package results

import (
	"fmt"
	"time"

	"github.com/unkeyed/byname/internal/core"
	"github.com/unkeyed/byname/internal/engines"
)

// Scope narrows results to some pairs. The zero Scope is every pair.
type Scope struct {
	Surface engines.Surface `json:"surface,omitempty" jsonschema:"only this surface key, such as claude_code; omit for every surface"`
	Model   string          `json:"model,omitempty" jsonschema:"only pairs on this model, such as claude-sonnet-4-5; omit for every model"`
}

func (sc Scope) check() error {
	if _, ok := engines.Lookup(sc.Surface); sc.Surface != "" && !ok {
		return fmt.Errorf("%w: surface %q is not a surface key; list them with GET /v1/surfaces", core.ErrInvalid, sc.Surface)
	}
	return nil
}

func (sc Scope) holds(p PairRef) bool {
	return (sc.Surface == "" || p.Surface == sc.Surface) && (sc.Model == "" || p.Model == sc.Model)
}

// Grouping is what one line is.
type Grouping string

const (
	// ByPair draws one line per surface and model pair.
	ByPair Grouping = "pair"
	// BySurface draws one line per surface, combining its models only where
	// they share an endpoint and fidelity, so no line averages an app
	// answer with an approximation.
	BySurface Grouping = "surface"
)

func (g Grouping) check() error {
	if g != ByPair && g != BySurface {
		return fmt.Errorf("%w: by must be pair or surface, got %q", core.ErrInvalid, g)
	}
	return nil
}

// scopedFrom and scopedWhere select the answers of runs started at or after
// $1 (null for all time) on the pairs in scope ($2 surface, $3 model, each
// ” for all). Joins go between the two. counted, after them, keeps only
// answers that count in scores: not those to a question that names your
// brand, unless someone counted it anyway.
const (
	scopedFrom = ` FROM answer a JOIN run r ON r.id = a.run_id JOIN surface_model m ON m.id = a.surface_model_id
	JOIN question q ON q.id = a.question_id `
	scopedWhere = ` WHERE ($1::timestamptz IS NULL OR coalesce(r.started_at, r.queued_at) >= $1)
	AND ($2::text = '' OR m.surface_key = $2) AND ($3::text = '' OR m.model = $3) `
	counted = ` AND q.counts_in_scores `
)

// lineKey selects the five columns of the Series a row belongs to under
// grouping $4. A surface line has pair id 0 and no model.
const lineKey = `CASE WHEN $4::text = 'surface' THEN 0 ELSE m.id END, m.surface_key,
	CASE WHEN $4::text = 'surface' THEN '' ELSE coalesce(m.model, '') END, a.endpoint, a.fidelity`

// lineModels aggregates the models a line combines, none for an app surface.
const lineModels = `coalesce(array_agg(DISTINCT m.model) FILTER (WHERE m.model IS NOT NULL AND m.model <> ''), '{}')`

func scopeArgs(since *time.Time, sc Scope, by Grouping) []any {
	return []any{since, string(sc.Surface), sc.Model, string(by)}
}
