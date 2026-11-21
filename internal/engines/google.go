package engines

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
)

// google is the Gemini API's generateContent with Grounding with Google
// Search. The google_search tool takes no location or language, and search
// is turned on, never forced.
var google = providerAPI{
	id: "google.generate-content", family: "google",
	base: "https://generativelanguage.googleapis.com", auth: googleAuth,
	build: googleRequest, parse: parseGoogle,
	models: modelList{id: "google.models", path: "/v1beta/models", query: url.Values{"pageSize": {"1000"}}, parse: parseGoogleModels},
}

func newGoogle(rs RouteSpec, c Conn) Adapter { return apiAdapter{rs: rs, conn: c, api: google} }

func probeGoogle(ctx context.Context, c Conn) error {
	_, err := apiAdapter{conn: c, api: google}.listModels(ctx)
	return err
}

func googleAuth(r *http.Request, secret string) error {
	r.Header.Set("x-goog-api-key", secret)
	return nil
}

type googleBody struct {
	Contents []googleContent `json:"contents"`
	Tools    []googleTool    `json:"tools"`
}

type googleContent struct {
	Role  string       `json:"role"`
	Parts []googlePart `json:"parts"`
}

type googlePart struct {
	Text string `json:"text"`
}

type googleTool struct {
	GoogleSearch struct{} `json:"google_search"`
}

func googleRequest(_ RouteSpec, r Request) (apiRequest, error) {
	return apiRequest{
		Method: http.MethodPost,
		Path:   "/v1beta/models/" + url.PathEscape(r.Model) + ":generateContent",
		Body: googleBody{
			Contents: []googleContent{{Role: "user", Parts: []googlePart{{Text: r.Question}}}},
			Tools:    []googleTool{{}},
		},
	}, nil
}

// parseGoogle reads a GenerateContentResponse. Parts marked thought are the
// model thinking and never part of the text. Grounding chunks are
// vertexaisearch redirect links, which the adapter resolves when it has a
// Resolver.
func parseGoogle(raw []byte) Result {
	var resp struct {
		ModelVersion string `json:"modelVersion"`
		Error        *struct {
			Code    int    `json:"code"`
			Status  string `json:"status"`
			Message string `json:"message"`
		} `json:"error"`
		PromptFeedback *struct {
			BlockReason string `json:"blockReason"`
		} `json:"promptFeedback"`
		Candidates []struct {
			FinishReason string `json:"finishReason"`
			Content      struct {
				Parts []struct {
					Text    string `json:"text"`
					Thought bool   `json:"thought"`
				} `json:"parts"`
			} `json:"content"`
			GroundingMetadata struct {
				WebSearchQueries []string `json:"webSearchQueries"`
				GroundingChunks  []struct {
					Web struct {
						URI string `json:"uri"`
					} `json:"web"`
				} `json:"groundingChunks"`
			} `json:"groundingMetadata"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return failed("google: %v", err)
	}
	if resp.Error != nil {
		return failed("google: %d %s: %s", resp.Error.Code, resp.Error.Status, resp.Error.Message)
	}
	if len(resp.Candidates) == 0 {
		reason := "no candidates"
		if resp.PromptFeedback != nil && resp.PromptFeedback.BlockReason != "" {
			reason = "prompt blocked: " + resp.PromptFeedback.BlockReason
		}
		return failed("google: %s", reason)
	}
	c := resp.Candidates[0]
	var text strings.Builder
	for _, p := range c.Content.Parts {
		if !p.Thought {
			text.WriteString(p.Text)
		}
	}
	urls := make([]string, 0, len(c.GroundingMetadata.GroundingChunks))
	for _, g := range c.GroundingMetadata.GroundingChunks {
		urls = append(urls, g.Web.URI)
	}
	res := answered(Result{
		Text:          text.String(),
		ReportedModel: resp.ModelVersion,
		Citations:     citations(urls),
		FanOut:        c.GroundingMetadata.WebSearchQueries,
	})
	if res.Status == StatusFailed && c.FinishReason != "" && c.FinishReason != "STOP" {
		res.Error = fmt.Sprintf("google: no answer text (finishReason %s)", c.FinishReason)
	}
	return res
}

// parseGoogleModels keeps the models that answer generateContent, without
// the "models/" prefix the list puts on each name.
func parseGoogleModels(raw []byte) ([]Model, error) {
	var resp struct {
		Models []struct {
			Name    string   `json:"name"`
			Methods []string `json:"supportedGenerationMethods"`
		} `json:"models"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("google model list: %w", err)
	}
	var models []Model
	for _, m := range resp.Models {
		if slices.Contains(m.Methods, "generateContent") {
			models = append(models, Model{Name: strings.TrimPrefix(m.Name, "models/")})
		}
	}
	return models, nil
}
