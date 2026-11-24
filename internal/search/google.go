package search

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/jwt"

	"github.com/unkeyed/byname/internal/core"
)

var googleSpec = providerSpec{
	Info: Info{
		Provider: Google, Title: "Google", Product: "Search Console",
		Methods: []MethodInfo{
			{Method: MethodOAuth, Label: "Sign in with Google"},
			{Method: MethodServiceAccount, Label: "Service account", Steps: []string{
				"In Google Cloud, create a service account and download a JSON key for it.",
				"In Search Console, add the service account's email as a user on your site. Restricted permission is enough.",
				"Paste the whole key file.",
			}},
		},
		StepDays: 1, SitePosition: true, BackfillMonths: 16,
		Notes: []string{
			"Google leaves rare searches out of its lists to keep searchers anonymous. Their clicks count as unlisted.",
			"Days are Pacific Time, as Google counts them.",
		},
	},
	zone: pacific, keyMethod: MethodServiceAccount,
	build: func(o Options) adapter {
		return &google{http: o.HTTPClient, endpoints: googleEndpoints(o.BaseURLs[Google]), client: o.GoogleClient, publicURL: o.PublicURL}
	},
}

// pacific is Search Console's day: every date Google reports is a Pacific
// Time calendar day.
var pacific = func() *time.Location {
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		return time.FixedZone("PT", -8*3600)
	}
	return loc
}()

// GoogleScope is the one Google scope Byname asks for: read-only Search
// Console.
const GoogleScope = "https://www.googleapis.com/auth/webmasters.readonly"

// rowLimit is the most rows Search Analytics returns per request.
const rowLimit = 25000

// chunkDays is how many days one round of requests covers, so a 16-month
// backfill commits as it goes and resumes where it stopped.
const chunkDays = 30

// endpoints are where Byname reaches Google. Production uses Google's;
// tests and local development point all three at a fake.
type endpoints struct {
	authURL  string // the OAuth consent page
	tokenURL string // the OAuth token endpoint, also for the service account's JWT grant
	apiBase  string // the Search Console API: sites, searchAnalytics and urlInspection
}

// googleEndpoints returns Google's endpoints, or a fake's at base, which
// serves the same paths from one origin.
func googleEndpoints(base string) endpoints {
	if base == "" {
		return endpoints{
			authURL:  "https://accounts.google.com/o/oauth2/v2/auth",
			tokenURL: "https://oauth2.googleapis.com/token",
			apiBase:  "https://searchconsole.googleapis.com",
		}
	}
	base = strings.TrimSuffix(base, "/")
	return endpoints{authURL: base + "/o/oauth2/v2/auth", tokenURL: base + "/token", apiBase: base}
}

// google is Search Console's adapter.
type google struct {
	http      *http.Client
	endpoints endpoints
	client    OAuthClient
	publicURL *url.URL
}

func (g *google) httpContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, oauth2.HTTPClient, g.http)
}

func (g *google) oauthConfig() *oauth2.Config {
	return &oauth2.Config{
		ClientID:     g.client.ID,
		ClientSecret: g.client.Secret,
		Endpoint:     oauth2.Endpoint{AuthURL: g.endpoints.authURL, TokenURL: g.endpoints.tokenURL, AuthStyle: oauth2.AuthStyleInParams},
		RedirectURL:  g.publicURL.JoinPath("/auth/gsc/callback").String(),
		Scopes:       []string{GoogleScope},
	}
}

func (g *google) jwtConfig(k serviceAccount) *jwt.Config {
	return &jwt.Config{Email: k.ClientEmail, PrivateKey: []byte(k.PrivateKey), PrivateKeyID: k.PrivateKeyID, Scopes: []string{GoogleScope}, TokenURL: g.endpoints.tokenURL}
}

// tokens gets access tokens from the stored secret: a refresh token, or a
// service account's JSON key.
func (g *google) tokens(ctx context.Context, cred credential) (oauth2.TokenSource, error) {
	if cred.method == MethodOAuth {
		return g.oauthConfig().TokenSource(g.httpContext(ctx), &oauth2.Token{RefreshToken: cred.secret}), nil
	}
	key, err := parseServiceAccount(cred.secret)
	if err != nil {
		return nil, err
	}
	return g.jwtConfig(key).TokenSource(g.httpContext(ctx)), nil
}

