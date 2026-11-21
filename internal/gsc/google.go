package gsc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/oauth2"

	"github.com/unkeyed/byname/internal/core"
)

// Scope is the one Google scope Byname asks for: read-only Search Console.
const Scope = "https://www.googleapis.com/auth/webmasters.readonly"

// rowLimit is the most rows Search Analytics returns per request.
const rowLimit = 25000

// Endpoints are where Byname reaches Google. Production uses Google's;
// tests and local development point all three at a fake.
type Endpoints struct {
	AuthURL  string // the OAuth consent page
	TokenURL string // the OAuth token endpoint, also for the service account's JWT grant
	APIBase  string // the Search Console API: sites, searchAnalytics and urlInspection
}

// GoogleEndpoints returns Google's endpoints, or a fake's at base, which
// serves the same paths from one origin.
func GoogleEndpoints(base string) Endpoints {
	if base == "" {
		return Endpoints{
			AuthURL:  "https://accounts.google.com/o/oauth2/v2/auth",
			TokenURL: "https://oauth2.googleapis.com/token",
			APIBase:  "https://searchconsole.googleapis.com",
		}
	}
	base = strings.TrimSuffix(base, "/")
	return Endpoints{AuthURL: base + "/o/oauth2/v2/auth", TokenURL: base + "/token", APIBase: base}
}

// googleAPI is the Search Console API with one credential's tokens.
type googleAPI struct {
	client *http.Client
	base   string
}

func (s *Service) api(ctx context.Context, ts oauth2.TokenSource) googleAPI {
	client := oauth2.NewClient(context.WithValue(ctx, oauth2.HTTPClient, s.http), ts)
	client.Timeout = 2 * time.Minute
	return googleAPI{client: client, base: s.endpoints.APIBase}
}

// Site is one Search Console property the credential can read.
type Site struct {
	URL        string `json:"site_url" jsonschema:"the property, such as sc-domain:example.com or https://www.example.com/"`
	Permission string `json:"permission_level" jsonschema:"Google's permission level, such as siteOwner or siteRestrictedUser"`
}

func (g googleAPI) sites(ctx context.Context) ([]Site, error) {
	var out struct {
		SiteEntry []struct {
			SiteURL         string `json:"siteUrl"`
			PermissionLevel string `json:"permissionLevel"`
		} `json:"siteEntry"`
	}
	if err := g.call(ctx, http.MethodGet, "/webmasters/v3/sites", nil, &out); err != nil {
		return nil, err
	}
	sites := []Site{}
	for _, e := range out.SiteEntry {
		if e.PermissionLevel == "siteUnverifiedUser" {
			continue
		}
		sites = append(sites, Site{URL: e.SiteURL, Permission: e.PermissionLevel})
	}
	return sites, nil
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
func (g googleAPI) queryAll(ctx context.Context, site string, from, to day, dims []string) (rows []queryRow, firstIncomplete string, err error) {
	req := queryRequest{StartDate: from.String(), EndDate: to.String(), Dimensions: dims, Type: "web", DataState: "all", RowLimit: rowLimit}
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
	return fmt.Errorf("%w: Google refused Byname's credential (%s); an admin can reconnect Search Console under Settings", core.ErrUpstream, why)
}
