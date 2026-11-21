// Package gsctest is a fake of the Google endpoints Byname's Search
// Console connection calls: the OAuth consent page and token endpoint
// (authorization code with PKCE, refresh token, and the service account's
// JWT bearer grant), sites.list, searchAnalytics.query with startRow paging
// and firstIncompleteDate, and urlInspection.index.inspect. Response shapes
// follow Google's API reference. fake.go uses only the standard library.
package gsctest

import (
	"cmp"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
)

// Scope is Search Console's read-only scope.
const Scope = "https://www.googleapis.com/auth/webmasters.readonly"

// Row is one day, query and page as Search Console counts it. An empty
// Query is an anonymized query: it counts toward site and page totals but
// never appears in a response grouped by query, as in Google's API.
type Row struct {
	Date        string
	Query       string
	Page        string
	Clicks      int
	Impressions int
	Position    float64
}

// Site is a property the fake account can see.
type Site struct {
	URL        string `json:"siteUrl"`
	Permission string `json:"permissionLevel"`
}

// Inspection is what urlInspection reports for one URL.
type Inspection struct {
	Verdict         string
	CoverageState   string
	LastCrawl       time.Time
	GoogleCanonical string
	UserCanonical   string
}

// Query is one searchAnalytics.query request the fake served.
type Query struct {
	Site       string
	Dimensions []string
	StartDate  string
	EndDate    string
	StartRow   int
	RowLimit   int
	DataState  string
	Rows       int
}

// Fake is the fake Google. Set its fields before serving; change data
// between calls with the methods, which lock.
type Fake struct {
	ClientID     string
	ClientSecret string
	// ServiceAccounts maps a service account email to its public key.
	ServiceAccounts map[string]*rsa.PublicKey
	// DenyConsent makes the consent page answer error=access_denied.
	DenyConsent bool
	// OmitRefreshToken makes the code exchange return no refresh token.
	OmitRefreshToken bool

	mu              sync.Mutex
	sites           []Site
	rows            map[string][]Row
	firstIncomplete string
	inspections     map[string]Inspection
	codes           map[string]grant
	refresh         map[string]bool
	access          map[string]bool
	queries         []Query
	tokenCalls      []url.Values
}

type grant struct {
	challenge, redirect string
	offline             bool
}

// NewFake returns a fake whose OAuth client is id and secret.
func NewFake(id, secret string) *Fake {
	return &Fake{
		ClientID: id, ClientSecret: secret, ServiceAccounts: map[string]*rsa.PublicKey{},
		rows: map[string][]Row{}, inspections: map[string]Inspection{},
		codes: map[string]grant{}, refresh: map[string]bool{}, access: map[string]bool{},
	}
}

// AddSite lets the account see site with Google's permission level, such
// as siteOwner.
func (f *Fake) AddSite(site, permission string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sites = append(f.sites, Site{site, permission})
}

// SetRows replaces site's rows for every date the given rows cover.
func (f *Fake) SetRows(site string, rows ...Row) {
	f.mu.Lock()
	defer f.mu.Unlock()
	dates := map[string]bool{}
	for _, r := range rows {
		dates[r.Date] = true
	}
	kept := slices.DeleteFunc(f.rows[site], func(r Row) bool { return dates[r.Date] })
	f.rows[site] = append(kept, rows...)
}

// SetFirstIncomplete sets the first day Google still revises; "" marks
// every day final.
func (f *Fake) SetFirstIncomplete(day string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.firstIncomplete = day
}

// SetInspection sets what urlInspection reports for pageURL.
func (f *Fake) SetInspection(pageURL string, in Inspection) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.inspections[pageURL] = in
}

// Queries returns every searchAnalytics.query request served so far.
func (f *Fake) Queries() []Query {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.queries)
}

// TokenCalls returns the form of every token request so far.
func (f *Fake) TokenCalls() []url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.tokenCalls)
}

