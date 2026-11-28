package engines

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"
)

// Tool is a Treg tool Byname calls for its own work, outside the surface
// registry, with the price estimates use until past calls report theirs.
type Tool struct {
	ID string
	// Model is the model a model tool runs; empty otherwise.
	Model         string
	SampleCostUSD float64
}

var (
	// BrandFinder reads one answer for the brands it names: Claude Haiku
	// through DataForSEO's LLM Responses endpoint, no web search.
	// DataForSEO bills $0.0006 a call plus the model's tokens, about $0.002
	// for an answer of 3,000 characters.
	BrandFinder = Tool{ID: claudeAPI.ID, Model: "claude-haiku-4-5", SampleCostUSD: 0.003}
	MCPReviewer = Tool{ID: claudeAPI.ID, Model: "claude-haiku-4-5", SampleCostUSD: 0.003}
	// BrandByDomain is Context.dev's brand record for a domain: logos and
	// colours. 10 Context.dev credits a call, charged only on a hit.
	BrandByDomain = Tool{ID: "branddev.brand.retrieve-simplified", SampleCostUSD: 0.02534}
	// BrandByName is the same record found by company name.
	BrandByName = Tool{ID: "branddev.brand.retrieve-by-name", SampleCostUSD: 0.02534}
)

// The limits DataForSEO's LLM Responses endpoint sets: every text field
// holds at most LLMFieldChars characters, and the conversation before the
// prompt at most LLMContextMessages messages.
const (
	LLMFieldChars      = 500
	LLMContextMessages = 10
)

// LLMTask is one call to a model tool.
type LLMTask struct {
	Tool   Tool
	System string
	// Context is sent as user messages before Prompt, such as a long text
	// split by SplitForLLM.
	Context         []string
	Prompt          string
	MaxOutputTokens int
	IdempotencyKey  string
}

type llmMessage struct {
	Role    string `json:"role"`
	Message string `json:"message"`
}

type llmTaskBody struct {
	UserPrompt      string       `json:"user_prompt"`
	ModelName       string       `json:"model_name"`
	SystemMessage   string       `json:"system_message,omitempty"`
	MessageChain    []llmMessage `json:"message_chain,omitempty"`
	MaxOutputTokens int          `json:"max_output_tokens,omitempty"`
	Temperature     float64      `json:"temperature"`
}

// Ask sends task through Treg and returns the model's reply as a Result:
// the text, or StatusFailed with the provider's error. An error means there
// is no reply: the task broke a limit, the connection failed, or Treg
// refused the call (*Refusal).
func (t Treg) Ask(ctx context.Context, task LLMTask) (Result, error) {
	fields := append([]string{task.System, task.Prompt}, task.Context...)
	for _, f := range fields {
		if utf8.RuneCountInString(f) > LLMFieldChars {
			return Result{}, fmt.Errorf("engines: an LLM task field is over %d characters", LLMFieldChars)
		}
	}
	if len(task.Context) > LLMContextMessages {
		return Result{}, fmt.Errorf("engines: an LLM task has over %d context messages", LLMContextMessages)
	}
	if strings.TrimSpace(task.Prompt) == "" || task.Tool.Model == "" {
		return Result{}, fmt.Errorf("engines: an LLM task needs a prompt and a model")
	}
	b := llmTaskBody{UserPrompt: task.Prompt, ModelName: task.Tool.Model, SystemMessage: task.System, MaxOutputTokens: task.MaxOutputTokens}
	for _, c := range task.Context {
		b.MessageChain = append(b.MessageChain, llmMessage{Role: "user", Message: c})
	}
	resp, body, err := t.do(ctx, http.MethodPost, task.Tool.ID, providerRequest{Body: []llmTaskBody{b}}, task.IdempotencyKey)
	if err != nil {
		return Result{}, err
	}
	var res Result
	if resp.StatusCode/100 == 2 {
		res = parseDataForSEOLLM(body)
	} else {
		res = failed("provider HTTP %d", resp.StatusCode)
	}
	res.Raw = body
	res.CostUSD = chargedUSD(resp.Header)
	res.CallID = resp.Header.Get("X-Treg-Call-Id")
	return res, nil
}

// SplitForLLM splits text into at most LLMContextMessages parts of at
// most LLMFieldChars characters each, breaking at whitespace where it can.
// truncated is true when the text did not fit and its end was left out.
func SplitForLLM(text string) (parts []string, truncated bool) {
	rest := strings.TrimSpace(text)
	for rest != "" {
		if len(parts) == LLMContextMessages {
			return parts, true
		}
		if utf8.RuneCountInString(rest) <= LLMFieldChars {
			return append(parts, rest), false
		}
		cut := byteOffset(rest, LLMFieldChars)
		if ws := strings.LastIndexAny(rest[:cut], " \n\t"); ws > cut/2 {
			cut = ws
		}
		parts = append(parts, strings.TrimSpace(rest[:cut]))
		rest = strings.TrimSpace(rest[cut:])
	}
	return parts, false
}

// byteOffset is the byte offset of the n-th rune of s.
func byteOffset(s string, n int) int {
	i := 0
	for range n {
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
	}
	return i
}
