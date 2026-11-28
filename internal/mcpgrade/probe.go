package mcpgrade

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
)

// observation is everything the probe saw, parsed, and nothing scored. SDK
// and oauthex types stay in here, unexported, so no wire type crosses the
// package boundary. A nil pointer means the probe that would fill it did
// not run.
type observation struct {
	endpoint string
	token    bool // a token was given

	// first is a bare initialize POST, always without the token: it decides
	// the transport and whether the server asks for sign-in.
	first     httpSeen
	legacySSE bool      // GET text/event-stream sent an endpoint event
	auth      *authSeen // only when first answered 401

	session    *sessionSeen
	sessionErr error // why there is no session, when one was tried

	html     httpSeen // GET endpoint with Accept: text/html
	manifest manifestSeen
	registry registrySeen
}

type httpSeen struct {
	status      int
	contentType string
	header      http.Header
	err         error
}

type authSeen struct {
	challenges   []oauthex.Challenge
	challengeErr error
	prmURL       string // where the resource metadata was found or last looked for
	prm          *oauthex.ProtectedResourceMetadata
	prmErr       error
	asURL        string // the first authorization server, "" when none was named
	as           *oauthex.AuthServerMeta
	asErr        error
}

type sessionSeen struct {
	init      *mcp.InitializeResult
	negotiate time.Duration
	tools     []*rawTool // from the wire, in listed order
	toolsErr  error
	// lists maps each declared list method (resources/list, prompts/list)
	// to its outcome.
	lists      map[string]error
	badVersion httpSeen // MCP-Protocol-Version: 1999-01-01
	unknownRPC rpcSeen  // method byname/unknown
	origin     httpSeen // Origin: https://byname-probe.invalid
}

type rpcSeen struct {
	httpSeen
	code int64 // JSON-RPC error code, 0 when none
}

type manifestSeen struct {
	at            string // the manifest URL that answered, "" when none
	namesEndpoint bool
}

type registrySeen struct {
	listed bool
	err    error // registry unreachable: the check is skipped, not failed
}

// rawTool is one tool as tools/list sent it, not as the SDK decoded it: the
// SDK drops tools it finds invalid and reads an undeclared readOnlyHint as
// false.
type rawTool struct {
	name, title, annotTitle, description string
	// hints holds the declared annotation hints by hintNames key; a missing
	// key was not declared.
	hints  map[string]bool
	input  json.RawMessage
	output json.RawMessage // nil when absent
}

var hintNames = []string{"read_only", "destructive", "idempotent", "open_world"}
