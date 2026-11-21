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
	"slices"
	"strings"
)

// providerAPI is one provider's own HTTP API, as the scraper, provider and
// gateway routes call it: where it lives, how a key authenticates, how a
// question becomes a request, and how the answer, its cost and the model
// list are read.
type providerAPI struct {
	// id and family name the API's fixtures, testdata/<family>/<id>.*.json.
	// For a provider route, id is also the endpoint stored on each answer.
	id, family string
	// base is the public base URL. A non-empty Conn.BaseURL replaces it.
	base  string
	auth  func(r *http.Request, secret string) error
	build func(RouteSpec, Request) (apiRequest, error)
	parse func(raw []byte) Result
	// cost reads what the call cost from a 2xx body. Nil for token-billed
	// APIs, whose responses carry no dollar figure.
	cost   func(raw []byte) *float64
	models modelList
}

// modelList is how an API lists the models it accepts.
type modelList struct {
	// id names the list's fixtures, as providerAPI.id does.
	id    string
	path  string
	query url.Values
	parse func(raw []byte) ([]Model, error)
	// static is the documented list of an API with no list endpoint.
	static []Model
}

type apiRequest struct {
	Method string
	Path   string
	Query  url.Values
	Body   any
}

// apiAdapter makes a route's calls to a provider's own API.
type apiAdapter struct {
	rs   RouteSpec
	conn Conn
	api  providerAPI
}

// Call follows Treg's contract: an error means there is no answer to store
// (an invalid request, a key the provider's auth cannot use, or a failed
// connection), and a provider error is a Result with StatusFailed.
func (a apiAdapter) Call(ctx context.Context, req Request) (Result, error) {
	if err := validateRoute(a.rs, req); err != nil {
		return Result{}, err
	}
	ar, err := a.api.build(a.rs, req)
	if err != nil {
		return Result{}, err
	}
	status, body, err := a.send(ctx, ar)
	if err != nil {
		return Result{}, err
	}
	res := a.api.parse(body)
	if status/100 == 2 {
		if a.api.cost != nil {
			res.CostUSD = a.api.cost(body)
		}
	} else {
		res = Result{Status: StatusFailed, Error: httpFailure(status, res)}
	}
	res.Raw = body
	if a.conn.Resolve != nil {
		res.Citations = resolveLinks(ctx, res.Citations, a.conn.Resolve)
	}
	return res, nil
}

// ListModels reads the models the route accepts for the bound surface.
func (a apiAdapter) ListModels(ctx context.Context, s Surface) ([]Model, error) {
	if s != a.rs.Spec.Surface {
		return nil, fmt.Errorf("engines: this adapter serves %s, not %q", a.rs.Spec.Surface, s)
	}
	if !a.rs.TakesModel() {
		return nil, fmt.Errorf("engines: surface %s takes no model on the %s route", s, a.rs.Route)
	}
	return a.listModels(ctx)
}

func (a apiAdapter) listModels(ctx context.Context) ([]Model, error) {
	m := a.api.models
	if m.static != nil {
		return slices.Clone(m.static), nil
	}
	if m.path == "" {
		return nil, fmt.Errorf("engines: %s has no model list", a.api.id)
	}
	status, body, err := a.send(ctx, apiRequest{Method: http.MethodGet, Path: m.path, Query: m.query})
	if err != nil {
		return nil, err
	}
	if status/100 != 2 {
		return nil, fmt.Errorf("engines: model list %s: %s", m.id, httpFailure(status, a.api.parse(body)))
	}
	return m.parse(body)
}

// httpFailure says why a non-2xx call failed, with the provider's own
// message when its error body parses to one.
func httpFailure(status int, parsed Result) string {
	if parsed.Status == StatusFailed && parsed.Error != "" {
		return fmt.Sprintf("provider HTTP %d: %s", status, parsed.Error)
	}
	return fmt.Sprintf("provider HTTP %d", status)
}

func (a apiAdapter) send(ctx context.Context, ar apiRequest) (int, []byte, error) {
	base := a.conn.BaseURL
	if base == "" {
		base = a.api.base
	}
	if base == "" {
		return 0, nil, fmt.Errorf("engines: %s needs a base URL", a.api.id)
	}
	target := strings.TrimRight(base, "/") + ar.Path
	if len(ar.Query) > 0 {
		target += "?" + ar.Query.Encode()
	}
	var body io.Reader
	if ar.Body != nil {
		b, err := json.Marshal(ar.Body)
		if err != nil {
			return 0, nil, err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, ar.Method, target, body)
	if err != nil {
		return 0, nil, fmt.Errorf("engines: %s: %w", a.api.id, err)
	}
	if ar.Body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if err := a.api.auth(req, a.conn.Secret); err != nil {
		return 0, nil, err
	}
	client := a.conn.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		// A *url.Error quotes the URL, and SerpApi's carries the key.
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return 0, nil, fmt.Errorf("engines: %s %s: %w", a.api.id, ar.Path, err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return 0, nil, fmt.Errorf("engines: %s %s: reading response: %w", a.api.id, ar.Path, err)
	}
	return resp.StatusCode, b, nil
}

// validateRoute checks a request against the route it is sent on. A model
// API always needs a model, even standing in for an app, and only a route
// whose search is forceable takes force_web_search.
func validateRoute(rs RouteSpec, r Request) error {
	switch {
	case r.Surface != rs.Spec.Surface:
		return fmt.Errorf("engines: this adapter serves %s, not %q", rs.Spec.Surface, r.Surface)
	case strings.TrimSpace(r.Question) == "":
		return errors.New("engines: empty question")
	case rs.TakesModel() && r.Model == "":
		return fmt.Errorf("engines: %s on the %s route needs a model", rs.Spec.Title, rs.Route)
	case !rs.TakesModel() && r.Model != "":
		return fmt.Errorf("engines: %s takes no model on the %s route, the app decides", rs.Spec.Title, rs.Route)
	case r.ForceWebSearch && rs.Search() != SearchForceable:
		return fmt.Errorf("engines: %s on the %s route cannot force web search", rs.Spec.Title, rs.Route)
	case len(r.Location) != 2:
		return fmt.Errorf("engines: location %q is not an ISO 3166-1 alpha-2 code", r.Location)
	case r.Language == "":
		return errors.New("engines: empty language")
	}
	return nil
}

func bearer(r *http.Request, secret string) error {
	r.Header.Set("Authorization", "Bearer "+secret)
	return nil
}

// parseModelIDs reads an OpenAI-style list, {"data": [{"id": ...}]}. The
// list does not say which models search, so WebSearch stays false.
func parseModelIDs(raw []byte) ([]Model, error) {
	var resp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("model list: %w", err)
	}
	models := make([]Model, 0, len(resp.Data))
	for _, m := range resp.Data {
		models = append(models, Model{Name: m.ID})
	}
	return models, nil
}

// apiError is the {"error": {"type", "code", "message"}} body OpenAI,
// Anthropic, Perplexity and most gateways send.
type apiError struct {
	Type    string          `json:"type"`
	Code    json.RawMessage `json:"code"`
	Message string          `json:"message"`
}

func (e apiError) String() string {
	kind := e.Type
	if c := strings.Trim(string(e.Code), `"`); c != "" && c != "null" {
		kind = c
	}
	if kind == "" {
		return e.Message
	}
	return kind + ": " + e.Message
}
