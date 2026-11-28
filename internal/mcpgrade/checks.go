package mcpgrade

import (
	"encoding/json"
	"fmt"
	"iter"
	"maps"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

type category struct {
	id, title string
	// required: when nothing in it can be assessed it counts as 0;
	// otherwise it is dropped from the mean.
	required bool
}

var categories = []category{
	{"connection", "Connection", true},
	{"authorization", "Authorization", false},
	{"metadata", "Server metadata", true},
	{"definitions", "Tool definitions", true},
	{"documentation", "Tool documentation", true},
	{"discoverability", "Discoverability", false},
}

// check is one catalog entry. Its id is "<category>.<name>", stable forever:
// stored in reports and used as a docs anchor.
type check struct {
	id, title, fix string
	weight         int
	eval           evaluator
}

func (c check) category() string {
	cat, _, _ := strings.Cut(c.id, ".")
	return cat
}

// evaluator is serverCheck or toolCheck, so a check is exactly one of them.
type evaluator interface{ isEvaluator() }

// serverCheck judges the observation as a whole.
type serverCheck func(o *observation) mark

// toolCheck judges one tool. The check's fraction is the mean over the tools
// it applies to, which for a pass/fail judge is the share of tools that
// pass. With no session or no tools it is skipped.
type toolCheck func(t *rawTool) mark

func (serverCheck) isEvaluator() {}
func (toolCheck) isEvaluator()   {}

// mark is a check's raw outcome before weighting.
type mark struct {
	fraction float64 // 1 pass, 0 fail, between partial
	skipped  bool
	finding  string
}

func pass(finding string) mark               { return mark{fraction: 1, finding: finding} }
func partial(f float64, finding string) mark { return mark{fraction: f, finding: finding} }
func fail(finding string) mark               { return mark{finding: finding} }
func skip(why string) mark                   { return mark{skipped: true, finding: why} }

// checks is the catalog, in display order.
var checks = []check{
	{id: "connection.https", weight: 10, title: "Served over HTTPS",
		fix: "Serve the endpoint over https.", eval: serverCheck(checkHTTPS)},
	{id: "connection.transport", weight: 10, title: "Speaks Streamable HTTP",
		fix: "Serve MCP over Streamable HTTP: answer a POST to this URL with JSON or an event stream.", eval: serverCheck(checkTransport)},
	{id: "connection.negotiate", weight: 10, title: "Session starts",
		fix: "Answer server/discover or initialize with serverInfo and capabilities.", eval: serverCheck(checkNegotiate)},
	{id: "connection.version", weight: 6, title: "Current protocol version",
		fix: "Support protocol version 2026-07-28.", eval: serverCheck(checkVersion)},
	{id: "connection.bad_version", weight: 3, title: "Rejects an unknown protocol version",
		fix: "Answer HTTP 400 when MCP-Protocol-Version names a version you do not support.", eval: serverCheck(checkBadVersion)},
	{id: "connection.unknown_method", weight: 3, title: "Rejects unknown methods",
		fix: "Answer an unknown method with JSON-RPC error -32601 (method not found).", eval: serverCheck(checkUnknownMethod)},
	{id: "connection.origin", weight: 2, title: "Refuses foreign origins",
		fix: "Answer HTTP 403 when the Origin header names a site you do not trust.", eval: serverCheck(checkOrigin)},

	{id: "authorization.www_authenticate", weight: 2, title: "Sign-in challenge names its metadata",
		fix: `Send WWW-Authenticate: Bearer resource_metadata="<url>" with every 401.`, eval: serverCheck(checkWWWAuthenticate)},
	{id: "authorization.resource_metadata", weight: 3, title: "Protected resource metadata",
		fix: "Serve RFC 9728 metadata at /.well-known/oauth-protected-resource with resource set to this URL and your authorization_servers.", eval: serverCheck(checkResourceMetadata)},
	{id: "authorization.as_metadata", weight: 2, title: "Authorization server metadata",
		fix: "Serve RFC 8414 metadata at the authorization server's /.well-known/oauth-authorization-server.", eval: serverCheck(checkASMetadata)},
	{id: "authorization.pkce", weight: 3, title: "PKCE with S256",
		fix: "List S256 in code_challenge_methods_supported.", eval: serverCheck(checkPKCE)},
	{id: "authorization.registration", weight: 2, title: "Clients can register themselves",
		fix: "Support Client ID Metadata Documents or dynamic client registration (registration_endpoint).", eval: serverCheck(checkRegistration)},

	{id: "metadata.name", weight: 10, title: "Server name",
		fix: "Set serverInfo.name.", eval: serverCheck(infoCheck("name", func(i info) bool { return i.Name != "" }))},
	{id: "metadata.title", weight: 10, title: "Server title",
		fix: "Set serverInfo.title to the name people should see.", eval: serverCheck(infoCheck("title", func(i info) bool { return i.Title != "" }))},
	{id: "metadata.description", weight: 20, title: "Server description",
		fix: "Set serverInfo.description to one sentence on what the server does.", eval: serverCheck(infoCheck("description", func(i info) bool { return i.Description != "" }))},
	{id: "metadata.version", weight: 10, title: "Server version",
		fix: "Set serverInfo.version.", eval: serverCheck(infoCheck("version", func(i info) bool { return i.Version != "" }))},
	{id: "metadata.website", weight: 10, title: "Website",
		fix: "Set serverInfo.websiteUrl to the server's home page.", eval: serverCheck(infoCheck("websiteUrl", func(i info) bool { return isWebURL(i.WebsiteURL) }))},
	{id: "metadata.icon", weight: 10, title: "Icon",
		fix: "Add serverInfo.icons with an SVG, or an image with sizes of at least 64x64.", eval: serverCheck(checkIcon)},
	{id: "metadata.instructions", weight: 20, title: "Instructions",
		fix: "Return instructions that tell an agent when and how to use the server.", eval: serverCheck(checkInstructions)},
	{id: "metadata.capabilities", weight: 10, title: "Capabilities declared",
		fix: "Declare the capabilities you serve: tools, resources or prompts.", eval: serverCheck(checkCapabilities)},

	{id: "definitions.listed", weight: 3, title: "Lists tools",
		fix: "Declare the tools capability and list at least one tool.", eval: serverCheck(checkListed)},
	{id: "definitions.name_chars", weight: 3, title: "Tool names use safe characters",
		fix: "Use only A-Z a-z 0-9 _ . - in tool names, 1 to 128 long.", eval: toolCheck(checkNameChars)},
	{id: "definitions.name_unique", weight: 3, title: "Tool names are unique",
		fix: "Give every tool a distinct name.", eval: serverCheck(checkNameUnique)},
	{id: "definitions.name_64", weight: 1, title: "Tool names fit 64 characters",
		fix: "Keep tool names to 64 characters; many clients cut or refuse longer ones.", eval: toolCheck(checkName64)},
	{id: "definitions.input_schema", weight: 3, title: "Input schemas are valid",
		fix: "Give every tool an inputSchema of type object, with x-mcp-header only on string, integer or boolean properties and each header name unique.", eval: toolCheck(checkInputSchema)},
	{id: "definitions.output_schema", weight: 2, title: "Output schemas are objects",
		fix: "Make every outputSchema a JSON Schema of type object.", eval: toolCheck(checkOutputSchema)},
	{id: "definitions.descriptions", weight: 2, title: "Tools have descriptions",
		fix: "Give every tool a description.", eval: toolCheck(checkHasDescription)},
	{id: "definitions.titles", weight: 1, title: "Tools have titles",
		fix: "Give every tool a title.", eval: toolCheck(checkHasTitle)},
	{id: "definitions.annotations", weight: 1, title: "Tools declare every hint",
		fix: "Declare readOnlyHint, destructiveHint, idempotentHint and openWorldHint on every tool.", eval: toolCheck(checkAllHints)},
	{id: "definitions.desc_2kb", weight: 1, title: "Descriptions fit 2 KB",
		fix: "Keep each tool description under 2 KB and move detail into the input descriptions.", eval: toolCheck(checkDesc2KB)},
	{id: "definitions.hidden_chars", weight: 3, title: "No hidden characters",
		fix: "Remove invisible, control and bidirectional characters from tool names, descriptions and schemas.", eval: toolCheck(checkHiddenChars)},
	{id: "definitions.tool_budget", weight: 1, title: "Tool count fits an agent",
		fix: "Serve 40 tools or fewer; merge or split the rest into other servers.", eval: serverCheck(checkToolBudget)},
	{id: "definitions.declared_lists", weight: 2, title: "Declared lists answer",
		fix: "Answer resources/list and prompts/list when you declare those capabilities.", eval: serverCheck(checkDeclaredLists)},

	{id: "documentation.title", weight: 15, title: "Titled for people",
		fix: "Give each tool a title that says more than its name.", eval: toolCheck(docTitle)},
	{id: "documentation.description", weight: 35, title: "Described in full",
		fix: "Describe each tool in at least 40 characters: what it does, when to use it and what it returns.", eval: toolCheck(docDescription)},
	{id: "documentation.inputs", weight: 30, title: "Inputs described",
		fix: "Give every input property a type and a description.", eval: toolCheck(docInputs)},
	{id: "documentation.annotations", weight: 10, title: "Behavior hints",
		fix: "Declare the four behavior hints on each tool.", eval: toolCheck(docHints)},
	{id: "documentation.output", weight: 10, title: "Output described",
		fix: "Add an outputSchema describing what each tool returns.", eval: toolCheck(docOutput)},

	{id: "discoverability.manifest", weight: 1, title: "Manifest names the endpoint",
		fix: "Serve /.well-known/mcp.json on the endpoint's origin naming this URL.", eval: serverCheck(checkManifest)},
	{id: "discoverability.setup_page", weight: 1, title: "Setup page",
		fix: "Answer a browser (Accept: text/html) at this URL with a page on how to connect.", eval: serverCheck(checkSetupPage)},
	{id: "discoverability.registry", weight: 1, title: "Listed in the MCP Registry",
		fix: "Publish the server to registry.modelcontextprotocol.io with this URL as a remote; a name under io.github.* may not be found by domain.", eval: serverCheck(checkRegistry)},
}

// needsToken reports whether the server asked for sign-in and no token was
// given, so nothing behind sign-in could be graded.
func needsToken(o *observation) bool {
	return o.session == nil && o.first.status == 401 && !o.token
}

func noSession(o *observation) string {
	if needsToken(o) {
		return "Needs a token to grade"
	}
	return "No session"
}

func withSession(o *observation, f func(s *sessionSeen) mark) mark {
	if o.session == nil {
		return skip(noSession(o))
	}
	return f(o.session)
}

func withTools(o *observation, f func(ts []*rawTool) mark) mark {
	return withSession(o, func(s *sessionSeen) mark {
		if len(s.tools) == 0 {
			return skip("No tools listed")
		}
		return f(s.tools)
	})
}

func withAuth(o *observation, f func(a *authSeen) mark) mark {
	if o.auth == nil {
		return skip("The server did not ask for sign-in")
	}
	return f(o.auth)
}

// errLine is an error as one short line for a finding.
func errLine(err error) string {
	s := strings.Join(strings.Fields(err.Error()), " ")
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}

func checkHTTPS(o *observation) mark {
	if strings.HasPrefix(strings.ToLower(o.endpoint), "https://") {
		return pass("Served over https")
	}
	return fail("Served over plain http")
}

// transportOf names the transport the server spoke. A 401 to the POST is
// the Streamable HTTP sign-in flow, so it counts as Streamable HTTP.
func transportOf(o *observation) string {
	switch {
	case o.session != nil, o.first.status == 401, o.first.ok():
		return "streamable_http"
	case o.legacySSE:
		return "sse"
	}
	return "none"
}

func (h httpSeen) ok() bool {
	ct := strings.ToLower(h.contentType)
	return h.err == nil && h.status >= 200 && h.status < 300 &&
		(strings.HasPrefix(ct, "application/json") || strings.HasPrefix(ct, "text/event-stream"))
}

func checkTransport(o *observation) mark {
	switch transportOf(o) {
	case "streamable_http":
		if o.session == nil && o.first.status == 401 {
			return pass("Answered POST with a 401 sign-in challenge")
		}
		return pass("Streamable HTTP")
	case "sse":
		return partial(0.5, "Only the deprecated HTTP+SSE transport answered")
	}
	if o.first.err != nil {
		return fail(errLine(o.first.err))
	}
	return fail(fmt.Sprintf("POST answered HTTP %d %s", o.first.status, o.first.contentType))
}

func checkNegotiate(o *observation) mark {
	switch {
	case o.session != nil:
		return pass(fmt.Sprintf("Started in %d ms", o.session.negotiate.Milliseconds()))
	case needsToken(o):
		return skip("Needs a token to grade")
	case o.sessionErr != nil:
		return fail(errLine(o.sessionErr))
	}
	return fail("No session")
}

func checkVersion(o *observation) mark {
	return withSession(o, func(s *sessionSeen) mark {
		v := s.init.ProtocolVersion
		f := "Negotiated " + v
		switch {
		case v >= "2026-07-28":
			return pass(f)
		case v >= "2025-11-25":
			return partial(0.75, f)
		case v >= "2025-06-18":
			return partial(0.5, f)
		}
		return fail(f)
	})
}

func checkBadVersion(o *observation) mark {
	return withSession(o, func(s *sessionSeen) mark {
		switch h := s.badVersion; {
		case h.err != nil:
			return fail(errLine(h.err))
		case h.status == 400:
			return pass("Answered HTTP 400")
		default:
			return fail(fmt.Sprintf("Answered HTTP %d to MCP-Protocol-Version 1999-01-01", h.status))
		}
	})
}

func checkUnknownMethod(o *observation) mark {
	return withSession(o, func(s *sessionSeen) mark {
		switch r := s.unknownRPC; {
		case r.code == -32601:
			return pass("Answered JSON-RPC error -32601")
		case r.code != 0:
			return fail(fmt.Sprintf("Answered JSON-RPC error %d", r.code))
		case r.err != nil:
			return fail(errLine(r.err))
		default:
			return fail(fmt.Sprintf("Answered HTTP %d with no JSON-RPC error", r.status))
		}
	})
}

func checkOrigin(o *observation) mark {
	return withSession(o, func(s *sessionSeen) mark {
		h, c := s.origin, s.control
		switch {
		case h.err != nil:
			return fail(errLine(h.err))
		case h.status == 403:
			return pass("Answered HTTP 403")
		case h.status == 400 && c.status >= 200 && c.status < 300:
			return pass(fmt.Sprintf("Answered HTTP 400, and HTTP %d without the Origin", c.status))
		case h.status == c.status:
			return fail(fmt.Sprintf("Answered HTTP %d with or without Origin %s", h.status, probeOrigin))
		}
		return fail(fmt.Sprintf("Answered HTTP %d to Origin %s", h.status, probeOrigin))
	})
}

func checkWWWAuthenticate(o *observation) mark {
	return withAuth(o, func(a *authSeen) mark {
		if a.challengeErr != nil {
			return fail("WWW-Authenticate does not parse: " + errLine(a.challengeErr))
		}
		for _, c := range a.challenges {
			if c.Scheme != "bearer" {
				continue
			}
			if c.Params["resource_metadata"] == "" {
				return partial(0.5, "Bearer challenge without resource_metadata")
			}
			return pass("Bearer challenge names resource_metadata")
		}
		return fail("No Bearer challenge in WWW-Authenticate")
	})
}

func checkResourceMetadata(o *observation) mark {
	return withAuth(o, func(a *authSeen) mark {
		if a.prm == nil {
			why := "not found"
			if a.prmErr != nil {
				why = errLine(a.prmErr)
			}
			return fail(fmt.Sprintf("No metadata at %s: %s", a.prmURL, why))
		}
		resource := sameURL(a.prm.Resource, o.endpoint)
		servers := len(a.prm.AuthorizationServers) > 0
		switch {
		case resource && servers:
			return pass("Found at " + a.prmURL)
		case servers:
			return partial(0.5, fmt.Sprintf("resource is %q, not this URL", a.prm.Resource))
		case resource:
			return partial(0.5, "No authorization_servers")
		}
		return fail(fmt.Sprintf("resource is %q and no authorization_servers", a.prm.Resource))
	})
}

func checkASMetadata(o *observation) mark {
	return withAuth(o, func(a *authSeen) mark {
		switch {
		case a.as != nil:
			return pass("Found for " + a.asURL)
		case a.asURL == "":
			return fail("No authorization server named to look up")
		case a.asErr != nil:
			return fail(errLine(a.asErr))
		}
		return fail("No metadata for " + a.asURL)
	})
}

func checkPKCE(o *observation) mark {
	return withAuth(o, func(a *authSeen) mark {
		switch {
		case a.as == nil:
			return fail("No authorization server metadata")
		case slices.Contains(a.as.CodeChallengeMethodsSupported, "S256"):
			return pass("S256 supported")
		case len(a.as.CodeChallengeMethodsSupported) == 0:
			return fail("No code_challenge_methods_supported")
		}
		return fail("code_challenge_methods_supported is " + strings.Join(a.as.CodeChallengeMethodsSupported, ", "))
	})
}

func checkRegistration(o *observation) mark {
	return withAuth(o, func(a *authSeen) mark {
		switch {
		case a.as == nil:
			return fail("No authorization server metadata")
		case a.as.ClientIDMetadataDocumentSupported:
			return pass("Client ID Metadata Documents")
		case a.as.RegistrationEndpoint != "":
			return pass("Dynamic client registration")
		}
		return fail("Neither Client ID Metadata Documents nor a registration_endpoint")
	})
}

type info struct {
	Name, Title, Description, Version, WebsiteURL string
}

func infoCheck(field string, ok func(info) bool) func(o *observation) mark {
	return func(o *observation) mark {
		return withSession(o, func(s *sessionSeen) mark {
			var i info
			if si := s.init.ServerInfo; si != nil {
				i = info{si.Name, si.Title, si.Description, si.Version, si.WebsiteURL}
			}
			if ok(i) {
				return pass("serverInfo." + field + " is set")
			}
			return fail("No serverInfo." + field)
		})
	}
}

func isWebURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != ""
}

