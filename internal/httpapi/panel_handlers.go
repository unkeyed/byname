package httpapi

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/unkeyed/byname/internal/config"
	"github.com/unkeyed/byname/internal/core"
	"github.com/unkeyed/byname/internal/engines"
	"github.com/unkeyed/byname/internal/results"
)

// Settings, brands, questions and surfaces: results:read to read,
// config:write to change.

func (s *server) getSettings(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	v, err := s.svc.Config.Settings(r.Context())
	return respond(w, http.StatusOK, v, err)
}

func (s *server) putSettings(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	var b config.Settings
	if err := readJSON(r, &b); err != nil {
		return err
	}
	v, err := s.svc.Config.PutSettings(r.Context(), b)
	return respond(w, http.StatusOK, v, err)
}

func (s *server) listBrands(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	v, err := s.svc.Config.Brands(r.Context())
	return respond(w, http.StatusOK, v, err)
}

func (s *server) upsertBrand(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	var b config.BrandInput
	if err := readJSON(r, &b); err != nil {
		return err
	}
	v, err := s.svc.Config.UpsertBrand(r.Context(), b)
	return respond(w, http.StatusOK, v, err)
}

func (s *server) updateBrand(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	id, err := pathID(r, "brand")
	if err != nil {
		return err
	}
	var b config.BrandInput
	if err := readJSON(r, &b); err != nil {
		return err
	}
	v, err := s.svc.Config.UpdateBrand(r.Context(), id, b)
	return respond(w, http.StatusOK, v, err)
}

func (s *server) deleteBrand(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	id, err := pathID(r, "brand")
	if err != nil {
		return err
	}
	return noContent(w, s.svc.Config.DeleteBrand(r.Context(), id))
}

func (s *server) getQuestions(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	v, err := s.svc.Config.CurrentQuestions(r.Context())
	return respond(w, http.StatusOK, v, err)
}

func (s *server) putQuestions(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	var b config.QuestionsInput
	if err := readJSON(r, &b); err != nil {
		return err
	}
	v, err := s.svc.Config.PutQuestions(r.Context(), b)
	return respond(w, http.StatusOK, v, err)
}

func (s *server) listQuestionSets(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	v, err := s.svc.Config.QuestionSets(r.Context())
	return respond(w, http.StatusOK, v, err)
}

func (s *server) getQuestionSet(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	id, err := pathID(r, "question set")
	if err != nil {
		return err
	}
	v, err := s.svc.Config.QuestionSet(r.Context(), id)
	return respond(w, http.StatusOK, v, err)
}

func (s *server) listSurfaces(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	v, err := s.svc.Config.Surfaces(r.Context())
	return respond(w, http.StatusOK, v, err)
}

func (s *server) putSurface(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	var b config.SurfaceInput
	if err := readJSON(r, &b); err != nil {
		return err
	}
	v, err := s.svc.Config.PutSurface(r.Context(), engines.Surface(r.PathValue("key")), b)
	return respond(w, http.StatusOK, v, err)
}

func (s *server) listSurfaceModels(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	v, err := s.svc.Config.LiveModels(r.Context(), engines.Surface(r.PathValue("key")))
	return respond(w, http.StatusOK, v, err)
}

// Runs: runs:write queues a check and pauses or resumes the schedule.

// runNow answers 201 with the queued run, or 200 with the run already
// queued or running.
func (s *server) runNow(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	run, created, err := s.svc.Checks.RunNow(r.Context())
	code := http.StatusOK
	if created {
		code = http.StatusCreated
	}
	return respond(w, code, run, err)
}

func (s *server) pauseChecks(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	v, err := s.svc.Config.SetPaused(r.Context(), true)
	return respond(w, http.StatusOK, v, err)
}

func (s *server) resumeChecks(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	v, err := s.svc.Config.SetPaused(r.Context(), false)
	return respond(w, http.StatusOK, v, err)
}

// Credentials: a signed-in admin only. No response ever carries a secret.

func (s *server) listCredentials(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	v, err := s.svc.Config.Credentials(r.Context())
	return respond(w, http.StatusOK, v, err)
}

