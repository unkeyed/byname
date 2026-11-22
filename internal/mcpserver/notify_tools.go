package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/unkeyed/byname/internal/app"
	"github.com/unkeyed/byname/internal/core"
	"github.com/unkeyed/byname/internal/notify"
)

// Notification channels hold secrets, so only a signed-in admin manages
// them and no tool does. Alerts are results, with read state per person.
func addNotifyTools(srv *mcp.Server, svc app.Services) {
	n := svc.Notify
	addTool(srv, core.ResultsRead, &mcp.Tool{Name: "list_alerts", Annotations: readOnly(),
		Description: "Alerts newest first: finished checks with what was gained and lost, refused or failed checks, spend caps reached, paused models, search goals reached, search moves, change verdicts, an expiring GitHub token and social digests, each with whether the person this connection acts for has read it, and the unread count. GET /v1/alerts."},
		func(ctx context.Context, a core.Actor, in notify.AlertQuery) (notify.AlertList, error) {
			return n.Alerts(ctx, a, in)
		})
	addTool(srv, core.ResultsRead, &mcp.Tool{Name: "mark_alerts_read", Annotations: mutates(false, true),
		Description: "Mark alerts read for the person this connection acts for, by id or all at once. POST /v1/alerts/read."},
		func(ctx context.Context, a core.Actor, in notify.ReadInput) (notify.ReadResult, error) {
			return n.MarkRead(ctx, a, in)
		})
}
