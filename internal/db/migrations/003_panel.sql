-- The panel: what this instance asks, where, and every answer it got.
-- Settings tables are written by internal/config, runs and answers by
-- internal/checks. Nothing here names a brand: the brand is a row.
--
-- The surface and fidelity lists below repeat internal/engines;
-- TestPanelChecksMatchEngines parses this file and fails when they disagree.

-- One row, always: the instance's settings. The scheduler reads the
-- schedule from here; there are no schedule environment variables.
CREATE TABLE instance_settings (
    id                  boolean PRIMARY KEY DEFAULT true CHECK (id),
    match_mode          text NOT NULL DEFAULT 'whole_word' CHECK (match_mode IN ('whole_word', 'self_anywhere')),
    market              text NOT NULL DEFAULT 'US' CHECK (market ~ '^[A-Z]{2}$'),
    language            text NOT NULL DEFAULT 'en' CHECK (language ~ '^[a-z]{2}$'),
    time_zone           text NOT NULL DEFAULT 'UTC',
    check_every_days    int NOT NULL DEFAULT 3 CHECK (check_every_days BETWEEN 1 AND 30),
    check_time          time NOT NULL DEFAULT '06:00',
    checks_paused       boolean NOT NULL DEFAULT false,
    -- NULL until the schedule is first saved: nothing runs on its own
    -- before someone has set the instance up.
    next_check_at       timestamptz,
    -- When the scheduler last claimed a check. Run now does not move it.
    last_check_at       timestamptz,
    spend_cap_run_usd   numeric(12, 6) NOT NULL DEFAULT 5 CHECK (spend_cap_run_usd > 0),
    spend_cap_month_usd numeric(12, 6) CHECK (spend_cap_month_usd > 0),
    concurrency         int NOT NULL DEFAULT 8 CHECK (concurrency BETWEEN 1 AND 32),
    retries             int NOT NULL DEFAULT 1 CHECK (retries BETWEEN 0 AND 3),
    timeout_s           int NOT NULL DEFAULT 180 CHECK (timeout_s BETWEEN 30 AND 600),
    -- NULL keeps raw responses forever.
    raw_retention_days  int CHECK (raw_retention_days >= 1),
    updated_at          timestamptz NOT NULL DEFAULT now()
);
INSERT INTO instance_settings DEFAULT VALUES;

