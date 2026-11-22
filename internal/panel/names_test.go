package panel

import "testing"

func TestNameKeyJoinsSpellingsOfOneBrand(t *testing.T) {
	for _, group := range [][]string{
		{"Vercel", "Vercel Inc.", "Vercel, Inc", "vercel.com", "https://www.vercel.com/pricing", "Vercel's", "VERCEL"},
		{"Monday.com", "monday", "Monday.com Ltd."},
		{"The Browser Company", "Browser Company"},
		{"Litware Cloud", "Litware Cloud GmbH", "litware  cloud"},
		{"AT&T", "at&t inc"},
		{"Example Shop", "Example Shop Pty Ltd"},
	} {
		want := NameKey(group[0])
		if want == "" {
			t.Fatalf("NameKey(%q) is empty", group[0])
		}
		for _, n := range group[1:] {
			if got := NameKey(n); got != want {
				t.Errorf("NameKey(%q) = %q, want %q like %q", n, got, want, group[0])
			}
		}
	}
}

func TestNameKeyKeepsDifferentBrandsApart(t *testing.T) {
	for _, pair := range [][2]string{
		{"Node.js", "Node"},
		{"Google", "Google Analytics"},
		{"Co", "Company"},
		{"aws.amazon.com", "AWS"},
	} {
		if NameKey(pair[0]) == NameKey(pair[1]) {
			t.Errorf("NameKey(%q) and NameKey(%q) are both %q", pair[0], pair[1], NameKey(pair[0]))
		}
	}
	if got := NameKey("shop.example.co.uk"); got != "example" {
		t.Errorf(`NameKey("shop.example.co.uk") = %q, want "example"`, got)
	}
}

func TestDisplayNameDropsCompanySuffix(t *testing.T) {
	for in, want := range map[string]string{
		"Vercel, Inc.":         "Vercel",
		"Stripe Inc":           "Stripe",
		"Litware Cloud GmbH":   "Litware Cloud",
		"Example Shop Pty Ltd": "Example Shop",
		"Contoso":              "Contoso",
		"  Fabrikam   Deploy ": "Fabrikam Deploy",
		"Co":                   "Co",
	} {
		if got := DisplayName(in); got != want {
			t.Errorf("DisplayName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCoversTrackedBrandsUnderEverySpelling(t *testing.T) {
	tracked := []Brand{
		{ID: 1, Name: "Northwind Cloud", Aliases: []string{"Northwind"}, Domains: []string{"northwind.example"}, Kind: KindSelf},
		{ID: 2, Name: "Contoso Edge", Domains: []string{"contoso-edge.example"}, Kind: KindCompetitor},
	}
	for _, c := range []struct {
		name, domain string
		want         bool
	}{
		{"Northwind Cloud, Inc.", "", true},
		{"Northwind", "", true},
		{"northwind.example", "", true},
		{"Northwind Labs", "", true},
		{"NW", "app.northwind.example", true},
		{"Contoso Edge Functions", "", true},
		{"Contoso Edge's", "", true},
		{"Contoso", "contoso.example", false},
		{"Litware Cloud", "litware.example", false},
	} {
		if got := Covers(tracked, c.name, c.domain, MatchWholeWord); got != c.want {
			t.Errorf("Covers(%q, %q) = %v, want %v", c.name, c.domain, got, c.want)
		}
	}
	if Covers(tracked, "NORTHWIND CLOUD PAGES", "", MatchWholeWord) || !Covers(tracked, "NORTHWIND CLOUD PAGES", "", MatchSelfAnywhere) {
		t.Error("a name with your brand in other capitals is yours only when your brand matches anywhere, as answers are scored")
	}
}