func checkIcon(o *observation) mark {
	return withSession(o, func(s *sessionSeen) mark {
		si := s.init.ServerInfo
		if si == nil || len(si.Icons) == 0 {
			return fail("No serverInfo.icons")
		}
		for _, ic := range si.Icons {
			if ic.MIMEType == "image/svg+xml" || strings.HasSuffix(strings.ToLower(ic.Source), ".svg") ||
				strings.HasPrefix(ic.Source, "data:image/svg+xml") {
				return pass("SVG icon")
			}
			for _, sz := range ic.Sizes {
				if sz == "any" || minSide(sz) >= 64 {
					return pass("Icon of " + sz)
				}
			}
		}
		return partial(0.5, "Icons are under 64x64 or declare no sizes")
	})
}

// minSide reads "48x48" as 48, the smaller side; 0 when it does not parse.
func minSide(size string) int {
	w, h, ok := strings.Cut(strings.ToLower(size), "x")
	if !ok {
		return 0
	}
	a, err1 := strconv.Atoi(w)
	b, err2 := strconv.Atoi(h)
	if err1 != nil || err2 != nil {
		return 0
	}
	return min(a, b)
}

func checkInstructions(o *observation) mark {
	return withSession(o, func(s *sessionSeen) mark {
		if strings.TrimSpace(s.init.Instructions) == "" {
			return fail("No instructions")
		}
		return pass(fmt.Sprintf("%d characters of instructions", len(s.init.Instructions)))
	})
}

