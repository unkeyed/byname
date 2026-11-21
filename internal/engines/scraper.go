package engines

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// scraperFamily is how a scraping provider's own API is reached with the
// team's key. The request and response bodies are the ones Treg passes
// through, so each pinned endpoint keeps its builder and parser and adds
// only its native path (Endpoint.Native).
type scraperFamily struct {
	base string
	auth func(r *http.Request, secret string) error
	cost func(raw []byte) *float64
}

var scraperFamilies = map[string]scraperFamily{
	// HTTP basic auth: https://docs.dataforseo.com/v3/ai_optimization/claude/llm_responses/live/
	"dataforseo": {base: "https://api.dataforseo.com", auth: dataforseoAuth, cost: dataforseoCost},
	// https://cloro.dev/docs/api-reference/introduction. cloro reports
	// credits in X-Credits-Charged, not dollars, so its cost stays missing.
	"cloro":   {base: "https://api.cloro.dev", auth: bearer},
	"serpapi": {base: "https://serpapi.com", auth: serpapiAuth},
}

func scraperAPI(e Endpoint) providerAPI {
	f := scraperFamilies[e.Family]
	api := providerAPI{
		id: e.ID, family: e.Family, base: f.base, auth: f.auth, cost: f.cost,
		build: func(_ RouteSpec, r Request) (apiRequest, error) {
			pr, err := e.build(e, r)
			return apiRequest{Method: e.Method, Path: e.Native, Query: pr.Query, Body: pr.Body}, err
		},
		parse: e.parse,
	}
	if e.NativeModels != "" {
		api.models = modelList{id: e.Models, path: e.NativeModels, parse: parseDataForSEOModels}
	}
	return api
}

func newScraper(rs RouteSpec, c Conn) Adapter {
	return apiAdapter{rs: rs, conn: c, api: scraperAPI(rs.Spec.Endpoint)}
}

// dataforseoAuth sends the API login and password as HTTP basic auth. The
// credential holds both as one secret, login:password.
func dataforseoAuth(r *http.Request, secret string) error {
	login, password, ok := strings.Cut(secret, ":")
	if !ok || login == "" || password == "" {
		return errors.New("engines: a DataForSEO key is the API login and password as login:password (app.dataforseo.com/api-access)")
	}
	r.SetBasicAuth(login, password)
	return nil
}

// dataforseoCost reads the response's top-level cost, the dollars charged
// for every task in it. A failed task costs 0, which DataForSEO reports.
func dataforseoCost(raw []byte) *float64 {
	var b struct {
		Cost *float64 `json:"cost"`
	}
	if json.Unmarshal(raw, &b) != nil {
		return nil
	}
	return b.Cost
}

func serpapiAuth(r *http.Request, secret string) error {
	q := r.URL.Query()
	q.Set("api_key", secret)
	r.URL.RawQuery = q.Encode()
	return nil
}

// probeDataForSEO reads the Claude model list, which DataForSEO does not
// charge for.
func probeDataForSEO(ctx context.Context, c Conn) error {
	_, err := apiAdapter{conn: c, api: scraperAPI(claudeAPI)}.listModels(ctx)
	return err
}

// probeSerpApi reads the Account API, which SerpApi does not count against
// the plan's searches (https://serpapi.com/account-api).
func probeSerpApi(ctx context.Context, c Conn) error {
	a := apiAdapter{conn: c, api: providerAPI{id: "serpapi.account", base: scraperFamilies["serpapi"].base, auth: serpapiAuth}}
	status, body, err := a.send(ctx, apiRequest{Method: http.MethodGet, Path: "/account.json"})
	if err != nil {
		return err
	}
	var b struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(body, &b)
	switch {
	case b.Error != "":
		return fmt.Errorf("engines: serpapi account: HTTP %d: %s", status, b.Error)
	case status/100 != 2:
		return fmt.Errorf("engines: serpapi account: HTTP %d", status)
	}
	return nil
}
