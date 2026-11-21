package engines

import (
	"fmt"
	"net/url"
	"strings"
)

func failed(format string, args ...any) Result {
	return Result{Status: StatusFailed, Error: fmt.Sprintf(format, args...)}
}

// answered fills Status from the text: an answer endpoint that returned no
// text gave no answer, which is a failure, not a "not named".
func answered(r Result) Result {
	if strings.TrimSpace(r.Text) == "" {
		r.Status = StatusFailed
		r.Error = "response has no answer text"
		return r
	}
	r.Status = StatusOK
	return r
}

// citations returns urls in order with Google redirect links unwrapped,
// empties dropped and repeats removed.
func citations(urls []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, u := range urls {
		u = unwrapRedirect(strings.TrimSpace(u))
		if u == "" || seen[u] {
			continue
		}
		seen[u] = true
		out = append(out, u)
	}
	return out
}

// unwrapRedirect replaces a Google redirect link (google.<tld>/url?q=… or
// ?url=…) with its target. Any other link is returned unchanged.
func unwrapRedirect(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Path != "/url" {
		return raw
	}
	if !strings.HasPrefix(strings.TrimPrefix(strings.ToLower(u.Hostname()), "www."), "google.") {
		return raw
	}
	q := u.Query()
	for _, key := range []string{"q", "url"} {
		if t := q.Get(key); isWebURL(t) {
			return t
		}
	}
	return raw
}

// needsLookup reports whether a link is a redirect whose target is not in
// the URL, so only following it (a Resolver) can tell where it goes.
func needsLookup(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil &&
		strings.EqualFold(u.Hostname(), "vertexaisearch.cloud.google.com") &&
		strings.HasPrefix(u.Path, "/grounding-api-redirect/")
}

func isWebURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}
