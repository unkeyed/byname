package gsc

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/jwt"

	"github.com/unkeyed/byname/internal/core"
)

// Method is how Byname signs in to Google.
type Method string

const (
	MethodOAuth          Method = "oauth"           // a person's Google account, through the instance's OAuth client
	MethodServiceAccount Method = "service_account" // a service account's JSON key
)

// methods is what each Method stores and how it gets an access token from
// the stored secret: a refresh token, or a service account's JSON key.
var methods = map[Method]struct {
	seal   string
	tokens func(s *Service, ctx context.Context, secret string) (oauth2.TokenSource, error)
}{
	MethodOAuth: {"gsc:oauth", func(s *Service, ctx context.Context, refresh string) (oauth2.TokenSource, error) {
		return s.oauthConfig().TokenSource(s.httpContext(ctx), &oauth2.Token{RefreshToken: refresh}), nil
	}},
	MethodServiceAccount: {"gsc:service_account", func(s *Service, ctx context.Context, raw string) (oauth2.TokenSource, error) {
		key, err := parseServiceAccount(raw)
		if err != nil {
			return nil, err
		}
		return s.jwtConfig(key).TokenSource(s.httpContext(ctx)), nil
	}},
}

// PropertyKind is a Search Console property type.
type PropertyKind string

const (
	Domain    PropertyKind = "domain"     // sc-domain:example.com, every protocol and subdomain
	URLPrefix PropertyKind = "url_prefix" // https://www.example.com/, one protocol and host
)

// kindOf parses a property as Search Console names it.
func kindOf(site string) (PropertyKind, error) {
	if host, ok := strings.CutPrefix(site, "sc-domain:"); ok && host != "" && !strings.ContainsAny(host, "/: ") {
		return Domain, nil
	}
	u, err := url.Parse(site)
	if err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" && strings.HasSuffix(site, "/") {
		return URLPrefix, nil
	}
	return "", fmt.Errorf("%w: property must be a domain property such as sc-domain:example.com or a URL prefix such as https://www.example.com/, got %q", core.ErrInvalid, site)
}

// Status is the connection as anyone with results:read sees it. It never
// carries the credential.
type Status struct {
	Connected bool `json:"connected"`
	// OAuthAvailable says whether this instance has a Google OAuth client
	// (GSC_GOOGLE_CLIENT_ID), which Sign in with Google needs.
	OAuthAvailable bool         `json:"oauth_available"`
	Method         Method       `json:"method" jsonschema:"oauth or service_account, empty when not connected"`
	Account        string       `json:"account" jsonschema:"the service account's email, empty for a Google sign-in"`
	Property       string       `json:"property" jsonschema:"the property synced, empty until one is picked"`
	PropertyKind   PropertyKind `json:"property_kind" jsonschema:"domain or url_prefix, empty until a property is picked"`
	Backfill       bool         `json:"backfill" jsonschema:"the first sync pulls all the history Google keeps, about 16 months"`
	ConnectedAt    *time.Time   `json:"connected_at"`
	LastSyncAt     *time.Time   `json:"last_sync_at" jsonschema:"when the last sync finished, null before the first"`
	LastSyncError  string       `json:"last_sync_error" jsonschema:"why the last sync failed, empty when it did not"`
	Syncing        bool         `json:"syncing"`
	Data
}

// Data is the stored data's extent for the property the Search page
// shows: the connected one, or the last one synced after a disconnect.
type Data struct {
	DataProperty string `json:"data_property" jsonschema:"the property the stored numbers are for, empty when nothing is stored"`
	FirstDay     string `json:"first_day" jsonschema:"the earliest stored day, empty when nothing is stored"`
	LatestFinal  string `json:"latest_final_day" jsonschema:"the latest day Google has finalized, which every window ends on"`
	// PartialDays are stored days Google still revises; the next sync
	// overwrites them.
	PartialDays []string `json:"partial_days"`
}

