package social

import "context"

// daysPerMonth is the average calendar month.
const daysPerMonth = 365.25 / 12

// CostEstimate is what social searches cost a month at the current
// settings. Each fetch makes at least one call per network and term; a
// first fetch, or a busy period, pages further back and costs more.
type CostEstimate struct {
	Enabled         bool    `json:"enabled" jsonschema:"whether social searches are on; month_usd is 0 while they are off"`
	FetchesPerMonth float64 `json:"fetches_per_month" jsonschema:"fetches in an average month at the fetch period"`
	Terms           int     `json:"terms" jsonschema:"terms each network is searched for"`
	TregNetworks    int     `json:"treg_networks" jsonschema:"networks on that are searched through Treg; 0 without a Treg token"`
	// TregPerFetchUSD is one call per Treg network on and term, at the Treg
	// catalog's prices.
	TregPerFetchUSD float64 `json:"treg_per_fetch_usd"`
	TregMonthUSD    float64 `json:"treg_month_usd" jsonschema:"what the Treg networks cost a month at these settings, whether social is on or not"`
	BlueskyOn       bool    `json:"bluesky_on" jsonschema:"Bluesky is searched; it costs nothing"`
	BlueskyMonthUSD float64 `json:"bluesky_month_usd" jsonschema:"always 0: Bluesky's own API is free"`
	MonthUSD        float64 `json:"month_usd" jsonschema:"what social searches cost a month: treg_month_usd while social is on, 0 while it is off"`
}

// CostEstimate prices a month of social searches at the current settings,
// counting one call per network and term each fetch.
func (s *Service) CostEstimate(ctx context.Context) (CostEstimate, error) {
	st, err := s.settings(ctx)
	if err != nil {
		return CostEstimate{}, err
	}
	_, token, err := s.keys.Treg(ctx)
	if err != nil {
		return CostEstimate{}, err
	}
	out := CostEstimate{Enabled: st.enabled, FetchesPerMonth: daysPerMonth * 24 / float64(st.every), Terms: len(st.terms)}
	for _, n := range st.networks(token) {
		switch n.Route {
		case ViaTreg:
			out.TregNetworks++
			out.TregPerFetchUSD += n.CostUSD * float64(out.Terms)
		case Direct:
			out.BlueskyOn = out.BlueskyOn || n.Network == Bluesky
		}
	}
	out.TregMonthUSD = out.TregPerFetchUSD * out.FetchesPerMonth
	if st.enabled {
		out.MonthUSD = out.TregMonthUSD
	}
	return out, nil
}
