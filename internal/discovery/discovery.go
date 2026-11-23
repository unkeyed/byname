// Package discovery finds the brands AI answers name that nobody tracks.
// A small model reads each new answer through Treg and lists the brands
// it names; the ones that are not your brand or a tracked competitor,
// under any spelling, are stored as untracked brands with the answers that
// named them. Track turns one into a competitor, Not a competitor hides
// it. Nothing here changes a score: scores read only tracked brands.
//
// Reading an answer is paid, so it runs only after onboarding, only while
// it is on and a Treg token is stored, and inside the spend caps. A read
// that fails never fails the check; the answer simply has no found brands
// until a later pass reads it again.
package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/unkeyed/byname/internal/db"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/unkeyed/byname/internal/config"
	"github.com/unkeyed/byname/internal/core"
	"github.com/unkeyed/byname/internal/engines"
	"github.com/unkeyed/byname/internal/panel"
)

// Config is what discovery reads from internal/config.
type Config interface {
	Treg(ctx context.Context) (engines.Treg, bool, error)
	Onboarded(ctx context.Context) (bool, error)
	PanelBrands(ctx context.Context) ([]panel.Brand, error)
	MatchRules(ctx context.Context) ([]panel.Brand, panel.MatchMode, error)
	BrandChange(ctx context.Context, f func(pgx.Tx) error) error
	Settings(ctx context.Context) (config.SettingsView, error)
}

type Service struct {
	db     db.Pool
	config Config
	now    func() time.Time
	log    *slog.Logger
}

type Options struct {
	Now func() time.Time
	Log *slog.Logger
}

func New(db db.Pool, cfg Config, o Options) *Service {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Log == nil {
		o.Log = slog.Default()
	}
	return &Service{db: db, config: cfg, now: o.Now, log: o.Log}
}

// maxAttempts is how many times an answer is read before it is left
// unread: a failed read is retried by later passes, at most this often.
const maxAttempts = 3

// The model's instructions. Each fits DataForSEO's 500-character field
// limit; TestPromptFitsTheTool checks.
const (
	system = `You read one answer from an AI assistant and list every brand it names: companies, and products or services sold by a company. ` +
		`Reply with JSON only: {"brands":[{"name":"...","kind":"brand","domain":"..."}]}, in order of first mention. ` +
		`Use kind "technology" for languages, standards, protocols and open-source projects with no single vendor. ` +
		`domain is the brand's main website, or "" if unsure. Leave out people, places, and sites cited only as sources.`
	prompt = `List the brands named in the answer above. JSON only.`
)

// Found is one brand the model listed.
type Found struct {
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Domain string `json:"domain"`
}

// parseFound reads the model's reply: the JSON object in it, fenced or
// not. Entries without a usable name are dropped; a domain that is not a
// hostname is blanked.
func parseFound(text string) ([]Found, error) {
	start, end := strings.IndexByte(text, '{'), strings.LastIndexByte(text, '}')
	if start < 0 || end < start {
		return nil, errors.New("the reply holds no JSON object")
	}
	var reply struct {
		Brands []Found `json:"brands"`
	}
	if err := json.Unmarshal([]byte(text[start:end+1]), &reply); err != nil {
		return nil, fmt.Errorf("the reply is not the JSON asked for: %w", err)
	}
	out := []Found{}
	for _, f := range reply.Brands {
		f.Name = panel.DisplayName(f.Name)
		f.Kind = strings.ToLower(strings.TrimSpace(f.Kind))
		f.Domain = hostname(f.Domain)
		if n := len([]rune(f.Name)); n < 1 || n > 80 || panel.NameKey(f.Name) == "" {
			continue
		}
		out = append(out, f)
	}
	return out, nil
}

// hostname is a domain as brand.domains stores it, or "".
func hostname(d string) string {
	d = strings.ToLower(strings.TrimSpace(d))
	if i := strings.Index(d, "://"); i >= 0 {
		d = d[i+3:]
	}
	if i := strings.IndexAny(d, "/?#:"); i >= 0 {
		d = d[:i]
	}
	d = strings.TrimPrefix(strings.TrimSuffix(d, "."), "www.")
	if !strings.Contains(d, ".") || strings.ContainsAny(d, " _@") || len(d) > 253 {
		return ""
	}
	return d
}

// ready reports whether new answers are read: the setting is on, a Treg
// token is stored and onboarding is finished.
func (s *Service) ready(ctx context.Context) (engines.Treg, bool, error) {
	var on bool
	if err := s.db.QueryRow(ctx, `SELECT find_brands FROM instance_settings`).Scan(&on); err != nil || !on {
		return engines.Treg{}, false, err
	}
	done, err := s.config.Onboarded(ctx)
	if err != nil || !done {
		return engines.Treg{}, false, err
	}
	return s.config.Treg(ctx)
}

// Ready is whether new answers are read now; a check asks once at its
// start.
func (s *Service) Ready(ctx context.Context) (bool, error) {
	_, ok, err := s.ready(ctx)
	return ok, err
}

// read is one answer to read: its id, the check it belongs to (0 for
// none) and its text.
type read struct {
	answer, run int64
	text        string
}

// call records where a read's cost goes: the check that made it, or the
// job.
type call struct {
	run, job int64
}

