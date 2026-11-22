package engines

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSplitForLLMKeepsWithinTheToolsLimits(t *testing.T) {
	word := "Northwind Cloud runs the edge. "
	short, cut := SplitForLLM("  " + word + " ")
	if cut || len(short) != 1 || short[0] != strings.TrimSpace(word) {
		t.Fatalf("short text = %q truncated %v, want one trimmed part", short, cut)
	}
	long := strings.Repeat(word, 200)
	parts, cut := SplitForLLM(long)
	if !cut || len(parts) != LLMContextMessages {
		t.Fatalf("long text: %d parts, truncated %v; want %d parts and truncated", len(parts), cut, LLMContextMessages)
	}
	for i, p := range parts {
		if n := utf8.RuneCountInString(p); n > LLMFieldChars || n < LLMFieldChars/2 {
			t.Errorf("part %d is %d characters", i, n)
		}
		if strings.HasSuffix(p, "Northw") {
			t.Errorf("part %d breaks inside a word: %q", i, p[len(p)-20:])
		}
	}
	accents := strings.Repeat("é", 1200)
	parts, _ = SplitForLLM(accents)
	for _, p := range parts {
		if !utf8.ValidString(p) || utf8.RuneCountInString(p) > LLMFieldChars {
			t.Errorf("a part of %d characters is cut inside a character or too long", utf8.RuneCountInString(p))
		}
	}
}

func TestAskSendsDataForSEOsTaskAndReadsTheReply(t *testing.T) {
	reply, err := os.ReadFile("testdata/dataforseo/dataforseo.x.ai-optimization-claude-llm-responses-live.find-brands.synthetic.json")
	if err != nil {
		t.Fatal(err)
	}
	var got []map[string]any
	var path, key string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, key = r.URL.Path, r.Header.Get("Idempotency-Key")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		w.Header().Set("X-Treg-Cost-Micro", "2100")
		w.Header().Set("X-Treg-Call-Id", "call_1")
		_, _ = w.Write(reply)
	}))
	defer srv.Close()
	res, err := Treg{BaseURL: srv.URL, Token: "t"}.Ask(context.Background(), LLMTask{
		Tool: BrandFinder, System: "list brands", Context: []string{"part one", "part two"}, Prompt: "JSON only", MaxOutputTokens: 100, IdempotencyKey: "k1",
	})
	if err != nil || res.Status != StatusOK || !strings.Contains(res.Text, `"Litware Cloud, Inc."`) || res.CostUSD == nil || *res.CostUSD != 0.0021 || res.CallID != "call_1" {
		t.Fatalf("ask = %+v, %v", res, err)
	}
	if path != "/call/"+BrandFinder.ID || key != "k1" || len(got) != 1 {
		t.Fatalf("sent %s key %q body %v", path, key, got)
	}
	task := got[0]
	chain, _ := task["message_chain"].([]any)
	if task["model_name"] != BrandFinder.Model || task["system_message"] != "list brands" || task["user_prompt"] != "JSON only" ||
		task["temperature"] != 0.0 || len(chain) != 2 || task["web_search"] != nil {
		t.Errorf("task = %v, want the model, the system message, two user messages, temperature 0 and no web search", task)
	}
	if _, err := (Treg{BaseURL: srv.URL}).Ask(context.Background(), LLMTask{Tool: BrandFinder, Prompt: strings.Repeat("x", LLMFieldChars+1)}); err == nil {
		t.Error("a prompt over the limit was sent")
	}
}
