package config_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/unkeyed/byname/internal/config"
	"github.com/unkeyed/byname/internal/core"
	"github.com/unkeyed/byname/internal/db"
	"github.com/unkeyed/byname/internal/engines"
	"github.com/unkeyed/byname/internal/engines/tregtest"
	"github.com/unkeyed/byname/internal/secretbox"
	"github.com/unkeyed/byname/internal/testdb"
)

// Each workspace sees only its own brand and questions, and every
// workspace shares the team and the provider keys.
func TestWorkspacesKeepTheirOwnSetup(t *testing.T) {
	// One connection, so every query below runs on it, switching between
	// the workspaces as it goes.
	cfg := testdb.New(t).Config()
	cfg.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	second := testdb.Workspace(t, pool)
	opts := config.Options{Secrets: secretbox.ForTest(), TregBaseURL: tregtest.New(t).URL, Now: time.Now}
	northwind, kyring := config.New(pool, opts), config.New(second, opts)

	if _, err := northwind.PutSettings(ctx, settings()); err != nil {
		t.Fatal(err)
	}
	other := settings()
	other.Brand = config.SelfBrand{Name: "Kyring", Domains: []string{"kyring.example"}}
	other.CheckEveryDays = 7
	if _, err := kyring.PutSettings(ctx, other); err != nil {
		t.Fatal(err)
	}
	if _, err := northwind.PutQuestions(ctx, config.QuestionsInput{Questions: []config.QuestionInput{
		q("Deploying", "How do I deploy a REST API?"),
		q("Hosting", "What is the best hosting platform?"),
	}}); err != nil {
		t.Fatal(err)
	}
	ks, err := kyring.PutQuestions(ctx, config.QuestionsInput{Questions: []config.QuestionInput{
		q("Contacts", "Which CRM keeps track of who signed up?"),
	}})
	if err != nil {
		t.Fatal(err)
	}

	for _, c := range []struct {
		svc        *config.Service
		brand      string
		every      int
		questions  int
		firstAsked string
	}{
		{northwind, "Northwind", 3, 2, "How do I deploy a REST API?"},
		{kyring, "Kyring", 7, 1, "Which CRM keeps track of who signed up?"},
	} {
		s, err := c.svc.Settings(ctx)
		if err != nil || s.Brand.Name != c.brand || s.CheckEveryDays != c.every {
			t.Fatalf("%s's settings: %+v %v", c.brand, s, err)
		}
		brands, err := c.svc.Brands(ctx)
		if err != nil || len(brands) != 1 || brands[0].Name != c.brand {
			t.Fatalf("%s's brands: %+v %v, want its own only", c.brand, brands, err)
		}
		set, err := c.svc.CurrentQuestions(ctx)
		if err != nil || set.Version != 1 || len(set.Questions) != c.questions || set.Questions[0].Text != c.firstAsked {
			t.Fatalf("%s's questions: %+v %v, want version 1 with its own %d", c.brand, set, err, c.questions)
		}
	}

	if _, err := northwind.SetCredential(ctx, "anthropic", "sk-ant-live-secret", ""); err != nil {
		t.Fatal(err)
	}
	cred, err := kyring.Credential(ctx, "anthropic")
	if err != nil || !cred.Set {
		t.Fatalf("the key one workspace set, read from another: %+v %v, want it set", cred, err)
	}
	if _, err := second.Exec(ctx, `INSERT INTO surface (key, enabled, route, credential_id)
		SELECT 'claude_code', true, 'provider', id FROM credential WHERE kind = 'anthropic'`); err != nil {
		t.Fatalf("a workspace's surface on the shared key: %v", err)
	}
	// The key is in use whichever workspace asks: Kyring's AI app runs
	// on it, though Northwind's do not.
	w, err := db.CurrentWorkspace(ctx, second)
	if err != nil {
		t.Fatal(err)
	}
	want := []config.CredentialUse{{Workspace: w.Name, Slug: w.Slug, Surfaces: []engines.Surface{engines.ClaudeCode}}}
	for _, svc := range []*config.Service{northwind, kyring} {
		if cred, err := svc.Credential(ctx, "anthropic"); err != nil || !reflect.DeepEqual(cred.UsedBy, want) {
			t.Fatalf("used by %+v %v, want %+v", cred.UsedBy, err, want)
		}
	}
	err = northwind.DeleteCredential(ctx, "anthropic")
	is(t, err, core.ErrConflict)
	if !strings.Contains(err.Error(), "Claude Code in "+w.Name) {
		t.Fatalf("deleting a key another workspace uses: %v, want it to name the workspace", err)
	}

	var userID int64
	if err := pool.QueryRow(ctx, `INSERT INTO users (email, role) VALUES ('maya@northwind.example', 'admin') RETURNING id`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := kyring.MarkHandled(ctx, core.Actor{UserID: userID}, ks.Questions[0].ID, config.QuestionPageInput{URL: "kyring.example/crm"}); err != nil {
		t.Fatalf("a teammate marking a page in another workspace: %v", err)
	}
	var pages int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM question_page`).Scan(&pages); err != nil || pages != 0 {
		t.Fatalf("the first workspace has %d marked pages (%v), want none", pages, err)
	}
}
