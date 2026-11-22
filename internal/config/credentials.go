package config

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/unkeyed/byname/internal/core"
	"github.com/unkeyed/byname/internal/engines"
)

// Credential is what Byname shows about a stored key. The key itself is
// never returned by any API.
type Credential struct {
	Kind           engines.CredentialKind `json:"kind"`
	Title          string                 `json:"title"`
	Hint           string                 `json:"hint" jsonschema:"what to paste"`
	Set            bool                   `json:"set"`
	Org            string                 `json:"org" jsonschema:"Treg's X-Treg-Org, only for a personal Treg token"`
	LastTestedAt   *time.Time             `json:"last_tested_at"`
	LastTestStatus string                 `json:"last_test_status" jsonschema:"ok, or why the last test failed"`
	UpdatedAt      *time.Time             `json:"updated_at"`
	UsedBy         []engines.Surface      `json:"used_by" jsonschema:"surfaces whose route calls with this key"`
}

var credentialHints = map[engines.CredentialKind]string{
	engines.CredentialTreg:       "The Treg team token. One token runs every surface on the Treg route.",
	engines.CredentialOpenAI:     "An OpenAI API key (sk-...).",
	engines.CredentialAnthropic:  "An Anthropic API key (sk-ant-...).",
	engines.CredentialPerplexity: "A Perplexity API key (pplx-...).",
	engines.CredentialGoogle:     "A Gemini API key from Google AI Studio.",
	engines.CredentialDataForSEO: "Your DataForSEO API login and password as login:password.",
	engines.CredentialCloro:      "A cloro API key.",
	engines.CredentialSerpApi:    "A SerpApi API key.",
	engines.CredentialGateway:    "The gateway's API key. Each surface on the gateway route sets its base URL.",
}

func credentialTitle(kind engines.CredentialKind) string {
	for _, a := range engines.Adapters() {
		if a.Credential == kind {
			return a.Title
		}
	}
	return string(kind)
}

func parseCredentialKind(raw string) (engines.CredentialKind, error) {
	k := engines.CredentialKind(raw)
	if !slices.Contains(engines.CredentialKinds, k) {
		return "", fmt.Errorf("%w: no credential kind %q; GET /v1/credentials lists them", core.ErrNotFound, raw)
	}
	return k, nil
}

func seal(kind engines.CredentialKind) string { return "credential:" + string(kind) }