func checkCapabilities(o *observation) mark {
	return withSession(o, func(s *sessionSeen) mark {
		c := s.init.Capabilities
		if c == nil {
			return fail("No capabilities")
		}
		var have []string
		if c.Tools != nil {
			have = append(have, "tools")
		}
		if c.Resources != nil {
			have = append(have, "resources")
		}
		if c.Prompts != nil {
			have = append(have, "prompts")
		}
		if len(have) == 0 {
			return fail("Declares none of tools, resources or prompts")
		}
		return pass("Declares " + strings.Join(have, ", "))
	})
}

func checkListed(o *observation) mark {
	return withSession(o, func(s *sessionSeen) mark {
		declared := s.init.Capabilities != nil && s.init.Capabilities.Tools != nil
		switch {
		case len(s.tools) > 0 && declared:
			return pass(fmt.Sprintf("%d tools", len(s.tools)))
		case len(s.tools) > 0:
			return partial(0.5, "Lists tools without declaring the tools capability")
		case s.toolsErr != nil:
			return fail("tools/list failed: " + errLine(s.toolsErr))
		}
		return fail("No tools listed")
	})
}

var toolName = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)

func checkNameChars(t *rawTool) mark {
	if toolName.MatchString(t.name) {
		return pass("")
	}
	return fail(fmt.Sprintf("Name %q", t.name))
}