// api is the Search Console API with cred's tokens.
func (g *google) api(ctx context.Context, cred credential) (googleAPI, error) {
	ts, err := g.tokens(ctx, cred)
	if err != nil {
		return googleAPI{}, err
	}
	client := oauth2.NewClient(g.httpContext(ctx), ts)
	client.Timeout = 2 * time.Minute
	return googleAPI{client: client, base: g.endpoints.apiBase}, nil
}

// serviceAccount is the part of a service account's JSON key Byname uses.
// The key's own token_uri is ignored: the assertion only ever goes to
// Google's token endpoint.
type serviceAccount struct {
	Type         string `json:"type"`
	ClientEmail  string `json:"client_email"`
	PrivateKey   string `json:"private_key"`
	PrivateKeyID string `json:"private_key_id"`
}

func parseServiceAccount(raw string) (serviceAccount, error) {
	var k serviceAccount
	if err := json.Unmarshal([]byte(raw), &k); err != nil {
		return k, fmt.Errorf("%w: key must be the service account's JSON key file as Google downloads it: %v", core.ErrInvalid, err)
	}
	if k.Type != "service_account" || !strings.Contains(k.ClientEmail, "@") || k.PrivateKey == "" {
		return k, fmt.Errorf("%w: key must be a service account's JSON key, with type service_account, client_email and private_key", core.ErrInvalid)
	}
	block, _ := pem.Decode([]byte(k.PrivateKey))
	if block == nil {
		return k, fmt.Errorf("%w: the key's private_key is not a PEM private key", core.ErrInvalid)
	}
	if _, err := x509.ParsePKCS8PrivateKey(block.Bytes); err != nil {
		if _, err := x509.ParsePKCS1PrivateKey(block.Bytes); err != nil {
			return k, fmt.Errorf("%w: the key's private_key is not an RSA private key", core.ErrInvalid)
		}
	}
	return k, nil
}

// verify checks that Google accepts the service account's JSON key and
// returns its email. Add that email as a user on the site in Search
// Console first.
func (g *google) verify(ctx context.Context, key string) (string, error) {
	k, err := parseServiceAccount(key)
	if err != nil {
		return "", err
	}
	if _, err := g.jwtConfig(k).TokenSource(g.httpContext(ctx)).Token(); err != nil {
		if re := (*oauth2.RetrieveError)(nil); errors.As(err, &re) {
			return "", tokenError(re)
		}
		return "", fmt.Errorf("%w: Google's token endpoint did not answer: %v", core.ErrUpstream, err)
	}
	return k.ClientEmail, nil
}

// SiteKind is a Search Console property type.
type SiteKind string

const (
	Domain    SiteKind = "domain"     // sc-domain:example.com, every protocol and subdomain
	URLPrefix SiteKind = "url_prefix" // https://www.example.com/, one protocol and host
)

// kindOf parses a property as Search Console names it.
func kindOf(site string) (SiteKind, bool) {
	if host, ok := strings.CutPrefix(site, "sc-domain:"); ok && host != "" && !strings.ContainsAny(host, "/: ") {
		return Domain, true
	}
	u, err := url.Parse(site)
	if err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" && strings.HasSuffix(site, "/") {
		return URLPrefix, true
	}
	return "", false
}

// sites lists the properties cred can read, from Google's sites.list.
func (g *google) sites(ctx context.Context, cred credential) ([]Site, error) {
	api, err := g.api(ctx, cred)
	if err != nil {
		return nil, err
	}
	var out struct {
		SiteEntry []struct {
			SiteURL         string `json:"siteUrl"`
			PermissionLevel string `json:"permissionLevel"`
		} `json:"siteEntry"`
	}
	if err := api.call(ctx, http.MethodGet, "/webmasters/v3/sites", nil, &out); err != nil {
		return nil, err
	}
	sites := []Site{}
	for _, e := range out.SiteEntry {
		kind, ok := kindOf(e.SiteURL)
		if e.PermissionLevel == "siteUnverifiedUser" || !ok {
			continue
		}
		sites = append(sites, Site{URL: e.SiteURL, Kind: kind, Permission: e.PermissionLevel})
	}
	return sites, nil
}

