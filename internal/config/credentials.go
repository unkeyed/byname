package config

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/unkeyed/byname/internal/core"
	"github.com/unkeyed/byname/internal/engines"
)

// Credential is what Byname shows about a stored secret. The secret itself
// is never returned by any API.
type Credential struct {
	Kind           string     `json:"kind"`
	Set            bool       `json:"set"`
	Org            string     `json:"org"`
	LastTestedAt   *time.Time `json:"last_tested_at"`
	LastTestStatus string     `json:"last_test_status" jsonschema:"ok, or why the last test failed"`
	UpdatedAt      *time.Time `json:"updated_at"`
}

const tregSeal = "credential:treg"

func (s *Service) credential(ctx context.Context) (Credential, error) {
	c := Credential{Kind: "treg"}
	err := s.db.QueryRow(ctx, `SELECT org, last_tested_at, coalesce(last_test_status, ''), updated_at FROM credential WHERE kind = 'treg'`).
		Scan(&c.Org, &c.LastTestedAt, &c.LastTestStatus, &c.UpdatedAt)
	if err == pgx.ErrNoRows {
		return c, nil
	}
	c.Set = err == nil
	return c, err
}

// Credentials lists every credential kind and whether it is set.
func (s *Service) Credentials(ctx context.Context) ([]Credential, error) {
	c, err := s.credential(ctx)
	return []Credential{c}, err
}

// SetTregToken stores the Treg token sealed with ENCRYPTION_KEY. org is
// Treg's X-Treg-Org, which only a personal token needs.
func (s *Service) SetTregToken(ctx context.Context, token, org string) (Credential, error) {
	token, org = strings.TrimSpace(token), strings.TrimSpace(org)
	if token == "" || len(token) > 512 || strings.ContainsAny(token, " \t\r\n") {
		return Credential{}, fmt.Errorf("%w: token must be the Treg token as Treg shows it, with no spaces", core.ErrInvalid)
	}
	_, err := s.db.Exec(ctx, `INSERT INTO credential (kind, ciphertext, org) VALUES ('treg', $1, $2)
		ON CONFLICT (kind) DO UPDATE SET ciphertext = excluded.ciphertext, org = excluded.org,
			last_tested_at = NULL, last_test_status = NULL, updated_at = now()`, s.secrets.Seal(token, tregSeal), org)
	if err != nil {
		return Credential{}, err
	}
	return s.credential(ctx)
}

// TestTregToken makes one free call, a model list, with the stored token
// and records how it went. A failed test is a result, not an error.
func (s *Service) TestTregToken(ctx context.Context) (Credential, error) {
	t, err := s.Treg(ctx)
	if err != nil {
		return Credential{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	status := "ok"
	if _, err := t.ListModels(ctx, engines.ClaudeCode); err != nil {
		status = err.Error()
	}
	if _, err := s.db.Exec(ctx, `UPDATE credential SET last_tested_at = $1, last_test_status = $2 WHERE kind = 'treg'`, s.now(), status); err != nil {
		return Credential{}, err
	}
	return s.credential(ctx)
}

// Treg is the Treg adapter with the stored token.
func (s *Service) Treg(ctx context.Context) (engines.Treg, error) {
	var sealed, org string
	err := s.db.QueryRow(ctx, `SELECT ciphertext, org FROM credential WHERE kind = 'treg'`).Scan(&sealed, &org)
	if err == pgx.ErrNoRows {
		return engines.Treg{}, fmt.Errorf("%w: no Treg token is set; an admin sets it under Settings", core.ErrInvalid)
	}
	if err != nil {
		return engines.Treg{}, err
	}
	token, err := s.secrets.Open(sealed, tregSeal)
	if err != nil {
		return engines.Treg{}, fmt.Errorf("open the Treg token: %w; was ENCRYPTION_KEY changed without ENCRYPTION_KEY_PREVIOUS?", err)
	}
	t := s.treg
	t.Token, t.Org = token, org
	return t, nil
}

// SealCredentials reseals credentials sealed under ENCRYPTION_KEY_PREVIOUS
// with ENCRYPTION_KEY, so a key rotation finishes with one restart.
func (s *Service) SealCredentials(ctx context.Context) (int, error) {
	var sealed string
	err := s.db.QueryRow(ctx, `SELECT ciphertext FROM credential WHERE kind = 'treg'`).Scan(&sealed)
	if err == pgx.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	resealed, changed, err := s.secrets.Reseal(sealed, tregSeal)
	if err != nil || !changed {
		return 0, err
	}
	_, err = s.db.Exec(ctx, `UPDATE credential SET ciphertext = $1 WHERE kind = 'treg' AND ciphertext = $2`, resealed, sealed)
	return 1, err
}
