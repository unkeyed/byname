package changes

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/unkeyed/byname/internal/core"
)

// Rule maps changed files to a page URL: a file whose repo path matches
// Glob is the page URL, with {1}, {2} and so on replaced by what each
// wildcard matched, in order. In Glob, * matches within one path segment
// and ** matches any number of segments, including none.
type Rule struct {
	Glob string `json:"glob" jsonschema:"a repo path pattern such as content/blog/*.mdx or app/**/page.tsx"`
	URL  string `json:"url" jsonschema:"the page URL, with {1}, {2} for what each wildcard matched, such as https://www.example.com/blog/{1}"`
}

// maxRules keeps a rule list something a person can read.
const maxRules = 50

// compiled is a rule ready to match.
type compiled struct {
	Rule
	re *regexp.Regexp
}

var placeholder = regexp.MustCompile(`\{(\d+)\}`)

// compileRules parses rules at the boundary: every glob must be a valid
// pattern, and every URL template an absolute http(s) URL that names no
// wildcard the glob lacks.
func compileRules(rules []Rule) ([]compiled, error) {
	if len(rules) > maxRules {
		return nil, fmt.Errorf("%w: at most %d path rules", core.ErrInvalid, maxRules)
	}
	out := make([]compiled, 0, len(rules))
	for i, r := range rules {
		r.Glob, r.URL = strings.TrimSpace(r.Glob), strings.TrimSpace(r.URL)
		where := fmt.Sprintf("rule %d", i+1)
		if r.Glob == "" || strings.HasPrefix(r.Glob, "/") || len(r.Glob) > 300 {
			return nil, fmt.Errorf("%w: %s: glob must be a repo path such as content/blog/*.mdx, without a leading slash", core.ErrInvalid, where)
		}
		re, wildcards := globRegexp(r.Glob)
		for _, m := range placeholder.FindAllStringSubmatch(r.URL, -1) {
			if n, _ := strconv.Atoi(m[1]); n < 1 || n > wildcards {
				return nil, fmt.Errorf("%w: %s: the URL uses %s but the glob has %d wildcards", core.ErrInvalid, where, m[0], wildcards)
			}
		}
		u, err := url.Parse(placeholder.ReplaceAllString(r.URL, "x"))
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || len(r.URL) > 500 {
			return nil, fmt.Errorf("%w: %s: url must be an absolute page URL such as https://www.example.com/blog/{1}", core.ErrInvalid, where)
		}
		out = append(out, compiled{r, re})
	}
	return out, nil
}

// globRegexp turns a glob into an anchored regexp with one group per
// wildcard. A **/ group leaves out its trailing slash.
func globRegexp(glob string) (*regexp.Regexp, int) {
	var b strings.Builder
	b.WriteString("^")
	n := 0
	for i := 0; i < len(glob); {
		switch {
		case strings.HasPrefix(glob[i:], "**/"):
			b.WriteString(`(?:(.*)/)?`)
			i += 3
			n++
		case strings.HasPrefix(glob[i:], "**"):
			b.WriteString(`(.*)`)
			i += 2
			n++
		case glob[i] == '*':
			b.WriteString(`([^/]*)`)
			i++
			n++
		default:
			j := i
			for j < len(glob) && glob[j] != '*' {
				j++
			}
			b.WriteString(regexp.QuoteMeta(glob[i:j]))
			i = j
		}
	}
	b.WriteString("$")
	return regexp.MustCompile(b.String()), n
}

// pageFor is the page URL the first matching rule gives path, or "" when
// no rule matches.
func pageFor(rules []compiled, path string) string {
	for _, r := range rules {
		m := r.re.FindStringSubmatch(path)
		if m == nil {
			continue
		}
		page := placeholder.ReplaceAllStringFunc(r.URL, func(p string) string {
			n, _ := strconv.Atoi(p[1 : len(p)-1])
			return m[n]
		})
		// An empty ** match leaves a doubled slash in the path.
		scheme, rest, _ := strings.Cut(page, "://")
		for strings.Contains(rest, "//") {
			rest = strings.ReplaceAll(rest, "//", "/")
		}
		return scheme + "://" + rest
	}
	return ""
}
