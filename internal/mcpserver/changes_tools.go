package mcpserver

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/unkeyed/byname/internal/app"
	"github.com/unkeyed/byname/internal/changes"
	"github.com/unkeyed/byname/internal/core"
)

type changeList struct {
	Changes []changes.Change `json:"changes"`
}

type markerList struct {
	Markers []changes.Marker `json:"markers"`
}

type controlList struct {
	ControlPages []changes.ControlPage `json:"control_pages"`
}

type rulesInput struct {
	Rules []changes.Rule `json:"rules" jsonschema:"the path rules in order; the first glob a changed file matches gives its page URL"`
}

// addChangeTools registers the site changes tools. Connecting the GitHub
// repo stays with a signed-in admin, so no tool sets or reads the token.
func addChangeTools(srv *mcp.Server, svc app.Services) {
	addTool(srv, core.ResultsRead, &mcp.Tool{Name: "list_changes", Annotations: readOnly(),
		Description: "Every site change, the latest merge first: the page, the pull request or manual log, Google's first crawl after the merge, the interim and verdict days, the page's before-period numbers, and the interim and final difference-in-differences reads against the control pages. GET /v1/changes."},
		func(ctx context.Context, _ core.Actor, _ noInput) (changeList, error) {
			v, err := svc.Changes.List(ctx)
			return changeList{v}, err
		})
	addTool(srv, core.ResultsRead, &mcp.Tool{Name: "get_change", Annotations: readOnly(),
		Description: "One site change with its timeline (merge, URL inspections, first crawl, interim and verdict days) and the full reads. GET /v1/changes/{id}."},
		func(ctx context.Context, _ core.Actor, in byID) (changes.Detail, error) {
			return svc.Changes.Get(ctx, in.ID)
		})
	addTool(srv, core.ResultsRead, &mcp.Tool{Name: "change_markers", Annotations: readOnly(),
		Description: "One marker per merged pull request or logged change between the from and to dates, on Search Console (Pacific Time) days, for marking a daily chart. GET /v1/changes/markers."},
		func(ctx context.Context, _ core.Actor, in changes.MarkerRange) (markerList, error) {
			v, err := svc.Changes.Markers(ctx, in)
			return markerList{v}, err
		})
	addTool(srv, core.ResultsRead, &mcp.Tool{Name: "change_settings", Annotations: readOnly(),
		Description: "When a change's interim and final reads fall, in days after the first crawl, and the thresholds a move must pass against the control pages. GET /v1/changes/settings."},
		func(ctx context.Context, _ core.Actor, _ noInput) (changes.Settings, error) {
			return svc.Changes.Settings(ctx)
		})
	addTool(srv, core.ResultsRead, &mcp.Tool{Name: "list_control_pages", Annotations: readOnly(),
		Description: "The control pages verdicts compare against, each with its hold date; a page past it is inactive and drops out of the control set. GET /v1/control-pages."},
		func(ctx context.Context, _ core.Actor, _ noInput) (controlList, error) {
			v, err := svc.Changes.ControlPages(ctx)
			return controlList{v}, err
		})
	addTool(srv, core.ResultsRead, &mcp.Tool{Name: "repo_status", Annotations: readOnly(),
		Description: "The GitHub repo site changes come from: whether a token is stored, its expiry, the path rules and the last hourly poll. Never the token. GET /v1/repo."},
		func(ctx context.Context, _ core.Actor, _ noInput) (changes.RepoStatus, error) {
			return svc.Changes.RepoStatus(ctx)
		})

	addTool(srv, core.ConfigWrite, &mcp.Tool{Name: "log_change", Annotations: mutates(false, false),
		Description: "Log a change to one page made outside GitHub, so recrawl watch and verdicts follow it. POST /v1/changes."},
		func(ctx context.Context, _ core.Actor, in changes.ManualChange) (changes.Detail, error) {
			return svc.Changes.LogChange(ctx, in)
		})
	addTool(srv, core.ConfigWrite, &mcp.Tool{Name: "delete_change", Annotations: mutates(true, true),
		Description: "Delete a change logged by hand. Changes from GitHub follow the path rules and cannot be deleted. DELETE /v1/changes/{id}."},
		func(ctx context.Context, _ core.Actor, in byID) (deleted, error) {
			return deleted{in.ID}, svc.Changes.DeleteChange(ctx, in.ID)
		})
	addTool(srv, core.ConfigWrite, &mcp.Tool{Name: "update_change_settings", Annotations: mutates(false, true),
		Description: "Replace the interim and verdict days and the thresholds. Changes already crawled keep their days. PUT /v1/changes/settings."},
		func(ctx context.Context, _ core.Actor, in changes.Settings) (changes.Settings, error) {
			return svc.Changes.PutSettings(ctx, in)
		})
	addTool(srv, core.ConfigWrite, &mcp.Tool{Name: "upsert_control_page", Annotations: mutates(false, true),
		Description: "Add a control page, or change the hold date and note of the one with the same URL. POST /v1/control-pages."},
		func(ctx context.Context, _ core.Actor, in changes.ControlInput) (changes.ControlPage, error) {
			return svc.Changes.UpsertControlPage(ctx, in)
		})
	addTool(srv, core.ConfigWrite, &mcp.Tool{Name: "delete_control_page", Annotations: mutates(true, true),
		Description: "Remove a control page. DELETE /v1/control-pages/{id}."},
		func(ctx context.Context, _ core.Actor, in byID) (deleted, error) {
			return deleted{in.ID}, svc.Changes.DeleteControlPage(ctx, in.ID)
		})
	addTool(srv, core.ConfigWrite, &mcp.Tool{Name: "update_path_rules", Annotations: mutates(false, true),
		Description: "Replace the rules that map a pull request's changed files to page URLs. The next poll reads the last 30 days of merges again with them. PUT /v1/repo/rules."},
		func(ctx context.Context, _ core.Actor, in rulesInput) (changes.RepoStatus, error) {
			if in.Rules == nil {
				return changes.RepoStatus{}, fmt.Errorf("%w: rules is required; send [] for none", core.ErrInvalid)
			}
			return svc.Changes.SetRules(ctx, in.Rules)
		})
}
