package engines

import (
	"encoding/json"
	"fmt"
)

type cloroResponse[T any] struct {
	Success bool            `json:"success"`
	Result  T               `json:"result"`
	Error   json.RawMessage `json:"error"`
}

type cloroAnswer struct {
	Text     string `json:"text"`
	Markdown string `json:"markdown"`
	Sources  []struct {
		URL string `json:"url"`
	} `json:"sources"`
}

func (a cloroAnswer) result() Result {
	text := a.Markdown
	if text == "" {
		text = a.Text
	}
	urls := make([]string, 0, len(a.Sources))
	for _, s := range a.Sources {
		urls = append(urls, s.URL)
	}
	return Result{Text: text, Citations: citations(urls)}
}

func cloroDecode[T any](raw []byte) (T, error) {
	var resp cloroResponse[T]
	if err := json.Unmarshal(raw, &resp); err != nil {
		return resp.Result, fmt.Errorf("cloro: %w", err)
	}
	if !resp.Success {
		return resp.Result, fmt.Errorf("cloro: %s", resp.Error)
	}
	return resp.Result, nil
}

// parseCloroAnswer reads the Gemini, Google AI Mode and Bing AI Mode scrapes.
func parseCloroAnswer(raw []byte) Result {
	a, err := cloroDecode[cloroAnswer](raw)
	if err != nil {
		return failed("%v", err)
	}
	return answered(a.result())
}

// parseCloroAIOverview reads a Google search page. cloro omits aioverview
// when Google showed none for the query, which is a stored result of its
// own, not a failure.
func parseCloroAIOverview(raw []byte) Result {
	page, err := cloroDecode[struct {
		AIOverview *cloroAnswer `json:"aioverview"`
	}](raw)
	if err != nil {
		return failed("%v", err)
	}
	if page.AIOverview == nil {
		return Result{Status: StatusNoAnswer}
	}
	return answered(page.AIOverview.result())
}
