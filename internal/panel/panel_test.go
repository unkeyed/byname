package panel

import (
	"reflect"
	"testing"

	"github.com/unkeyed/byname/internal/engines"
)

const (
	northwind BrandID = iota + 1
	contoso
	fabrikam
	redis
	northwindPages
)

var brands = []Brand{
	{ID: northwind, Name: "Northwind", Aliases: []string{"Northwind Compute"}, Domains: []string{"northwind.example", "nwcdn.example"}, Kind: KindSelf},
	{ID: contoso, Name: "Contoso", Aliases: []string{"Contoso Gateway"}, Domains: []string{"contoso.example"}, Kind: KindCompetitor},
	{ID: fabrikam, Name: "Fabrikam", Kind: KindCompetitor},
	{ID: redis, Name: "Redis", Kind: KindTechnology},
	{ID: northwindPages, Name: "Northwind Pages", Kind: KindCompetitor},
}

func ok(text string, cites ...string) engines.Result {
	return engines.Result{Status: engines.StatusOK, Text: text, Citations: cites}
}

func TestScoreMentions(t *testing.T) {
	whole := Settings{MatchMode: MatchWholeWord}
	anywhere := Settings{MatchMode: MatchSelfAnywhere}
	for _, tc := range []struct {
		name      string
		text      string
		settings  Settings
		mentions  []Mention
		mentioned bool
		position  int
	}{
		{
			name:      "position is order of first mention",
			text:      "Contoso is popular. Northwind is newer. Contoso again, then Fabrikam.",
			mentions:  []Mention{{contoso, 1}, {northwind, 2}, {fabrikam, 3}},
			mentioned: true, position: 2,
		},
		{
			name:      "an alias counts as its brand, once",
			text:      "Northwind Compute runs it, and Northwind bills it.",
			mentions:  []Mention{{northwind, 1}},
			mentioned: true, position: 1,
		},
		{
			name:      "technologies are recorded without a position",
			text:      "Redis works, but Contoso Gateway and Northwind are managed.",
			mentions:  []Mention{{redis, 0}, {contoso, 1}, {northwind, 2}},
			mentioned: true, position: 2,
		},
		{
			name:     "the longest overlapping name wins",
			text:     "Northwind Pages hosts static sites.",
			mentions: []Mention{{northwindPages, 1}},
		},
		{
			name: "whole word and case-sensitive by default",
			text: "northwind, NORTHWIND and Northwinds are not the name.",
		},
		{
			name:      "own name anywhere, ignoring case, when enabled",
			text:      "Try northwind.example or Contoso.",
			settings:  anywhere,
			mentions:  []Mention{{northwind, 1}, {contoso, 2}},
			mentioned: true, position: 1,
		},
		{
			name:     "the anywhere fallback is for the self brand only",
			text:     "contoso and fabrikam in lowercase.",
			settings: anywhere,
		},
		{
			name:      "punctuation and possessives are word boundaries",
			text:      "(Northwind's) edge, then \"Contoso\".",
			mentions:  []Mention{{northwind, 1}, {contoso, 2}},
			mentioned: true, position: 1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := tc.settings
			if s.MatchMode == "" {
				s = whole
			}
			got := Score(ok(tc.text), brands, s)
			if !reflect.DeepEqual(got.Mentions, tc.mentions) {
				t.Errorf("mentions %v, want %v", got.Mentions, tc.mentions)
			}
			if got.Mentioned != tc.mentioned || got.Position != tc.position {
				t.Errorf("mentioned=%v position=%d, want %v %d", got.Mentioned, got.Position, tc.mentioned, tc.position)
			}
		})
	}
}

func TestScoreCitations(t *testing.T) {
	got := Score(ok("Contoso is the usual pick.",
		"https://docs.northwind.example/ratelimit",
		"https://www.contoso.example/pricing",
		"https://nwcdn.example/asset",
		"https://northwind.example.attacker.example/x",
		"https://notnorthwind.example/",
		"https://WWW.Northwind.Example/",
	), brands, Settings{MatchMode: MatchWholeWord})

	want := []Citation{
		{"https://docs.northwind.example/ratelimit", "docs.northwind.example", true},
		{"https://www.contoso.example/pricing", "contoso.example", false},
		{"https://nwcdn.example/asset", "nwcdn.example", true},
		{"https://northwind.example.attacker.example/x", "northwind.example.attacker.example", false},
		{"https://notnorthwind.example/", "notnorthwind.example", false},
		{"https://WWW.Northwind.Example/", "northwind.example", true},
	}
	if !reflect.DeepEqual(got.Citations, want) {
		t.Errorf("citations\n%v\nwant\n%v", got.Citations, want)
	}
	if !got.Cited || got.Mentioned {
		t.Errorf("an own-domain citation without the name must be cited and not mentioned: cited=%v mentioned=%v", got.Cited, got.Mentioned)
	}
}

func TestScoreKeepsStatus(t *testing.T) {
	for _, st := range []engines.Status{engines.StatusFailed, engines.StatusNoAnswer} {
		got := Score(engines.Result{Status: st}, brands, Settings{MatchMode: MatchWholeWord})
		if got.Status != st || got.Mentioned || got.Cited || len(got.Mentions) != 0 {
			t.Errorf("%s: got %+v", st, got)
		}
	}
}

func TestScoreWithoutASelfBrand(t *testing.T) {
	got := Score(ok("Contoso and Northwind.", "https://northwind.example/"), brands[1:], Settings{MatchMode: MatchSelfAnywhere})
	if got.Mentioned || got.Cited {
		t.Errorf("got %+v", got)
	}
}
