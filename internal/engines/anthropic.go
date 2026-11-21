package engines

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
)

// anthropic is the Messages API with the web search server tool.
var anthropic = providerAPI{
	id: "anthropic.messages", family: "anthropic",
	base: "https://api.anthropic.com", auth: anthropicAuth,
	build: anthropicRequest, parse: parseAnthropic,
	models: modelList{id: "anthropic.models", path: "/v1/models", query: url.Values{"limit": {"1000"}}, parse: parseModelIDs},
}

func newAnthropic(rs RouteSpec, c Conn) Adapter { return apiAdapter{rs: rs, conn: c, api: anthropic} }

func probeAnthropic(ctx context.Context, c Conn) error {
	_, err := apiAdapter{conn: c, api: anthropic}.listModels(ctx)
	return err
}

func anthropicAuth(r *http.Request, secret string) error {
	r.Header.Set("x-api-key", secret)
	r.Header.Set("anthropic-version", "2023-06-01")
	return nil
}

// anthropicMaxTokens leaves room for thinking, which current models run by
// default and which counts against max_tokens, ahead of the answer.
const anthropicMaxTokens = 16000

type anthropicBody struct {
	Model      string               `json:"model"`
	MaxTokens  int                  `json:"max_tokens"`
	Messages   []anthropicMessage   `json:"messages"`
	Tools      []anthropicTool      `json:"tools"`
	ToolChoice *anthropicToolChoice `json:"tool_choice,omitempty"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// anthropicTool is web_search_20250305, the version every Claude model
// accepts; the newer dynamic-filtering version needs a 4.6 or later model.
type anthropicTool struct {
	Type         string             `json:"type"`
	Name         string             `json:"name"`
	UserLocation *anthropicLocation `json:"user_location,omitempty"`
}

type anthropicLocation struct {
	Type    string `json:"type"`
	Country string `json:"country"`
}

type anthropicToolChoice struct {
	Type string `json:"type"`
	Name string `json:"name"`
}

func anthropicRequest(_ RouteSpec, r Request) (apiRequest, error) {
	body := anthropicBody{
		Model:     r.Model,
		MaxTokens: anthropicMaxTokens,
		Messages:  []anthropicMessage{{Role: "user", Content: r.Question}},
		Tools: []anthropicTool{{
			Type: "web_search_20250305", Name: "web_search",
			UserLocation: &anthropicLocation{Type: "approximate", Country: r.Location},
		}},
	}
	if r.ForceWebSearch {
		body.ToolChoice = &anthropicToolChoice{Type: "tool", Name: "web_search"}
	}
	return apiRequest{Method: http.MethodPost, Path: "/v1/messages", Body: body}, nil
}

// parseAnthropic reads a Message. The answer is its text blocks: the API
// splits cited text into blocks of its own mid-sentence, so consecutive
// blocks are joined as they came and a paragraph break goes only where a
// search sits between them. Thinking blocks are skipped.
func parseAnthropic(raw []byte) Result {
	var resp struct {
		Model      string    `json:"model"`
		StopReason string    `json:"stop_reason"`
		Error      *apiError `json:"error"`
		Content    []struct {
			Type      string `json:"type"`
			Text      string `json:"text"`
			Citations []struct {
				Type string `json:"type"`
				URL  string `json:"url"`
			} `json:"citations"`
			Name  string `json:"name"`
			Input struct {
				Query string `json:"query"`
			} `json:"input"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return failed("anthropic: %v", err)
	}
	switch {
	case resp.Error != nil:
		return failed("anthropic: %s", resp.Error)
	case resp.StopReason == "refusal":
		return failed("anthropic: the model declined to answer (stop_reason refusal)")
	}
	var text strings.Builder
	var urls, fanOut []string
	afterText := false
	for _, b := range resp.Content {
		switch b.Type {
		case "text":
			if !afterText && text.Len() > 0 {
				text.WriteString("\n\n")
			}
			text.WriteString(b.Text)
			afterText = true
			for _, c := range b.Citations {
				if c.Type == "web_search_result_location" {
					urls = append(urls, c.URL)
				}
			}
			continue
		case "server_tool_use":
			if b.Name == "web_search" && b.Input.Query != "" {
				fanOut = append(fanOut, b.Input.Query)
			}
		}
		if b.Type != "thinking" && b.Type != "redacted_thinking" {
			afterText = false
		}
	}
	return answered(Result{
		Text:          strings.TrimSpace(text.String()),
		ReportedModel: resp.Model,
		Citations:     citations(urls),
		FanOut:        fanOut,
	})
}
