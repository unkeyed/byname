// Package auth answers "who is calling?" for every inbound request.
// Agents present API keys (KeyStore) to REST, or OAuth access tokens
// (OAuthServer) to MCP. Humans present a session cookie (Users). Each
// resolves to a core.Actor carrying the caller's role and the scopes it
// may use, which handlers pass to the services.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"errors"
	"fmt"
	"github.com/unkeyed/byname/internal/db"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/unkeyed/byname/internal/core"
)

var (
	ErrInvalidKey   = errors.New("invalid api key")
	ErrUnauthorized = errors.New("unauthorized")
)

// KeyStore issues and verifies API keys for REST. Exactly one
// implementation is live per process, chosen once in NewKeyStore.
type KeyStore interface {
	// Verify returns the key behind a presented token, or ErrInvalidKey.
	// Any other error means the store could not decide (Unkey down, database
	// gone), and callers must treat it as a failure, never as a pass.
	Verify(ctx context.Context, token string) (VerifiedKey, error)
	// Create returns the plaintext once. It is never retrievable again.
	// The key's scopes must be ones by's role has.
	Create(ctx context.Context, by core.Actor, opts KeyOptions) (plaintext string, k Key, err error)
	List(ctx context.Context) ([]Key, error)
	// Revoke stops a key verifying. An id this store did not issue is
	// core.ErrNotFound.
	Revoke(ctx context.Context, id string) error
	// RevokeCreatedBy stops every key the user created verifying. Remove
	// calls it inside the transaction that deletes the user, before the
	// delete clears api_keys.created_by.
	RevokeCreatedBy(ctx context.Context, tx pgx.Tx, userID int64) error
	// Mode is "builtin" or "unkey", shown on the Settings page.
	Mode() string
}

type Key struct {
	ID         string       `json:"id"`
	Name       string       `json:"name"`
	Prefix     string       `json:"prefix"` // "byn_ab12" so people can tell keys apart
	Scopes     []core.Scope `json:"scopes"` // as granted; a request gets only what the creator's role still allows
	ExpiresAt  *time.Time   `json:"expires_at,omitempty"`
	CreatedAt  time.Time    `json:"created_at"`
	LastUsedAt *time.Time   `json:"last_used_at,omitempty"`
}

// KeyOptions is what a person chooses when creating a key.
type KeyOptions struct {
	Name string `json:"name"`
	// Scopes is what the key may do: at least one, and only scopes the
	// creator's role has.
	Scopes []core.Scope `json:"scopes"`
	// ExpiresAt, when set, is when the key stops verifying.
	ExpiresAt *time.Time `json:"expires_at"`
}

// maxKeyLifetime bounds ExpiresAt, so "expires" always means within a few
// years rather than effectively never.
const maxKeyLifetime = 2 * 366 * 24 * time.Hour

// VerifiedKey is a key that verified: what it was granted and who created
// it. Authenticator.Caller turns it into an actor with the creator's
// current role.
type VerifiedKey struct {
	ID        string
	Name      string
	CreatedBy int64 // users.id, or 0 when the store does not know
	Scopes    []core.Scope
}

// check validates and normalizes the options both stores accept.
func (o KeyOptions) check(by core.Actor, now time.Time) (KeyOptions, error) {
	name, err := keyName(o.Name)
	if err != nil {
		return o, err
	}
	o.Name = name
	if o.Scopes, err = core.ParseScopes(o.Scopes); err != nil {
		return o, err
	}
	if len(o.Scopes) == 0 {
		return o, fmt.Errorf("%w: choose at least one scope", core.ErrInvalid)
	}
	if err := core.Grant(by.Role, o.Scopes); err != nil {
		return o, err
	}
	if o.ExpiresAt != nil {
		t := o.ExpiresAt.UTC().Truncate(time.Second)
		if !t.After(now.Add(time.Minute)) || t.After(now.Add(maxKeyLifetime)) {
			return o, fmt.Errorf("%w: expires_at must be in the future and within two years", core.ErrInvalid)
		}
		o.ExpiresAt = &t
	}
	return o, nil
}

type UnkeyConfig struct {
	RootKey string
	APIID   string
}

// NewKeyStore picks Unkey when both UNKEY_ROOT_KEY and UNKEY_API_ID are set,
// otherwise the built-in Postgres store. Setting only one of the two is a
// config error raised by main before this is called.
func NewKeyStore(db db.Pool, unkey UnkeyConfig) KeyStore {
	if unkey.RootKey != "" {
		return newUnkeyKeys(unkey, unkeyBaseURL)
	}
	return &dbKeys{db: db}
}

const keyPrefix = "byn_"

// keyName validates the human label both stores require.
func keyName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 255 {
		return "", fmt.Errorf("%w: key name must be 1 to 255 characters", core.ErrInvalid)
	}
	return name, nil
}

