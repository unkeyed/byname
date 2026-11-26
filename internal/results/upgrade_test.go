package results_test

import (
	"testing"

	"github.com/unkeyed/byname/internal/config"
	"github.com/unkeyed/byname/internal/db"
	"github.com/unkeyed/byname/internal/results"
	"github.com/unkeyed/byname/internal/secretbox"
	"github.com/unkeyed/byname/internal/testdb"
)

// An instance that asked each question once upgrades in place: its answers
// read as each cell's first sample, it still asks once, and the cell can
// take a second sample.
func TestUpgradeKeepsAnswersAsTheFirstSample(t *testing.T) {
	pool := testdb.Through(t, "017_search.sql")
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	var setID, questionID, pairID, runID, answerID int64
	scan := func(dst *int64, sql string, args ...any) {
		t.Helper()
		if err := pool.QueryRow(ctx, sql, args...).Scan(dst); err != nil {
			t.Fatal(err)
		}
	}
	scan(&setID, `INSERT INTO question_set (version) VALUES (1) RETURNING id`)
	scan(&questionID, `INSERT INTO question (set_id, ord, group_name, text) VALUES ($1, 0, 'Deploying', 'How do I deploy a Go API?') RETURNING id`, setID)
	exec(`INSERT INTO surface (key, enabled) VALUES ('claude_code', true)`)
	scan(&pairID, `INSERT INTO surface_model (surface_key, model, fidelity) VALUES ('claude_code', 'claude-sonnet-5-5', 'model_match') RETURNING id`)
	scan(&runID, `INSERT INTO run (question_set_id, trigger, status) VALUES ($1, 'run_now', 'completed') RETURNING id`, setID)
	scan(&answerID, `INSERT INTO answer (run_id, question_id, surface_model_id, endpoint, fidelity, status, attempts, mentioned, cited, position)
		VALUES ($1, $2, $3, 'claude-live', 'model_match', 'ok', 1, true, false, 1) RETURNING id`, runID, questionID, pairID)

	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("upgrade: %v", err)
	}

	answers, err := results.New(pool).RunAnswers(ctx, runID)
	if err != nil || len(answers) != 1 || answers[0].ID != answerID || answers[0].Sample != 1 || !answers[0].Mentioned {
		t.Fatalf("answers after the upgrade = %+v %v, want the one answer, named, as sample 1", answers, err)
	}
	st, err := config.New(pool, config.Options{Secrets: secretbox.ForTest()}).Settings(ctx)
	if err != nil || st.Samples != 1 {
		t.Fatalf("samples after the upgrade = %d %v, want 1 so a check costs what it did", st.Samples, err)
	}
	insert := `INSERT INTO answer (run_id, question_id, surface_model_id, sample, endpoint, fidelity, status, attempts, mentioned, cited)
		VALUES ($1, $2, $3, $4, 'claude-live', 'model_match', 'ok', 1, false, false)`
	if _, err := pool.Exec(ctx, insert, runID, questionID, pairID, 1); err == nil {
		t.Error("a second sample 1 in the same cell was stored, want it refused")
	}
	exec(insert, runID, questionID, pairID, 2)
}