// pull fetches want in chunkDays rounds, oldest first, so a long backfill
// commits as it goes.
func (g *google) pull(ctx context.Context, cred credential, site string, want span, put func(report) error) error {
	api, err := g.api(ctx, cred)
	if err != nil {
		return err
	}
	for from := want.from; !want.to.before(from); from = from.add(chunkDays) {
		to := from.add(chunkDays - 1)
		if want.to.before(to) {
			to = want.to
		}
		if err := api.pullChunk(ctx, site, span{from, to}, put); err != nil {
			return fmt.Errorf("sync %s to %s: %w", from, to, err)
		}
	}
	return nil
}

// pullChunk fetches and hands over each dimension for w in turn, so a sync
// holds one in memory at a time. Days on or after the earliest
// firstIncompleteDate a response reported are partial.
func (g googleAPI) pullChunk(ctx context.Context, site string, w span, put func(report) error) error {
	finalBefore := w.to.add(1).String()
	fetch := func(dims ...string) ([]queryRow, error) {
		rows, first, err := g.queryAll(ctx, site, w, dims)
		if first != "" && first < finalBefore {
			finalBefore = first
		}
		return rows, err
	}
	sums, err := fetch("date")
	if err != nil {
		return err
	}
	for _, d := range []struct {
		dim dim
		key string
	}{{queries, "query"}, {pages, "page"}} {
		rs, err := fetch("date", d.key)
		if err != nil {
			return err
		}
		if err := put(report{dim: d.dim, span: w, rows: googleRows(rs, finalBefore)}); err != nil {
			return err
		}
	}
	return put(report{dim: totals, span: w, rows: googleRows(sums, finalBefore)})
}

// googleRows turns Search Analytics rows keyed by date, and by query or
// page when asked, into rows of one day each.
func googleRows(in []queryRow, finalBefore string) []row {
	out := make([]row, 0, len(in))
	for _, r := range in {
		if len(r.Keys) == 0 {
			continue
		}
		t, err := time.Parse(time.DateOnly, r.Keys[0])
		if err != nil {
			continue
		}
		d := day{t}
		var key string
		if len(r.Keys) > 1 {
			key = r.Keys[1]
		}
		out = append(out, row{first: d, last: d, key: key, clicks: int64(math.Round(r.Clicks)), impressions: int64(math.Round(r.Impressions)),
			position: r.Position, final: r.Keys[0] < finalBefore})
	}
	return out
}

// googleAPI is the Search Console API with one credential's tokens.
type googleAPI struct {
	client *http.Client
	base   string
}

// queryRequest is searchAnalytics.query's body.
type queryRequest struct {
	StartDate  string   `json:"startDate"`
	EndDate    string   `json:"endDate"`
	Dimensions []string `json:"dimensions"`
	Type       string   `json:"type"`
	DataState  string   `json:"dataState"`
	RowLimit   int      `json:"rowLimit"`
	StartRow   int      `json:"startRow"`
}

type queryRow struct {
	Keys        []string `json:"keys"`
	Clicks      float64  `json:"clicks"`
	Impressions float64  `json:"impressions"`
	Position    float64  `json:"position"`
}

type queryResponse struct {
	Rows     []queryRow `json:"rows"`
	Metadata struct {
		FirstIncompleteDate string `json:"firstIncompleteDate"`
	} `json:"metadata"`
}

// queryAll pages through searchAnalytics.query, rowLimit rows at a time,
// for web search with fresh data included. firstIncomplete is the earliest
// firstIncompleteDate any page reported, "" when every day is final.
func (g googleAPI) queryAll(ctx context.Context, site string, w span, dims []string) (rows []queryRow, firstIncomplete string, err error) {
	req := queryRequest{StartDate: w.from.String(), EndDate: w.to.String(), Dimensions: dims, Type: "web", DataState: "all", RowLimit: rowLimit}
	for {
		var page queryResponse
		if err := g.call(ctx, http.MethodPost, "/webmasters/v3/sites/"+url.QueryEscape(site)+"/searchAnalytics/query", req, &page); err != nil {
			return nil, "", err
		}
		if f := page.Metadata.FirstIncompleteDate; f != "" && (firstIncomplete == "" || f < firstIncomplete) {
			firstIncomplete = f
		}
		rows = append(rows, page.Rows...)
		if len(page.Rows) < rowLimit {
			return rows, firstIncomplete, nil
		}
		req.StartRow += len(page.Rows)
	}
}

