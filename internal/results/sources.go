package results

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/unkeyed/byname/internal/panel"
)

// PageCheck is what Byname found on a cited page the last time it read it.
type PageCheck struct {
	Status    string    `json:"status" jsonschema:"ok, failed, disallowed (robots.txt) or refused (not http(s), or a private address)"`
	FetchedAt time.Time `json:"fetched_at"`
	// SelfPresent is whether the page's HTML names you, null unless status
	// is ok. A page built in the browser can read as absent.
	SelfPresent *bool    `json:"self_present"`
	Brands      []string `json:"brands" jsonschema:"other brands the page names"`
	Competitors []string `json:"competitors" jsonschema:"the brands that are competitors"`
	Error       string   `json:"error"`
}

// SourcePage is one cited URL over the answers in range.
type SourcePage struct {
	URL       string `json:"url"`
	Domain    string `json:"domain"`
	IsSelf    bool   `json:"is_self" jsonschema:"on one of your domains"`
	Citations int    `json:"citations" jsonschema:"times answers cited it"`
	Answers   int    `json:"answers" jsonschema:"answers that cited it"`
	// Check is null until the page has been fetched.
	Check *PageCheck `json:"check"`
	// Outreach marks a page that names a competitor and not you, on a site
	// that is neither yours nor a competitor's.
	Outreach bool `json:"outreach"`
}

// SourceDomain is one cited domain over the answers in range.
type SourceDomain struct {
	Domain    string `json:"domain"`
	IsSelf    bool   `json:"is_self"`
	Citations int    `json:"citations"`
	Answers   int    `json:"answers"`
	Pages     int    `json:"pages"`
	Outreach  int    `json:"outreach" jsonschema:"pages flagged as outreach targets"`
}

// Sources is the most cited domains and pages over the runs in range on
// the pairs in scope.
type Sources struct {
	Answered    int            `json:"answered" jsonschema:"answers that did not fail"`
	Citing      int            `json:"citing" jsonschema:"answers that cited at least one page"`
	Citations   int            `json:"citations"`
	DomainCount int            `json:"domain_count"`
	PageCount   int            `json:"page_count"`
	Domains     []SourceDomain `json:"domains" jsonschema:"the 50 most cited"`
	Pages       []SourcePage   `json:"pages" jsonschema:"the 100 most cited"`
}

const (
	topDomains = 50
	topPages   = 100
)

// Sources counts what the answers started since (nil for all time) on the
// pairs in scope cited, with what each page says about you and your
// competitors.
func (s *Service) Sources(ctx context.Context, since *time.Time, sc Scope) (Sources, error) {
	out := Sources{Domains: []SourceDomain{}, Pages: []SourcePage{}}
	if err := sc.check(); err != nil {
		return out, err
	}
	args := []any{since, string(sc.Surface), sc.Model}
	if err := s.db.QueryRow(ctx, `SELECT count(*) FILTER (WHERE a.status <> 'failed'),
			count(*) FILTER (WHERE EXISTS (SELECT 1 FROM citation c WHERE c.answer_id = a.id))`+scopedFrom+scopedWhere, args...).
		Scan(&out.Answered, &out.Citing); err != nil {
		return out, err
	}
	rows, err := s.db.Query(ctx, `SELECT c.url, min(c.domain), bool_or(c.is_self), count(*), count(DISTINCT c.answer_id),
			p.status, p.fetched_at, p.self_present, coalesce(p.brands_present, '{}'), coalesce(p.error, '')`+scopedFrom+`
			JOIN citation c ON c.answer_id = a.id LEFT JOIN source_page p ON p.url = c.url`+scopedWhere+`
		GROUP BY c.url, p.url ORDER BY count(*) DESC, count(DISTINCT c.answer_id) DESC, c.url`, args...)
	if err != nil {
		return out, err
	}
	kinds, err := s.brandKinds(ctx)
	if err != nil {
		return out, err
	}
	brands, err := s.brands(ctx)
	if err != nil {
		return out, err
	}
	var rivals []string
	for _, b := range brands {
		if b.Kind == panel.KindCompetitor {
			rivals = append(rivals, b.Domains...)
		}
	}
	pages, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (SourcePage, error) {
		var p SourcePage
		var status *string
		var fetched *time.Time
		var check PageCheck
		if err := r.Scan(&p.URL, &p.Domain, &p.IsSelf, &p.Citations, &p.Answers, &status, &fetched, &check.SelfPresent, &check.Brands, &check.Error); err != nil {
			return p, err
		}
		if status != nil {
			check.Status, check.FetchedAt = *status, *fetched
			check.Competitors = competitorsIn(check.Brands, kinds)
			p.Check = &check
			p.Outreach = !p.IsSelf && !onDomains(p.Domain, rivals) && check.SelfPresent != nil && !*check.SelfPresent && len(check.Competitors) > 0
		}
		return p, nil
	})
	if err != nil {
		return out, err
	}
	domains := map[string]*SourceDomain{}
	var order []string
	for _, p := range pages {
		out.Citations += p.Citations
		d := domains[p.Domain]
		if d == nil {
			d = &SourceDomain{Domain: p.Domain}
			domains[p.Domain] = d
			order = append(order, p.Domain)
		}
		d.IsSelf = d.IsSelf || p.IsSelf
		d.Citations += p.Citations
		d.Pages++
		if p.Outreach {
			d.Outreach++
		}
	}
	// A domain's answers are distinct answers, which page counts cannot add up to.
	answers, err := s.db.Query(ctx, `SELECT c.domain, count(DISTINCT c.answer_id)`+scopedFrom+`JOIN citation c ON c.answer_id = a.id`+scopedWhere+`GROUP BY c.domain`, args...)
	if err != nil {
		return out, err
	}
	for answers.Next() {
		var domain string
		var n int
		if err := answers.Scan(&domain, &n); err != nil {
			return out, err
		}
		if d := domains[domain]; d != nil {
			d.Answers = n
		}
	}
	if err := answers.Err(); err != nil {
		return out, err
	}
	for _, name := range order {
		out.Domains = append(out.Domains, *domains[name])
	}
	slices.SortStableFunc(out.Domains, func(a, b SourceDomain) int {
		return cmp.Or(cmp.Compare(b.Citations, a.Citations), cmp.Compare(b.Answers, a.Answers), cmp.Compare(a.Domain, b.Domain))
	})
	out.DomainCount, out.PageCount = len(out.Domains), len(pages)
	out.Domains = out.Domains[:min(len(out.Domains), topDomains)]
	out.Pages = pages[:min(len(pages), topPages)]
	return out, nil
}

// brandKinds maps each brand's lowercased name to its kind, to read
// source_page.brands_present, which stores names.
func (s *Service) brandKinds(ctx context.Context) (map[string]panel.BrandKind, error) {
	brands, err := s.brands(ctx)
	out := map[string]panel.BrandKind{}
	for _, b := range brands {
		out[strings.ToLower(b.Name)] = b.Kind
	}
	return out, err
}

func competitorsIn(names []string, kinds map[string]panel.BrandKind) []string {
	out := []string{}
	for _, n := range names {
		if kinds[strings.ToLower(n)] == panel.KindCompetitor {
			out = append(out, n)
		}
	}
	return out
}

// onDomains reports whether host is one of domains or a subdomain of one.
// Citations store their host lowercased and without "www.".
func onDomains(host string, domains []string) bool {
	for _, d := range domains {
		d = strings.TrimPrefix(strings.ToLower(d), "www.")
		if d != "" && (host == d || strings.HasSuffix(host, "."+d)) {
			return true
		}
	}
	return false
}
