package config_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
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

// Each entity sees only its own brand and questions, and every
// entity shares the team and the provider keys.
func TestEntitiesKeepTheirOwnSetup(t *testing.T) {
	cfg := testdb.New(t).Config()
	cfg.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	second := testdb.Entity(t, pool)
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
		t.Fatalf("the key one entity set, read from another: %+v %v, want it set", cred, err)
	}
	if _, err := second.Exec(ctx, `INSERT INTO surface (key, enabled, route, credential_id)
		SELECT 'claude_code', true, 'provider', id FROM credential WHERE kind = 'anthropic'`); err != nil {
		t.Fatalf("an entity's surface on the shared key: %v", err)
	}
	w, err := db.CurrentEntity(ctx, second)
	if err != nil {
		t.Fatal(err)
	}
	want := []config.CredentialUse{{Entity: w.Name, Slug: w.Slug, Surfaces: []engines.Surface{engines.ClaudeCode}}}
	for _, svc := range []*config.Service{northwind, kyring} {
		if cred, err := svc.Credential(ctx, "anthropic"); err != nil || !reflect.DeepEqual(cred.UsedBy, want) {
			t.Fatalf("used by %+v %v, want %+v", cred.UsedBy, err, want)
		}
	}
	err = northwind.DeleteCredential(ctx, "anthropic")
	is(t, err, core.ErrConflict)
	if !strings.Contains(err.Error(), "Claude Code in "+w.Name) {
		t.Fatalf("deleting a key another entity uses: %v, want it to name the entity", err)
	}

	var userID int64
	if err := pool.QueryRow(ctx, `INSERT INTO users (email, role) VALUES ('maya@northwind.example', 'admin') RETURNING id`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := kyring.MarkHandled(ctx, core.Actor{UserID: userID}, ks.Questions[0].ID, config.QuestionPageInput{URL: "kyring.example/crm"}); err != nil {
		t.Fatalf("a teammate marking a page in another entity: %v", err)
	}
	var pages int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM question_page`).Scan(&pages); err != nil || pages != 0 {
		t.Fatalf("the first entity has %d marked pages (%v), want none", pages, err)
	}
}

// Saving the brand step names a new instance's first entity after the
// brand, once, and leaves every other entity's name alone.
func TestTheBrandStepNamesTheFirstEntity(t *testing.T) {
	pool := testdb.New(t)
	second := testdb.Entity(t, pool)
	opts := config.Options{Secrets: secretbox.ForTest(), Now: time.Now}
	first, later := config.New(pool, opts), config.New(second, opts)
	before, err := db.CurrentEntity(ctx, second)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		svc   *config.Service
		brand string
	}{{first, "Northwind Cloud"}, {later, "Kyring"}} {
		s := settings()
		s.Brand.Name = c.brand
		if _, err := c.svc.PutSettings(ctx, s); err != nil {
			t.Fatal(err)
		}
		if _, err := c.svc.SaveStep(ctx, config.StepBrand); err != nil {
			t.Fatal(err)
		}
	}
	if w, err := db.CurrentEntity(ctx, pool); err != nil || w.Name != "Northwind Cloud" || w.Slug != "northwind-cloud" {
		t.Fatalf("first entity = %+v %v, want it named after its brand", w, err)
	}
	if w, err := db.CurrentEntity(ctx, second); err != nil || w != before {
		t.Fatalf("a later entity = %+v %v, want it as made, %+v", w, err, before)
	}

	s := settings()
	s.Brand.Name = "Renamed"
	if _, err := first.PutSettings(ctx, s); err != nil {
		t.Fatal(err)
	}
	if _, err := first.SaveStep(ctx, config.StepBrand); err != nil {
		t.Fatal(err)
	}
	if w, err := db.CurrentEntity(ctx, pool); err != nil || w.Slug != "northwind-cloud" {
		t.Fatalf("saving the brand step again = %+v %v, want the slug links already use", w, err)
	}
}

// The gateway key is shared, and each entity's surfaces set the
// gateway's base URL, so testing the key calls every base URL any
// entity uses and says how each went.
func TestTestingTheGatewayKeyCallsEveryEntitiesGateway(t *testing.T) {
	pool := testdb.New(t)
	second, err := db.CreateEntity(ctx, pool, "Kyring", "kyring")
	if err != nil {
		t.Fatal(err)
	}
	kyringDB := db.In(pool, second.Schema)
	gateway := func(code int) (*httptest.Server, *atomic.Int32) {
		var calls atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			w.WriteHeader(code)
			fmt.Fprint(w, `{"data":[{"id":"m"}],"error":{"message":"bad key"}}`)
		}))
		t.Cleanup(srv.Close)
		return srv, &calls
	}
	svc := config.New(pool, config.Options{Secrets: secretbox.ForTest(), Now: time.Now})
	if _, err := svc.SetCredential(ctx, "gateway", "gw-secret", ""); err != nil {
		t.Fatal(err)
	}
	good, goodCalls := gateway(http.StatusOK)
	bad, badCalls := gateway(http.StatusUnauthorized)
	for _, c := range []struct {
		pool db.Pool
		url  string
	}{{pool, good.URL}, {kyringDB, bad.URL}} {
		if _, err := c.pool.Exec(ctx, `INSERT INTO surface (key, enabled, route, base_url, credential_id)
			SELECT $2, true, 'gateway', $1, id FROM credential WHERE kind = 'gateway'`, c.url, engines.ChatGPTApp); err != nil {
			t.Fatal(err)
		}
	}

	c, err := svc.TestCredential(ctx, "gateway")
	if err != nil {
		t.Fatal(err)
	}
	if goodCalls.Load() != 1 || badCalls.Load() != 1 {
		t.Fatalf("gateway calls: %d to the first entity's, %d to Kyring's; want one each (status %q)", goodCalls.Load(), badCalls.Load(), c.LastTestStatus)
	}
	if !strings.Contains(c.LastTestStatus, "kyring") || !strings.Contains(c.LastTestStatus, bad.URL) || c.LastTestStatus == "ok" {
		t.Fatalf("last_test_status = %q, want Kyring's gateway at %s named as failing", c.LastTestStatus, bad.URL)
	}

	if _, err := kyringDB.Exec(ctx, `UPDATE surface SET base_url = $1 WHERE key = $2`, good.URL, engines.ChatGPTApp); err != nil {
		t.Fatal(err)
	}
	if c, err := svc.TestCredential(ctx, "gateway"); err != nil || c.LastTestStatus != "ok" || goodCalls.Load() != 2 {
		t.Fatalf("both entities on one working gateway: %q, %v, %d calls to it; want ok from one call", c.LastTestStatus, err, goodCalls.Load())
	}
}
