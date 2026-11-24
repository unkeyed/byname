package searchtest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"
)

// BingFake is Bing Webmaster Tools' JSON API as Byname calls it:
// GetUserSites, GetRankAndTrafficStats, GetQueryStats and GetPageStats,
// each a GET with ?apikey= and, but for GetUserSites, ?siteUrl=. Bodies are
// shaped like recorded live responses: {"d": [...]} rows carrying __type,
// /Date(ms)/ dates and AvgClickPosition -1, and a refusal as HTTP 400 with
// {"ErrorCode", "Message"}, throttles included. No method takes dates, so
// each returns everything set for the site.
type BingFake struct {
	// Key is the one API key the fake accepts.
	Key string
	// DateOffset, such as "+0530", makes every date the documented
	// /Date(ms+0530)/ form, ms being midnight of the day in that offset.
	DateOffset string

	mu      sync.Mutex
	sites   []BingSite
	traffic map[string][]BingDay
	queries map[string][]BingWeek
	pages   map[string][]BingWeek
	refuse  map[string]int
	calls   []BingCall
}

// BingSite is a site the key's account has added.
type BingSite struct {
	URL      string
	Verified bool
}

// BingDay is one day of a site's totals, as GetRankAndTrafficStats counts
// them.
type BingDay struct {
	Date        string
	Clicks      int
	Impressions int
}

// BingWeek is one search or page in Bing's weekly top list. Date is the
// day Bing dates the week by, a Friday in live data. Key is the search or
// the page's URL. Position is AvgImpressionPosition; 0 sends -1, Bing's
// no value.
type BingWeek struct {
	Date        string
	Key         string
	Clicks      int
	Impressions int
	Position    int
}

// BingCall is one request the fake served.
type BingCall struct {
	Method  string
	SiteURL string
}

// Bing is a running BingFake.
type Bing struct {
	*BingFake
	URL string
}

// NewBing starts a fake Bing for one test that accepts key.
func NewBing(t testing.TB, key string) *Bing {
	t.Helper()
	f := &BingFake{Key: key, traffic: map[string][]BingDay{}, queries: map[string][]BingWeek{}, pages: map[string][]BingWeek{}, refuse: map[string]int{}}
	srv := httptest.NewServer(f.Handler())
	t.Cleanup(srv.Close)
	return &Bing{BingFake: f, URL: srv.URL}
}

// AddSite adds a site to the key's account.
func (f *BingFake) AddSite(url string, verified bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sites = append(f.sites, BingSite{url, verified})
}

// SetTraffic replaces site's daily totals.
func (f *BingFake) SetTraffic(site string, days ...BingDay) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.traffic[site] = days
}

// SetQueries replaces site's weekly top searches.
func (f *BingFake) SetQueries(site string, weeks ...BingWeek) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queries[site] = weeks
}

// SetPages replaces site's weekly top pages.
func (f *BingFake) SetPages(site string, weeks ...BingWeek) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pages[site] = weeks
}

// Refuse makes method answer with Bing's ErrorCode code, such as 4 for
// ThrottleUser; 0 stops it.
func (f *BingFake) Refuse(method string, code int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refuse[method] = code
}

// Calls returns every request served so far.
func (f *BingFake) Calls() []BingCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

// bingErrors are Bing's documented ErrorCode names.
var bingErrors = map[int]string{1: "InternalError", 2: "UnknownError", 3: "InvalidApiKey", 4: "ThrottleUser", 5: "ThrottleHost",
	6: "UserBlocked", 7: "InvalidUrl", 8: "InvalidParameter", 11: "NotFound", 13: "NotAllowed", 14: "ERROR!!! NotAuthorized", 17: "ThrottleIP"}

func bingError(w http.ResponseWriter, code int) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(map[string]any{"ErrorCode": code, "Message": bingErrors[code]})
}

// Handler serves /GetUserSites, /GetRankAndTrafficStats, /GetQueryStats and
// /GetPageStats under the fake's URL, which stands for
// https://ssl.bing.com/webmaster/api.svc/json.
func (f *BingFake) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /GetUserSites", f.serve(func(string) any {
		out := []map[string]any{}
		for _, s := range f.sites {
			out = append(out, map[string]any{"__type": "Site:#Microsoft.Bing.Webmaster.Api", "AuthenticationCode": "0123456789ABCDEF0123456789ABCDEF",
				"DnsVerificationCode": "0123456789ABCDEF0123456789ABCDEF", "IsVerified": s.Verified, "Url": s.URL})
		}
		return out
	}))
	mux.HandleFunc("GET /GetRankAndTrafficStats", f.serve(func(site string) any {
		out := []map[string]any{}
		for _, d := range f.traffic[site] {
			out = append(out, map[string]any{"__type": "RankAndTrafficStats:#Microsoft.Bing.Webmaster.Api", "Clicks": d.Clicks, "Date": f.date(d.Date), "Impressions": d.Impressions})
		}
		return out
	}))
	weeks := func(of map[string][]BingWeek) func(string) any {
		return func(site string) any {
			out := []map[string]any{}
			for _, w := range of[site] {
				pos := w.Position
				if pos == 0 {
					pos = -1
				}
				out = append(out, map[string]any{"__type": "QueryStats:#Microsoft.Bing.Webmaster.Api", "AvgClickPosition": -1, "AvgImpressionPosition": pos,
					"Clicks": w.Clicks, "Date": f.date(w.Date), "Impressions": w.Impressions, "Query": w.Key})
			}
			return out
		}
	}
	mux.HandleFunc("GET /GetQueryStats", f.serve(weeks(f.queries)))
	mux.HandleFunc("GET /GetPageStats", f.serve(weeks(f.pages)))
	return mux
}

// serve checks the key and the site as Bing does, then answers rows(site)
// as {"d": rows}.
func (f *BingFake) serve(rows func(site string) any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		method := r.URL.Path[1:]
		site := r.URL.Query().Get("siteUrl")
		f.mu.Lock()
		defer f.mu.Unlock()
		f.calls = append(f.calls, BingCall{Method: method, SiteURL: site})
		if code := f.refuse[method]; code != 0 {
			bingError(w, code)
			return
		}
		if r.URL.Query().Get("apikey") != f.Key {
			bingError(w, 3)
			return
		}
		if method != "GetUserSites" && !slices.ContainsFunc(f.sites, func(s BingSite) bool { return s.URL == site && s.Verified }) {
			bingError(w, 14)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]any{"d": rows(site)})
	}
}

// date writes a calendar day as Bing does: midnight UTC in milliseconds,
// or midnight in DateOffset with the offset after it.
func (f *BingFake) date(day string) string {
	t, err := time.Parse(time.DateOnly, day)
	if err != nil {
		panic(err)
	}
	if f.DateOffset == "" {
		return fmt.Sprintf("/Date(%d)/", t.UnixMilli())
	}
	off, err := time.Parse("-0700", f.DateOffset)
	if err != nil {
		panic(err)
	}
	_, secs := off.Zone()
	return fmt.Sprintf("/Date(%d%s)/", t.Add(-time.Duration(secs)*time.Second).UnixMilli(), f.DateOffset)
}
