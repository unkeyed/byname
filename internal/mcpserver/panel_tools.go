package mcpserver

import (
	"context"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/unkeyed/byname/internal/app"
	"github.com/unkeyed/byname/internal/checks"
	"github.com/unkeyed/byname/internal/config"
	"github.com/unkeyed/byname/internal/core"
	"github.com/unkeyed/byname/internal/engines"
	"github.com/unkeyed/byname/internal/results"
)

// MCP needs an object as a tool's output, so lists come wrapped.
type (
	brandList struct {
		Brands []config.Brand `json:"brands"`
	}
	questionSetList struct {
		QuestionSets []config.QuestionSetSummary `json:"question_sets"`
	}
	surfaceList struct {
		Surfaces []config.Surface `json:"surfaces"`
	}
	modelList struct {
		Models []config.Model `json:"models"`
	}
	runList struct {
		Runs []results.Run `json:"runs"`
	}
	answerList struct {
		Answers []results.AnswerSummary `json:"answers"`
	}
	rateList struct {
		Series []results.RateSeries `json:"series"`
	}
	shareList struct {
		Series []results.ShareSeries `json:"series"`
	}
	jobList struct {
		Jobs []results.Job `json:"jobs"`
	}
)

type deleted struct {
	Deleted int64 `json:"deleted"`
}

type byID struct {
	ID int64 `json:"id"`
}

type updateBrandInput struct {
	ID int64 `json:"id"`
	config.BrandInput
}

type surfaceKey struct {
	Surface engines.Surface `json:"surface" jsonschema:"a surface key from list_surfaces, such as claude_code"`
}

type updateSurfaceInput struct {
	Surface engines.Surface `json:"surface" jsonschema:"a surface key from list_surfaces, such as claude_code"`
	config.SurfaceInput
}

type sinceInput struct {
	Since *time.Time `json:"since,omitempty" jsonschema:"only runs started at or after this RFC 3339 time; omit for all time"`
}

type compareInput struct {
	Before int64 `json:"before" jsonschema:"the earlier run's id"`
	After  int64 `json:"after" jsonschema:"the later run's id"`
}

type historyInput struct {
	QuestionID int64      `json:"question_id"`
	Since      *time.Time `json:"since,omitempty" jsonschema:"only runs started at or after this RFC 3339 time; omit for all time"`
}

type gapsInput struct {
	RunID int64 `json:"run_id,omitempty" jsonschema:"omit for the latest run with answers"`
}

type limitInput struct {
	Limit int `json:"limit,omitempty" jsonschema:"1 to 200, default 50"`
}

type runNowResult struct {
	checks.Run
	Created bool `json:"created" jsonschema:"false when a run was already queued or running and is returned instead"`
}