func checkNameUnique(o *observation) mark {
	return withTools(o, func(ts []*rawTool) mark {
		seen := map[string]int{}
		var dups []string
		for _, t := range ts {
			if seen[t.name]++; seen[t.name] == 2 {
				dups = append(dups, t.name)
			}
		}
		if len(dups) == 0 {
			return pass(fmt.Sprintf("%d distinct names", len(ts)))
		}
		return fail("Listed more than once: " + strings.Join(dups, ", "))
	})
}

func checkName64(t *rawTool) mark {
	if len(t.name) <= 64 {
		return pass("")
	}
	return fail(fmt.Sprintf("Name is %d characters", len(t.name)))
}

func checkInputSchema(t *rawTool) mark {
	if len(t.input) == 0 || string(t.input) == "null" {
		return fail("No inputSchema")
	}
	var s struct {
		Type any `json:"type"`
	}
	if err := json.Unmarshal(t.input, &s); err != nil {
		return fail("inputSchema is not a JSON object")
	}
	if s.Type != "object" {
		return fail(fmt.Sprintf("inputSchema type is %v, not object", s.Type))
	}
	if err := headerAnnotations(t.input); err != nil {
		return fail(err.Error() + "; the Go SDK drops this tool")
	}
	return pass("")
}

// headerAnnotations mirrors the go-sdk's check of x-mcp-header annotations,
// which drops a tool from tools/list when it fails.
func headerAnnotations(schema json.RawMessage) error {
	type prop struct {
		Type       string          `json:"type"`
		XMCPHeader json.RawMessage `json:"x-mcp-header"`
		Properties map[string]json.RawMessage
	}
	seen := map[string]bool{}
	var walk func(props map[string]json.RawMessage, prefix string) error
	walk = func(props map[string]json.RawMessage, prefix string) error {
		for _, name := range slices.Sorted(maps.Keys(props)) {
			var p prop
			if json.Unmarshal(props[name], &p) != nil {
				continue
			}
			path := prefix + name
			if p.XMCPHeader != nil {
				if p.Type != "string" && p.Type != "integer" && p.Type != "boolean" {
					return fmt.Errorf("x-mcp-header on %s, a %q property", path, p.Type)
				}
				var h string
				if json.Unmarshal(p.XMCPHeader, &h) != nil || h == "" || strings.ContainsFunc(h, func(r rune) bool { return !isTChar(r) }) {
					return fmt.Errorf("x-mcp-header on %s is not a header name", path)
				}
				if seen[strings.ToLower(h)] {
					return fmt.Errorf("x-mcp-header %q is used twice", h)
				}
				seen[strings.ToLower(h)] = true
			}
			if err := walk(p.Properties, path+"."); err != nil {
				return err
			}
		}
		return nil
	}
	var root struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if json.Unmarshal(schema, &root) != nil {
		return nil
	}
	return walk(root.Properties, "")
}

