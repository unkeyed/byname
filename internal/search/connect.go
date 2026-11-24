package search

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/unkeyed/byname/internal/core"
)

// Status is one provider's connection as anyone with results:read sees
// it. It never carries the credential.
type Status struct {
	Info
	Connected     bool       `json:"connected"`
	Method        Method     `json:"method" jsonschema:"how Byname signs in, empty when not connected"`
	Account       string     `json:"account" jsonschema:"the service account's email; empty for a Google sign-in and for Bing"`
	Site          string     `json:"site" jsonschema:"the site synced, empty until one is picked"`
	SiteKind      SiteKind   `json:"site_kind" jsonschema:"Google's property type, domain or url_prefix; empty for other providers and until a site is picked"`
	Backfill      bool       `json:"backfill" jsonschema:"the first sync pulls all the history the provider keeps, backfill_months back"`
	ConnectedAt   *time.Time `json:"connected_at"`
	LastSyncAt    *time.Time `json:"last_sync_at" jsonschema:"when the last sync finished, null before the first"`
	LastSyncError string     `json:"last_sync_error" jsonschema:"why the last sync failed, empty when it did not"`
	Syncing       bool       `json:"syncing"`
	Data
}

// Data is the stored data's extent for the site the Search page shows:
// the connected one, or the last one synced after a disconnect.
type Data struct {
	DataSite    string `json:"data_site" jsonschema:"the site the stored numbers are for, empty when nothing is stored"`
	FirstDay    string `json:"first_day" jsonschema:"the earliest stored day, empty when nothing is stored"`
	LatestFinal string `json:"latest_final_day" jsonschema:"the latest final day every window ends on; for a weekly provider, the end of its latest final week"`
	// PartialDays are stored days the provider still revises; the next
	// sync overwrites them.
	PartialDays []string `json:"partial_days"`
}

// ProviderList is every provider, connected or not.
type ProviderList struct {
	Providers []Status `json:"providers"`
}

// Providers is the status of every provider, in registry order.
func (s *Service) Providers(ctx context.Context) (ProviderList, error) {
	out := ProviderList{Providers: []Status{}}
	for _, sp := range registry {
		st, err := s.Status(ctx, sp.Provider)
		if err != nil {
			return out, err
		}
		out.Providers = append(out.Providers, st)
	}
	return out, nil
}

