package search

import "context"

// adapter is one provider's API, parsed into Byname's shape. It holds no
// database handle: pull hands its reports to put, which stores them.
type adapter interface {
	// verify checks a pasted key with the provider and returns the account
	// it belongs to, "" when the provider names none.
	verify(ctx context.Context, key string) (account string, err error)
	// sites lists the sites cred can read.
	sites(ctx context.Context, cred credential) ([]Site, error)
	// pull fetches site's numbers for want and hands them to put. Each
	// span's totals come last: the next sync resumes from the stored
	// totals, so a span cut off before them is pulled again in full. A
	// provider whose API takes no dates pulls all it keeps instead.
	pull(ctx context.Context, cred credential, site string, want span, put func(report) error) error
}

// credential is a stored credential, opened. It never leaves the package.
type credential struct {
	method Method
	secret string
}

// dim is what a report counts: the whole site, or each search or page.
type dim int

const (
	totals dim = iota
	queries
	pages
)

// report is one dimension's rows over span. The store commits it in one
// transaction: the partial rows stored wholly inside span go, then every
// row is upserted, a final row never replaced by a partial one.
type report struct {
	dim  dim
	span span
	rows []row
}

// row is one measurement over first to last: a day, or one of Bing's weeks.
// Site totals are always one day.
type row struct {
	first, last         day
	key                 string // the query or page; "" for totals
	clicks, impressions int64
	position            float64 // average position, 0 when the provider gives none
	final               bool
}
