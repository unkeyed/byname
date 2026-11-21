package engines

import (
	"encoding/json"
	"strings"
)

type serpapiResponse struct {
	SearchMetadata struct {
		Status string `json:"status"`
	} `json:"search_metadata"`
	Error      string         `json:"error"`
	Header     string         `json:"header"`
	Markdown   string         `json:"markdown"`
	TextBlocks []serpapiBlock `json:"text_blocks"`
	References []struct {
		Link string `json:"link"`
	} `json:"references"`
}

type serpapiBlock struct {
	Snippet string         `json:"snippet"`
	List    []serpapiBlock `json:"list"`
}

// parseSerpApi reads the Bing Copilot and Brave AI Mode engines. Brave sends
// the whole answer as markdown; Copilot sends a header and text blocks.
func parseSerpApi(raw []byte) Result {
	var resp serpapiResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return failed("serpapi: %v", err)
	}
	if resp.Error != "" {
		return failed("serpapi: %s", resp.Error)
	}
	if resp.SearchMetadata.Status != "Success" {
		return failed("serpapi: search status %q", resp.SearchMetadata.Status)
	}
	text := resp.Markdown
	if text == "" {
		parts := []string{resp.Header}
		for _, b := range resp.TextBlocks {
			parts = append(parts, b.Snippet)
			for _, li := range b.List {
				parts = append(parts, "- "+li.Snippet)
			}
		}
		text = strings.TrimSpace(strings.Join(parts, "\n\n"))
	}
	urls := make([]string, 0, len(resp.References))
	for _, r := range resp.References {
		urls = append(urls, r.Link)
	}
	return answered(Result{Text: text, Citations: citations(urls)})
}