// Inspection is what Google's URL Inspection reports about one URL in the
// connected property's index.
type Inspection struct {
	URL string `json:"url"`
	// Verdict is PASS, PARTIAL, FAIL or NEUTRAL.
	Verdict string `json:"verdict"`
	// CoverageState is Google's words, such as "Submitted and indexed".
	CoverageState string `json:"coverage_state"`
	// LastCrawl is nil when Google has not crawled the URL.
	LastCrawl       *time.Time `json:"last_crawl"`
	GoogleCanonical string     `json:"google_canonical"`
	UserCanonical   string     `json:"user_canonical"`
}

func (g googleAPI) inspect(ctx context.Context, site, pageURL string) (Inspection, error) {
	var out struct {
		InspectionResult struct {
			IndexStatusResult struct {
				Verdict         string `json:"verdict"`
				CoverageState   string `json:"coverageState"`
				LastCrawlTime   string `json:"lastCrawlTime"`
				GoogleCanonical string `json:"googleCanonical"`
				UserCanonical   string `json:"userCanonical"`
			} `json:"indexStatusResult"`
		} `json:"inspectionResult"`
	}
	body := map[string]string{"inspectionUrl": pageURL, "siteUrl": site, "languageCode": "en-US"}
	if err := g.call(ctx, http.MethodPost, "/v1/urlInspection/index:inspect", body, &out); err != nil {
		return Inspection{}, err
	}
	r := out.InspectionResult.IndexStatusResult
	in := Inspection{URL: pageURL, Verdict: r.Verdict, CoverageState: r.CoverageState, GoogleCanonical: r.GoogleCanonical, UserCanonical: r.UserCanonical}
	if r.LastCrawlTime != "" {
		t, err := time.Parse(time.RFC3339, r.LastCrawlTime)
		if err != nil {
			return Inspection{}, fmt.Errorf("%w: URL Inspection sent lastCrawlTime %q, which is not an RFC 3339 time", core.ErrUpstream, r.LastCrawlTime)
		}
		in.LastCrawl = &t
	}
	return in, nil
}

// call sends one API request and decodes the JSON answer. Google's own
// errors become core.ErrUpstream with Google's message, which says what to
// fix, such as a property the account cannot read.
func (g googleAPI) call(ctx context.Context, method, path string, body, out any) error {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, g.base+path, r)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := g.client.Do(req)
	if err != nil {
		if re := (*oauth2.RetrieveError)(nil); errors.As(err, &re) {
			return tokenError(re)
		}
		return fmt.Errorf("%w: Search Console did not answer: %v", core.ErrUpstream, err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 64<<20))
	if err != nil {
		return fmt.Errorf("%w: reading Search Console's answer: %v", core.ErrUpstream, err)
	}
	if res.StatusCode != http.StatusOK {
		var e struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		msg := strings.TrimSpace(string(raw))
		if json.Unmarshal(raw, &e) == nil && e.Error.Message != "" {
			msg = e.Error.Message
		}
		if len(msg) > 300 {
			msg = msg[:300]
		}
		return fmt.Errorf("%w: Search Console answered %d: %s", core.ErrUpstream, res.StatusCode, msg)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("%w: Search Console sent something that is not the expected JSON: %v", core.ErrUpstream, err)
	}
	return nil
}

// tokenError turns Google refusing a token into a message that says what
// to do, without echoing anything secret.
func tokenError(re *oauth2.RetrieveError) error {
	why := re.ErrorCode
	if re.ErrorDescription != "" {
		why += ": " + re.ErrorDescription
	}
	if why == "" {
		why = fmt.Sprintf("status %d", re.Response.StatusCode)
	}
	return fmt.Errorf("%w: Google refused Byname's credential (%s); an admin can reconnect Google under Settings > Search engines", core.ErrUpstream, why)
}
