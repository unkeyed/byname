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
	summaryList struct {
		Runs []results.RunSummary `json:"runs"`
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

type surfaceModelsInput struct {
	Surface engines.Surface `json:"surface" jsonschema:"a surface key from list_surfaces, such as claude_code"`
	config.ModelsQuery
}

type questionScoringInput struct {
	ID int64 `json:"id" jsonschema:"a question of the current set, from get_questions"`
	config.QuestionScoringInput
}

type stepInput struct {
	Step config.StepKey `json:"step" jsonschema:"brand, competitors, questions, surfaces, search_console or schedule"`
}

type updateSurfaceInput struct {
	Surface engines.Surface `json:"surface" jsonschema:"a surface key from list_surfaces, such as claude_code"`
	config.SurfaceInput
}

type scopedInput struct {
	Since *time.Time `json:"since,omitempty" jsonschema:"only runs started at or after this RFC 3339 time; omit for all time"`
	results.Scope
}

type linesInput struct {
	scopedInput
	By results.Grouping `json:"by,omitempty" jsonschema:"pair (default): one line per surface and model pair; surface: one per surface, combining its models only where they share an endpoint and fidelity"`
}

type runQuestionsInput struct {
	ID int64 `json:"id" jsonschema:"the run's id"`
	results.Scope
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
	RunID int64 `json:"run_id,omitempty" jsonschema:"omit for the latest run with answers on the pairs in scope"`
	results.Scope
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
		Description: "Every surface Byname can ask on: its current route (Treg, a scraping provider's own key, a direct provider key, or a gateway) and endpoint, the routes it can take with the fidelity each gives, whether it is on, and its surface and model pairs with their last test. GET /v1/surfaces."},
		func(ctx context.Context, _ core.Actor, _ noInput) (surfaceList, error) {
			v, err := c.Surfaces(ctx)
			return surfaceList{v}, err
		})
	addTool(srv, core.ConfigWrite, &mcp.Tool{Name: "update_surface", Annotations: mutates(false, true),
		Description: "Turn a surface on or off, move it to another route, and, when models is given, set its models (each checked against the route's live list). App surfaces read by Treg or a scraping key take no models; on a provider or gateway route they need one. Moving route resets each pair's test. Keys are set by an admin in the UI. PUT /v1/surfaces/{key}."},
		func(ctx context.Context, _ core.Actor, in updateSurfaceInput) (config.Surface, error) {
			return c.PutSurface(ctx, in.Surface, in.SurfaceInput)
		})
	addTool(srv, core.ResultsRead, &mcp.Tool{Name: "list_surface_models", Annotations: readOnly(),
		Description: "A surface's live model list on its current route, or on another route given in route, read for free. GET /v1/surfaces/{key}/models."},
		func(ctx context.Context, _ core.Actor, in surfaceModelsInput) (modelList, error) {
			v, err := c.LiveModels(ctx, in.Surface, in.ModelsQuery)
			return modelList{v}, err
		})
	addTool(srv, core.ResultsRead, &mcp.Tool{Name: "get_onboarding", Annotations: readOnly(),
		Description: "How far onboarding has come: each step and whether it is saved, when it was finished, and what still blocks finishing. GET /v1/onboarding."},
		func(ctx context.Context, _ core.Actor, _ noInput) (config.Onboarding, error) {
			return c.Onboarding(ctx)
		})
	addTool(srv, core.ConfigWrite, &mcp.Tool{Name: "save_onboarding_step", Annotations: mutates(false, true),
		Description: "Mark an onboarding step saved, after saving its data with the matching tool, or to skip an optional step (competitors, search_console). PUT /v1/onboarding/steps/{step}."},
		func(ctx context.Context, _ core.Actor, in stepInput) (config.Onboarding, error) {
			return c.SaveStep(ctx, in.Step)
		})
	addTool(srv, core.ConfigWrite, &mcp.Tool{Name: "finish_onboarding", Annotations: mutates(false, true),
		Description: "Finish onboarding once the required steps are saved and every enabled pair has passed test_pair. Sets the first scheduled check; queue one now with run_now. POST /v1/onboarding/finish."},
		func(ctx context.Context, _ core.Actor, _ noInput) (config.Onboarding, error) {
			return c.FinishOnboarding(ctx)
		})
	addTool(srv, core.ConfigWrite, &mcp.Tool{Name: "test_pair", Annotations: &mcp.ToolAnnotations{DestructiveHint: new(false), IdempotentHint: true, OpenWorldHint: new(true)},
		Description: "Make one short paid call for a surface and model pair through its route. Reports the status, the reported cost, whether it searched and cited, and sets the pair's fidelity from what it showed. POST /v1/pairs/{id}/test."},
		func(ctx context.Context, _ core.Actor, in byID) (config.PairTest, error) {
			return c.TestPair(ctx, in.ID)
		})
	addTool(srv, core.ResultsRead, &mcp.Tool{Name: "estimate_cost", Annotations: readOnly(),
		Description: "The estimated cost of one check: calls (questions times enabled pairs) and each pair's cost per call from its endpoint's past answers or the sampled cost, with pairs that have neither counted as unknown. GET /v1/estimate."},
		func(ctx context.Context, _ core.Actor, _ noInput) (config.Estimate, error) {
			return c.Estimate(ctx)
		})
	addTool(srv, core.ConfigWrite, &mcp.Tool{Name: "set_question_scoring", Annotations: mutates(false, true),
		Description: "Count a question's answers in scores, leave them out, or go back to the default (null). By default a question whose text names your brand is still asked but left out of every score. The choice covers the earlier versions the question carries on. PUT /v1/questions/{id}/scoring."},
		func(ctx context.Context, _ core.Actor, in questionScoringInput) (config.Question, error) {
			return c.SetQuestionScoring(ctx, in.ID, in.QuestionScoringInput)
		})
	addTool(srv, core.ResultsRead, &mcp.Tool{Name: "check_naming", Annotations: readOnly(),
		Description: "Check text before saving it: which brands each question names, by the rules answers are scored with (a question naming your brand is left out of scores by default), and whether each alias is a common English word that everyday text would match. Stores nothing. POST /v1/naming-check."},
		func(ctx context.Context, _ core.Actor, in config.NamingCheckInput) (config.NamingCheck, error) {
			return c.CheckNaming(ctx, in)
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
	addTool(srv, core.ResultsRead, &mcp.Tool{Name: "run_questions", Annotations: readOnly(),
		Description: "One run as the questions table: a row per question and a column per pair, each cell named, cited and position with its change since the previous run, and per row how many pairs named you and your best position. GET /v1/runs/{id}/questions."},
		func(ctx context.Context, _ core.Actor, in runQuestionsInput) (results.QuestionTable, error) {
			return r.RunQuestions(ctx, in.ID, in.Scope)
		})
	addTool(srv, core.ResultsRead, &mcp.Tool{Name: "mention_rates", Annotations: readOnly(),
		Description: "Mention rate and citation rate per run for each line: one per pair, or per surface with by=surface. Filter by surface and model. GET /v1/metrics/rates."},
		func(ctx context.Context, _ core.Actor, in linesInput) (rateList, error) {
			v, err := r.Rates(ctx, in.Since, in.Scope, in.By)
			return rateList{v}, err
		})
	addTool(srv, core.ResultsRead, &mcp.Tool{Name: "share_of_mentions", Annotations: readOnly(),
		Description: "Each brand's share of mentions against your competitors, per run for each series. Technologies are left out. Filter by surface and model. GET /v1/metrics/share."},
		func(ctx context.Context, _ core.Actor, in scopedInput) (shareList, error) {
			v, err := r.Share(ctx, in.Since, in.Scope)
			return shareList{v}, err
		})
	addTool(srv, core.ResultsRead, &mcp.Tool{Name: "results_summary", Annotations: readOnly(),
		Description: "The headline per run: how many question and pair answers named or cited you out of those answered, and your share of brand mentions. Filter by surface and model. GET /v1/metrics/summary."},
		func(ctx context.Context, _ core.Actor, in scopedInput) (summaryList, error) {
			v, err := r.Summary(ctx, in.Since, in.Scope)
			return summaryList{v}, err
		})
	addTool(srv, core.ResultsRead, &mcp.Tool{Name: "competitors", Annotations: readOnly(),
		Description: "Share of voice over the runs in range: each brand's mentions, share and rank, and how often each line's answers named each brand. Technologies are left out. GET /v1/metrics/competitors."},
		func(ctx context.Context, _ core.Actor, in linesInput) (results.Competitors, error) {
			return r.Competitors(ctx, in.Since, in.Scope, in.By)
		})
	addTool(srv, core.ResultsRead, &mcp.Tool{Name: "gaps", Annotations: readOnly(),
		Description: "Questions where at least one pair did not name you on a run, ranked by opportunity, with the competitors named instead and each pair's answer with the web searches its engine ran, where the endpoint exposes them. Filter by surface and model. GET /v1/gaps."},
		func(ctx context.Context, _ core.Actor, in gapsInput) (results.Gaps, error) {
			return r.Gaps(ctx, in.RunID, in.Scope)
		})
	addTool(srv, core.ResultsRead, &mcp.Tool{Name: "list_jobs", Annotations: readOnly(),
		Description: "The jobs log newest first: each AI check and model list refresh with its status, error and spend. GET /v1/jobs."},
		func(ctx context.Context, _ core.Actor, in limitInput) (jobList, error) {
			v, err := r.Jobs(ctx, in.Limit)
			return jobList{v}, err
		})
}
