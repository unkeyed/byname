package mcpserver

import (
	"context"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/unkeyed/byname/internal/app"
	"github.com/unkeyed/byname/internal/core"
	"github.com/unkeyed/byname/internal/results"
)

type engineInput struct {
	Since *time.Time `json:"since,omitempty" jsonschema:"only runs started at or after this RFC 3339 time; omit for all time"`
	results.Line
}

// addPageTools adds the reads behind the Prompts and Gaps pages and the
// engine drawer.
func addPageTools(srv *mcp.Server, svc app.Services) {
	r := svc.Results
	addTool(srv, core.ResultsRead, &mcp.Tool{Name: "prompts", Annotations: readOnly(),
		Description: "Every question of the latest run in range: the pairs that named you there, your best position, the competitors named instead, and per run whether some pair named you, only cited you, did not, or every call failed. Filter by surface and model. GET /v1/prompts."},
		func(ctx context.Context, _ core.Actor, in scopedInput) (results.Prompts, error) {
			return r.Prompts(ctx, in.Since, in.Scope)
		})
	addTool(srv, core.ResultsRead, &mcp.Tool{Name: "cited_sources", Annotations: readOnly(),
		Description: "The domains and pages answers cited most over the runs in range, whether each page's HTML names you and which brands it names. Pages that name a competitor but not you, on sites neither yours nor a competitor's, are flagged as outreach targets. Filter by surface and model. GET /v1/sources."},
		func(ctx context.Context, _ core.Actor, in scopedInput) (results.Sources, error) {
			return r.Sources(ctx, in.Since, in.Scope)
		})
	addTool(srv, core.ResultsRead, &mcp.Tool{Name: "engine_view", Annotations: readOnly(),
		Description: "One chart line (a surface, or one of its pairs, on one endpoint and fidelity, as mention_rates reports it): visibility, questions it named you on and your average position on its latest run, visibility per run, its brand ranking, and the questions it named you on or not, with who it named instead. Never mixes fidelity labels. GET /v1/metrics/engine."},
		func(ctx context.Context, _ core.Actor, in engineInput) (results.Engine, error) {
			return r.Engine(ctx, in.Since, in.Line)
		})
}