// Credentials lists every kind, set or not, Treg first.
func (s *Service) Credentials(ctx context.Context) ([]Credential, error) {
	out := make([]Credential, len(engines.CredentialKinds))
	byKind := map[engines.CredentialKind]*Credential{}
	for i, k := range engines.CredentialKinds {
		out[i] = Credential{Kind: k, Title: credentialTitle(k), Hint: credentialHints[k], UsedBy: []engines.Surface{}}
		byKind[k] = &out[i]
	}
	rows, err := s.db.Query(ctx, `SELECT kind, org, last_tested_at, coalesce(last_test_status, ''), updated_at FROM credential`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var k engines.CredentialKind
		var c Credential
		if err := rows.Scan(&k, &c.Org, &c.LastTestedAt, &c.LastTestStatus, &c.UpdatedAt); err != nil {
			return nil, err
		}
		if dst, ok := byKind[k]; ok {
			dst.Set, dst.Org, dst.LastTestedAt, dst.LastTestStatus, dst.UpdatedAt = true, c.Org, c.LastTestedAt, c.LastTestStatus, c.UpdatedAt
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	states, err := s.surfaceStates(ctx, s.db)
	if err != nil {
		return nil, err
	}
	for _, spec := range engines.All() {
		st := states[spec.Surface]
		rs, err := engines.RouteOf(spec.Surface, st.route)
		if err != nil {
			continue
		}
		if st.enabled || st.route != engines.RouteTreg {
			c := byKind[rs.Credential()]
			c.UsedBy = append(c.UsedBy, spec.Surface)
		}
	}
	return out, nil
}

func (s *Service) Credential(ctx context.Context, kind engines.CredentialKind) (Credential, error) {
	all, err := s.Credentials(ctx)
	for _, c := range all {
		if c.Kind == kind {
			return c, err
		}
	}
	return Credential{}, err
}

// SetCredential stores a key sealed with ENCRYPTION_KEY, replacing the one
// of that kind. org is only for a personal Treg token.
func (s *Service) SetCredential(ctx context.Context, rawKind, secret, org string) (Credential, error) {
	kind, err := parseCredentialKind(rawKind)
	if err != nil {
		return Credential{}, err
	}
	secret, org = strings.TrimSpace(secret), strings.TrimSpace(org)
	switch {
	case secret == "" || len(secret) > 1024 || strings.ContainsAny(secret, " \t\r\n"):
		return Credential{}, fmt.Errorf("%w: secret must be the %s key as %s shows it, with no spaces", core.ErrInvalid, credentialTitle(kind), credentialTitle(kind))
	case kind == engines.CredentialDataForSEO && !strings.Contains(secret, ":"):
		return Credential{}, fmt.Errorf("%w: a DataForSEO key is your API login and password as login:password", core.ErrInvalid)
	case org != "" && kind != engines.CredentialTreg:
		return Credential{}, fmt.Errorf("%w: org is only for a personal Treg token", core.ErrInvalid)
	}
	_, err = s.db.Exec(ctx, `INSERT INTO credential (kind, ciphertext, org) VALUES ($1, $2, $3)
		ON CONFLICT (kind) DO UPDATE SET ciphertext = excluded.ciphertext, org = excluded.org,
			last_tested_at = NULL, last_test_status = NULL, updated_at = now()`, kind, s.secrets.Seal(secret, seal(kind)), org)
	if err != nil {
		return Credential{}, err
	}
	return s.Credential(ctx, kind)
}

// TestCredential makes one free call with the stored key, where the
// provider has one, and records how it went. A failed test is a result,
// not an error.
func (s *Service) TestCredential(ctx context.Context, rawKind string) (Credential, error) {
	kind, err := parseCredentialKind(rawKind)
	if err != nil {
		return Credential{}, err
	}
	conn, err := s.conn(ctx, kind)
	if err != nil {
		return Credential{}, err
	}
	status := "ok"
	if kind == engines.CredentialGateway {
		err = s.db.QueryRow(ctx, `SELECT base_url FROM surface WHERE route = 'gateway' ORDER BY enabled DESC, key LIMIT 1`).Scan(&conn.BaseURL)
		if err == pgx.ErrNoRows {
			status = "untested: no surface uses the gateway yet, and its base URL is set per surface"
		} else if err != nil {
			return Credential{}, err
		}
	}
	if status == "ok" {
		pctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		err := engines.Probe(pctx, kind, conn)
		cancel()
		switch {
		case errors.Is(err, engines.ErrNoProbe):
			status = "untested: " + credentialTitle(kind) + " has no free call to test a key with; Test a surface that uses it"
		case err != nil:
			status = err.Error()
		}
	}
	if _, err := s.db.Exec(ctx, `UPDATE credential SET last_tested_at = $2, last_test_status = $3 WHERE kind = $1`, kind, s.now(), status); err != nil {
		return Credential{}, err
	}
	return s.Credential(ctx, kind)
}

// DeleteCredential removes a key no surface uses.
func (s *Service) DeleteCredential(ctx context.Context, rawKind string) error {
	kind, err := parseCredentialKind(rawKind)
	if err != nil {
		return err
	}
	c, err := s.Credential(ctx, kind)
	if err != nil {
		return err
	}
	if len(c.UsedBy) > 0 {
		return fmt.Errorf("%w: %s is used by %s; move those surfaces to another route first", core.ErrConflict, c.Title, joinSurfaces(c.UsedBy))
	}
	tag, err := s.db.Exec(ctx, `DELETE FROM credential WHERE kind = $1`, kind)
	if pg := (*pgconn.PgError)(nil); errors.As(err, &pg) && pg.Code == "23503" {
		return fmt.Errorf("%w: %s is still used by a surface; move it to another route first", core.ErrConflict, c.Title)
	}
	if err == nil && tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: no %s key is set", core.ErrNotFound, c.Title)
	}
	return err
}

func joinSurfaces(ss []engines.Surface) string {
	var titles []string
	for _, k := range ss {
		spec, _ := engines.Lookup(k)
		titles = append(titles, spec.Title)
	}
	return strings.Join(titles, ", ")
}

// conn opens the stored key of kind into what an adapter calls with.
func (s *Service) conn(ctx context.Context, kind engines.CredentialKind) (engines.Conn, error) {
	var sealed, org string
	err := s.db.QueryRow(ctx, `SELECT ciphertext, org FROM credential WHERE kind = $1`, kind).Scan(&sealed, &org)
	if err == pgx.ErrNoRows {
		return engines.Conn{}, fmt.Errorf("%w: no %s key is set; an admin sets it under Settings > Credentials", core.ErrInvalid, credentialTitle(kind))
	}
	if err != nil {
		return engines.Conn{}, err
	}
	secret, err := s.secrets.Open(sealed, seal(kind))
	if err != nil {
		return engines.Conn{}, fmt.Errorf("open the %s key: %w; was ENCRYPTION_KEY changed without ENCRYPTION_KEY_PREVIOUS?", credentialTitle(kind), err)
	}
	return engines.Conn{Secret: secret, Org: org, BaseURL: s.baseURLs[kind], Client: s.client, Resolve: s.resolve}, nil
}

// Treg opens the stored Treg token as a client for Treg tools outside the
// surface registry, such as social search. ok is false when none is set.
func (s *Service) Treg(ctx context.Context) (engines.Treg, bool, error) {
	var set bool
	if err := s.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM credential WHERE kind = $1)`, engines.CredentialTreg).Scan(&set); err != nil || !set {
		return engines.Treg{}, false, err
	}
	c, err := s.conn(ctx, engines.CredentialTreg)
	if err != nil {
		return engines.Treg{}, false, err
	}
	return engines.Treg{Client: c.Client, BaseURL: c.BaseURL, Token: c.Secret, Org: c.Org}, true, nil
}

// SealCredentials reseals keys sealed under ENCRYPTION_KEY_PREVIOUS with
// ENCRYPTION_KEY, so a key rotation finishes with one restart.
func (s *Service) SealCredentials(ctx context.Context) (int, error) {
	rows, err := s.db.Query(ctx, `SELECT kind, ciphertext FROM credential ORDER BY kind`)
	if err != nil {
		return 0, err
	}
	type row struct {
		kind   engines.CredentialKind
		sealed string
	}
	all, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (row, error) {
		var v row
		return v, r.Scan(&v.kind, &v.sealed)
	})
	if err != nil {
		return 0, err
	}
	n := 0
	for _, c := range all {
		resealed, changed, err := s.secrets.Reseal(c.sealed, seal(c.kind))
		if err != nil {
			return n, err
		}
		if !changed {
			continue
		}
		if _, err := s.db.Exec(ctx, `UPDATE credential SET ciphertext = $1 WHERE kind = $2 AND ciphertext = $3`, resealed, c.kind, c.sealed); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}
