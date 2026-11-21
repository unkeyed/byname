package engines

import (
	"encoding/json"
	"net/http"
)

// perplexity is Sonar's chat completions. Sonar searches on every call and
// has no model list endpoint, so the list is the documented sonar models.
var perplexity = providerAPI{
	id: "perplexity.chat-completions", family: "perplexity",
	base: "https://api.perplexity.ai", auth: bearer,
	build: perplexityRequest, parse: parseChatCompletion("perplexity"), cost: perplexityCost,
	models: modelList{static: []Model{
		{Name: "sonar", WebSearch: true},
		{Name: "sonar-pro", WebSearch: true},
		{Name: "sonar-reasoning-pro", WebSearch: true},
		{Name: "sonar-deep-research", WebSearch: true},
	}},
}

func newPerplexity(rs RouteSpec, c Conn) Adapter { return apiAdapter{rs: rs, conn: c, api: perplexity} }

func perplexityRequest(_ RouteSpec, r Request) (apiRequest, error) {
	body := chatBody{
		Model:            r.Model,
		Messages:         []chatMessage{{Role: "user", Content: r.Question}},
		WebSearchOptions: &chatWebOptions{},
	}
	body.WebSearchOptions.UserLocation.Country = r.Location
	return apiRequest{Method: http.MethodPost, Path: "/chat/completions", Body: body}, nil
}

// perplexityCost reads usage.cost.total_cost, the request fee and tokens
// together in dollars.
func perplexityCost(raw []byte) *float64 {
	var b struct {
		Usage struct {
			Cost struct {
				TotalCost *float64 `json:"total_cost"`
			} `json:"cost"`
		} `json:"usage"`
	}
	if json.Unmarshal(raw, &b) != nil {
		return nil
	}
	return b.Usage.Cost.TotalCost
}
