package search

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/unkeyed/byname/internal/core"
)

var bingSpec = providerSpec{
	Info: Info{
		Provider: Bing, Title: "Bing", Product: "Bing Webmaster Tools",
		Methods: []MethodInfo{{Method: MethodAPIKey, Label: "API key", Steps: []string{
			"In Bing Webmaster Tools, open Settings, then API access.",
			"Generate an API key, or copy the one you have. One key reads every site your account has verified.",
			"Paste it here. Byname stores it encrypted, only reads with it, and never shows it again.",
		}}},
		StepDays: 7,
		Notes: []string{
			"Bing reports searches and pages a week at a time, so totals and top lists here cover whole weeks.",
			"Bing lists its top searches each week. Clicks on the rest count as unlisted.",
			"Bing's totals count every kind of result: web, chat, news, images and videos.",
			"Bing doesn't report the whole site's average position.",
			"Bing's API keeps about six months. Byname keeps every week it has copied.",
			"Days are UTC days, as Bing counts them.",
		},
	},
	zone: time.UTC, keyMethod: MethodAPIKey,
	build: func(o Options) adapter {
		base := strings.TrimSuffix(o.BaseURLs[Bing], "/")
		if base == "" {
			base = "https://ssl.bing.com/webmaster/api.svc/json"
		}
		return &bing{http: o.HTTPClient, base: base}
	},
}

// bing is Bing Webmaster Tools' adapter, over its JSON API with the
// account's API key. No method takes dates: each returns all Bing keeps,
// so every sync reads it all and upserts.
type bing struct {
	http *http.Client
	base string
}

// verify checks the key against GetUserSites. Bing names no account.
func (b *bing) verify(ctx context.Context, key string) (string, error) {
	if key == "" {
		return "", fmt.Errorf("%w: paste the API key from Bing Webmaster Tools", core.ErrInvalid)
	}
	var sites []bingSite
	return "", b.call(ctx, key, "GetUserSites", "", &sites)
}

type bingSite struct {
	URL        string `json:"Url"`
	IsVerified bool   `json:"IsVerified"`
}

// sites lists the key's verified sites, named exactly as Bing has them:
// every other call must send that name byte for byte.
func (b *bing) sites(ctx context.Context, cred credential) ([]Site, error) {
	var all []bingSite
	if err := b.call(ctx, cred.secret, "GetUserSites", "", &all); err != nil {
		return nil, err
	}
	out := []Site{}
	for _, s := range all {
		if s.IsVerified {
			out = append(out, Site{URL: s.URL})
		}
	}
	return out, nil
}

// pull reads the weekly searches and pages, then the daily totals last,
// three calls in all. want is ignored: Bing takes no dates.
func (b *bing) pull(ctx context.Context, cred credential, site string, _ span, put func(report) error) error {
	for _, c := range []struct {
		method string
		dim    dim
	}{{"GetQueryStats", queries}, {"GetPageStats", pages}} {
		var rows []bingStats
		if err := b.call(ctx, cred.secret, c.method, site, &rows); err != nil {
			return err
		}
		if r, ok := weekly(c.dim, rows); ok {
			if err := put(r); err != nil {
				return err
			}
		}
	}
	var days []bingTraffic
	if err := b.call(ctx, cred.secret, "GetRankAndTrafficStats", site, &days); err != nil {
		return err
	}
	if r, ok := daily(days); ok {
		return put(r)
	}
	return nil
}

// bingStats is a GetQueryStats row, or a GetPageStats row with the page's
// URL in Query. AvgClickPosition is left out: Bing sends -1, no value, on
// nearly every row.
type bingStats struct {
	AvgImpressionPosition float64  `json:"AvgImpressionPosition"`
	Clicks                int64    `json:"Clicks"`
	Impressions           int64    `json:"Impressions"`
	Date                  bingDate `json:"Date"`
	Query                 string   `json:"Query"`
}

type bingTraffic struct {
	Clicks      int64    `json:"Clicks"`
	Impressions int64    `json:"Impressions"`
	Date        bingDate `json:"Date"`
}

// week is the days a weekly row dated end covers. Bing dates its weeks on
// Fridays and does not say which seven days they hold; this takes the
// seven ending on the date.
func week(end day) span { return span{end.add(-6), end} }

// weekly turns a list of weekly top rows into a report. The newest week
// is partial: Bing is still counting it.
func weekly(d dim, in []bingStats) (report, bool) {
	if len(in) == 0 {
		return report{}, false
	}
	newest := in[0].Date.day
	for _, s := range in {
		if newest.before(s.Date.day) {
			newest = s.Date.day
		}
	}
	r := report{dim: d, span: week(newest)}
	for _, s := range in {
		w := week(s.Date.day)
		if w.from.before(r.span.from) {
			r.span.from = w.from
		}
		r.rows = append(r.rows, row{first: w.from, last: w.to, key: s.Query, clicks: s.Clicks, impressions: s.Impressions,
			position: max(0, s.AvgImpressionPosition), final: s.Date.day != newest})
	}
	return r, true
}

