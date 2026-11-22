package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/unkeyed/byname/internal/app"
	"github.com/unkeyed/byname/internal/config"
	"github.com/unkeyed/byname/internal/core"
	"github.com/unkeyed/byname/internal/discovery"
	"github.com/unkeyed/byname/internal/logos"
	"github.com/unkeyed/byname/internal/results"
)

type hiddenList struct {
	Brands []results.HiddenBrand `json:"brands"`
}

type logoList struct {
	Logos []logos.Logo `json:"logos"`
}

type brandKind struct {
	BrandID int64 `json:"brand_id"`
	Hidden  bool  `json:"hidden" jsonschema:"true once marked as not a competitor, false once back on the list"`
}

// addOtherBrandsTools registers the tools for the brands answers name that
// you don't track, and for logos.
func addOtherBrandsTools(srv *mcp.Server, svc app.Services) {
	addTool(srv, core.ResultsRead, &mcp.Tool{Name: "list_other_brands", Annotations: readOnly(),
		Description: "The brands AI answers named that you don't track, most named first: how many answers in range named each, the questions it was named for most, and the change since the check before. They count in no score. Also how many answers in range were read for other brands. GET /v1/other-brands."},
		func(ctx context.Context, _ core.Actor, in scopedInput) (results.OtherBrands, error) {
			return svc.Results.OtherBrands(ctx, in.Since, in.Scope)
		})
	addTool(srv, core.ResultsRead, &mcp.Tool{Name: "list_hidden_brands", Annotations: readOnly(),
		Description: "The brands someone marked as not a competitor, with how many answers named each. GET /v1/other-brands/hidden."},
		func(ctx context.Context, _ core.Actor, _ noInput) (hiddenList, error) {
			v, err := svc.Results.HiddenBrands(ctx)
			return hiddenList{v}, err
		})
	addTool(srv, core.ResultsRead, &mcp.Tool{Name: "get_other_brands_settings", Annotations: readOnly(),
		Description: "Whether each new answer is read for other brands and what reading one costs, and whether logos are looked up, what one new brand costs and the monthly limit on new lookups. Both go through Treg. GET /v1/other-brands/settings."},
		func(ctx context.Context, _ core.Actor, _ noInput) (discovery.Settings, error) {
			return svc.Discovery.Settings(ctx)
		})
	addTool(srv, core.ResultsRead, &mcp.Tool{Name: "get_brand_backfill", Annotations: readOnly(),
		Description: "How many past answers were never read for other brands, what reading them would cost, and the latest pass someone started. GET /v1/other-brands/backfill."},
		func(ctx context.Context, _ core.Actor, _ noInput) (discovery.Backfill, error) {
			return svc.Discovery.Backfill(ctx)
		})
	addTool(srv, core.ResultsRead, &mcp.Tool{Name: "list_logos", Annotations: readOnly(),
		Description: "Every brand with a logo, with its colour and domain. The logo itself is an image at GET /v1/brands/{id}/logo. GET /v1/logos."},
		func(ctx context.Context, _ core.Actor, _ noInput) (logoList, error) {
			v, err := svc.Logos.Logos(ctx)
			return logoList{v}, err
		})

	addTool(srv, core.ConfigWrite, &mcp.Tool{Name: "track_brand", Annotations: mutates(false, true),
		Description: "Track a brand AI answers named as a competitor (id from list_other_brands). It counts in scores from then on, and every past answer is scored again, locally and for free. POST /v1/other-brands/{id}/track."},
		func(ctx context.Context, _ core.Actor, in byID) (config.Brand, error) {
			return svc.Discovery.Track(ctx, in.ID)
		})
	addTool(srv, core.ConfigWrite, &mcp.Tool{Name: "hide_brand", Annotations: mutates(false, true),
		Description: "Mark a brand AI answers named as not a competitor, so it leaves the list of brands you don't track; unhide_brand brings it back. POST /v1/other-brands/{id}/hide."},
		func(ctx context.Context, _ core.Actor, in byID) (brandKind, error) {
			return brandKind{in.ID, true}, svc.Discovery.Hide(ctx, in.ID)
		})
	addTool(srv, core.ConfigWrite, &mcp.Tool{Name: "unhide_brand", Annotations: mutates(false, true),
		Description: "Put a brand marked as not a competitor back on the list of brands you don't track. POST /v1/other-brands/{id}/unhide."},
		func(ctx context.Context, _ core.Actor, in byID) (brandKind, error) {
			return brandKind{in.ID, false}, svc.Discovery.Unhide(ctx, in.ID)
		})
	addTool(srv, core.ConfigWrite, &mcp.Tool{Name: "update_other_brands_settings", Annotations: mutates(false, true),
		Description: "Turn reading each new answer for other brands on or off, turn logo lookups on or off, and set the monthly limit on new logo lookups (read them with get_other_brands_settings first). Both cost money through Treg. PUT /v1/other-brands/settings."},
		func(ctx context.Context, _ core.Actor, in discovery.SettingsInput) (discovery.Settings, error) {
			return svc.Discovery.PutSettings(ctx, in)
		})
	addTool(srv, core.RunsWrite, &mcp.Tool{Name: "start_brand_backfill", Annotations: mutates(false, true),
		Description: "Read every past answer never read for other brands, as one job in Check history that starts within a minute. It costs cost_usd from get_brand_backfill, within the monthly spend cap. A pass already queued or running is returned instead. POST /v1/other-brands/backfill."},
		func(ctx context.Context, _ core.Actor, _ noInput) (discovery.Backfill, error) {
			return svc.Discovery.StartBackfill(ctx)
		})
}
