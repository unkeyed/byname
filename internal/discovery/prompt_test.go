package discovery

import (
	"slices"
	"testing"
	"unicode/utf8"

	"github.com/unkeyed/byname/internal/engines"
)

func TestPromptFitsTheTool(t *testing.T) {
	for name, s := range map[string]string{"system": system, "prompt": prompt} {
		if n := utf8.RuneCountInString(s); n > engines.LLMFieldChars {
			t.Errorf("%s is %d characters, over the tool's %d", name, n, engines.LLMFieldChars)
		}
	}
}

func TestParseFoundReadsTheModelsReply(t *testing.T) {
	got, err := parseFound("Here you go:\n```json\n" + `{"brands":[{"name":"Vercel, Inc.","kind":"Brand","domain":"https://www.vercel.com/"},` +
		`{"name":"","kind":"brand"},{"name":"Docker","kind":"technology","domain":"docker.com"},{"name":"Render","kind":"brand","domain":"not a host"}]}` + "\n```")
	want := []Found{{Name: "Vercel", Kind: "brand", Domain: "vercel.com"}, {Name: "Docker", Kind: "technology", Domain: "docker.com"}, {Name: "Render", Kind: "brand"}}
	if err != nil || !slices.Equal(got, want) {
		t.Errorf("parseFound = %+v, %v; want %+v", got, err, want)
	}
	for _, bad := range []string{"I could not find any brands.", `{"brands": "none"}`} {
		if _, err := parseFound(bad); err == nil {
			t.Errorf("parseFound(%q) succeeded, want an error so the read is retried", bad)
		}
	}
}
