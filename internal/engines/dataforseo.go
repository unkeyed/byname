package engines

import (
	"encoding/json"
	"fmt"
	"strings"
)

const dataforseoOK = 20000

type dataforseoResponse[T any] struct {
	StatusCode    int    `json:"status_code"`
	StatusMessage string `json:"status_message"`
	Tasks         []struct {
		StatusCode    int    `json:"status_code"`
		StatusMessage string `json:"status_message"`
		Result        []T    `json:"result"`
	} `json:"tasks"`
}

// dataforseoTask returns the result list of the one task every Byname request
// sends, or the status DataForSEO reported instead.
func dataforseoTask[T any](raw []byte) ([]T, error) {
	var resp dataforseoResponse[T]
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("dataforseo: %w", err)
	}
	if resp.StatusCode != dataforseoOK {
		return nil, fmt.Errorf("dataforseo %d: %s", resp.StatusCode, resp.StatusMessage)
	}
	if len(resp.Tasks) == 0 {
		return nil, fmt.Errorf("dataforseo: response has no task")
	}
	t := resp.Tasks[0]
	if t.StatusCode != dataforseoOK {
		return nil, fmt.Errorf("dataforseo %d: %s", t.StatusCode, t.StatusMessage)
	}
	return t.Result, nil
}

type dataforseoLLMResult struct {
	ModelName string `json:"model_name"`
	Items     []struct {
		Type     string `json:"type"`
		Sections []struct {
			Type        string `json:"type"`
			Text        string `json:"text"`
			Annotations []struct {
				URL string `json:"url"`
			} `json:"annotations"`
		} `json:"sections"`
	} `json:"items"`
	FanOutQueries []string `json:"fan_out_queries"`
}

// parseDataForSEOLLM reads the LLM Responses shape shared by the Claude,
// ChatGPT, Gemini and Perplexity endpoints. Only "message" items are the
// answer; "reasoning" items (gpt-5, gemini-2.5-pro) are the model thinking
// and never part of the text.
func parseDataForSEOLLM(raw []byte) Result {
	results, err := dataforseoTask[dataforseoLLMResult](raw)
	if err != nil {
		return failed("%v", err)
	}
	if len(results) == 0 {
		return failed("dataforseo: task has no result")
	}
	res := results[0]
	var texts, urls []string
	for _, item := range res.Items {
		if item.Type != "message" {
			continue
		}
		for _, s := range item.Sections {
			if s.Type != "text" {
				continue
			}
			texts = append(texts, s.Text)
			for _, a := range s.Annotations {
				urls = append(urls, a.URL)
			}
		}
	}
	return answered(Result{
		Text:          strings.Join(texts, "\n\n"),
		ReportedModel: res.ModelName,
		Citations:     citations(urls),
		FanOut:        res.FanOutQueries,
	})
}

type dataforseoScraperResult struct {
	Model    string `json:"model"`
	Markdown string `json:"markdown"`
	Sources  []struct {
		URL string `json:"url"`
	} `json:"sources"`
	FanOutQueries []string `json:"fan_out_queries"`
}

func parseDataForSEOScraper(raw []byte) Result {
	results, err := dataforseoTask[dataforseoScraperResult](raw)
	if err != nil {
		return failed("%v", err)
	}
	if len(results) == 0 {
		return failed("dataforseo: task has no result")
	}
	res := results[0]
	urls := make([]string, 0, len(res.Sources))
	for _, s := range res.Sources {
		urls = append(urls, s.URL)
	}
	return answered(Result{
		Text:          res.Markdown,
		ReportedModel: res.Model,
		Citations:     citations(urls),
		FanOut:        res.FanOutQueries,
	})
}

// Model is one entry of an endpoint's live model list.
type Model struct {
	Name      string
	WebSearch bool
}

func parseDataForSEOModels(raw []byte) ([]Model, error) {
	results, err := dataforseoTask[struct {
		ModelName          string `json:"model_name"`
		WebSearchSupported bool   `json:"web_search_supported"`
	}](raw)
	if err != nil {
		return nil, err
	}
	models := make([]Model, 0, len(results))
	for _, m := range results {
		models = append(models, Model{Name: m.ModelName, WebSearch: m.WebSearchSupported})
	}
	return models, nil
}