func addConfigTools(srv *mcp.Server, svc app.Services) {
	c := svc.Config
	addTool(srv, core.ResultsRead, &mcp.Tool{Name: "get_settings", Annotations: readOnly(),
		Description: "The instance's settings: your brand, market, language, schedule (with the next and last check), spend caps and run limits. GET /v1/settings."},
		func(ctx context.Context, _ core.Actor, _ noInput) (config.SettingsView, error) {
			return c.Settings(ctx)
		})
	addTool(srv, core.ConfigWrite, &mcp.Tool{Name: "update_settings", Annotations: mutates(false, true),
		Description: "Replace the settings, every field at once (read them with get_settings first). Changing the schedule moves the next check. PUT /v1/settings."},
		func(ctx context.Context, _ core.Actor, in config.Settings) (config.SettingsView, error) {
			return c.PutSettings(ctx, in)
		})

	addTool(srv, core.ResultsRead, &mcp.Tool{Name: "list_brands", Annotations: readOnly(),
		Description: "The brand table: your own brand (kind self), competitors and technologies. Technologies never count toward share of mentions. GET /v1/brands."},
		func(ctx context.Context, _ core.Actor, _ noInput) (brandList, error) {
			b, err := c.Brands(ctx)
			return brandList{b}, err
		})
	addTool(srv, core.ConfigWrite, &mcp.Tool{Name: "upsert_brand", Annotations: mutates(false, true),
		Description: "Add a competitor or technology, or replace the one with the same name ignoring case. Your own brand is set with update_settings. POST /v1/brands."},
		func(ctx context.Context, _ core.Actor, in config.BrandInput) (config.Brand, error) {
			return c.UpsertBrand(ctx, in)
		})
	addTool(srv, core.ConfigWrite, &mcp.Tool{Name: "update_brand", Annotations: mutates(false, true),
		Description: "Replace a competitor or technology by id, which can rename it. PUT /v1/brands/{id}."},
		func(ctx context.Context, _ core.Actor, in updateBrandInput) (config.Brand, error) {
			return c.UpdateBrand(ctx, in.ID, in.BrandInput)
		})
	addTool(srv, core.ConfigWrite, &mcp.Tool{Name: "delete_brand", Annotations: mutates(true, true),
		Description: "Delete a competitor or technology and its recorded mentions. DELETE /v1/brands/{id}."},
		func(ctx context.Context, _ core.Actor, in byID) (deleted, error) {
			return deleted{in.ID}, c.DeleteBrand(ctx, in.ID)
		})

	addTool(srv, core.ResultsRead, &mcp.Tool{Name: "get_questions", Annotations: readOnly(),
		Description: "The current question set: its version and questions, and how it carries on the previous version. GET /v1/questions."},
		func(ctx context.Context, _ core.Actor, _ noInput) (config.QuestionSet, error) {
			return c.CurrentQuestions(ctx)
		})
	addTool(srv, core.ConfigWrite, &mcp.Tool{Name: "update_questions", Annotations: mutates(false, true),
		Description: "Replace the questions. Any change creates a new question set version; a question with the same text carries on by itself, and every other current question must be named in a new question's replaces or in retired. Sending the current set again changes nothing. PUT /v1/questions."},
		func(ctx context.Context, _ core.Actor, in config.QuestionsInput) (config.QuestionSet, error) {
			return c.PutQuestions(ctx, in)
		})
	addTool(srv, core.ResultsRead, &mcp.Tool{Name: "list_question_sets", Annotations: readOnly(),
		Description: "Every question set version, newest first. GET /v1/question-sets."},
		func(ctx context.Context, _ core.Actor, _ noInput) (questionSetList, error) {
			v, err := c.QuestionSets(ctx)
			return questionSetList{v}, err
		})
	addTool(srv, core.ResultsRead, &mcp.Tool{Name: "get_question_set", Annotations: readOnly(),
		Description: "One question set version by id with its questions. GET /v1/question-sets/{id}."},
		func(ctx context.Context, _ core.Actor, in byID) (config.QuestionSet, error) {
			return c.QuestionSet(ctx, in.ID)
		})

	addTool(srv, core.ResultsRead, &mcp.Tool{Name: "list_surfaces", Annotations: readOnly(),
		Description: "Every surface Byname can ask on, its pinned Treg endpoint and fidelity, whether it is on, and its surface and model pairs. GET /v1/surfaces."},
		func(ctx context.Context, _ core.Actor, _ noInput) (surfaceList, error) {
			v, err := c.Surfaces(ctx)
			return surfaceList{v}, err
		})
	addTool(srv, core.ConfigWrite, &mcp.Tool{Name: "update_surface", Annotations: mutates(false, true),
		Description: "Turn a surface on or off and, when models is given, set its models (each checked against the endpoint's live list). App surfaces take no models. PUT /v1/surfaces/{key}."},
		func(ctx context.Context, _ core.Actor, in updateSurfaceInput) (config.Surface, error) {
			return c.PutSurface(ctx, in.Surface, in.SurfaceInput)
		})
	addTool(srv, core.ResultsRead, &mcp.Tool{Name: "list_surface_models", Annotations: readOnly(),
		Description: "A surface's live model list, read through Treg for free. GET /v1/surfaces/{key}/models."},
		func(ctx context.Context, _ core.Actor, in surfaceKey) (modelList, error) {
			v, err := c.LiveModels(ctx, in.Surface)
			return modelList{v}, err
		})

	addTool(srv, core.RunsWrite, &mcp.Tool{Name: "run_now", Annotations: &mcp.ToolAnnotations{DestructiveHint: new(false), IdempotentHint: true, OpenWorldHint: new(true)},
		Description: "Queue an AI check now without moving the schedule. It runs in the background and spends Treg credit; follow it with get_run. When a run is already queued or running, that run is returned. POST /v1/runs."},
		func(ctx context.Context, _ core.Actor, _ noInput) (runNowResult, error) {
			run, created, err := svc.Checks.RunNow(ctx)
			return runNowResult{run, created}, err
		})
	addTool(srv, core.RunsWrite, &mcp.Tool{Name: "pause_checks", Annotations: mutates(false, true),
		Description: "Pause scheduled checks, keeping the schedule. POST /v1/checks/pause."},
		func(ctx context.Context, _ core.Actor, _ noInput) (config.SettingsView, error) {
			return c.SetPaused(ctx, true)
		})
	addTool(srv, core.RunsWrite, &mcp.Tool{Name: "resume_checks", Annotations: mutates(false, true),
		Description: "Resume scheduled checks. A check missed while paused is not run; the next one is. POST /v1/checks/resume."},
		func(ctx context.Context, _ core.Actor, _ noInput) (config.SettingsView, error) {
			return c.SetPaused(ctx, false)
		})
}

