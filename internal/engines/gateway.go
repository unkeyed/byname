package engines

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
)

// gateway is an OpenAI-compatible gateway's chat completions at
// Conn.BaseURL, such as https://openrouter.ai/api/v1. The request carries
// nothing provider-specific, so whether the model searches is the model's
// call, and the test call shows it.
var gateway = providerAPI{
	id: "gateway.chat-completions", family: "gateway",
	auth:  bearer,
	build: gatewayRequest, parse: parseChatCompletion("gateway"), cost: gatewayCost,
	models: modelList{id: "gateway.models", path: "/models", parse: parseModelIDs},
}

func newGateway(rs RouteSpec, c Conn) Adapter { return apiAdapter{rs: rs, conn: c, api: gateway} }

func probeGateway(ctx context.Context, c Conn) error {
	_, err := apiAdapter{conn: c, api: gateway}.listModels(ctx)
	return err
}

type chatBody struct {
	Model            string          `json:"model"`
	Messages         []chatMessage   `json:"messages"`
	WebSearchOptions *chatWebOptions `json:"web_search_options,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatWebOptions struct {
	UserLocation struct {
		Country string `json:"country"`
	} `json:"user_location"`
}

func gatewayRequest(_ RouteSpec, r Request) (apiRequest, error) {
	return apiRequest{Method: http.MethodPost, Path: "/chat/completions", Body: chatBody{
		Model:    r.Model,
		Messages: []chatMessage{{Role: "user", Content: r.Question}},
	}}, nil
}

// thinkBlock is the reasoning some models (Perplexity's sonar-reasoning
// models among them) put ahead of the answer in the message content.
var thinkBlock = regexp.MustCompile(`(?s)^\s*<think>.*?</think>`)

// parseChatCompletion reads a chat completion. Citations come from the
// message's url_citation annotations (OpenRouter and OpenAI's shape), then
// Perplexity's search_results and the bare citations list some gateways
// pass through.
func parseChatCompletion(name string) func(raw []byte) Result {
	return func(raw []byte) Result {
		var resp struct {
			Model   string    `json:"model"`
			Error   *apiError `json:"error"`
			Choices []struct {
				Message struct {
					Content     string `json:"content"`
					Annotations []struct {
						Type        string `json:"type"`
						URLCitation struct {
							URL string `json:"url"`
						} `json:"url_citation"`
					} `json:"annotations"`
				} `json:"message"`
			} `json:"choices"`
			SearchResults []struct {
				URL string `json:"url"`
			} `json:"search_results"`
			Citations []string `json:"citations"`
		}
		if err := json.Unmarshal(raw, &resp); err != nil {
			return failed("%s: %v", name, err)
		}
		if resp.Error != nil {
			return failed("%s: %s", name, resp.Error)
		}
		if len(resp.Choices) == 0 {
			return failed("%s: response has no choices", name)
		}
		msg := resp.Choices[0].Message
		var urls []string
		for _, a := range msg.Annotations {
			if a.Type == "url_citation" {
				urls = append(urls, a.URLCitation.URL)
			}
		}
		for _, s := range resp.SearchResults {
			urls = append(urls, s.URL)
		}
		urls = append(urls, resp.Citations...)
		return answered(Result{
			Text:          strings.TrimSpace(thinkBlock.ReplaceAllString(msg.Content, "")),
			ReportedModel: resp.Model,
			Citations:     citations(urls),
		})
	}
}

// gatewayCost reads usage.cost, the dollars a gateway such as OpenRouter
// reports per call. A gateway that reports none leaves the cost missing.
func gatewayCost(raw []byte) *float64 {
	var b struct {
		Usage struct {
			Cost *float64 `json:"cost"`
		} `json:"usage"`
	}
	if json.Unmarshal(raw, &b) != nil {
		return nil
	}
	return b.Usage.Cost
}