// daily turns GetRankAndTrafficStats into a report of one row a day, the
// newest partial. Bing reports no position for the whole site.
func daily(in []bingTraffic) (report, bool) {
	if len(in) == 0 {
		return report{}, false
	}
	r := report{dim: totals, span: span{in[0].Date.day, in[0].Date.day}}
	for _, t := range in {
		if t.Date.day.before(r.span.from) {
			r.span.from = t.Date.day
		}
		if r.span.to.before(t.Date.day) {
			r.span.to = t.Date.day
		}
	}
	for _, t := range in {
		r.rows = append(r.rows, row{first: t.Date.day, last: t.Date.day, clicks: t.Clicks, impressions: t.Impressions, final: t.Date.day != r.span.to})
	}
	return r, true
}

// bingDate is a WCF JSON date, /Date(1786665600000)/ or with an offset,
// /Date(1316156400000-0700)/, read as the calendar day it names.
type bingDate struct{ day }

var bingDateRE = regexp.MustCompile(`^/Date\((-?\d+)([+-]\d{4})?\)/$`)

func (d *bingDate) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	m := bingDateRE.FindStringSubmatch(s)
	if m == nil {
		return fmt.Errorf("Bing sent the date %q, which is not /Date(milliseconds)/", s)
	}
	ms, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil {
		return fmt.Errorf("Bing sent the date %q: %v", s, err)
	}
	t := time.UnixMilli(ms).UTC()
	if off := m[2]; off != "" {
		h, _ := strconv.Atoi(off[1:3])
		mins, _ := strconv.Atoi(off[3:])
		shift := time.Duration(h)*time.Hour + time.Duration(mins)*time.Minute
		if off[0] == '-' {
			shift = -shift
		}
		t = t.Add(shift)
	}
	d.day = dayOf(t)
	return nil
}

// bingError is what Bing answers a refused call with, throttles included,
// as HTTP 400. Its Message is unreliable, such as "ERROR!!! NotAuthorized",
// so Byname reads ErrorCode.
type bingError struct {
	Code    int    `json:"ErrorCode"`
	Message string `json:"Message"`
}

// bingFixes say what each ErrorCode means for the person reading it.
var bingFixes = map[int]string{
	1:  "Bing had an internal error; Byname tries again in an hour",
	2:  "Bing had an unknown error; Byname tries again in an hour",
	3:  "Bing refused the API key; generate a new one under Settings, API access in Bing Webmaster Tools, and paste it under Settings > Search engines",
	4:  "Bing is throttling this account's API key; Byname tries again in an hour",
	5:  "Bing is throttling requests for this site; Byname tries again in an hour",
	6:  "Bing has blocked this account; check Bing Webmaster Tools",
	7:  "Bing does not accept this site's address; pick the site again under Settings > Search engines",
	8:  "Bing refused a parameter Byname sent; update Byname",
	11: "Bing does not know this site under this API key; pick one of the key's sites under Settings > Search engines",
	13: "the account this API key belongs to may not read this site; verify the site in Bing Webmaster Tools",
	14: "the account this API key belongs to may not read this site; verify the site in Bing Webmaster Tools",
	16: "Bing retired a method Byname calls; update Byname",
	17: "Bing is throttling this server's address; Byname tries again in an hour",
}

func (e bingError) err() error {
	fix, ok := bingFixes[e.Code]
	if !ok {
		fix = "Byname tries again in an hour"
	}
	return fmt.Errorf("%w: Bing answered error %d: %s", core.ErrUpstream, e.Code, fix)
}

// call GETs one method with the key and decodes the rows in {"d": ...}
// into out. The key rides in the query string, so no error carries the
// URL or anything Bing echoes back unread.
func (b *bing) call(ctx context.Context, key, method, site string, out any) error {
	q := url.Values{"apikey": {key}}
	if site != "" {
		q.Set("siteUrl", site)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.base+"/"+method+"?"+q.Encode(), nil)
	if err != nil {
		return fmt.Errorf("%w: Bing's API address is not a URL", core.ErrUpstream)
	}
	res, err := b.http.Do(req)
	if ue := (*url.Error)(nil); errors.As(err, &ue) {
		err = ue.Err
	}
	if err != nil {
		msg := err.Error()
		if key != "" {
			msg = strings.ReplaceAll(msg, key, "…")
		}
		return fmt.Errorf("%w: Bing did not answer: %s", core.ErrUpstream, msg)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 64<<20))
	if err != nil {
		return fmt.Errorf("%w: reading Bing's answer was cut off", core.ErrUpstream)
	}
	if res.StatusCode != http.StatusOK {
		var e bingError
		if json.Unmarshal(raw, &e) == nil && e.Code != 0 {
			return e.err()
		}
		if res.StatusCode == http.StatusNotFound {
			return fmt.Errorf("%w: Bing does not know the method %s; update Byname", core.ErrUpstream, method)
		}
		return fmt.Errorf("%w: Bing answered %d to %s", core.ErrUpstream, res.StatusCode, method)
	}
	var body struct {
		D json.RawMessage `json:"d"`
	}
	if err := json.Unmarshal(raw, &body); err != nil || len(body.D) == 0 {
		return fmt.Errorf("%w: Bing sent something that is not the expected JSON from %s", core.ErrUpstream, method)
	}
	if string(body.D) == "null" {
		return nil
	}
	if err := json.Unmarshal(body.D, out); err != nil {
		return fmt.Errorf("%w: Bing sent rows Byname could not read from %s: %v", core.ErrUpstream, method, err)
	}
	return nil
}