func addResultTools(srv *mcp.Server, svc app.Services) {
	r := svc.Results
	addTool(srv, core.ResultsRead, &mcp.Tool{Name: "list_runs", Annotations: readOnly(),
		Description: "Runs newest first, with status, spend, calls with no reported cost, how many answers named you, and success rate per endpoint. GET /v1/runs."},
		func(ctx context.Context, _ core.Actor, in results.RunsQuery) (runList, error) {
			v, err := r.Runs(ctx, in)
			return runList{v}, err
		})
	addTool(srv, core.ResultsRead, &mcp.Tool{Name: "get_run", Annotations: readOnly(),
		Description: "One run by id. GET /v1/runs/{id}."},
		func(ctx context.Context, _ core.Actor, in byID) (results.Run, error) { return r.Run(ctx, in.ID) })
	addTool(srv, core.ResultsRead, &mcp.Tool{Name: "list_run_answers", Annotations: readOnly(),
		Description: "A run's answers: each question and pair with whether it named or cited you, your position, the brands named and the cost. GET /v1/runs/{id}/answers."},
		func(ctx context.Context, _ core.Actor, in byID) (answerList, error) {
			v, err := r.RunAnswers(ctx, in.ID)
			return answerList{v}, err
		})
	addTool(srv, core.ResultsRead, &mcp.Tool{Name: "get_answer", Annotations: readOnly(),
		Description: "One answer in full: its text, citations, the engine's own searches and the raw provider response. GET /v1/answers/{id}."},
		func(ctx context.Context, _ core.Actor, in byID) (results.Answer, error) { return r.Answer(ctx, in.ID) })
	addTool(srv, core.ResultsRead, &mcp.Tool{Name: "compare_runs", Annotations: readOnly(),
		Description: "Every question and pair gained, lost, moved or unchanged from one run to a later one, with counts and the net change. GET /v1/compare?before=&after=."},
		func(ctx context.Context, _ core.Actor, in compareInput) (results.Comparison, error) {
			return r.Compare(ctx, in.Before, in.After)
		})
	addTool(srv, core.ResultsRead, &mcp.Tool{Name: "question_history", Annotations: readOnly(),
		Description: "One question's answers across runs, one series per pair, with a one-line change summary per run. GET /v1/questions/{id}/history."},
		func(ctx context.Context, _ core.Actor, in historyInput) (results.History, error) {
			return r.QuestionHistory(ctx, in.QuestionID, in.Since)
		})
	addTool(srv, core.ResultsRead, &mcp.Tool{Name: "mention_rates", Annotations: readOnly(),
		Description: "Mention rate and citation rate per run for each series (pair, endpoint and fidelity). GET /v1/metrics/rates."},
		func(ctx context.Context, _ core.Actor, in sinceInput) (rateList, error) {
			v, err := r.Rates(ctx, in.Since)
			return rateList{v}, err
		})
	addTool(srv, core.ResultsRead, &mcp.Tool{Name: "share_of_mentions", Annotations: readOnly(),
		Description: "Each brand's share of mentions against your competitors, per run for each series. Technologies are left out. GET /v1/metrics/share."},
		func(ctx context.Context, _ core.Actor, in sinceInput) (shareList, error) {
			v, err := r.Share(ctx, in.Since)
			return shareList{v}, err
		})
	addTool(srv, core.ResultsRead, &mcp.Tool{Name: "gaps", Annotations: readOnly(),
		Description: "Questions where at least one pair did not name you on a run, ranked by opportunity, with the competitors named instead and the searches the engines ran. GET /v1/gaps."},
		func(ctx context.Context, _ core.Actor, in gapsInput) (results.Gaps, error) {
			return r.Gaps(ctx, in.RunID)
		})
	addTool(srv, core.ResultsRead, &mcp.Tool{Name: "list_jobs", Annotations: readOnly(),
		Description: "The jobs log newest first: each AI check and model list refresh with its status, error and spend. GET /v1/jobs."},
		func(ctx context.Context, _ core.Actor, in limitInput) (jobList, error) {
			v, err := r.Jobs(ctx, in.Limit)
			return jobList{v}, err
		})
}