-- The brand table. Exactly one row may be the instance's own brand (self),
-- which Settings edits; the rest are competitors and technologies.
-- Technologies are recorded but never count toward share of mentions.
-- domains are hostnames, the first the primary one.
CREATE TABLE brand (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name       text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 80),
    aliases    text[] NOT NULL DEFAULT '{}',
    domains    text[] NOT NULL DEFAULT '{}',
    kind       text NOT NULL CHECK (kind IN ('self', 'competitor', 'technology')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX brand_name ON brand (lower(name));
CREATE UNIQUE INDEX brand_one_self ON brand (kind) WHERE kind = 'self';

-- Question sets are versioned and never edited: any change to the questions
-- is a new version, and question_map says which new question carries on
-- each old one, so history follows a question across versions.
CREATE TABLE question_set (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    version    int NOT NULL UNIQUE CHECK (version >= 1),
    notes      text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE question (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    set_id     bigint NOT NULL REFERENCES question_set (id),
    ord        int NOT NULL,
    group_name text NOT NULL CHECK (char_length(group_name) BETWEEN 1 AND 80),
    text       text NOT NULL CHECK (char_length(text) BETWEEN 10 AND 300),
    UNIQUE (set_id, ord)
);

-- Each old question maps to at most one new one. A question with no row
-- here was retired in the next version.
CREATE TABLE question_map (
    old_question_id bigint PRIMARY KEY REFERENCES question (id),
    new_question_id bigint NOT NULL REFERENCES question (id),
    CHECK (old_question_id <> new_question_id)
);
CREATE INDEX question_map_new ON question_map (new_question_id);

-- The places people ask, one row per surface the instance has configured.
-- Each is pinned to one endpoint in internal/engines' registry.
CREATE TABLE surface (
    key        text PRIMARY KEY CHECK (key IN (
        'chatgpt_app', 'gemini_app', 'google_ai_mode', 'google_ai_overview', 'copilot', 'bing_ai_mode',
        'brave', 'claude_code', 'gemini_cli', 'codex_cli', 'claude_app', 'perplexity_app')),
    enabled    boolean NOT NULL DEFAULT false,
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- A surface and model pair: its own series with its own fidelity. model is
-- NULL on app surfaces, where the app decides. A pair is never deleted,
-- because answers point at it; it is disabled instead. paused_reason is set
-- when the model leaves the endpoint's live model list.
CREATE TABLE surface_model (
    id               bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    surface_key      text NOT NULL REFERENCES surface (key),
    model            text CHECK (model <> ''),
    fidelity         text NOT NULL CHECK (fidelity IN ('app', 'model_match', 'approximation')),
    force_web_search boolean NOT NULL DEFAULT false,
    enabled          boolean NOT NULL DEFAULT true,
    last_tested_at   timestamptz,
    test_searched    boolean,
    paused_reason    text,
    created_at       timestamptz NOT NULL DEFAULT now(),
    UNIQUE NULLS NOT DISTINCT (surface_key, model)
);

-- Secrets people paste in the app, sealed with ENCRYPTION_KEY. No API ever
-- returns ciphertext or plaintext. org is Treg's X-Treg-Org, which only a
-- personal token needs.
CREATE TABLE credential (
    id               bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    kind             text NOT NULL UNIQUE CHECK (kind = 'treg'),
    ciphertext       text NOT NULL,
    org              text NOT NULL DEFAULT '',
    last_tested_at   timestamptz,
    last_test_status text,
    updated_at       timestamptz NOT NULL DEFAULT now()
);

-- One AI check. spend_usd sums the answers' reported costs;
-- spend_missing_calls counts answered calls that reported none, which are
-- never counted as $0. A run left running by a crash is resumed, never
-- started again.
CREATE TABLE run (
    id                  bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    question_set_id     bigint NOT NULL REFERENCES question_set (id),
    trigger             text NOT NULL CHECK (trigger IN ('scheduled', 'run_now')),
    status              text NOT NULL DEFAULT 'queued'
                        CHECK (status IN ('queued', 'running', 'completed', 'capped', 'refused', 'failed')),
    queued_at           timestamptz NOT NULL DEFAULT now(),
    started_at          timestamptz,
    finished_at         timestamptz,
    spend_usd           numeric(12, 6) NOT NULL DEFAULT 0,
    spend_missing_calls int NOT NULL DEFAULT 0,
    -- Why a run stopped early: a cap, Treg's refusal, or an internal error.
    error               text,
    -- Treg's error code when it refused, such as insufficient_balance.
    refusal_code        text
);
CREATE INDEX run_open ON run (id) WHERE status IN ('queued', 'running');

-- One answer per run, question and pair. endpoint and fidelity are stored
-- on every answer: changing either starts a new series. cost_usd is NULL
-- when the call reported no cost. raw is the provider's body (a JSON
-- string when it was not JSON), NULL once raw_retention_days pass.
CREATE TABLE answer (
    id               bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    run_id           bigint NOT NULL REFERENCES run (id) ON DELETE CASCADE,
    question_id      bigint NOT NULL REFERENCES question (id),
    surface_model_id bigint NOT NULL REFERENCES surface_model (id),
    endpoint         text NOT NULL,
    fidelity         text NOT NULL CHECK (fidelity IN ('app', 'model_match', 'approximation')),
    reported_model   text NOT NULL DEFAULT '',
    status           text NOT NULL CHECK (status IN ('ok', 'no_answer', 'failed')),
    error            text NOT NULL DEFAULT '',
    attempts         int NOT NULL CHECK (attempts >= 1),
    mentioned        boolean NOT NULL,
    cited            boolean NOT NULL,
    position         int CHECK (position >= 1),
    cost_usd         numeric(12, 6),
    text             text NOT NULL DEFAULT '',
    raw              jsonb,
    fan_out          text[] NOT NULL DEFAULT '{}',
    call_id          text NOT NULL DEFAULT '',
    answered_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (run_id, question_id, surface_model_id),
    CHECK (position IS NULL OR mentioned)
);
CREATE INDEX answer_question ON answer (question_id);
CREATE INDEX answer_surface_model ON answer (surface_model_id);

-- Every brand an answer names. position is the order of first mention among
-- non-technology brands, NULL for a technology.
CREATE TABLE brand_mention (
    answer_id bigint NOT NULL REFERENCES answer (id) ON DELETE CASCADE,
    brand_id  bigint NOT NULL REFERENCES brand (id) ON DELETE CASCADE,
    position  int CHECK (position >= 1),
    PRIMARY KEY (answer_id, brand_id)
);
CREATE INDEX brand_mention_brand ON brand_mention (brand_id);

-- Every URL an answer cites, in answer order.
CREATE TABLE citation (
    answer_id bigint NOT NULL REFERENCES answer (id) ON DELETE CASCADE,
    ord       int NOT NULL,
    url       text NOT NULL,
    domain    text NOT NULL,
    is_self   boolean NOT NULL,
    PRIMARY KEY (answer_id, ord)
);

-- The jobs log: each AI check, scheduled or Run now, and each daily model
-- list refresh, with how it ended.
CREATE TABLE job_run (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    job         text NOT NULL CHECK (job IN ('ai_check', 'model_refresh')),
    trigger     text NOT NULL CHECK (trigger IN ('scheduled', 'run_now')),
    run_id      bigint UNIQUE REFERENCES run (id) ON DELETE CASCADE,
    status      text NOT NULL DEFAULT 'running'
                CHECK (status IN ('queued', 'running', 'completed', 'capped', 'refused', 'failed', 'skipped')),
    started_at  timestamptz NOT NULL DEFAULT now(),
    finished_at timestamptz,
    error       text,
    CHECK ((job = 'ai_check') = (run_id IS NOT NULL) OR status = 'skipped')
);
CREATE INDEX job_run_job ON job_run (job, started_at DESC);
