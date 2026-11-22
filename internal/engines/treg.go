package engines

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// TregURL is Treg's public base URL.
const TregURL = "https://treg.to"

// maxBody bounds what one answer may occupy in memory. Treg's own replay
// limit is 2 MB, so this is well above any answer.
const maxBody = 32 << 20

// Treg calls a surface's pinned endpoint through Treg with one team token.
// The request and response bodies are the provider's own; Treg adds only
// headers.
type Treg struct {
	Client *http.Client
	// BaseURL defaults to TregURL.
	BaseURL string
	Token   string
	// Org is sent as X-Treg-Org, which only a personal token needs.
	Org string
	// Resolve follows redirect links whose target is not in the URL. Nil
	// keeps those links as they came.
	Resolve Resolver
}

// Refusal is Treg declining a call on its own behalf (X-Treg-Error: 1), such
// as 402 insufficient_balance or 429 org_rate_limit. Nothing reached the
// provider and nothing was charged, so it is never an engine failure.
type Refusal struct {
	StatusCode int
	Code       string
	Message    string
	// RetryAfter is how long Treg asked the caller to wait, or 0.
	RetryAfter time.Duration
}

func (r *Refusal) Error() string {
	return fmt.Sprintf("treg refused the call: %d %s: %s", r.StatusCode, r.Code, r.Message)
}

// Call asks req.Question on req.Surface. An error means there is no answer
// to store: the request was invalid, the connection failed (retry with the
// same IdempotencyKey), or Treg refused it (*Refusal). A provider error is a
// Result with StatusFailed, so it counts against the endpoint's success rate.
func (t Treg) Call(ctx context.Context, req Request) (Result, error) {
	spec, ok := Lookup(req.Surface)
	if !ok {
		return Result{}, fmt.Errorf("engines: unknown surface %q", req.Surface)
	}
	e := spec.Endpoint
	if err := validate(e, req); err != nil {
		return Result{}, err
	}
	pr, err := e.build(e, req)
	if err != nil {
		return Result{}, err
	}
	resp, body, err := t.do(ctx, e.Method, e.ID, pr, req.IdempotencyKey)
	if err != nil {
		return Result{}, err
	}
	var res Result
	if resp.StatusCode/100 == 2 {
		res = e.Decode(body)
	} else {
		res = failed("provider HTTP %d", resp.StatusCode)
	}
	res.Raw = body
	res.CostUSD = chargedUSD(resp.Header)
	res.CallID = resp.Header.Get("X-Treg-Call-Id")
	if t.Resolve != nil {
		res.Citations = resolveLinks(ctx, res.Citations, t.Resolve)
	}
	return res, nil
}

// ListModels reads a surface's live model list from its free Treg tool.
func (t Treg) ListModels(ctx context.Context, s Surface) ([]Model, error) {
	spec, ok := Lookup(s)
	if !ok {
		return nil, fmt.Errorf("engines: unknown surface %q", s)
	}
	if !spec.Endpoint.TakesModel() {
		return nil, fmt.Errorf("engines: surface %s takes no model", s)
	}
	resp, body, err := t.do(ctx, http.MethodGet, spec.Endpoint.Models, providerRequest{}, "")
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("engines: model list %s: provider HTTP %d", spec.Endpoint.Models, resp.StatusCode)
	}
	return parseDataForSEOModels(body)
}

func validate(e Endpoint, r Request) error {
	switch {
	case strings.TrimSpace(r.Question) == "":
		return errors.New("engines: empty question")
	case e.TakesModel() && r.Model == "":
		return fmt.Errorf("engines: %s needs a model", e.ID)
	case !e.TakesModel() && r.Model != "":
		return fmt.Errorf("engines: %s takes no model, the app decides", e.ID)
	case len(r.Location) != 2:
		return fmt.Errorf("engines: location %q is not an ISO 3166-1 alpha-2 code", r.Location)
	case r.Language == "":
		return errors.New("engines: empty language")
	}
	return nil
}

// ToolCall is one call to a Treg tool outside the surface registry, such
// as a social network's post search. Query and Body are the provider's
// own query string and JSON body.
type ToolCall struct {
	Method string
	Tool   string
	Query  url.Values
	Body   any
	// MaxCostUSD, when above 0, is sent as X-Treg-Route-Max-Cost, so Treg
	// refuses any source dearer than it instead of failing over to one.
	MaxCostUSD float64
}

// ToolResponse is the provider's answer to a ToolCall.
type ToolResponse struct {
	StatusCode int
	Body       []byte
	// CostUSD is nil when the call reported no cost, never 0 for unknown.
	CostUSD *float64
	CallID  string
}

