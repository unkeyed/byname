// Package mcpgrade grades an entity's own MCP server against the MCP spec
// essentials: a deterministic 0 to 100 score, a letter, category scores and
// one result per check, from one probe of the server's metadata. It never
// calls a tool.
package mcpgrade

import (
	"context"
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
	// Client makes every request. Grade adds the deadline, body caps and
	// request budget itself.
	Client *http.Client
	// RegistryURL replaces https://registry.modelcontextprotocol.io, for
	// tests.
	RegistryURL string
}

const (
	deadline        = 30 * time.Second
	rules           = "2026-11"
	defaultRegistry = "https://registry.modelcontextprotocol.io"
)

// Grade probes the server once and scores it. A server that is down,
// refuses us or speaks nonsense is not an error: it is a low grade whose
// checks say why. The error is only ctx's, so a shutdown never yields a
// half-probed report.
func Grade(ctx context.Context, t Target) (Report, error) {
	pctx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()
	o := probe(pctx, t)
	if err := ctx.Err(); err != nil {
		return Report{}, err
	}
	return score(o), nil
}

// Report is one grade.
type Report struct {
	Rules      string        `json:"rules" jsonschema:"the version of the check catalog this grade used"`
	Score      int           `json:"score" jsonschema:"0 to 100, the equal-weight mean of the categories that count"`
	Grade      Letter        `json:"grade"`
	Partial    PartialReason `json:"partial" jsonschema:"empty when complete; else why a required category counted as 0: needs_token, no_session or no_tools"`
	Server     ServerInfo    `json:"server"`
	Categories []Category    `json:"categories"`
	Tools      []Tool        `json:"tools" jsonschema:"each tool as listed, with its documentation score by the rules"`
}

// PartialReason says why a required category could not be assessed and
// counted as 0. Complete is the empty reason.
type PartialReason string

const (
	Complete   PartialReason = ""
	NeedsToken PartialReason = "needs_token"
	NoSession  PartialReason = "no_session"
	NoTools    PartialReason = "no_tools"
)

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
	// Review is joined in by Key when a grade is read, so a review shows
	// on every grade where the tool is unchanged. A stored report has none.
	Review *ToolReview `json:"review" jsonschema:"the model's review of this definition; null before one"`
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