// Status reports the connection and the data stored, never the credential.
func (s *Service) Status(ctx context.Context) (Status, error) {
	st := Status{OAuthAvailable: s.client.set()}
	var prop, kind *string
	if err := s.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM job_run WHERE job = 'gsc_sync' AND status = 'running')`).Scan(&st.Syncing); err != nil {
		return st, err
	}
	err := s.db.QueryRow(ctx, `SELECT method, account, property, property_kind, backfill, connected_at, last_sync_at,
			coalesce(last_sync_error, '')
		FROM gsc_connection`).Scan(&st.Method, &st.Account, &prop, &kind, &st.Backfill, &st.ConnectedAt, &st.LastSyncAt, &st.LastSyncError)
	switch {
	case err == nil:
		st.Connected = true
		if prop != nil {
			st.Property, st.PropertyKind = *prop, PropertyKind(*kind)
		}
	case !errors.Is(err, pgx.ErrNoRows):
		return st, err
	}
	st.Data, err = s.data(ctx)
	return st, err
}

func (s *Service) data(ctx context.Context) (Data, error) {
	d := Data{PartialDays: []string{}}
	prop, err := s.dataProperty(ctx)
	if err != nil || prop == "" {
		return d, err
	}
	d.DataProperty = prop
	err = s.db.QueryRow(ctx, `SELECT coalesce(to_char(min(date), 'YYYY-MM-DD'), ''),
			coalesce(to_char(max(date) FILTER (WHERE is_final), 'YYYY-MM-DD'), ''),
			coalesce(array_agg(to_char(date, 'YYYY-MM-DD') ORDER BY date) FILTER (WHERE NOT is_final), '{}')
		FROM gsc_site_daily WHERE property = $1`, prop).Scan(&d.FirstDay, &d.LatestFinal, &d.PartialDays)
	return d, err
}

// dataProperty is the property the read API reports on: the connected
// one, else the one with the latest stored day. "" when there is neither.
func (s *Service) dataProperty(ctx context.Context) (string, error) {
	var p string
	err := s.db.QueryRow(ctx, `SELECT coalesce(
		(SELECT property FROM gsc_connection WHERE property IS NOT NULL),
		(SELECT property FROM gsc_site_daily ORDER BY date DESC LIMIT 1), '')`).Scan(&p)
	return p, err
}

func (s *Service) httpContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, oauth2.HTTPClient, s.http)
}

func (s *Service) oauthConfig() *oauth2.Config {
	return &oauth2.Config{
		ClientID:     s.client.ID,
		ClientSecret: s.client.Secret,
		Endpoint:     oauth2.Endpoint{AuthURL: s.endpoints.AuthURL, TokenURL: s.endpoints.TokenURL, AuthStyle: oauth2.AuthStyleInParams},
		RedirectURL:  s.publicURL.JoinPath("/auth/gsc/callback").String(),
		Scopes:       []string{Scope},
	}
}

// oauthState is what the state parameter carries through Google's consent
// page, sealed so nobody else can mint or read one: who started the
// connection, the PKCE verifier, and when it stops being accepted.
type oauthState struct {
	UserID   int64  `json:"u"`
	Verifier string `json:"v"`
	Expires  int64  `json:"e"`
}

const stateSeal = "gsc:oauth-state"

// StartOAuth returns Google's consent page URL for the admin a, asking for
// read-only Search Console access with offline access, so Google returns a
// refresh token. The browser comes back to /auth/gsc/callback.
func (s *Service) StartOAuth(a core.Actor) (string, error) {
	if !s.client.set() {
		return "", fmt.Errorf("%w: Sign in with Google needs this instance's own Google OAuth client; set GSC_GOOGLE_CLIENT_ID and GSC_GOOGLE_CLIENT_SECRET, or connect a service account instead", core.ErrInvalid)
	}
	st := oauthState{UserID: a.UserID, Verifier: oauth2.GenerateVerifier(), Expires: s.now().Add(15 * time.Minute).Unix()}
	raw, err := json.Marshal(st)
	if err != nil {
		return "", err
	}
	state := s.secrets.Seal(string(raw), stateSeal)
	return s.oauthConfig().AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce, oauth2.S256ChallengeOption(st.Verifier)), nil
}

// OAuthCallback is what Google sends the browser back with.
type OAuthCallback struct {
	State string
	Code  string
	Error string // Google's error parameter, such as access_denied
}

// FinishOAuth exchanges the code for a refresh token and stores it sealed,
// keeping the property already picked, if any. Only the admin who started
// the connection can finish it.
func (s *Service) FinishOAuth(ctx context.Context, a core.Actor, cb OAuthCallback) error {
	if cb.Error != "" {
		return fmt.Errorf("%w: Google sign-in ended with %q; try again and allow Search Console access", core.ErrInvalid, cb.Error)
	}
	raw, err := s.secrets.Open(cb.State, stateSeal)
	var st oauthState
	if err != nil || json.Unmarshal([]byte(raw), &st) != nil || st.UserID != a.UserID {
		return fmt.Errorf("%w: this Google sign-in was not started here by you; start it again from Settings > Search Console", core.ErrInvalid)
	}
	if s.now().Unix() > st.Expires {
		return fmt.Errorf("%w: the Google sign-in took longer than 15 minutes; start it again", core.ErrInvalid)
	}
	if cb.Code == "" {
		return fmt.Errorf("%w: Google sent no authorization code; start the sign-in again", core.ErrInvalid)
	}
	tok, err := s.oauthConfig().Exchange(s.httpContext(ctx), cb.Code, oauth2.VerifierOption(st.Verifier))
	if re := (*oauth2.RetrieveError)(nil); errors.As(err, &re) {
		return tokenError(re)
	}
	if err != nil {
		return fmt.Errorf("%w: Google's token endpoint did not answer: %v", core.ErrUpstream, err)
	}
	if granted, _ := tok.Extra("scope").(string); granted != "" && !slices.Contains(strings.Fields(granted), Scope) {
		return fmt.Errorf("%w: Google did not grant Search Console access; sign in again and leave the Search Console box ticked", core.ErrInvalid)
	}
	if tok.RefreshToken == "" {
		return fmt.Errorf("%w: Google sent no refresh token, so Byname could not sync after today; remove Byname's access at myaccount.google.com/permissions and sign in again", core.ErrUpstream)
	}
	return s.store(ctx, MethodOAuth, "", tok.RefreshToken)
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

func (s *Service) jwtConfig(k serviceAccount) *jwt.Config {
	return &jwt.Config{Email: k.ClientEmail, PrivateKey: []byte(k.PrivateKey), PrivateKeyID: k.PrivateKeyID, Scopes: []string{Scope}, TokenURL: s.endpoints.TokenURL}
}

// ConnectServiceAccount checks that Google accepts the service account's
// JSON key, then stores it sealed, keeping the property already picked.
// Add the service account's email as a user on the property in Search
// Console first.
func (s *Service) ConnectServiceAccount(ctx context.Context, key string) (Status, error) {
	k, err := parseServiceAccount(strings.TrimSpace(key))
	if err != nil {
		return Status{}, err
	}
	if _, err := s.jwtConfig(k).TokenSource(s.httpContext(ctx)).Token(); err != nil {
		if re := (*oauth2.RetrieveError)(nil); errors.As(err, &re) {
			return Status{}, tokenError(re)
		}
		return Status{}, fmt.Errorf("%w: Google's token endpoint did not answer: %v", core.ErrUpstream, err)
	}
	if err := s.store(ctx, MethodServiceAccount, k.ClientEmail, strings.TrimSpace(key)); err != nil {
		return Status{}, err
	}
	return s.Status(ctx)
}

func (s *Service) store(ctx context.Context, m Method, account, secret string) error {
	_, err := s.db.Exec(ctx, `INSERT INTO gsc_connection (method, account, ciphertext, connected_at) VALUES ($1, $2, $3, $4)
		ON CONFLICT (id) DO UPDATE SET method = excluded.method, account = excluded.account, ciphertext = excluded.ciphertext,
			connected_at = excluded.connected_at, last_sync_error = NULL`,
		m, account, s.secrets.Seal(secret, methods[m].seal), s.now())
	return err
}

// connection is the stored connection with its credential opened.
type connection struct {
	property string
	backfill bool
	lastSync *time.Time
	tokens   oauth2.TokenSource
}

var errNotConnected = fmt.Errorf("%w: Search Console is not connected; an admin connects it under Settings > Search Console", core.ErrInvalid)

func (s *Service) connection(ctx context.Context) (connection, error) {
	var c connection
	var m Method
	var sealed string
	var prop *string
	err := s.db.QueryRow(ctx, `SELECT method, ciphertext, property, backfill, last_sync_at FROM gsc_connection`).Scan(&m, &sealed, &prop, &c.backfill, &c.lastSync)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, errNotConnected
	}
	if err != nil {
		return c, err
	}
	if prop != nil {
		c.property = *prop
	}
	spec := methods[m]
	secret, err := s.secrets.Open(sealed, spec.seal)
	if err != nil {
		return c, fmt.Errorf("open the Search Console credential: %w; was ENCRYPTION_KEY changed without ENCRYPTION_KEY_PREVIOUS?", err)
	}
	c.tokens, err = spec.tokens(s, ctx, secret)
	return c, err
}

// Property is one property the connected account can read.
type Property struct {
	Site
	Kind PropertyKind `json:"kind"`
	// Suggested marks the property that matches the brand's primary
	// domain, preferring its domain property.
	Suggested bool `json:"suggested"`
}

// Properties lists the properties the connected account can read, from
// Google's sites.list.
func (s *Service) Properties(ctx context.Context) ([]Property, error) {
	c, err := s.connection(ctx)
	if err != nil {
		return nil, err
	}
	sites, err := s.api(ctx, c.tokens).sites(ctx)
	if err != nil {
		return nil, err
	}
	var domain string
	if err := s.db.QueryRow(ctx, `SELECT coalesce((SELECT domains[1] FROM brand WHERE kind = 'self'), '')`).Scan(&domain); err != nil {
		return nil, err
	}
	out := []Property{}
	for _, site := range sites {
		kind, err := kindOf(site.URL)
		if err != nil {
			continue
		}
		out = append(out, Property{Site: site, Kind: kind})
	}
	if i := suggest(out, domain); i >= 0 {
		out[i].Suggested = true
	}
	return out, nil
}

// suggest picks the property for the primary domain: its domain property,
// else a URL prefix on the domain or its www host, https first.
func suggest(props []Property, domain string) int {
	if domain == "" {
		return -1
	}
	best, rank := -1, 99
	for i, p := range props {
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

// PropertyInput picks the property to sync.
type PropertyInput struct {
	Property string `json:"property" jsonschema:"a site_url from the account's properties"`
	// Backfill makes the first sync pull every day Google keeps, about 16
	// months, rather than the last 28 days. Defaults to true.
	Backfill *bool `json:"backfill,omitempty"`
}

// SetProperty picks the property to sync, which must be one the account
// can read. The next scheduler tick, within a minute, syncs it.
func (s *Service) SetProperty(ctx context.Context, in PropertyInput) (Status, error) {
	kind, err := kindOf(strings.TrimSpace(in.Property))
	if err != nil {
		return Status{}, err
	}
	props, err := s.Properties(ctx)
	if err != nil {
		return Status{}, err
	}
	if !slices.ContainsFunc(props, func(p Property) bool { return p.URL == strings.TrimSpace(in.Property) }) {
		return Status{}, fmt.Errorf("%w: the connected account cannot read %s; add it as a user on that property in Search Console, or pick one of its properties", core.ErrInvalid, in.Property)
	}
	backfill := in.Backfill == nil || *in.Backfill
	_, err = s.db.Exec(ctx, `UPDATE gsc_connection SET property = $1, property_kind = $2, backfill = $3,
			last_sync_at = CASE WHEN property IS DISTINCT FROM $1 OR (backfill <> $3 AND $3) THEN NULL ELSE last_sync_at END,
			last_sync_error = NULL`,
		strings.TrimSpace(in.Property), kind, backfill)
	if err != nil {
		return Status{}, err
	}
	return s.Status(ctx)
}

// Disconnect deletes the connection and its credential. The synced numbers
// stay, so the Search page keeps its history.
func (s *Service) Disconnect(ctx context.Context) error {
	_, err := s.db.Exec(ctx, `DELETE FROM gsc_connection`)
	return err
}

// SealSecrets reseals a credential sealed under ENCRYPTION_KEY_PREVIOUS
// with ENCRYPTION_KEY, so a key rotation finishes with one restart.
func (s *Service) SealSecrets(ctx context.Context) (int, error) {
	var m Method
	var sealed string
	err := s.db.QueryRow(ctx, `SELECT method, ciphertext FROM gsc_connection`).Scan(&m, &sealed)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	resealed, changed, err := s.secrets.Reseal(sealed, methods[m].seal)
	if err != nil || !changed {
		return 0, err
	}
	_, err = s.db.Exec(ctx, `UPDATE gsc_connection SET ciphertext = $1 WHERE ciphertext = $2`, resealed, sealed)
	return 1, err
}
