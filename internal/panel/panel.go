// Package panel scores answers against the brand table and compares runs.
//
// Everything here is a pure function over values: no database, no clock, no
// network. Mentions and citations are separate facts. A citation of your
// domain without your name in the text is cited, not mentioned.
package panel

import (
	"net/url"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/unkeyed/byname/internal/engines"
)

type (
	BrandID        int64
	QuestionID     int64
	SurfaceModelID int64
)

// BrandKind decides how a brand counts. Technologies are recorded but never
// get a position and never count toward share of voice. Untracked and
// hidden brands are ones AI answers named that nobody tracks; they are
// never scored, so Score never sees them.
type BrandKind string

const (
	KindSelf       BrandKind = "self"
	KindCompetitor BrandKind = "competitor"
	KindTechnology BrandKind = "technology"
	// KindUntracked is a brand found in answers that you don't track.
	KindUntracked BrandKind = "untracked"
	// KindHidden is a found brand someone said is not a competitor.
	KindHidden BrandKind = "hidden"
)

// TrackedKinds are the kinds answers are scored against.
var TrackedKinds = []BrandKind{KindSelf, KindCompetitor, KindTechnology}

type Brand struct {
	ID      BrandID
	Name    string
	Aliases []string
	// Domains are hostnames; the first is the primary domain. A link to any
	// of them or their subdomains is a citation of the brand.
	Domains []string
	Kind    BrandKind
}

// MatchMode is how the self brand's name is found in an answer.
type MatchMode string

const (
	// MatchWholeWord matches names and aliases as whole words, case-sensitive.
	MatchWholeWord MatchMode = "whole_word"
	// MatchSelfAnywhere also counts the self brand's name anywhere in the
	// text, ignoring case. Other brands stay whole-word.
	MatchSelfAnywhere MatchMode = "self_anywhere"
)

type Settings struct {
	MatchMode MatchMode
}

// Mention is one brand named in an answer. Position is the 1-based order of
// first mention among non-technology brands, and 0 for a technology.
type Mention struct {
	Brand    BrandID
	Position int
}

type Citation struct {
	URL    string
	Domain string
	IsSelf bool
}

// Scored is one answer after scoring.
type Scored struct {
	Status engines.Status
	// Mentioned: the self brand is named in the text.
	Mentioned bool
	// Cited: a cited URL is on one of the self brand's domains.
	Cited bool
	// Position is the self brand's position, or 0 when not named.
	Position int
	// Mentions are every brand named, in order of first mention.
	Mentions  []Mention
	Citations []Citation
}

// Score finds the brands an answer names and classifies what it cites.
// brands should hold at most one KindSelf brand.
func Score(r engines.Result, brands []Brand, s Settings) Scored {
	out := Scored{Status: r.Status, Mentions: mentions(r.Text, brands, s.MatchMode)}
	self, hasSelf := selfBrand(brands)
	for _, m := range out.Mentions {
		if hasSelf && m.Brand == self.ID {
			out.Mentioned, out.Position = true, m.Position
		}
	}
	for _, u := range r.Citations {
		c := Citation{URL: u, Domain: domainOf(u)}
		c.IsSelf = hasSelf && onDomains(c.Domain, self.Domains)
		out.Cited = out.Cited || c.IsSelf
		out.Citations = append(out.Citations, c)
	}
	return out
}

func selfBrand(brands []Brand) (Brand, bool) {
	for _, b := range brands {
		if b.Kind == KindSelf {
			return b, true
		}
	}
	return Brand{}, false
}

// Named is every brand text names, in order of first mention, by the rules
// Score uses for an answer's text.
func Named(text string, brands []Brand, mode MatchMode) []Mention {
	return mentions(text, brands, mode)
}

type span struct{ start, end, brand int }

// mentions finds every brand in text. Where two names overlap ("Northwind"
// inside "Northwind Pages"), the longest match wins and the shorter one does
// not count.
func mentions(text string, brands []Brand, mode MatchMode) []Mention {
	var spans []span
	for i, b := range brands {
		for _, term := range append([]string{b.Name}, b.Aliases...) {
			spans = append(spans, wholeWord(text, term, i)...)
		}
		if mode == MatchSelfAnywhere && b.Kind == KindSelf {
			spans = append(spans, anyCase(text, b.Name, i)...)
		}
	}
	sort.SliceStable(spans, func(i, j int) bool {
		if spans[i].start != spans[j].start {
			return spans[i].start < spans[j].start
		}
		return spans[i].end > spans[j].end
	})

	var out []Mention
	seen := map[int]bool{}
	position, covered := 0, 0
	for _, sp := range spans {
		if sp.start < covered {
			continue
		}
		covered = sp.end
		if seen[sp.brand] {
			continue
		}
		seen[sp.brand] = true
		m := Mention{Brand: brands[sp.brand].ID}
		if brands[sp.brand].Kind != KindTechnology {
			position++
			m.Position = position
		}
		out = append(out, m)
	}
	return out
}

// NamesTerm reports whether text names term as a whole word, in any case.
// "Unkey", "@unkey", "#unkey" and "unkey.com" name Unkey; "monkey" does not.
func NamesTerm(text, term string) bool {
	return len(wholeWord(strings.ToLower(text), strings.ToLower(strings.TrimSpace(term)), 0)) > 0
}

func wholeWord(text, term string, brand int) []span {
	if term == "" {
		return nil
	}
	var out []span
	for from := 0; ; {
		i := strings.Index(text[from:], term)
		if i < 0 {
			return out
		}
		start := from + i
		end := start + len(term)
		if boundaryBefore(text, start) && boundaryAfter(text, end) {
			out = append(out, span{start, end, brand})
		}
		from = start + 1
	}
}

func anyCase(text, term string, brand int) []span {
	if term == "" {
		return nil
	}
	var out []span
	for i := 0; i+len(term) <= len(text); i++ {
		if utf8.RuneStart(text[i]) && strings.EqualFold(text[i:i+len(term)], term) {
			out = append(out, span{i, i + len(term), brand})
		}
	}
	return out
}

func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}

func boundaryBefore(text string, i int) bool {
	r, _ := utf8.DecodeLastRuneInString(text[:i])
	return i == 0 || !isWordRune(r)
}

func boundaryAfter(text string, i int) bool {
	r, _ := utf8.DecodeRuneInString(text[i:])
	return i == len(text) || !isWordRune(r)
}

// domainOf is a URL's host, lowercased, without "www.". It is "" for a URL
// that does not parse.
func domainOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return normalizeHost(u.Hostname())
}

func normalizeHost(h string) string {
	return strings.TrimPrefix(strings.TrimSuffix(strings.ToLower(h), "."), "www.")
}

func onDomains(host string, domains []string) bool {
	if host == "" {
		return false
	}
	for _, d := range domains {
		d = normalizeHost(d)
		if d != "" && (host == d || strings.HasSuffix(host, "."+d)) {
			return true
		}
	}
	return false
}
