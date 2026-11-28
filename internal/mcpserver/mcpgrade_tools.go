package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/unkeyed/byname/internal/app"
	"github.com/unkeyed/byname/internal/core"
	"github.com/unkeyed/byname/internal/mcpgrade"
)

func addMCPGradeTools(srv *mcp.Server, svc app.Services) {
	addTool(srv, core.ResultsRead, &mcp.Tool{Name: "mcp_server_grade", Annotations: readOnly(),
		Description: "The entity's own MCP server graded against the MCP spec essentials: the URL, whether a token is stored (never the token), the newest grade in full (0 to 100 score, letter, whether it is partial and why, each category's score and each check's status, finding and fix, and each tool's documentation score and model review) and the scores of the newest 50 grades, and the paid review's status, score and estimate. GET /v1/mcp-server."},
		func(ctx context.Context, _ core.Actor, _ noInput) (mcpgrade.Server, error) {
			return svc.MCPGrade.Server(ctx)
		})
	addTool(srv, core.ConfigWrite, &mcp.Tool{Name: "set_mcp_server", Annotations: mutates(false, true),
		Description: "Set the entity's MCP server URL, its Streamable HTTP endpoint. A URL on another origin deletes the stored token; an empty URL removes the server and its token and keeps the grades. Does not grade; call grade_mcp_server next. PUT /v1/mcp-server."},
		func(ctx context.Context, _ core.Actor, in mcpgrade.URLInput) (mcpgrade.Server, error) {
			return svc.MCPGrade.SetURL(ctx, in.URL)
		})
	addTool(srv, core.RunsWrite, &mcp.Tool{Name: "grade_mcp_server", Annotations: &mcp.ToolAnnotations{DestructiveHint: new(false), OpenWorldHint: new(true)},
		Description: "Grade the entity's MCP server now, such as after a deploy, and return the new grade. Reads the server's metadata and tool list, never calls a tool, and takes up to 30 seconds. POST /v1/mcp-server/grade."},
		func(ctx context.Context, _ core.Actor, _ noInput) (mcpgrade.Server, error) {
			return svc.MCPGrade.Run(ctx)
		})
	addTool(srv, core.RunsWrite, &mcp.Tool{Name: "review_mcp_tools", Annotations: &mcp.ToolAnnotations{DestructiveHint: new(false), IdempotentHint: true, OpenWorldHint: new(true)},
		Description: "Queue a paid review of the newest grade's tool documentation: a model scores each tool's title, description and inputs from 0 to 100 with a reason, one Treg call per tool (about $0.003) for tools without a review of their current definition, at most 100. Needs a Treg token; runs within a minute, stops at the monthly spend cap, and never changes the grade's score. Read the result with mcp_server_grade. POST /v1/mcp-server/review."},
		func(ctx context.Context, _ core.Actor, _ noInput) (mcpgrade.Server, error) {
			return svc.MCPGrade.StartReview(ctx)
		})
}