// CallTool sends c through Treg. An error means there is no answer: the
// connection failed or Treg refused the call (*Refusal). A provider error
// is a ToolResponse with its status code.
func (t Treg) CallTool(ctx context.Context, c ToolCall) (ToolResponse, error) {
	req, err := t.newRequest(ctx, c.Method, c.Tool, providerRequest{Query: c.Query, Body: c.Body}, "")
	if err != nil {
		return ToolResponse{}, err
	}
	if c.MaxCostUSD > 0 {
		req.Header.Set("X-Treg-Route-Max-Cost", strconv.FormatFloat(c.MaxCostUSD, 'f', -1, 64))
	}
	resp, body, err := t.send(req, c.Tool)
	if err != nil {
		return ToolResponse{}, err
	}
	return ToolResponse{StatusCode: resp.StatusCode, Body: body, CostUSD: chargedUSD(resp.Header), CallID: resp.Header.Get("X-Treg-Call-Id")}, nil
}

// do sends one call and returns the provider's response, or the Refusal
// when Treg declined it.
func (t Treg) do(ctx context.Context, method, tool string, pr providerRequest, idempotencyKey string) (*http.Response, []byte, error) {
	httpReq, err := t.newRequest(ctx, method, tool, pr, idempotencyKey)
	if err != nil {
		return nil, nil, err
	}
	return t.send(httpReq, tool)
}

func (t Treg) send(httpReq *http.Request, tool string) (*http.Response, []byte, error) {
	client := t.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, nil, fmt.Errorf("engines: treg %s: %w", tool, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, nil, fmt.Errorf("engines: treg %s: reading response: %w", tool, err)
	}
	if refusal := tregRefusal(resp, body); refusal != nil {
		return nil, nil, refusal
	}
	return resp, body, nil
}

// newRequest is Treg's request contract: the provider's method and body
// sent to /call/<tool id>, authenticated with X-Treg-Token.
func (t Treg) newRequest(ctx context.Context, method, tool string, pr providerRequest, idempotencyKey string) (*http.Request, error) {
	base := t.BaseURL
	if base == "" {
		base = TregURL
	}
	target := strings.TrimRight(base, "/") + "/call/" + tool
	if len(pr.Query) > 0 {
		target += "?" + pr.Query.Encode()
	}
	var body io.Reader
	if pr.Body != nil {
		b, err := json.Marshal(pr.Body)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(b)
	}
	httpReq, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("X-Treg-Token", t.Token)
	if t.Org != "" {
		httpReq.Header.Set("X-Treg-Org", t.Org)
	}
	if pr.Body != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	if idempotencyKey != "" {
		httpReq.Header.Set("Idempotency-Key", idempotencyKey)
	}
	return httpReq, nil
}

// tregRefusal is Treg's refusal contract: its own refusals carry
// X-Treg-Error: 1 and a JSON body naming the error code. Without that
// header the status and body are the provider's.
func tregRefusal(resp *http.Response, body []byte) *Refusal {
	if resp.Header.Get("X-Treg-Error") != "1" {
		return nil
	}
	var b struct {
		Error       string  `json:"error"`
		Message     string  `json:"message"`
		RetryAfterS float64 `json:"retry_after_s"`
	}
	_ = json.Unmarshal(body, &b)
	r := &Refusal{StatusCode: resp.StatusCode, Code: b.Error, Message: b.Message}
	if b.RetryAfterS > 0 {
		r.RetryAfter = time.Duration(b.RetryAfterS * float64(time.Second))
	} else if s, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil {
		r.RetryAfter = time.Duration(s) * time.Second
	}
	return r
}

// chargedUSD reads what the call cost from X-Treg-Cost-Micro. No header
// means the call was not metered, which Byname records as a missing cost.
// An idempotent replay reports 0 and moves the first call's charge to
// X-Treg-Original-Cost-Micro; Byname only replays when that first answer was
// lost, so the original charge is the one to record.
func chargedUSD(h http.Header) *float64 {
	header := "X-Treg-Cost-Micro"
	if h.Get("X-Treg-Idempotent-Replay") == "true" {
		header = "X-Treg-Original-Cost-Micro"
	}
	micro, err := strconv.ParseInt(h.Get(header), 10, 64)
	if err != nil {
		return nil
	}
	usd := float64(micro) / 1e6
	return &usd
}

// Resolver returns where a redirect link leads.
type Resolver func(ctx context.Context, link string) (string, error)

// HeadResolver resolves a redirect link with one HEAD request, reading the
// Location header instead of following it.
func HeadResolver(client *http.Client) Resolver {
	c := *client
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return func(ctx context.Context, link string) (string, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodHead, link, nil)
		if err != nil {
			return "", err
		}
		resp, err := c.Do(req)
		if err != nil {
			return "", err
		}
		resp.Body.Close()
		loc, err := resp.Location()
		if err != nil {
			return "", fmt.Errorf("engines: %s did not redirect (HTTP %d)", link, resp.StatusCode)
		}
		return loc.String(), nil
	}
}

// resolveLinks replaces each link that needs a lookup with its target. A
// link that fails to resolve is kept, so a citation is never lost.
func resolveLinks(ctx context.Context, links []string, resolve Resolver) []string {
	out := make([]string, len(links))
	for i, l := range links {
		out[i] = l
		if needsLookup(l) {
			if target, err := resolve(ctx, l); err == nil && isWebURL(target) {
				out[i] = target
			}
		}
	}
	return citations(out)
}