func isTChar(r rune) bool {
	return r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("!#$%&'*+-.^_`|~", r))
}

func checkOutputSchema(t *rawTool) mark {
	if t.output == nil {
		return skip("No outputSchema")
	}
	var s struct {
		Type any `json:"type"`
	}
	if json.Unmarshal(t.output, &s) != nil || s.Type != "object" {
		return fail(fmt.Sprintf("outputSchema type is %v, not object", s.Type))
	}
	return pass("")
}

func checkHasDescription(t *rawTool) mark {
	if strings.TrimSpace(t.description) == "" {
		return fail("No description")
	}
	return pass("")
}

func (t *rawTool) displayTitle() string {
	if t.title != "" {
		return t.title
	}
	return t.annotTitle
}

func checkHasTitle(t *rawTool) mark {
	if strings.TrimSpace(t.displayTitle()) == "" {
		return fail("No title")
	}
	return pass("")
}

func missingHints(t *rawTool) []string {
	var missing []string
	for _, h := range hintNames {
		if _, ok := t.hints[h]; !ok {
			missing = append(missing, h)
		}
	}
	return missing
}

func checkAllHints(t *rawTool) mark {
	if m := missingHints(t); len(m) > 0 {
		return fail("No " + strings.Join(m, ", ") + " hint")
	}
	return pass("")
}