// displayPrefix is the part of a key shown in lists: "byn_" plus four
// characters, enough to tell keys apart and far too short to guess from.
func displayPrefix(key string) string {
	return key[:min(len(key), len(keyPrefix)+4)]
}

// wellFormedKey is whether token could be a key Byname issued: "byn_"
// then 52 base32 characters (built-in) or about 44 base58 characters
// (Unkey, 32 bytes).
func wellFormedKey(token string) bool {
	rest, ok := strings.CutPrefix(token, keyPrefix)
	return ok && len(rest) >= 32 && len(rest) <= 64 && !strings.ContainsFunc(rest, func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9')
	})
}

// tokenEncoding is lowercase RFC 4648 base32 without padding, so tokens are
// safe in URLs, headers and cookies.
var tokenEncoding = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

// newToken returns 32 random bytes, encoded. API keys, session cookies and
// invite tokens all use it, and each is stored only as hashToken(token).
func newToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b) // never fails since Go 1.24
	return tokenEncoding.EncodeToString(b)
}

// hashToken is the stored form of a token. Tokens carry 256 random bits, so
// a plain SHA-256 is enough and no slow hash is needed.
func hashToken(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}

// dbKeys stores SHA-256(token) in api_keys. Verify looks a key up by its
// hash alone, so no secret byte is ever compared in Go.
type dbKeys struct {
	db db.Pool
}

func (k *dbKeys) Verify(ctx context.Context, token string) (VerifiedKey, error) {
	// The UPDATE only fires when last_used_at is a minute stale, so a busy
	// key costs one write a minute, not one per request.
	var id int64
	var createdBy *int64
	var v VerifiedKey
	err := k.db.QueryRow(ctx, `
		WITH k AS (
			SELECT id, name, scopes, created_by, last_used_at FROM api_keys
			WHERE token_hash = $1 AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > now())
		), touch AS (
			UPDATE api_keys SET last_used_at = now() FROM k
			WHERE api_keys.id = k.id
			  AND (k.last_used_at IS NULL OR k.last_used_at < now() - interval '1 minute')
		)
		SELECT id, name, scopes, created_by FROM k`, hashToken(token)).Scan(&id, &v.Name, &v.Scopes, &createdBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return VerifiedKey{}, ErrInvalidKey
	}
	if err != nil {
		return VerifiedKey{}, err
	}
	v.ID = strconv.FormatInt(id, 10)
	if createdBy != nil {
		v.CreatedBy = *createdBy
	}
	return v, nil
}

func (k *dbKeys) Create(ctx context.Context, by core.Actor, opts KeyOptions) (string, Key, error) {
	opts, err := opts.check(by, time.Now())
	if err != nil {
		return "", Key{}, err
	}
	token := keyPrefix + newToken()
	key := Key{Name: opts.Name, Prefix: displayPrefix(token), Scopes: opts.Scopes, ExpiresAt: opts.ExpiresAt}
	var id int64
	err = k.db.QueryRow(ctx, `
		INSERT INTO api_keys (name, prefix, token_hash, created_by, scopes, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING id, created_at`,
		key.Name, key.Prefix, hashToken(token), userID(by), key.Scopes, key.ExpiresAt).Scan(&id, &key.CreatedAt)
	if err != nil {
		return "", Key{}, err
	}
	key.ID = strconv.FormatInt(id, 10)
	return token, key, nil
}

func (k *dbKeys) List(ctx context.Context) ([]Key, error) {
	rows, err := k.db.Query(ctx, `
		SELECT id::text, name, prefix, scopes, expires_at, created_at, last_used_at FROM api_keys
		WHERE revoked_at IS NULL ORDER BY created_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Key, error) {
		var key Key
		err := row.Scan(&key.ID, &key.Name, &key.Prefix, &key.Scopes, &key.ExpiresAt, &key.CreatedAt, &key.LastUsedAt)
		return key, err
	})
}

// Revoke is idempotent: revoking a revoked key succeeds and keeps the first
// revocation time.
func (k *dbKeys) Revoke(ctx context.Context, id string) error {
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return core.ErrNotFound
	}
	tag, err := k.db.Exec(ctx, `UPDATE api_keys SET revoked_at = coalesce(revoked_at, now()) WHERE id = $1`, n)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return core.ErrNotFound
	}
	return nil
}

func (k *dbKeys) RevokeCreatedBy(ctx context.Context, tx pgx.Tx, userID int64) error {
	_, err := tx.Exec(ctx, `UPDATE api_keys SET revoked_at = coalesce(revoked_at, now()) WHERE created_by = $1`, userID)
	return err
}

func (k *dbKeys) Mode() string { return "builtin" }

// userID is the users.id behind a signed-in person, or nil for a key or
// an app acting for one.
func userID(a core.Actor) *int64 {
	if a.Via != core.ViaSession || a.UserID == 0 {
		return nil
	}
	return &a.UserID
}
