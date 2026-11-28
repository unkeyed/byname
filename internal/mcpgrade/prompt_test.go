package mcpgrade

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/unkeyed/byname/internal/engines"
)

func TestReviewFitsTheTool(t *testing.T) {
	for name, f := range map[string]string{"system": reviewSystem, "prompt": reviewPrompt, "cut prompt": cutNote + reviewPrompt} {
		if n := utf8.RuneCountInString(f); n > engines.LLMFieldChars {
			t.Errorf("the %s is %d characters, over %d", name, n, engines.LLMFieldChars)
		}
	}
	big := Tool{Name: "search", Title: "Search", Description: strings.Repeat("Searches the documentation. ", maxDescription/28), Hints: []string{"read_only"}}
	for i := range 60 {
		big.Params = append(big.Params, Param{Name: fmt.Sprintf("filter_%d", i), Type: "string", Required: i == 0, Description: "Narrows the search to one product area."})
	}
	parts, cut := engines.SplitForLLM(render(big))
	if !cut || len(parts) != engines.LLMContextMessages {
		t.Errorf("an 8 KB tool split into %d parts, cut %v; want %d parts, cut", len(parts), cut, engines.LLMContextMessages)
	}
	for i, p := range parts {
		if n := utf8.RuneCountInString(p); n > engines.LLMFieldChars {
			t.Errorf("part %d is %d characters", i, n)
		}
	}
	if !strings.HasPrefix(parts[0], "Tool name: search\nTitle: Search\nDescription: Searches") {
		t.Errorf("the definition starts %q", parts[0][:60])
	}
	small := render(Tool{Name: "ping", Params: []Param{{Name: "host", Required: true}}})
	for _, want := range []string{"Title: (none)", "Description: (none)", "- host (no type, required): (none)", "Behavior hints: none", "Output schema: no"} {
		if !strings.Contains(small, want) {
			t.Errorf("a bare tool renders without %q:\n%s", want, small)
		}
	}
}

func TestParseReview(t *testing.T) {
	long := strings.Repeat("é", 400)
	for _, c := range []struct {
		name, reply string
		want        ToolReview
		err         string
	}{
		{name: "plain", reply: `{"title":{"score":80,"reason":" Clear. "},"description":{"score":40,"reason":"Say what it returns."},"inputs":{"score":100,"reason":"None."}}`,
			want: ToolReview{Score: 70, Title: ReviewPart{80, "Clear."}, Description: ReviewPart{40, "Say what it returns."}, Inputs: ReviewPart{100, "None."}}},
		{name: "fenced with prose", reply: "Here it is:\n```json\n{\"title\":{\"score\":0,\"reason\":\"\"},\"description\":{\"score\":0,\"reason\":\"\"},\"inputs\":{\"score\":0,\"reason\":\"\"}}\n```",
			want: ToolReview{}},
		{name: "fractional score rounds", reply: `{"title":{"score":79.6,"reason":""},"description":{"score":100,"reason":""},"inputs":{"score":100,"reason":""}}`,
			want: ToolReview{Score: 96, Title: ReviewPart{Score: 80}, Description: ReviewPart{Score: 100}, Inputs: ReviewPart{Score: 100}}},
		{name: "no JSON", reply: "I cannot judge this tool.", err: "no JSON object"},
		{name: "broken JSON", reply: `{"title":{"score":80}`, err: "not the JSON asked for"},
		{name: "missing part", reply: `{"title":{"score":80,"reason":""},"description":{"score":80,"reason":""}}`, err: "no inputs part"},
		{name: "missing score", reply: `{"title":{"reason":""},"description":{"score":80,"reason":""},"inputs":{"score":80,"reason":""}}`, err: "title score"},
		{name: "over 100", reply: `{"title":{"score":80,"reason":""},"description":{"score":101,"reason":""},"inputs":{"score":80,"reason":""}}`, err: "description score"},
		{name: "negative", reply: `{"title":{"score":80,"reason":""},"description":{"score":80,"reason":""},"inputs":{"score":-1,"reason":""}}`, err: "inputs score"},
	} {
		got, err := parseReview(c.reply)
		switch {
		case c.err != "" && (err == nil || !strings.Contains(err.Error(), c.err)):
			t.Errorf("%s: error %v, want one saying %q", c.name, err, c.err)
		case c.err == "" && err != nil:
			t.Errorf("%s: %v", c.name, err)
		case c.err == "" && got != c.want:
			t.Errorf("%s: %+v, want %+v", c.name, got, c.want)
		}
	}
	got, err := parseReview(`{"title":{"score":50,"reason":"` + long + `"},"description":{"score":50,"reason":""},"inputs":{"score":50,"reason":""}}`)
	if err != nil || utf8.RuneCountInString(got.Title.Reason) != maxReason || !strings.HasSuffix(got.Title.Reason, "…") {
		t.Errorf("a 400-character reason kept %d characters, %v; want %d ending in an ellipsis", utf8.RuneCountInString(got.Title.Reason), err, maxReason)
	}
}