func checkDesc2KB(t *rawTool) mark {
	if len(t.description) <= 2048 {
		return pass("")
	}
	return fail(fmt.Sprintf("Description is %d bytes", len(t.description)))
}

func checkHiddenChars(t *rawTool) mark {
	where := map[string]string{"name": t.name, "title": t.title, "annotations.title": t.annotTitle, "description": t.description}
	for _, k := range slices.Sorted(maps.Keys(where)) {
		if r, ok := hiddenRune(where[k]); ok {
			return fail(fmt.Sprintf("U+%04X in %s", r, k))
		}
	}
	var schema any
	if json.Unmarshal(t.input, &schema) == nil {
		for s := range stringsIn(schema) {
			if r, ok := hiddenRune(s); ok {
				return fail(fmt.Sprintf("U+%04X in inputSchema", r))
			}
		}
	}
	return pass("")
}

// hiddenRune finds a character a person reading the text would not see:
// format characters (zero-width, bidirectional, tag) and control characters
// other than tab and newline.
func hiddenRune(s string) (rune, bool) {
	for _, r := range s {
		if unicode.Is(unicode.Cf, r) || (unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t') {
			return r, true
		}
	}
	return 0, false
}

// stringsIn yields every string in a decoded JSON value, keys included.
func stringsIn(v any) iter.Seq[string] {
	return func(yield func(string) bool) {
		var walk func(v any) bool
		walk = func(v any) bool {
			switch v := v.(type) {
			case string:
				return yield(v)
			case []any:
				for _, e := range v {
					if !walk(e) {
						return false
					}
				}
			case map[string]any:
				for k, e := range v {
					if !yield(k) || !walk(e) {
						return false
					}
				}
			}
			return true
		}
		walk(v)
	}
}