// Status reports p's connection and the data stored, never the credential.
func (s *Service) Status(ctx context.Context, p Provider) (Status, error) {
	sp, err := lookup(p)
	if err != nil {
		return Status{}, err
	}
	st := Status{Info: sp.Info}
	st.Methods = slices.Clone(sp.Methods)
	for i, m := range st.Methods {
		st.Methods[i].Available = m.Method != MethodOAuth || s.google.client.set()
	}
	if err := s.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM job_run WHERE job = 'search_sync' AND provider = $1 AND status = 'running')`, p).Scan(&st.Syncing); err != nil {
		return st, err
	}
	var site, kind *string
	err = s.db.QueryRow(ctx, `SELECT method, account, site, site_kind, backfill, connected_at, last_sync_at, coalesce(last_sync_error, '')
		FROM search_connection WHERE provider = $1`, p).Scan(&st.Method, &st.Account, &site, &kind, &st.Backfill, &st.ConnectedAt, &st.LastSyncAt, &st.LastSyncError)
	switch {
	case err == nil:
		st.Connected = true
		if site != nil {
			st.Site = *site
		}
		if kind != nil {
			st.SiteKind = SiteKind(*kind)
		}
	case !errors.Is(err, pgx.ErrNoRows):
		return st, err
	}
	st.Data, err = s.data(ctx, p)
	return st, err
}

func (s *Service) data(ctx context.Context, p Provider) (Data, error) {
	d := Data{PartialDays: []string{}}
	sc, ok, err := s.scope(ctx, p)
	d.DataSite = sc.site
	if err != nil || !ok {
		return d, err
	}
	d.FirstDay = sc.first.String()
	if sc.hasFinal {
		d.LatestFinal = sc.anchor.String()
	}
	err = s.db.QueryRow(ctx, `SELECT coalesce(array_agg(to_char(day, 'YYYY-MM-DD') ORDER BY day), '{}')
		FROM search_site_daily WHERE provider = $1 AND site = $2 AND NOT is_final`, p, sc.site).Scan(&d.PartialDays)
	return d, err
}

// dataSite is the site the read API reports on for p: the connected one,
// else the one with the latest stored day. "" when there is neither.
func (s *Service) dataSite(ctx context.Context, p Provider) (string, error) {
	var site string
	err := s.db.QueryRow(ctx, `SELECT coalesce(
		(SELECT site FROM search_connection WHERE provider = $1 AND site IS NOT NULL),
		(SELECT site FROM search_site_daily WHERE provider = $1 ORDER BY day DESC LIMIT 1), '')`, p).Scan(&site)
	return site, err
}

// ConnectKey checks a pasted key with p, then stores it sealed, keeping
// the site already picked: a service account's JSON key for Google. For
// Google, add the service account's email as a user on the site in Search
// Console first.
func (s *Service) ConnectKey(ctx context.Context, p Provider, key string) (Status, error) {
	sp, err := lookup(p)
	if err != nil {
		return Status{}, err
	}
	key = strings.TrimSpace(key)
	account, err := s.adapters[p].verify(ctx, key)
	if err != nil {
		return Status{}, err
	}
	if err := s.store(ctx, p, sp.keyMethod, account, key); err != nil {
		return Status{}, err
	}
	return s.Status(ctx, p)
}

// sealLabel binds a sealed credential to its provider and method.
func sealLabel(p Provider, m Method) string { return "search:" + string(p) + ":" + string(m) }

func (s *Service) store(ctx context.Context, p Provider, m Method, account, secret string) error {
	_, err := s.db.Exec(ctx, `INSERT INTO search_connection (provider, method, account, ciphertext, connected_at) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (provider) DO UPDATE SET method = excluded.method, account = excluded.account, ciphertext = excluded.ciphertext,
			connected_at = excluded.connected_at, last_sync_error = NULL`,
		p, m, account, s.secrets.Seal(secret, sealLabel(p, m)), s.now())
	return err
}

// conn is a stored connection with its credential opened.
type conn struct {
	cred     credential
	site     string
	backfill bool
	lastSync *time.Time
}

func (s *Service) connection(ctx context.Context, p Provider) (conn, error) {
	sp, err := lookup(p)
	if err != nil {
		return conn{}, err
	}
	var c conn
	var sealed string
	var site *string
	err = s.db.QueryRow(ctx, `SELECT method, ciphertext, site, backfill, last_sync_at FROM search_connection WHERE provider = $1`, p).
		Scan(&c.cred.method, &sealed, &site, &c.backfill, &c.lastSync)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, fmt.Errorf("%w: %s is not connected; an admin connects it under Settings > Search engines", core.ErrInvalid, sp.Title)
	}
	if err != nil {
		return c, err
	}
	if site != nil {
		c.site = *site
	}
	if c.cred.secret, err = s.secrets.Open(sealed, sealLabel(p, c.cred.method)); err != nil {
		return c, fmt.Errorf("open the %s credential: %w; was ENCRYPTION_KEY changed without ENCRYPTION_KEY_PREVIOUS?", sp.Title, err)
	}
	return c, nil
}

// Site is one site the connected account can read.
type Site struct {
	URL        string   `json:"url" jsonschema:"the site as the provider names it, such as sc-domain:example.com or https://www.example.com/"`
	Kind       SiteKind `json:"kind" jsonschema:"Google's property type, domain or url_prefix; empty for other providers"`
	Permission string   `json:"permission" jsonschema:"Google's permission level, such as siteOwner or siteRestrictedUser; empty for other providers"`
	// Suggested marks the site that matches the brand's primary domain,
	// preferring Google's domain property.
	Suggested bool `json:"suggested"`
}

// Sites lists the sites p's connected account can read.
func (s *Service) Sites(ctx context.Context, p Provider) ([]Site, error) {
	c, err := s.connection(ctx, p)
	if err != nil {
		return nil, err
	}
	sites, err := s.adapters[p].sites(ctx, c.cred)
	if err != nil {
		return nil, err
	}
	var domain string
	if err := s.db.QueryRow(ctx, `SELECT coalesce((SELECT domains[1] FROM brand WHERE kind = 'self'), '')`).Scan(&domain); err != nil {
		return nil, err
	}
	if i := suggest(sites, domain); i >= 0 {
		sites[i].Suggested = true
	}
	return sites, nil
}

// suggest picks the site for the primary domain: Google's domain property,
// else the domain or its www host, https first.
func suggest(sites []Site, domain string) int {
	if domain == "" {
		return -1
	}
	best, rank := -1, 99
	for i, p := range sites {
		r := 99
		switch {
		case p.URL == "sc-domain:"+domain:
			r = 0
		case p.URL == "https://"+domain+"/" || p.URL == "https://www."+domain+"/":
			r = 1
		case p.URL == "http://"+domain+"/" || p.URL == "http://www."+domain+"/":
			r = 2
		}
		if r < rank {
			best, rank = i, r
		}
	}
	return best
}

// SiteInput picks the site to sync.
type SiteInput struct {
	Site string `json:"site" jsonschema:"a url from the provider's sites"`
	// Backfill makes the first sync pull all the history the provider
	// keeps rather than the last 28 days. Defaults to true; it means
	// nothing for a provider that reads all it keeps on every sync.
	Backfill *bool `json:"backfill,omitempty"`
}

// SetSite picks the site to sync for p, which must be one the account can
// read. The next scheduler tick, within a minute, syncs it.
func (s *Service) SetSite(ctx context.Context, p Provider, in SiteInput) (Status, error) {
	sp, err := lookup(p)
	if err != nil {
		return Status{}, err
	}
	want := strings.TrimSpace(in.Site)
	sites, err := s.Sites(ctx, p)
	if err != nil {
		return Status{}, err
	}
	i := slices.IndexFunc(sites, func(x Site) bool { return x.URL == want })
	if i < 0 {
		return Status{}, fmt.Errorf("%w: the account connected to %s cannot read %q; pick one of the sites it lists", core.ErrInvalid, sp.Product, in.Site)
	}
	backfill := sp.BackfillMonths > 0 && (in.Backfill == nil || *in.Backfill)
	_, err = s.db.Exec(ctx, `UPDATE search_connection SET site = $2, site_kind = nullif($3, ''), backfill = $4,
			last_sync_at = CASE WHEN site IS DISTINCT FROM $2 OR (backfill <> $4 AND $4) THEN NULL ELSE last_sync_at END,
			last_sync_error = NULL
		WHERE provider = $1`, p, want, sites[i].Kind, backfill)
	if err != nil {
		return Status{}, err
	}
	return s.Status(ctx, p)
}

// Disconnect deletes p's connection and its credential. The synced numbers
// stay, so the Search page keeps its history.
func (s *Service) Disconnect(ctx context.Context, p Provider) error {
	if _, err := lookup(p); err != nil {
		return err
	}
	_, err := s.db.Exec(ctx, `DELETE FROM search_connection WHERE provider = $1`, p)
	return err
}

// SealSecrets reseals each credential sealed under ENCRYPTION_KEY_PREVIOUS
// with ENCRYPTION_KEY, so a key rotation finishes with one restart.
func (s *Service) SealSecrets(ctx context.Context) (int, error) {
	rows, err := s.db.Query(ctx, `SELECT provider, method, ciphertext FROM search_connection`)
	if err != nil {
		return 0, err
	}
	type stored struct {
		provider Provider
		method   Method
		sealed   string
	}
	all, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (stored, error) {
		var c stored
		return c, r.Scan(&c.provider, &c.method, &c.sealed)
	})
	if err != nil {
		return 0, err
	}
	n := 0
	for _, c := range all {
		resealed, changed, err := s.secrets.Reseal(c.sealed, sealLabel(c.provider, c.method))
		if err != nil {
			return n, err
		}
		if !changed {
			continue
		}
		if _, err := s.db.Exec(ctx, `UPDATE search_connection SET ciphertext = $2 WHERE provider = $1 AND ciphertext = $3`, c.provider, resealed, c.sealed); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}
