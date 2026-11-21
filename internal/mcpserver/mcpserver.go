// Package mcpserver exposes Byname's services as MCP tools over stateless
// Streamable HTTP at /mcp, authenticated with OAuth access tokens that act
// as the person who approved the client, with the scopes they chose.
// Each tool is an input struct and a few lines that call one service
// method, and names the one scope a caller needs to run it.
package mcpserver

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/google/jsonschema-go/jsonschema"

	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/unkeyed/byname/internal/auth"
	"github.com/unkeyed/byname/internal/core"
	"github.com/unkeyed/byname/internal/instance"
)

const actorKey = "actor"

// ScopeMeta is the _meta key on every tool that names the scope it needs,
// so a client can tell which tools its connection may call.
const ScopeMeta = "byname.dev/scope"

const instructions = `Byname tracks how this team's brand shows up in AI answers and in Google Search Console. Call status for the version and whether onboarding is done. Each tool's _meta names the scope it needs ("byname.dev/scope"); a tool your connection lacks the scope for fails, and the person who approved this connection can reconnect it with more.`

// AccessTokens verifies the OAuth access tokens /mcp accepts:
// auth.OAuthServer in production.
type AccessTokens interface {
	// VerifyAccessToken returns the token's actor and expiry, or an error
	// wrapping auth.ErrInvalidToken. Any other error means it could not decide.
	VerifyAccessToken(ctx context.Context, token string) (core.Actor, time.Time, error)
	ResourceMetadataURL() string
}

// Handler returns the /mcp handler. Stateless, so any replica serves any
// request without sticky sessions.
func Handler(svc *instance.Service, tokens AccessTokens, version string) http.Handler {
	srv := mcp.NewServer(&mcp.Implementation{Name: "byname", Version: version}, &mcp.ServerOptions{Instructions: instructions})
	addTools(srv, svc)
	h := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv },
		&mcp.StreamableHTTPOptions{Stateless: true})
	return requireToken(tokens, h)
}

type verifiedKey struct{}

// requireToken answers a missing or rejected token with 401 and the
// WWW-Authenticate challenge that starts an MCP client's OAuth flow
// (RFC 9728 section 5.1), with error="invalid_token" when a token was sent
// (RFC 6750 section 3.1). The SDK's RequireBearerToken cannot add that
// error, so it only carries the verified token to the tools.
func requireToken(tokens AccessTokens, next http.Handler) http.Handler {
	challenge := fmt.Sprintf("Bearer resource_metadata=%q", tokens.ResourceMetadataURL())
	carry := mcpauth.RequireBearerToken(func(_ context.Context, _ string, r *http.Request) (*mcpauth.TokenInfo, error) {
		return r.Context().Value(verifiedKey{}).(*mcpauth.TokenInfo), nil
	}, nil)(next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scheme, token, _ := strings.Cut(r.Header.Get("Authorization"), " ")
		token = strings.TrimSpace(token)
		if !strings.EqualFold(scheme, "Bearer") || token == "" {
			w.Header().Set("WWW-Authenticate", challenge)
			http.Error(w, "unauthorized: connect your MCP client to sign in to Byname", http.StatusUnauthorized)
			return
		}
		a, expires, err := tokens.VerifyAccessToken(r.Context(), token)
		if errors.Is(err, auth.ErrInvalidToken) {
			w.Header().Set("WWW-Authenticate", challenge+`, error="invalid_token"`)
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		if err != nil {
			slog.ErrorContext(r.Context(), "verify MCP access token", "err", err)
			http.Error(w, "the database is unavailable; retry shortly", http.StatusServiceUnavailable)
			return
		}
		info := &mcpauth.TokenInfo{UserID: a.ID, Expiration: expires, Extra: map[string]any{actorKey: a}}
		carry.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), verifiedKey{}, info)))
	})
}

// actorOf reads the actor the bearer middleware stored for this request.
func actorOf(req *mcp.CallToolRequest) core.Actor {
	if req.Extra == nil || req.Extra.TokenInfo == nil {
		return core.Actor{}
	}
	a, _ := req.Extra.TokenInfo.Extra[actorKey].(core.Actor)
	return a
}

func readOnly() *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: new(false)}
}

// addTool registers a tool that callers with scope may run, whose result
// is out as structured content, and as text: the same JSON for clients
// that ignore structured content. Errors reach the agent through
// toolError. Every tool registers here.
func addTool[In, Out any](srv *mcp.Server, scope core.Scope, t *mcp.Tool, run func(context.Context, core.Actor, In) (Out, error)) {
	in, err := jsonschema.For[In](nil)
	if err != nil {
		panic(err)
	}
	out, err := jsonschema.For[Out](nil)
	if err != nil {
		panic(err)
	}
	t.InputSchema, t.OutputSchema = in, out
	t.Meta = mcp.Meta{ScopeMeta: string(scope)}
	mcp.AddTool(srv, t, func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
		var out Out
		actor := actorOf(req)
		if !actor.Has(scope) {
			return nil, out, fmt.Errorf("%s needs the %s scope, which this connection does not have; the person who approved it can disconnect it under Settings and connect again with %s", t.Name, scope, scope)
		}
		out, err := run(ctx, actor, in)
		if err != nil {
			return nil, out, toolError(t.Name, err)
		}
		j, err := json.Marshal(out)
		if err != nil {
			return nil, out, toolError(t.Name, err)
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(j)}}}, out, nil
	})
}

// toolError is the error text an agent sees, which is all MCP gives it. The
// caller's mistakes keep the service's message. Anything else is logged
// under a request id and reported by that id only, as REST does, so SQL and
// network details never reach the agent.
func toolError(tool string, err error) error {
	if errors.Is(err, core.ErrInvalid) || errors.Is(err, core.ErrNotFound) || errors.Is(err, core.ErrConflict) || errors.Is(err, core.ErrForbidden) || errors.Is(err, core.ErrUpstream) {
		return err
	}
	id := requestID()
	slog.Error("mcp tool failed", "request_id", id, "tool", tool, "err", err)
	var netErr net.Error
	if errors.As(err, &netErr) {
		return fmt.Errorf("Byname's database is unavailable; retry shortly (request_id %s)", id)
	}
	return fmt.Errorf("internal error (request_id %s); retrying will not help, the Byname admin can find the cause in the server log", id)
}

func requestID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

type noInput struct{}

func addTools(srv *mcp.Server, svc *instance.Service) {
	addTool(srv, core.ResultsRead, &mcp.Tool{Name: "status", Annotations: readOnly(), Description: "Byname's version and whether onboarding is done. GET /v1/status returns the same."},
		func(ctx context.Context, _ core.Actor, _ noInput) (instance.Status, error) {
			return svc.Status(ctx)
		})
}
