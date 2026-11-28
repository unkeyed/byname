// Package mcpgrade grades an entity's own MCP server against the MCP spec
// essentials: a deterministic 0 to 100 score, a letter, category scores and
// one result per check, from one probe of the server's metadata. It never
// calls a tool.
//
// Grade is the whole surface. probe does every request and fills an
// observation; score turns that observation into a Report and does no I/O.
// The checks table in checks.go is the catalog: adding a check is one entry
// there.
package mcpgrade

import (
	"net/http"
	"time"
)

// Target is one server to grade.
type Target struct {
	// URL is the Streamable HTTP endpoint.
	URL string
	// Token, when set, is sent as "Authorization: Bearer" on requests to URL
	// itself and nowhere else: not to well-known documents, authorization
	// servers or the registry. Authorization is always probed without it.
	Token string
	// Client makes every request; production passes
	// sources.PublicClient(allowPrivate). Grade adds the deadline, body caps
	// and request budget itself.
	Client *http.Client
	// RegistryURL replaces https://registry.modelcontextprotocol.io, for
	// tests.
	RegistryURL string
}

const (
	deadline = 30 * time.Second
	// rules names this catalog. It changes whenever a check, weight or
	// threshold does, so a stored grade can say it used older rules.
	rules           = "2026-11"
	defaultRegistry = "https://registry.modelcontextprotocol.io"
)

// Report is one grade.
type Report struct {
	Rules      string     `json:"rules" jsonschema:"the version of the check catalog this grade used"`
	Score      int        `json:"score" jsonschema:"0 to 100, the equal-weight mean of the categories that count"`
	Grade      Letter     `json:"grade"`
	Server     ServerInfo `json:"server"`
	Categories []Category `json:"categories"`
	Tools      []Tool     `json:"tools" jsonschema:"each tool as listed, with its documentation score by the rules"`
}

// Letter is A+, A, B, C, D or F.
type Letter string

func letter(score int) Letter {
	switch {
	case score >= 95:
		return "A+"
	case score >= 85:
		return "A"
	case score >= 70:
		return "B"
	case score >= 55:
		return "C"
	case score >= 40:
		return "D"
	}
	return "F"
}

// ServerInfo is what the server says about itself, for the report header.
type ServerInfo struct {
	Name            string `json:"name"`
	Title           string `json:"title"`
	Version         string `json:"version"`
	ProtocolVersion string `json:"protocol_version" jsonschema:"empty when no session was made"`
	Transport       string `json:"transport" jsonschema:"streamable_http, sse or none"`
	NegotiateMS     *int   `json:"negotiate_ms"`
	TokenUsed       bool   `json:"token_used"`
}

// Category is one equal-weight part of the score.
type Category struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// Score is null when the category does not count: an optional one with
	// nothing to assess. A required one that cannot be assessed counts as 0
	// with Note saying why, such as "Needs a token to grade".
	Score  *int    `json:"score"`
	Note   string  `json:"note"`
	Checks []Check `json:"checks"`
}

// Status is a check's outcome.
type Status string

const (
	Pass    Status = "pass"
	Partial Status = "partial"
	Fail    Status = "fail"
	Skipped Status = "skipped" // not applicable; out of the denominator
)

// Check is one check's result in a report.
type Check struct {
	ID       string  `json:"id"`
	Title    string  `json:"title"`
	Weight   int     `json:"weight"`
	Status   Status  `json:"status"`
	Fraction float64 `json:"fraction" jsonschema:"share of the weight earned, 0 to 1; 0 when skipped"`
	Finding  string  `json:"finding" jsonschema:"one line: what was seen"`
	Fix      string  `json:"fix" jsonschema:"one line, empty on pass"`
}

// Tool is one listed tool as the grade saw it on the wire, including tools
// a client SDK would drop.
type Tool struct {
	Name        string     `json:"name"`
	Title       string     `json:"title"`
	Description string     `json:"description" jsonschema:"as listed, cut at 8 KB"`
	Params      []Param    `json:"params"`
	Hints       []string   `json:"hints" jsonschema:"the annotation hints it declares true, such as read_only"`
	HasOutput   bool       `json:"has_output_schema"`
	Score       int        `json:"score" jsonschema:"documentation score by the rules, 0 to 100"`
	Marks       []ToolMark `json:"marks"`
	// Key is a hash of the definition above, so a later review can skip a
	// tool that did not change.
	Key string `json:"key"`
}

// Param is one top-level input property.
type Param struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Required    bool   `json:"required"`
	Description string `json:"description"`
}

// ToolMark is one per-tool check's outcome for this tool.
type ToolMark struct {
	Check   string `json:"check"`
	Status  Status `json:"status"`
	Finding string `json:"finding"`
}
