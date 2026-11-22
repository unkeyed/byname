package panel

import (
	"strings"
	"unicode"
)

// NameKey is a brand name normalised so the spellings of one brand meet:
// "Vercel", "Vercel Inc.", "Vercel, Inc", "vercel.com" and "Vercel's" are
// all "vercel". It lowercases, takes a domain down to its name, drops a
// possessive, a leading "the" and trailing company suffixes, and keeps
// only letters, digits, "&" and "+" between single spaces. It is "" when
// nothing is left.
func NameKey(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	for _, p := range []string{"'s", "’s"} {
		s = strings.TrimSuffix(s, p)
	}
	s = DisplayName(s)
	if host, ok := hostOf(s); ok {
		s = domainName(host)
	}
	words := strings.FieldsFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '&' && r != '+'
	})
	if len(words) > 1 && words[0] == "the" {
		words = words[1:]
	}
	for len(words) > 1 && companySuffixes[words[len(words)-1]] {
		words = words[:len(words)-1]
	}
	return strings.Join(words, " ")
}

// DisplayName is how a found brand is shown and matched once tracked: the
// name as written, without a trailing company suffix ("Vercel, Inc." is
// "Vercel"), since answers rarely repeat the suffix.
func DisplayName(name string) string {
	s := strings.Join(strings.Fields(name), " ")
	for {
		cut := strings.LastIndexAny(s, " ,")
		if cut < 0 || !companySuffixes[strings.ToLower(strings.Trim(s[cut+1:], "."))] {
			return s
		}
		s = strings.TrimRight(s[:cut], " ,")
	}
}

// companySuffixes are the legal-form words a brand name can end with.
var companySuffixes = map[string]bool{
	"inc": true, "incorporated": true, "llc": true, "ltd": true, "limited": true, "corp": true, "corporation": true,
	"co": true, "company": true, "gmbh": true, "plc": true, "ag": true, "sa": true, "sas": true, "bv": true,
	"nv": true, "oy": true, "ab": true, "as": true, "pty": true, "srl": true, "spa": true, "kk": true,
}

// hostOf reads s as a bare domain or URL, such as "render.com" or
// "https://www.render.com/pricing", when its last label is a known
// top-level domain, so "Node.js" is not a domain.
func hostOf(s string) (string, bool) {
	if strings.ContainsAny(s, " \t") {
		return "", false
	}
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	if i := strings.IndexAny(s, "/?#:"); i >= 0 {
		s = s[:i]
	}
	s = normalizeHost(s)
	i := strings.LastIndexByte(s, '.')
	if i <= 0 || !topLevel[s[i+1:]] {
		return "", false
	}
	return s, true
}

// domainName is the label a domain is registered under: "aws.amazon.com"
// is "amazon", "shop.example.co.uk" is "example".
func domainName(host string) string {
	labels := strings.Split(host, ".")
	if len(labels) < 2 {
		return host
	}
	n := len(labels) - 1
	if n >= 2 && secondLevel[labels[n-1]] {
		n--
	}
	return labels[n-1]
}

// topLevel are the top-level domains a brand's site is likely on. A name
// ending in anything else, such as "Node.js", is read as a name.
var topLevel = map[string]bool{
	"com": true, "net": true, "org": true, "io": true, "ai": true, "co": true, "app": true, "dev": true, "so": true,
	"cloud": true, "tech": true, "xyz": true, "me": true, "us": true, "uk": true, "de": true, "fr": true, "es": true,
	"it": true, "nl": true, "se": true, "no": true, "dk": true, "fi": true, "ch": true, "at": true, "be": true,
	"ca": true, "au": true, "nz": true, "jp": true, "in": true, "br": true, "mx": true, "eu": true, "sh": true,
	"gg": true, "to": true, "fm": true, "tv": true, "inc": true, "store": true, "shop": true, "site": true,
	"online": true, "agency": true, "studio": true, "example": true,
}

// secondLevel are the labels of two-part public suffixes such as co.uk.
var secondLevel = map[string]bool{"co": true, "com": true, "org": true, "net": true, "ac": true, "gov": true}

// Covers reports whether a found name or domain is one of the tracked
// brands under another spelling: its key is a tracked name's or alias's,
// its text names a tracked brand ("Contoso Edge Functions"), or its domain
// is on a tracked brand's domains, by the match mode answers are scored
// with. A covered brand is never listed as one you don't track.
func Covers(tracked []Brand, name, domain string, mode MatchMode) bool {
	key := NameKey(name)
	host := normalizeHost(domain)
	for _, b := range tracked {
		for _, n := range append([]string{b.Name}, b.Aliases...) {
			if k := NameKey(n); k != "" && k == key {
				return true
			}
		}
		for _, d := range b.Domains {
			if d = normalizeHost(d); d != "" && (key == domainName(d) || onDomains(host, []string{d})) {
				return true
			}
		}
	}
	return len(Named(name, tracked, mode)) > 0
}