func checkToolBudget(o *observation) mark {
	return withTools(o, func(ts []*rawTool) mark {
		f := fmt.Sprintf("%d tools", len(ts))
		switch n := len(ts); {
		case n <= 40:
			return pass(f)
		case n <= 80:
			return partial(0.5, f)
		}
		return fail(f)
	})
}

func checkDeclaredLists(o *observation) mark {
	return withSession(o, func(s *sessionSeen) mark {
		if len(s.lists) == 0 {
			return skip("Declares no resources or prompts")
		}
		var broken []string
		for _, m := range slices.Sorted(maps.Keys(s.lists)) {
			if err := s.lists[m]; err != nil {
				broken = append(broken, m+": "+errLine(err))
			}
		}
		if len(broken) == 0 {
			return pass("Every declared list answers")
		}
		return partial(1-float64(len(broken))/float64(len(s.lists)), strings.Join(broken, "; "))
	})
}

// words is s lowercased with separators as spaces, to tell whether a title
// or description only restates the name.
func words(s string) string {
	return strings.Join(strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}), " ")
}

func docTitle(t *rawTool) mark {
	title := t.displayTitle()
	switch {
	case strings.TrimSpace(title) == "":
		return fail("No title")
	case words(title) == words(t.name):
		return fail("Title only repeats the name")
	}
	return pass("")
}