// Handler serves Google's paths: /o/oauth2/v2/auth, /token,
// /webmasters/v3/sites, /webmasters/v3/sites/{site}/searchAnalytics/query
// and /v1/urlInspection/index:inspect.
func (f *Fake) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /o/oauth2/v2/auth", f.consent)
	mux.HandleFunc("POST /token", f.token)
	mux.HandleFunc("GET /webmasters/v3/sites", f.authed(f.listSites))
	mux.HandleFunc("POST /webmasters/v3/sites/{site}/searchAnalytics/query", f.authed(f.query))
	mux.HandleFunc("POST /v1/urlInspection/index:inspect", f.authed(f.inspect))
	return mux
}

func random() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// consent stands in for Google's consent page: it approves at once and
// sends the browser back with a code, as a person clicking Allow would.
func (f *Fake) consent(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	redirect, err := url.Parse(q.Get("redirect_uri"))
	if err != nil || redirect.Scheme == "" || q.Get("client_id") != f.ClientID {
		http.Error(w, "Error 400: invalid_request (unknown client or redirect_uri)", http.StatusBadRequest)
		return
	}
	back := redirect.Query()
	back.Set("state", q.Get("state"))
	switch {
	case f.DenyConsent:
		back.Set("error", "access_denied")
	case q.Get("response_type") != "code" || !slices.Contains(strings.Fields(q.Get("scope")), Scope) || q.Get("code_challenge_method") != "S256":
		back.Set("error", "invalid_request")
	default:
		code := "4/" + random()
		f.mu.Lock()
		f.codes[code] = grant{challenge: q.Get("code_challenge"), redirect: redirect.String(), offline: q.Get("access_type") == "offline"}
		f.mu.Unlock()
		back.Set("code", code)
		back.Set("scope", Scope)
	}
	redirect.RawQuery = back.Encode()
	http.Redirect(w, r, redirect.String(), http.StatusFound)
}

func tokenError(w http.ResponseWriter, code, desc string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code, "error_description": desc})
}

func (f *Fake) token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		tokenError(w, "invalid_request", "bad form")
		return
	}
	f.mu.Lock()
	f.tokenCalls = append(f.tokenCalls, r.PostForm)
	f.mu.Unlock()
	form := r.PostForm
	id, secret, basic := r.BasicAuth()
	if !basic {
		id, secret = form.Get("client_id"), form.Get("client_secret")
	}
	out := map[string]any{"token_type": "Bearer", "expires_in": 3599, "scope": Scope}
	switch form.Get("grant_type") {
	case "authorization_code":
		if id != f.ClientID || secret != f.ClientSecret {
			tokenError(w, "invalid_client", "Unauthorized")
			return
		}
		f.mu.Lock()
		g, ok := f.codes[form.Get("code")]
		delete(f.codes, form.Get("code"))
		f.mu.Unlock()
		sum := sha256.Sum256([]byte(form.Get("code_verifier")))
		switch {
		case !ok:
			tokenError(w, "invalid_grant", "Malformed auth code.")
			return
		case base64.RawURLEncoding.EncodeToString(sum[:]) != g.challenge:
			tokenError(w, "invalid_grant", "Invalid code verifier.")
			return
		case form.Get("redirect_uri") != g.redirect:
			tokenError(w, "redirect_uri_mismatch", "Bad Request")
			return
		}
		if g.offline && !f.OmitRefreshToken {
			rt := "1//" + random()
			f.mu.Lock()
			f.refresh[rt] = true
			f.mu.Unlock()
			out["refresh_token"] = rt
		}
	case "refresh_token":
		f.mu.Lock()
		ok := f.refresh[form.Get("refresh_token")]
		f.mu.Unlock()
		if id != f.ClientID || secret != f.ClientSecret {
			tokenError(w, "invalid_client", "Unauthorized")
			return
		}
		if !ok {
			tokenError(w, "invalid_grant", "Token has been expired or revoked.")
			return
		}
	case "urn:ietf:params:oauth:grant-type:jwt-bearer":
		if err := f.checkAssertion(form.Get("assertion"), "http://"+r.Host+"/token"); err != nil {
			tokenError(w, "invalid_grant", err.Error())
			return
		}
	default:
		tokenError(w, "unsupported_grant_type", "Invalid grant_type: "+form.Get("grant_type"))
		return
	}
	at := "ya29." + random()
	f.mu.Lock()
	f.access[at] = true
	f.mu.Unlock()
	out["access_token"] = at
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// checkAssertion verifies a service account's RS256-signed JWT the way
// Google's token endpoint does: a known issuer, its signature, the scope,
// the audience and the expiry.
func (f *Fake) checkAssertion(assertion, aud string) error {
	parts := strings.Split(assertion, ".")
	if len(parts) != 3 {
		return fmt.Errorf("Invalid JWT: malformed")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return fmt.Errorf("Invalid JWT: payload")
	}
	var claims struct {
		Iss   string `json:"iss"`
		Scope string `json:"scope"`
		Aud   string `json:"aud"`
		Exp   int64  `json:"exp"`
	}
	if err := json.Unmarshal(raw, &claims); err != nil {
		return fmt.Errorf("Invalid JWT: claims")
	}
	pub, ok := f.ServiceAccounts[claims.Iss]
	if !ok {
		return fmt.Errorf("Invalid JWT Signature.")
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err != nil || rsa.VerifyPKCS1v15(pub, crypto.SHA256, sum[:], sig) != nil {
		return fmt.Errorf("Invalid JWT Signature.")
	}
	switch {
	case !slices.Contains(strings.Fields(claims.Scope), Scope):
		return fmt.Errorf("Invalid OAuth scope or ID token audience provided.")
	case claims.Aud != aud:
		return fmt.Errorf("Invalid JWT: Audience mismatch")
	case claims.Exp < time.Now().Unix():
		return fmt.Errorf("Invalid JWT: Token must be a short-lived token and in a reasonable timeframe.")
	}
	return nil
}

// googleError writes Google's API error shape.
func googleError(w http.ResponseWriter, code int, status, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": code, "message": msg, "status": status}})
}

func (f *Fake) authed(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		f.mu.Lock()
		ok := f.access[token]
		f.mu.Unlock()
		if !ok {
			googleError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "Request had invalid authentication credentials. Expected OAuth 2 access token.")
			return
		}
		next(w, r)
	}
}

