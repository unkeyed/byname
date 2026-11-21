package engines

import (
	"fmt"
	"net/url"
)

// The parameter names below come from each tool's input schema in Treg's
// catalog (GET https://treg.to/catalog/endpoints/<id>).

type dataforseoLLMTask struct {
	UserPrompt     string `json:"user_prompt"`
	ModelName      string `json:"model_name"`
	WebSearch      bool   `json:"web_search,omitempty"`
	ForceWebSearch bool   `json:"force_web_search,omitempty"`
	Country        string `json:"web_search_country_iso_code,omitempty"`
}

// dataforseoLLMRequest builds a DataForSEO LLM Responses task. The Gemini
// endpoint has no web_search_country_iso_code field, so country is false
// there.
func dataforseoLLMRequest(country bool) func(Endpoint, Request) (providerRequest, error) {
	return func(e Endpoint, r Request) (providerRequest, error) {
		t := dataforseoLLMTask{UserPrompt: r.Question, ModelName: r.Model}
		switch e.Search {
		case SearchForceable:
			t.WebSearch = true
			t.ForceWebSearch = r.ForceWebSearch
		case SearchEnableOnly:
			t.WebSearch = true
		}
		if country {
			t.Country = r.Location
		}
		return providerRequest{Body: []dataforseoLLMTask{t}}, nil
	}
}

type dataforseoScraperTask struct {
	Keyword      string `json:"keyword"`
	LocationCode int    `json:"location_code"`
	LanguageCode string `json:"language_code"`
}

func dataforseoScraperRequest(_ Endpoint, r Request) (providerRequest, error) {
	code, ok := dataforseoLocationCodes[r.Location]
	if !ok {
		return providerRequest{}, fmt.Errorf("engines: no DataForSEO location code for country %q", r.Location)
	}
	return providerRequest{Body: []dataforseoScraperTask{{
		Keyword: r.Question, LocationCode: code, LanguageCode: r.Language,
	}}}, nil
}

// dataforseoLocationCodes maps ISO 3166-1 alpha-2 to DataForSEO's location
// codes, which are Google Ads geo target ids (2000 + ISO 3166-1 numeric).
var dataforseoLocationCodes = map[string]int{
	"AU": 2036, "BR": 2076, "CA": 2124, "DE": 2276, "ES": 2724, "FR": 2250,
	"GB": 2826, "IE": 2372, "IN": 2356, "IT": 2380, "JP": 2392, "MX": 2484,
	"NL": 2528, "NZ": 2554, "SE": 2752, "SG": 2702, "US": 2840,
}

// cloro's plain "text" runs block boundaries together ("…the same.How It
// Works"), which defeats whole-word brand matching, so every cloro request
// asks for markdown and the parser prefers it.
type cloroInclude struct {
	Markdown   bool                `json:"markdown,omitempty"`
	AIOverview *cloroIncludeMarkup `json:"aioverview,omitempty"`
}

type cloroIncludeMarkup struct {
	Markdown bool `json:"markdown"`
}

type cloroPromptBody struct {
	Prompt  string       `json:"prompt"`
	Country string       `json:"country"`
	Include cloroInclude `json:"include"`
}

// cloroPromptRequest serves the Gemini and Bing AI Mode scrapes.
func cloroPromptRequest(_ Endpoint, r Request) (providerRequest, error) {
	return providerRequest{Body: cloroPromptBody{
		Prompt: r.Question, Country: r.Location, Include: cloroInclude{Markdown: true},
	}}, nil
}

type cloroGoogleBody struct {
	Prompt  string       `json:"prompt,omitempty"`
	Query   string       `json:"query,omitempty"`
	GL      string       `json:"gl"`
	HL      string       `json:"hl,omitempty"`
	Include cloroInclude `json:"include"`
}

func cloroAIModeRequest(_ Endpoint, r Request) (providerRequest, error) {
	return providerRequest{Body: cloroGoogleBody{
		Prompt: r.Question, GL: r.Location, HL: r.Language, Include: cloroInclude{Markdown: true},
	}}, nil
}

func cloroOrganicRequest(_ Endpoint, r Request) (providerRequest, error) {
	return providerRequest{Body: cloroGoogleBody{
		Query: r.Question, GL: r.Location, HL: r.Language,
		Include: cloroInclude{AIOverview: &cloroIncludeMarkup{Markdown: true}},
	}}, nil
}

// serpapiRequest builds a SerpApi query. The catalog lists only engine and q
// for both AI engines, so location and language are left to SerpApi.
func serpapiRequest(engine string) func(Endpoint, Request) (providerRequest, error) {
	return func(_ Endpoint, r Request) (providerRequest, error) {
		return providerRequest{Query: url.Values{"engine": {engine}, "q": {r.Question}}}, nil
	}
}
