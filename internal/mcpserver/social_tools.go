package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/unkeyed/byname/internal/app"
	"github.com/unkeyed/byname/internal/core"
	"github.com/unkeyed/byname/internal/social"
)

type leftOutList struct {
	Accounts []social.LeftOut `json:"accounts"`
}

// addSocialTools registers the social mentions tools.
func addSocialTools(srv *mcp.Server, svc app.Services) {
	addTool(srv, core.ResultsRead, &mcp.Tool{Name: "list_social_mentions", Annotations: readOnly(),
		Description: "Posts that mention the brand on X, Reddit, Hacker News, LinkedIn, YouTube, Threads and Bluesky, newest first, each with its author, text, engagement and link, without those by left-out accounts unless include_hidden is set, with how many match and how many are hidden. GET /v1/social/mentions."},
		func(ctx context.Context, _ core.Actor, in social.MentionQuery) (social.MentionList, error) {
			return svc.Social.Mentions(ctx, in)
		})
	addTool(srv, core.ResultsRead, &mcp.Tool{Name: "get_social_digest", Annotations: readOnly(),
		Description: "One day's or week's social mentions grouped by network with the most engaged first, the authors new in it, and what the searches cost, without left-out accounts; the latest period that has ended unless day names one. GET /v1/social/digest."},
		func(ctx context.Context, _ core.Actor, in social.DigestQuery) (social.Digest, error) {
			return svc.Social.Digest(ctx, in)
		})
	addTool(srv, core.ResultsRead, &mcp.Tool{Name: "get_social_settings", Annotations: readOnly(),
		Description: "Whether social searches are on (they start off), the search terms, each network with whether it is on, its route (Treg or direct) and its latest call, how often searches run and when the digest goes out. GET /v1/social/settings."},
		func(ctx context.Context, _ core.Actor, _ noInput) (social.Settings, error) {
			return svc.Social.Settings(ctx)
		})
	addTool(srv, core.ResultsRead, &mcp.Tool{Name: "list_left_out_accounts", Annotations: readOnly(),
		Description: "The accounts whose mentions are hidden, such as teammates and the company's own accounts, each with its label and how many stored mentions it hides. GET /v1/social/left-out."},
		func(ctx context.Context, _ core.Actor, _ noInput) (leftOutList, error) {
			v, err := svc.Social.LeftOutAccounts(ctx)
			return leftOutList{v}, err
		})

	addTool(srv, core.ConfigWrite, &mcp.Tool{Name: "update_social_settings", Annotations: mutates(false, true),
		Description: "Turn social searches on or off (enabled; omit it to keep the setting), replace the search terms, the fetch period and the digest schedule, and turn networks on or off (read them with get_social_settings first). Treg networks cost money per search and nothing runs before onboarding is finished. PUT /v1/social/settings."},
		func(ctx context.Context, _ core.Actor, in social.SettingsInput) (social.Settings, error) {
			return svc.Social.PutSettings(ctx, in)
		})
	addTool(srv, core.ConfigWrite, &mcp.Tool{Name: "leave_out_account", Annotations: mutates(false, true),
		Description: "Leave an account out of social mentions, or relabel one already left out; its past mentions are hidden too. POST /v1/social/left-out."},
		func(ctx context.Context, _ core.Actor, in social.LeftOutInput) (social.LeftOut, error) {
			return svc.Social.LeaveOut(ctx, in)
		})
	addTool(srv, core.ConfigWrite, &mcp.Tool{Name: "remove_left_out_account", Annotations: mutates(true, true),
		Description: "Take an account off the left-out list, so its mentions show again. DELETE /v1/social/left-out/{id}."},
		func(ctx context.Context, _ core.Actor, in byID) (deleted, error) {
			return deleted{in.ID}, svc.Social.RemoveLeftOut(ctx, in.ID)
		})
}
