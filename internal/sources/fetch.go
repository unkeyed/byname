package sources

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/unkeyed/byname/internal/panel"
)

// refusedError is the SSRF guard turning down an address.
type refusedError struct{ addr netip.Addr }

func (e *refusedError) Error() string {
	return fmt.Sprintf("%s is a private, loopback or otherwise non-public address", e.addr)
}

// publicAddr reports whether a page may be fetched from ip: a global
// unicast address outside the private, shared and documentation ranges.
func publicAddr(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, p := range nonPublic {
		if p.Contains(ip) {
			return false
		}
	}
	return true
}

var nonPublic = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"), // carrier-grade NAT
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"), // NAT64 can reach IPv4 private ranges
	netip.MustParsePrefix("2001:db8::/32"),
}

// newClient is the HTTP client every fetch uses. It never goes through an
// environment proxy, follows at most five redirects and only to http(s),
// and, unless allowPrivate, refuses to connect to a non-public address.
// The check runs on the address being dialled, after DNS, so a name that
// resolves to 127.0.0.1 or rebinds between lookups is refused too.
func newClient(allowPrivate bool) *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	if !allowPrivate {
		dialer.Control = func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ip, err := netip.ParseAddr(host)
			if err != nil || !publicAddr(ip) {
				return &refusedError{ip}
			}
			return nil
		}
	}
	return &http.Client{
		Transport: &http.Transport{
			Proxy:                 nil,
			DialContext:           dialer.DialContext,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 15 * time.Second,
			MaxIdleConnsPerHost:   1,
			IdleConnTimeout:       30 * time.Second,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("more than 5 redirects")
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return fmt.Errorf("redirect to a %s URL", req.URL.Scheme)
			}
			return nil
		},
	}
}

// page is one fetch's outcome, as source_page stores it.
type page struct {
	url        string
	domain     string
	status     Status
	httpStatus *int
	hash       *string
	self       *bool
	brands     []string
	err        string
}

type fetcher struct {
	client  *http.Client
	agent   string
	timeout time.Duration
	maxBody int64
	brands  []panel.Brand
	mode    panel.MatchMode
}

func (f *fetcher) get(ctx context.Context, u *url.URL, limit int64) (*http.Response, []byte, error) {
	ctx, cancel := context.WithTimeout(ctx, f.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", f.agent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,text/plain;q=0.9,*/*;q=0.1")
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	return resp, body, err
}

// robots reads a host's robots.txt. A missing one (4xx) allows everything.
// One that cannot be read is an error, and the host's pages wait for the
// next refresh rather than being fetched without it.
func (f *fetcher) robots(ctx context.Context, origin *url.URL) (robots, error) {
	resp, body, err := f.get(ctx, origin.JoinPath("/robots.txt"), 512<<10)
	switch {
	case err != nil:
		return robots{}, err
	case resp.StatusCode >= 400 && resp.StatusCode < 500:
		return allowAll, nil
	case resp.StatusCode >= 300:
		return robots{}, fmt.Errorf("robots.txt answered HTTP %d", resp.StatusCode)
	}
	return parseRobots(string(body), robotsToken), nil
}

// fetch reads one page, allowed by rules, and matches the brands in its
// text.
func (f *fetcher) fetch(ctx context.Context, u *url.URL, rules robots) page {
	p := page{url: u.String(), domain: hostOf(u), brands: []string{}}
	if !rules.allowed(u.RequestURI()) {
		p.status, p.err = StatusDisallowed, "robots.txt disallows this page"
		return p
	}
	resp, body, err := f.get(ctx, u, f.maxBody)
	if err != nil {
		return failure(p, err)
	}
	p.httpStatus = &resp.StatusCode
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		p.status, p.err = StatusFailed, fmt.Sprintf("HTTP %d", resp.StatusCode)
		return p
	}
	if int64(len(body)) > f.maxBody {
		p.status, p.err = StatusFailed, fmt.Sprintf("larger than %d KiB", f.maxBody>>10)
		return p
	}
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		if mt, _, _ := mime.ParseMediaType(ct); mt != "text/html" && mt != "application/xhtml+xml" && mt != "text/plain" {
			p.status, p.err = StatusFailed, fmt.Sprintf("not a web page (%s)", mt)
			return p
		}
	}
	sum := sha256.Sum256(body)
	hash := hex.EncodeToString(sum[:])
	self := false
	kinds := map[panel.BrandID]panel.Brand{}
	for _, b := range f.brands {
		kinds[b.ID] = b
	}
	for _, m := range panel.Named(htmlText(body), f.brands, f.mode) {
		if kinds[m.Brand].Kind == panel.KindSelf {
			self = true
		} else {
			p.brands = append(p.brands, kinds[m.Brand].Name)
		}
	}
	p.status, p.hash, p.self = StatusOK, &hash, &self
	return p
}

func failure(p page, err error) page {
	var refused *refusedError
	if errors.As(err, &refused) {
		p.status, p.err = StatusRefused, refused.Error()
		return p
	}
	p.status = StatusFailed
	var uerr *url.Error
	if errors.As(err, &uerr) {
		err = uerr.Err
	}
	if errors.Is(err, context.DeadlineExceeded) {
		p.err = "timed out"
	} else {
		p.err = err.Error()
	}
	return p
}

var (
	hiddenBlocks = regexp.MustCompile(`(?is)<!--.*?-->|<(script|style|noscript|template|svg)\b.*?</(script|style|noscript|template|svg)\s*>`)
	tags         = regexp.MustCompile(`(?s)<[^>]*>`)
	spaces       = regexp.MustCompile(`\s+`)
)

// htmlText is the text a reader sees in the HTML as served: tags, scripts
// and styles dropped and entities decoded. A page built in the browser can
// read as empty, which the UI says.
func htmlText(body []byte) string {
	s := hiddenBlocks.ReplaceAllString(string(body), " ")
	s = tags.ReplaceAllString(s, " ")
	return strings.TrimSpace(spaces.ReplaceAllString(html.UnescapeString(s), " "))
}

// hostOf is a URL's host, lowercased and without "www.", as citations
// store their domain.
func hostOf(u *url.URL) string {
	return strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
}