// setTregToken body {"token": "...", "org": "..."}; org only for a
// personal Treg token.
func (s *server) setTregToken(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	var b struct {
		Token string `json:"token"`
		Org   string `json:"org"`
	}
	if err := readJSON(r, &b); err != nil {
		return err
	}
	return s.recentOnly(r, func() error {
		v, err := s.svc.Config.SetTregToken(r.Context(), b.Token, b.Org)
		return respond(w, http.StatusOK, v, err)
	})
}

func (s *server) testTregToken(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	v, err := s.svc.Config.TestTregToken(r.Context())
	return respond(w, http.StatusOK, v, err)
}

// Results: results:read.

func (s *server) listRuns(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	var q results.RunsQuery
	var err error
	if q.Limit, err = queryInt(r, "limit"); err != nil {
		return err
	}
	before, err := queryInt(r, "before")
	if err != nil {
		return err
	}
	q.Before = int64(before)
	v, err := s.svc.Results.Runs(r.Context(), q)
	return respond(w, http.StatusOK, v, err)
}

func (s *server) getRun(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	id, err := pathID(r, "run")
	if err != nil {
		return err
	}
	v, err := s.svc.Results.Run(r.Context(), id)
	return respond(w, http.StatusOK, v, err)
}

func (s *server) listRunAnswers(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	id, err := pathID(r, "run")
	if err != nil {
		return err
	}
	v, err := s.svc.Results.RunAnswers(r.Context(), id)
	return respond(w, http.StatusOK, v, err)
}

func (s *server) getAnswer(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	id, err := pathID(r, "answer")
	if err != nil {
		return err
	}
	v, err := s.svc.Results.Answer(r.Context(), id)
	return respond(w, http.StatusOK, v, err)
}

func (s *server) compareRuns(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	before, err := queryInt(r, "before")
	if err != nil {
		return err
	}
	after, err := queryInt(r, "after")
	if err != nil {
		return err
	}
	if before == 0 || after == 0 {
		return fmt.Errorf("%w: give both runs as ?before=<run id>&after=<run id>", core.ErrInvalid)
	}
	v, err := s.svc.Results.Compare(r.Context(), int64(before), int64(after))
	return respond(w, http.StatusOK, v, err)
}

func (s *server) questionHistory(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	id, err := pathID(r, "question")
	if err != nil {
		return err
	}
	since, err := querySince(r)
	if err != nil {
		return err
	}
	v, err := s.svc.Results.QuestionHistory(r.Context(), id, since)
	return respond(w, http.StatusOK, v, err)
}

func (s *server) mentionRates(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	since, err := querySince(r)
	if err != nil {
		return err
	}
	v, err := s.svc.Results.Rates(r.Context(), since)
	return respond(w, http.StatusOK, v, err)
}

func (s *server) shareOfMentions(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	since, err := querySince(r)
	if err != nil {
		return err
	}
	v, err := s.svc.Results.Share(r.Context(), since)
	return respond(w, http.StatusOK, v, err)
}

func (s *server) listGaps(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	run, err := queryInt(r, "run")
	if err != nil {
		return err
	}
	v, err := s.svc.Results.Gaps(r.Context(), int64(run))
	return respond(w, http.StatusOK, v, err)
}

func (s *server) listJobs(w http.ResponseWriter, r *http.Request, _ core.Actor) error {
	limit, err := queryInt(r, "limit")
	if err != nil {
		return err
	}
	v, err := s.svc.Results.Jobs(r.Context(), limit)
	return respond(w, http.StatusOK, v, err)
}

func pathID(r *http.Request, what string) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		return 0, fmt.Errorf("%w: no such %s", core.ErrNotFound, what)
	}
	return id, nil
}

// queryInt reads a non-negative integer query parameter, 0 when absent.
func queryInt(r *http.Request, name string) (int, error) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("%w: %s must be a whole number, got %q", core.ErrInvalid, name, raw)
	}
	return n, nil
}

// querySince reads ?since= as an RFC 3339 time or a date (midnight UTC).
func querySince(r *http.Request) (*time.Time, error) {
	raw := r.URL.Query().Get("since")
	if raw == "" {
		return nil, nil
	}
	for _, layout := range []string{time.RFC3339, time.DateOnly} {
		if t, err := time.Parse(layout, raw); err == nil {
			return &t, nil
		}
	}
	return nil, fmt.Errorf("%w: since must be a date such as 2026-11-01 or an RFC 3339 time, got %q", core.ErrInvalid, raw)
}