func docDescription(t *rawTool) mark {
	d := strings.TrimSpace(t.description)
	switch {
	case d == "":
		return fail("No description")
	case words(d) == words(t.name) || words(d) == words(t.displayTitle()):
		return fail("Description only repeats the name")
	case len(d) < 40:
		return partial(0.5, fmt.Sprintf("Description is %d characters", len(d)))
	}
	return pass("")
}

func docInputs(t *rawTool) mark {
	ps := params(t.input)
	if len(ps) == 0 {
		return pass("No inputs")
	}
	var short []string
	for _, p := range ps {
		if !p.typed || strings.TrimSpace(p.Description) == "" {
			short = append(short, p.Name)
		}
	}
	if len(short) == 0 {
		return pass("")
	}
	return partial(1-float64(len(short))/float64(len(ps)), "Undescribed or untyped: "+strings.Join(short, ", "))
}

func docHints(t *rawTool) mark {
	m := missingHints(t)
	if len(m) == 0 {
		return pass("")
	}
	return partial(1-float64(len(m))/float64(len(hintNames)), "No "+strings.Join(m, ", ")+" hint")
}

func docOutput(t *rawTool) mark {
	if t.output == nil {
		return fail("No outputSchema")
	}
	return pass("")
}

type param struct {
	Param
	typed bool // has a type, or a $ref, enum, const or combinator standing in for one
}

// params reads the top-level properties of an input schema, by name.
func params(schema json.RawMessage) []param {
	var s struct {
		Properties map[string]map[string]json.RawMessage `json:"properties"`
		Required   []string                              `json:"required"`
	}
	if json.Unmarshal(schema, &s) != nil {
		return nil
	}
	var ps []param
	for _, name := range slices.Sorted(maps.Keys(s.Properties)) {
		prop := s.Properties[name]
		p := param{Param: Param{Name: name, Required: slices.Contains(s.Required, name)}}
		_ = json.Unmarshal(prop["description"], &p.Description)
		var one string
		var many []string
		if json.Unmarshal(prop["type"], &one) == nil {
			p.Type = one
		} else if json.Unmarshal(prop["type"], &many) == nil {
			p.Type = strings.Join(many, "|")
		}
		for _, k := range []string{"type", "$ref", "enum", "const", "anyOf", "oneOf", "allOf"} {
			if _, ok := prop[k]; ok {
				p.typed = true
			}
		}
		ps = append(ps, p)
	}
	return ps
}

func checkManifest(o *observation) mark {
	switch m := o.manifest; {
	case m.at == "":
		return fail("No /.well-known/mcp.json or /mcp.json")
	case m.namesEndpoint:
		return pass(m.at + " names this endpoint")
	default:
		return partial(0.5, m.at+" does not name this endpoint")
	}
}

func checkSetupPage(o *observation) mark {
	h := o.html
	switch {
	case h.err != nil:
		return fail(errLine(h.err))
	case h.status == 200 && strings.HasPrefix(strings.ToLower(h.contentType), "text/html"):
		return pass("A browser gets a page")
	}
	return fail(fmt.Sprintf("A browser gets HTTP %d %s", h.status, h.contentType))
}

func checkRegistry(o *observation) mark {
	switch r := o.registry; {
	case r.err != nil:
		return skip("Registry unreachable: " + errLine(r.err))
	case r.listed:
		return pass("Listed with this URL as a remote")
	default:
		return fail("No registry entry lists this URL")
	}
}

// sameURL compares two URLs ignoring the case of scheme and host and a
// trailing slash.
func sameURL(a, b string) bool { return normURL(a) == normURL(b) }

func normURL(s string) string {
	u, err := url.Parse(strings.TrimSpace(s))
	if err != nil {
		return s
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	u.Fragment = ""
	u.Path = strings.TrimSuffix(u.Path, "/")
	u.RawPath = ""
	return u.String()
}
