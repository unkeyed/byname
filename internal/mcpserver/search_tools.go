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

type onProvider struct {
	Provider search.Provider `json:"provider" jsonschema:"the search engine: google or bing, as search_status lists them"`
}

type rangeInput struct {
	onProvider
	search.Range
}

type pagesInput struct {
	onProvider
	Days  int `json:"days,omitempty" jsonschema:"window length in days, 1 to 480, default 28; rounded to whole weeks for Bing"`
	Limit int `json:"limit,omitempty" jsonschema:"rows, 1 to 200, default 25"`
}

type queriesInput struct {
	onProvider
	search.Top
}

type brandedTermsInput struct {
	Terms *[]string `json:"terms" jsonschema:"the branded search terms; an empty list goes back to your brand's name and aliases"`
}

// addSearchTools registers the search engine tools. Every read names its
// provider, and no tool adds two engines together: their days and what
// they count differ. Connecting a provider stays with a signed-in admin,
// so no tool connects or disconnects.
func addSearchTools(srv *mcp.Server, svc app.Services) {
	addTool(srv, core.ResultsRead, &mcp.Tool{Name: "search_status", Annotations: readOnly(),
		Description: "Every search engine Byname reads (Google Search Console, Bing Webmaster Tools): whether it is connected, the site synced, the last sync and its error, the latest final day every window ends on, the partial days the next sync overwrites, how many days one row of searches covers (7 for Bing's weekly top lists) and how its numbers differ. Never the credential. GET /v1/search/providers."},
		func(ctx context.Context, _ core.Actor, _ noInput) (search.ProviderList, error) {
			return svc.Search.Providers(ctx)
		})
	addTool(srv, core.ResultsRead, &mcp.Tool{Name: "search_summary", Annotations: readOnly(),
		Description: "One search engine's headline: clicks, appearances (impressions), click rate (CTR), average position (0 when the engine gives none, as Bing does for the whole site), and clicks from searches with your name, without it (non-brand) and unlisted, over the latest 28 days ending on the latest final day, with the 28 days before for the trend. For Bing the windows are whole weeks. GET /v1/search/{provider}/summary."},
		func(ctx context.Context, _ core.Actor, in onProvider) (search.Summary, error) {
			return svc.Search.Summary(ctx, in.Provider)
		})
	addTool(srv, core.ResultsRead, &mcp.Tool{Name: "search_daily", Annotations: readOnly(),
		Description: "One search engine's site clicks, appearances (impressions), click rate (CTR) and average position per day, partial days marked final false. Defaults to the 90 days ending on the latest day stored. GET /v1/search/{provider}/daily."},
		func(ctx context.Context, _ core.Actor, in rangeInput) (search.DailySeries, error) {
			return svc.Search.Daily(ctx, in.Provider, in.Range)
		})
	addTool(srv, core.ResultsRead, &mcp.Tool{Name: "search_brand_split", Annotations: readOnly(),
		Description: "One search engine's clicks from searches with your name against searches without it (non-brand) over trailing 28-day windows ending on final days only, one a day for Google and one a week for Bing. Unlisted clicks are never non-brand. GET /v1/search/{provider}/brand."},
		func(ctx context.Context, _ core.Actor, in rangeInput) (search.BrandSeries, error) {
			return svc.Search.Brand(ctx, in.Provider, in.Range)
		})
	addTool(srv, core.ResultsRead, &mcp.Tool{Name: "top_pages", Annotations: readOnly(),
		Description: "One search engine's top pages by clicks over a window ending on the latest final day. For Bing these are sums of its weekly top lists. GET /v1/search/{provider}/pages."},
		func(ctx context.Context, _ core.Actor, in pagesInput) (search.PageList, error) {
			return svc.Search.TopPages(ctx, in.Provider, search.Top{Days: in.Days, Limit: in.Limit})
		})
	addTool(srv, core.ResultsRead, &mcp.Tool{Name: "top_queries", Annotations: readOnly(),
		Description: "One search engine's top search queries by clicks over a window ending on the latest final day, each marked branded or not; non_brand leaves branded ones out. For Bing these are sums of its weekly top lists. GET /v1/search/{provider}/queries."},
		func(ctx context.Context, _ core.Actor, in queriesInput) (search.QueryList, error) {
			return svc.Search.TopQueries(ctx, in.Provider, in.Top)
		})

	addTool(srv, core.ResultsRead, &mcp.Tool{Name: "list_goals", Annotations: readOnly(),
		Description: "Every search engine's goals by due date, each with its provider and its progress over its trailing 28- or 90-day window ending on that provider's latest final day (91 days for a 90-day Bing goal, whole weeks). GET /v1/goals."},
		func(ctx context.Context, _ core.Actor, _ noInput) (goalList, error) {
			v, err := svc.Search.Goals(ctx)
			return goalList{v}, err
		})
	addTool(srv, core.ConfigWrite, &mcp.Tool{Name: "upsert_goal", Annotations: mutates(false, true),
		Description: "Add a search goal for one provider, or set the target of the goal with the same provider, metric, window and due date. POST /v1/goals."},
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
		Description: "Replace the branded search terms. A search query containing one, ignoring case, is brand, on every search engine; an empty list goes back to your brand's name and aliases. PUT /v1/settings/branded-terms."},
		func(ctx context.Context, _ core.Actor, in brandedTermsInput) (config.SettingsView, error) {
			if in.Terms == nil {
				return config.SettingsView{}, fmt.Errorf("%w: terms is required; send [] to go back to your brand's name and aliases", core.ErrInvalid)
			}
			return svc.Config.SetBrandedTerms(ctx, *in.Terms)
		})
}