// Find reads one stored answer of check runID for other brands and
// stores what it names. It is the check's call, right after the answer
// is stored, so its cost counts against the check's spend cap. Every
// outcome is recorded; an error means it could not be recorded.
func (s *Service) Find(ctx context.Context, answerID, runID int64, text string) error {
	treg, ok, err := s.ready(ctx)
	if err != nil || !ok {
		return err
	}
	_, err = s.find(ctx, treg, read{answer: answerID, run: runID, text: text}, call{run: runID})
	return err
}

// find asks the model about one answer and records the call and, when it
// answered, the brands it found. It returns Treg's refusal when Treg
// declined the call, so a pass can stop asking.
func (s *Service) find(ctx context.Context, treg engines.Treg, r read, c call) (*engines.Refusal, error) {
	var attempt int
	if err := s.db.QueryRow(ctx, `SELECT count(*) + 1 FROM brand_call WHERE purpose = 'find' AND answer_id = $1 AND status <> 'refused'`, r.answer).Scan(&attempt); err != nil {
		return nil, err
	}
	parts, _ := engines.SplitForLLM(r.text)
	res, err := treg.Ask(ctx, engines.LLMTask{
		Tool: engines.BrandFinder, System: system, Context: parts, Prompt: prompt, MaxOutputTokens: 1500,
		IdempotencyKey: fmt.Sprintf("byname/find/answer-%d/%d", r.answer, attempt),
	})
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	refusal, _ := errors.AsType[*engines.Refusal](err)
	var found []Found
	switch {
	case err != nil:
		res = engines.Result{Status: engines.StatusFailed, Error: err.Error()}
	case res.Status == engines.StatusFailed:
	default:
		if found, err = parseFound(res.Text); err != nil {
			res.Status, res.Error = engines.StatusFailed, err.Error()
		}
	}
	status, msg := "ok", (*string)(nil)
	switch {
	case refusal != nil:
		status, msg = "refused", &res.Error
	case res.Status == engines.StatusFailed:
		status, msg = "failed", &res.Error
	}
	// The call is recorded on its own first: it is paid whatever happens
	// to what it found.
	var call int64
	if err := s.db.QueryRow(ctx, `INSERT INTO brand_call (purpose, answer_id, run_id, job_run_id, tool, status, cost_usd, treg_call_id, error, called_at)
		VALUES ('find', $1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id`,
		r.answer, nullID(c.run), nullID(c.job), engines.BrandFinder.ID, status, res.CostUSD, res.CallID, msg, s.now()).Scan(&call); err != nil {
		return refusal, err
	}
	if status != "ok" {
		return refusal, nil
	}
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error { return s.record(ctx, tx, r.answer, found) })
	if err != nil && ctx.Err() == nil {
		_, uerr := s.db.Exec(ctx, `UPDATE brand_call SET status = 'failed', error = $2 WHERE id = $1`, call, "storing the brands it found: "+err.Error())
		return refusal, uerr
	}
	return refusal, err
}

// record stores the found brands of one answer that no tracked brand
// covers, as untracked brands, and which of them the answer named.
// Technologies are dropped.
func (s *Service) record(ctx context.Context, tx pgx.Tx, answerID int64, found []Found) error {
	tracked, mode, err := s.config.MatchRules(ctx)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	ord := 0
	for _, f := range found {
		key := panel.NameKey(f.Name)
		if f.Kind != "brand" || seen[key] || panel.Covers(tracked, f.Name, f.Domain, mode) {
			continue
		}
		seen[key] = true
		id, err := upsertFound(ctx, tx, f, key)
		if err != nil {
			return err
		}
		ord++
		if _, err := tx.Exec(ctx, `INSERT INTO discovered_mention (answer_id, brand_id, ord) VALUES ($1, $2, $3)
			ON CONFLICT (answer_id, brand_id) DO NOTHING`, answerID, id, ord); err != nil {
			return err
		}
	}
	return nil
}

// upsertFound returns the untracked or hidden brand f is, found by its
// key or its domain, adding this spelling to its aliases; or adds it.
func upsertFound(ctx context.Context, tx pgx.Tx, f Found, key string) (int64, error) {
	var id int64
	err := tx.QueryRow(ctx, `SELECT id FROM brand WHERE key = $1 OR ($2 <> '' AND key IS NOT NULL AND domains[1] = $2)
		ORDER BY key = $1 DESC, id LIMIT 1`, key, f.Domain).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `INSERT INTO brand (name, domains, kind, key) VALUES ($1, $2, 'untracked', $3)
			ON CONFLICT (key) WHERE key IS NOT NULL DO UPDATE SET updated_at = now() RETURNING id`,
			f.Name, domains(f.Domain), key).Scan(&id)
	}
	if err != nil {
		return 0, err
	}
	_, err = tx.Exec(ctx, `UPDATE brand SET
			aliases = CASE WHEN lower($2) = lower(name) OR lower($2) = ANY (SELECT lower(a) FROM unnest(aliases) a) OR cardinality(aliases) >= 10
				THEN aliases ELSE aliases || $2::text END,
			domains = CASE WHEN cardinality(domains) = 0 THEN $3 ELSE domains END
		WHERE id = $1`, id, f.Name, domains(f.Domain))
	return id, err
}

func domains(d string) []string {
	if d == "" {
		return []string{}
	}
	return []string{d}
}

func nullID(id int64) *int64 {
	if id == 0 {
		return nil
	}
	return &id
}

// errNotFound is the error for an id that is no found brand.
func errNotFound(id int64) error {
	return fmt.Errorf("%w: no brand %d among the brands found in answers; list them with GET /v1/other-brands", core.ErrNotFound, id)
}
