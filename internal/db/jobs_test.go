package db_test

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/unkeyed/byname/internal/testdb"
)

var (
	jobInsert  = regexp.MustCompile(`INSERT INTO job_run \(job\b`)
	jobLiteral = regexp.MustCompile(`INSERT INTO job_run \(job[^)]*\)\s*VALUES \('([a-z_]+)'`)
)

// jobsWritten is every job name the Go code outside tests writes to
// job_run, read from its INSERT statements. An INSERT whose job is not a
// SQL literal fails the test, so a job can never slip past it.
func jobsWritten(t *testing.T) []string {
	t.Helper()
	var jobs []string
	err := filepath.WalkDir("../..", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == "node_modules" || d.Name() == "web" || strings.HasPrefix(d.Name(), ".")) && path != "../.." {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		inserts := len(jobInsert.FindAllIndex(src, -1))
		literals := jobLiteral.FindAllSubmatch(src, -1)
		if inserts != len(literals) {
			t.Errorf("%s writes job_run without a literal job name; write it as VALUES ('<job>', ...) so this test sees it", path)
		}
		for _, m := range literals {
			jobs = append(jobs, string(m[1]))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(jobs)
	return slices.Compact(jobs)
}

// TestJobCheckCoversEveryJob fails when the code writes a job name the
// job_run CHECK, as the migrations leave it, refuses.
func TestJobCheckCoversEveryJob(t *testing.T) {
	jobs := jobsWritten(t)
	for _, want := range []string{"ai_check", "model_refresh", "gsc_sync", "source_fetch", "site_changes", "recrawl_watch", "verdicts"} {
		if !slices.Contains(jobs, want) {
			t.Errorf("found no INSERT for job %s; the scan of the code is broken", want)
		}
	}
	pool := testdb.New(t)
	for _, job := range jobs {
		if _, err := pool.Exec(context.Background(), `INSERT INTO job_run (job, trigger, status) VALUES ($1, 'scheduled', 'skipped')`, job); err != nil {
			t.Errorf("job_run refuses job %q, which the code writes: %v", job, err)
		}
	}
}
