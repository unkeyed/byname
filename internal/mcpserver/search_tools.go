package mcpserver

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/unkeyed/byname/internal/app"
	"github.com/unkeyed/byname/internal/config"
	"github.com/unkeyed/byname/internal/core"
	"github.com/unkeyed/byname/internal/search"
)

type goalList struct {
	Goals []search.Goal `json:"goals"`
}

type updateGoalInput struct {
	ID int64 `json:"id"`
	search.GoalInput
}

type pagesInput struct {
	Days  int `json:"days,omitempty" jsonschema:"window length in days, 1 to 480, default 28"`
	Limit int `json:"limit,omitempty" jsonschema:"rows, 1 to 200, default 25"`
}

type brandedTermsInput struct {
	Terms *[]string `json:"terms" jsonschema:"the branded search terms; an empty list goes back to your brand's name and aliases"`
}

// addSearchTools registers the Search Console tools. Connecting Search
// Console stays with a signed-in admin, so no tool connects or disconnects.
func addSearchTools(srv *mcp.Server, svc app.Services) {
	addTool(srv, core.ResultsRead, &mcp.Tool{Name: "search_console_status", Annotations: readOnly(),
		Description: "Whether Search Console is connected, the property synced, the last sync and its error, the latest day Google has finalized and the partial days the next sync overwrites. Never the credential. GET /v1/search."},
		func(ctx context.Context, _ core.Actor, _ noInput) (search.Status, error) {
			return svc.Search.Status(ctx, search.Google)
		})
	addTool(srv, core.ResultsRead, &mcp.Tool{Name: "search_summary", Annotations: readOnly(),
		Description: "The Search Console headline: clicks, appearances (impressions), click rate (CTR), average position, and clicks from searches with your name, without it (non-brand) and anonymized over the latest 28 days ending on the latest final day, with the 28 days before for the trend. GET /v1/search/summary."},
		func(ctx context.Context, _ core.Actor, _ noInput) (search.Summary, error) {
			return svc.Search.Summary(ctx, search.Google)
		})
	addTool(srv, core.ResultsRead, &mcp.Tool{Name: "search_daily", Annotations: readOnly(),
		Description: "The site's Search Console clicks, appearances (impressions), click rate (CTR) and average position per day, partial days marked final false. Defaults to the 90 days ending on the latest day stored. GET /v1/search/daily."},
		func(ctx context.Context, _ core.Actor, in search.Range) (search.DailySeries, error) {
			return svc.Search.Daily(ctx, search.Google, in)
		})
	addTool(srv, core.ResultsRead, &mcp.Tool{Name: "search_brand_split", Annotations: readOnly(),
		Description: "Clicks from searches with your name against searches without it (non-brand) over trailing 28-day windows, one per day, ending on final days only. Anonymized clicks are never non-brand. GET /v1/search/brand."},
		func(ctx context.Context, _ core.Actor, in search.Range) (search.BrandSeries, error) {
			return svc.Search.Brand(ctx, search.Google, in)
		})
	addTool(srv, core.ResultsRead, &mcp.Tool{Name: "top_pages", Annotations: readOnly(),
		Description: "The top pages by Search Console clicks over a window ending on the latest final day. GET /v1/search/pages."},
		func(ctx context.Context, _ core.Actor, in pagesInput) (search.PageList, error) {
			return svc.Search.TopPages(ctx, search.Google, search.Top{Days: in.Days, Limit: in.Limit})
		})
	addTool(srv, core.ResultsRead, &mcp.Tool{Name: "top_queries", Annotations: readOnly(),
		Description: "The top search queries by Search Console clicks over a window ending on the latest final day, each marked branded or not; non_brand leaves branded ones out. GET /v1/search/queries."},
		func(ctx context.Context, _ core.Actor, in search.Top) (search.QueryList, error) {
			return svc.Search.TopQueries(ctx, search.Google, in)
		})

	addTool(srv, core.ResultsRead, &mcp.Tool{Name: "list_goals", Annotations: readOnly(),
		Description: "The search goals by due date, each with its progress over its trailing 28- or 90-day window ending on the latest final day. GET /v1/goals."},
		func(ctx context.Context, _ core.Actor, _ noInput) (goalList, error) {
			v, err := svc.Search.Goals(ctx)
			return goalList{v}, err
		})
	addTool(srv, core.ConfigWrite, &mcp.Tool{Name: "upsert_goal", Annotations: mutates(false, true),
		Description: "Add a search goal, or set the target of the goal with the same metric, window and due date. POST /v1/goals."},
		func(ctx context.Context, _ core.Actor, in search.GoalInput) (search.Goal, error) {
			return svc.Search.UpsertGoal(ctx, in)
		})
	addTool(srv, core.ConfigWrite, &mcp.Tool{Name: "update_goal", Annotations: mutates(false, true),
		Description: "Replace a search goal by id. PUT /v1/goals/{id}."},
		func(ctx context.Context, _ core.Actor, in updateGoalInput) (search.Goal, error) {
			return svc.Search.UpdateGoal(ctx, in.ID, in.GoalInput)
		})
	addTool(srv, core.ConfigWrite, &mcp.Tool{Name: "delete_goal", Annotations: mutates(true, true),
		Description: "Delete a search goal. DELETE /v1/goals/{id}."},
		func(ctx context.Context, _ core.Actor, in byID) (deleted, error) {
			return deleted{in.ID}, svc.Search.DeleteGoal(ctx, in.ID)
		})
	addTool(srv, core.ConfigWrite, &mcp.Tool{Name: "update_branded_terms", Annotations: mutates(false, true),
		Description: "Replace the branded search terms. A Search Console query containing one, ignoring case, is brand; an empty list goes back to your brand's name and aliases. PUT /v1/settings/branded-terms."},
		func(ctx context.Context, _ core.Actor, in brandedTermsInput) (config.SettingsView, error) {
			if in.Terms == nil {
				return config.SettingsView{}, fmt.Errorf("%w: terms is required; send [] to go back to your brand's name and aliases", core.ErrInvalid)
			}
			return svc.Config.SetBrandedTerms(ctx, *in.Terms)
		})
}
