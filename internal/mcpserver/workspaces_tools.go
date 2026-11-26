package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/unkeyed/byname/internal/core"
	"github.com/unkeyed/byname/internal/workspaces"
)

// Workspaces lists the brands this instance tracks: workspaces.Registry.
type Workspaces interface {
	List(context.Context) ([]workspaces.Summary, error)
}

type workspaceList struct {
	Workspaces []workspaces.Summary `json:"workspaces"`
}

func addWorkspaceTools(srv *mcp.Server, ws Workspaces) {
	addTool(srv, core.ResultsRead, &mcp.Tool{Name: "list_workspaces", Annotations: readOnly(),
		Description: "Every brand this Byname instance tracks, each a workspace with its own settings, questions, checks and results, and whether its onboarding is finished. Each MCP connection reads and changes one workspace: /mcp the default, /w/<slug>/mcp the one with that slug. The REST API reaches any at /w/<slug>/v1/... GET /v1/workspaces."},
		func(ctx context.Context, _ core.Actor, _ noInput) (workspaceList, error) {
			v, err := ws.List(ctx)
			return workspaceList{v}, err
		})
}
