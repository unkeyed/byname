package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/unkeyed/byname/internal/core"
	"github.com/unkeyed/byname/internal/entities"
)

// Entities lists the brands this instance tracks: entities.Registry.
type Entities interface {
	List(context.Context) ([]entities.Summary, error)
}

type entityList struct {
	Entities []entities.Summary `json:"entities"`
}

func addEntityTools(srv *mcp.Server, all Entities) {
	addTool(srv, core.ResultsRead, &mcp.Tool{Name: "list_entities", Annotations: readOnly(),
		Description: "Every brand this Byname instance tracks, each an entity with its own settings, questions, checks and results, and whether its onboarding is finished. Each MCP connection reads and changes one entity: /mcp the default, /e/<slug>/mcp the one with that slug. The REST API reaches any at /e/<slug>/v1/... GET /v1/entities."},
		func(ctx context.Context, _ core.Actor, _ noInput) (entityList, error) {
			v, err := all.List(ctx)
			return entityList{v}, err
		})
}
