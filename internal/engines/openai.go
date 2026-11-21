package engines

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

// openAI is the Responses API with the hosted web_search tool.
// https://developers.openai.com/api/docs/guides/tools-web-search
var openAI = providerAPI{
	id: "openai.responses", family: "openai",
	base: "https://api.openai.com", auth: bearer,
	build: openAIRequest, parse: parseOpenAI,
	models: modelList{id: "openai.models", path: "/v1/models", parse: parseModelIDs},
}

func newOpenAI(rs RouteSpec, c Conn) Adapter { return apiAdapter{rs: rs, conn: c, api: openAI} }

func probeOpenAI(ctx context.Context, c Conn) error {
	_, err := apiAdapter{conn: c, api: openAI}.listModels(ctx)
	return err
}

type openAIBody struct {
	Model string       `json:"model"`
	Input string       `json:"input"`
	Tools []openAITool `json:"tools"`
	// ToolChoice "required" makes the model call a tool, and web_search is
	// the only one.
	ToolChoice string `json:"tool_choice,omitempty"`
}

type openAITool struct {
	Type         string          `json:"type"`
	UserLocation *openAILocation `json:"user_location,omitempty"`
}

type openAILocation struct {
	Type    string `json:"type"`
	Country string `json:"country"`
}

func openAIRequest(_ RouteSpec, r Request) (apiRequest, error) {
	body := openAIBody{
		Model: r.Model,
		Input: r.Question,
		Tools: []openAITool{{Type: "web_search", UserLocation: &openAILocation{Type: "approximate", Country: r.Location}}},
	}
	if r.ForceWebSearch {
		body.ToolChoice = "required"
	}
	return apiRequest{Method: http.MethodPost, Path: "/v1/responses", Body: body}, nil
}

// parseOpenAI reads a Response. Only message items are the answer;
// reasoning items are the model thinking. Each web_search_call is one of
// the model's own searches.
func parseOpenAI(raw []byte) Result {
	var resp struct {
		Model             string    `json:"model"`
		Status            string    `json:"status"`
		Error             *apiError `json:"error"`
		IncompleteDetails *struct {
			Reason string `json:"reason"`
		} `json:"incomplete_details"`
		Output []struct {
			Type   string `json:"type"`
			Action struct {
				Query string `json:"query"`
			} `json:"action"`
			Content []struct {
				Type        string `json:"type"`
				Text        string `json:"text"`
				Annotations []struct {
					Type string `json:"type"`
					URL  string `json:"url"`
				} `json:"annotations"`
			} `json:"content"`
		} `json:"output"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return failed("openai: %v", err)
	}
	switch {
	case resp.Error != nil:
		return failed("openai: %s", resp.Error)
	case resp.Status == "incomplete" && resp.IncompleteDetails != nil:
		return failed("openai: response incomplete: %s", resp.IncompleteDetails.Reason)
	case resp.Status != "completed":
		return failed("openai: response status %q", resp.Status)
	}
	var texts, urls, fanOut []string
	for _, item := range resp.Output {
		switch item.Type {
		case "web_search_call":
			if item.Action.Query != "" {
				fanOut = append(fanOut, item.Action.Query)
			}
		case "message":
			for _, c := range item.Content {
				if c.Type != "output_text" {
					continue
				}
				texts = append(texts, c.Text)
				for _, a := range c.Annotations {
					if a.Type == "url_citation" {
						urls = append(urls, a.URL)
					}
				}
			}
		}
	}
	return answered(Result{
		Text:          strings.Join(texts, "\n\n"),
		ReportedModel: resp.Model,
		Citations:     citations(urls),
		FanOut:        fanOut,
	})
}
