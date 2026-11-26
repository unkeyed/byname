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
	UsedBy         []CredentialUse        `json:"used_by" jsonschema:"each entity with AI apps whose route calls with this key, and those apps; every entity shares the key"`
}

// CredentialUse is the surfaces in one entity whose route calls with a
// key.
type CredentialUse struct {
	Entity   string            `json:"entity" jsonschema:"the entity's name"`
	Slug     string            `json:"slug" jsonschema:"the entity's slug"`
	Surfaces []engines.Surface `json:"surfaces"`
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

// settingsPage is where an admin sets a key of kind: Treg has its own page,
// every other key is under Provider keys.
func settingsPage(kind engines.CredentialKind) string {
	if kind == engines.CredentialTreg {
		return "Treg"
	}
	return "Provider keys"
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
		out[i] = Credential{Kind: k, Title: credentialTitle(k), Hint: credentialHints[k], UsedBy: []CredentialUse{}}
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
	uses, err := s.credentialUses(ctx)
	if err != nil {
		return nil, err
	}
	for _, u := range uses {
		byKind[u.kind].UsedBy = append(byKind[u.kind].UsedBy, u.CredentialUse)
	}
	return out, nil
}

type credentialUse struct {
	kind engines.CredentialKind
	CredentialUse
}

type entityRef struct{ name, slug, schema string }

// everyEntity is every entity, in the order they were made, with the
// schema its tables are in, for a query that reads all of them.
func (s *Service) everyEntity(ctx context.Context) ([]entityRef, error) {
	rows, err := s.db.Query(ctx, `SELECT name, slug, schema_name FROM entity ORDER BY id`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (entityRef, error) {
		var w entityRef
		return w, r.Scan(&w.name, &w.slug, &w.schema)
	})
}

// acrossEntities is query run on each entity's table, as one UNION
// ALL whose rows start with the entity's index in all. query has one
// %s, the schema, and its first column is %d, the index.
func acrossEntities(all []entityRef, query string) string {
	union := make([]string, len(all))
	for i, w := range all {
		union[i] = fmt.Sprintf(query, i, pgx.Identifier{w.schema}.Sanitize())
	}
	return strings.Join(union, " UNION ALL ")
}

func (s *Service) credentialUses(ctx context.Context) ([]credentialUse, error) {
	all, err := s.everyEntity(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(ctx, acrossEntities(all, `SELECT %d, key, enabled, route FROM %s.surface`))
	if err != nil {
		return nil, err
	}
	states := make([]map[engines.Surface]surfaceState, len(all))
	for rows.Next() {
		var i int
		var k engines.Surface
		var st surfaceState
		if err := rows.Scan(&i, &k, &st.enabled, &st.route); err != nil {
			return nil, err
		}
		if states[i] == nil {
			states[i] = map[engines.Surface]surfaceState{}
		}
		states[i][k] = st
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var out []credentialUse
	for i, w := range all {
		byKind := map[engines.CredentialKind]*credentialUse{}
		var kinds []engines.CredentialKind
		for _, spec := range engines.All() {
			st, ok := states[i][spec.Surface]
			if !ok || !(st.enabled || st.route != engines.RouteTreg) {
				continue
			}
			rs, err := engines.RouteOf(spec.Surface, st.route)
			if err != nil {
				continue
			}
			u := byKind[rs.Credential()]
			if u == nil {
				u = &credentialUse{kind: rs.Credential(), CredentialUse: CredentialUse{Entity: w.name, Slug: w.slug}}
				byKind[u.kind] = u
				kinds = append(kinds, u.kind)
			}
			u.Surfaces = append(u.Surfaces, spec.Surface)
		}
		for _, k := range kinds {
			out = append(out, *byKind[k])
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
	var status string
	if kind == engines.CredentialGateway {
		if status, err = s.testGateways(ctx, conn); err != nil {
			return Credential{}, err
		}
	} else {
		status = probe(ctx, kind, conn)
	}
	if _, err := s.db.Exec(ctx, `UPDATE credential SET last_tested_at = $2, last_test_status = $3 WHERE kind = $1`, kind, s.now(), status); err != nil {
		return Credential{}, err
	}
	return s.Credential(ctx, kind)
}

// probe is how one free call with conn went: ok, or why not.
func probe(ctx context.Context, kind engines.CredentialKind, conn engines.Conn) string {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	err := engines.Probe(ctx, kind, conn)
	switch {
	case errors.Is(err, engines.ErrNoProbe):
		return "untested: " + credentialTitle(kind) + " has no free call to test a key with; Test a surface that uses it"
	case err != nil:
		return err.Error()
	}
	return "ok"
}

// testGateways calls each gateway base URL a surface in any entity
// uses, since they all share the key. Several URLs read ok only when every
// one answered, and otherwise say how each went and which entities use
// it.
func (s *Service) testGateways(ctx context.Context, conn engines.Conn) (string, error) {
	all, err := s.everyEntity(ctx)
	if err != nil {
		return "", err
	}
	rows, err := s.db.Query(ctx, acrossEntities(all, `SELECT %d, base_url FROM %s.surface WHERE route = 'gateway'`)+` ORDER BY 1, 2`)
	if err != nil {
		return "", err
	}
	var urls []string
	usedBy := map[string][]string{}
	for rows.Next() {
		var i int
		var url string
		if err := rows.Scan(&i, &url); err != nil {
			return "", err
		}
		if !slices.Contains(urls, url) {
			urls = append(urls, url)
		}
		if slug := all[i].slug; !slices.Contains(usedBy[url], slug) {
			usedBy[url] = append(usedBy[url], slug)
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	switch len(urls) {
	case 0:
		return "untested: no surface in any entity uses the gateway yet, and its base URL is set per surface", nil
	case 1:
		conn.BaseURL = urls[0]
		return probe(ctx, engines.CredentialGateway, conn), nil
	}
	ok := true
	each := make([]string, len(urls))
	for i, url := range urls {
		conn.BaseURL = url
		status := probe(ctx, engines.CredentialGateway, conn)
		ok = ok && status == "ok"
		each[i] = fmt.Sprintf("%s at %s, used by %s", status, url, strings.Join(usedBy[url], ", "))
	}
	if ok {
		return "ok", nil
	}
	return strings.Join(each, "; "), nil
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
		var where []string
		for _, u := range c.UsedBy {
			where = append(where, joinSurfaces(u.Surfaces)+" in "+u.Entity)
		}
		return fmt.Errorf("%w: %s is used by %s; move those surfaces to another route first", core.ErrConflict, c.Title, strings.Join(where, "; "))
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
		return engines.Conn{}, fmt.Errorf("%w: no %s key is set; an admin sets it under Settings > %s", core.ErrInvalid, credentialTitle(kind), settingsPage(kind))
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