func (f *Fake) listSites(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	out := map[string]any{}
	if len(f.sites) > 0 {
		out["siteEntry"] = slices.Clone(f.sites)
	}
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func (f *Fake) canRead(site string) bool {
	return slices.ContainsFunc(f.sites, func(s Site) bool { return s.URL == site && s.Permission != "siteUnverifiedUser" })
}

type aggregate struct {
	keys                []string
	clicks, impressions int
	weighted            float64
}

func (f *Fake) query(w http.ResponseWriter, r *http.Request) {
	site := r.PathValue("site")
	var req struct {
		StartDate  string   `json:"startDate"`
		EndDate    string   `json:"endDate"`
		Dimensions []string `json:"dimensions"`
		Type       string   `json:"type"`
		DataState  string   `json:"dataState"`
		RowLimit   int      `json:"rowLimit"`
		StartRow   int      `json:"startRow"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		googleError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "Invalid JSON payload received.")
		return
	}
	_, e1 := time.Parse(time.DateOnly, req.StartDate)
	_, e2 := time.Parse(time.DateOnly, req.EndDate)
	if e1 != nil || e2 != nil {
		googleError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "Request contains an invalid argument.")
		return
	}
	for _, d := range req.Dimensions {
		if !slices.Contains([]string{"date", "query", "page", "country", "device", "searchAppearance"}, d) {
			googleError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "Request contains an invalid argument.")
			return
		}
	}
	limit := cmp.Or(req.RowLimit, 1000)
	if limit < 1 || limit > 25000 {
		googleError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "rowLimit must be between 1 and 25000.")
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.canRead(site) {
		googleError(w, http.StatusForbidden, "PERMISSION_DENIED", fmt.Sprintf("User does not have sufficient permission for site '%s'. See also: https://support.google.com/webmasters/answer/2451999.", site))
		return
	}
	byQuery := slices.Contains(req.Dimensions, "query")
	groups := map[string]*aggregate{}
	for _, row := range f.rows[site] {
		if row.Date < req.StartDate || row.Date > req.EndDate || (byQuery && row.Query == "") {
			continue
		}
		if req.DataState != "all" && f.firstIncomplete != "" && row.Date >= f.firstIncomplete {
			continue
		}
		var keys []string
		for _, d := range req.Dimensions {
			keys = append(keys, map[string]string{"date": row.Date, "query": row.Query, "page": row.Page}[d])
		}
		id := strings.Join(keys, "\x00")
		a := groups[id]
		if a == nil {
			a = &aggregate{keys: keys}
			groups[id] = a
		}
		a.clicks += row.Clicks
		a.impressions += row.Impressions
		a.weighted += row.Position * float64(row.Impressions)
	}
	all := make([]*aggregate, 0, len(groups))
	for _, a := range groups {
		all = append(all, a)
	}
	slices.SortFunc(all, func(a, b *aggregate) int {
		return cmp.Or(cmp.Compare(b.clicks, a.clicks), cmp.Compare(b.impressions, a.impressions), slices.Compare(a.keys, b.keys))
	})
	page := all[min(req.StartRow, len(all)):min(req.StartRow+limit, len(all))]
	type outRow struct {
		Keys        []string `json:"keys,omitempty"`
		Clicks      float64  `json:"clicks"`
		Impressions float64  `json:"impressions"`
		CTR         float64  `json:"ctr"`
		Position    float64  `json:"position"`
	}
	resp := map[string]any{"responseAggregationType": "byProperty"}
	if slices.Contains(req.Dimensions, "page") {
		resp["responseAggregationType"] = "byPage"
	}
	if len(page) > 0 {
		rows := make([]outRow, 0, len(page))
		for _, a := range page {
			o := outRow{Keys: a.keys, Clicks: float64(a.clicks), Impressions: float64(a.impressions)}
			if a.impressions > 0 {
				o.CTR = float64(a.clicks) / float64(a.impressions)
				o.Position = a.weighted / float64(a.impressions)
			}
			rows = append(rows, o)
		}
		resp["rows"] = rows
	}
	if req.DataState == "all" && f.firstIncomplete != "" && f.firstIncomplete <= req.EndDate && slices.Contains(req.Dimensions, "date") {
		resp["metadata"] = map[string]string{"firstIncompleteDate": f.firstIncomplete}
	}
	f.queries = append(f.queries, Query{Site: site, Dimensions: req.Dimensions, StartDate: req.StartDate, EndDate: req.EndDate,
		StartRow: req.StartRow, RowLimit: limit, DataState: req.DataState, Rows: len(page)})
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (f *Fake) inspect(w http.ResponseWriter, r *http.Request) {
	var req struct {
		InspectionURL string `json:"inspectionUrl"`
		SiteURL       string `json:"siteUrl"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.InspectionURL == "" {
		googleError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "Request contains an invalid argument.")
		return
	}
	f.mu.Lock()
	in, known := f.inspections[req.InspectionURL]
	ok := f.canRead(req.SiteURL)
	f.mu.Unlock()
	if !ok {
		googleError(w, http.StatusForbidden, "PERMISSION_DENIED", "You do not own this site, or the inspected URL is not part of this property.")
		return
	}
	status := map[string]any{"verdict": "NEUTRAL", "coverageState": "URL is unknown to Google", "robotsTxtState": "ROBOTS_TXT_STATE_UNSPECIFIED",
		"indexingState": "INDEXING_STATE_UNSPECIFIED", "pageFetchState": "PAGE_FETCH_STATE_UNSPECIFIED"}
	if known {
		status = map[string]any{"verdict": in.Verdict, "coverageState": in.CoverageState, "robotsTxtState": "ALLOWED",
			"indexingState": "INDEXING_ALLOWED", "pageFetchState": "SUCCESSFUL", "crawledAs": "MOBILE",
			"googleCanonical": in.GoogleCanonical, "userCanonical": in.UserCanonical}
		if !in.LastCrawl.IsZero() {
			status["lastCrawlTime"] = in.LastCrawl.UTC().Format(time.RFC3339)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"inspectionResult": map[string]any{
		"inspectionResultLink": "https://search.google.com/search-console/inspect?resource_id=" + url.QueryEscape(req.SiteURL) + "&id=" + url.QueryEscape(req.InspectionURL),
		"indexStatusResult":    status,
	}})
}
