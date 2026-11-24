-- Other brands: every brand AI answers name that you don't track, found by
-- a small model reading each answer through Treg, and each brand's logo
-- and colour from Context.dev through Treg. internal/discovery writes the
-- brands it finds and what people decide about them, internal/logos the
-- logos.

-- untracked: named in answers, found by discovery. hidden: someone said it
-- is not a competitor. Neither counts in any score, since scores read
-- brand_mention and tracked_brand, which hold tracked brands only. Track
-- turns either into a competitor. key is the normalised name an untracked
-- or hidden brand is found again by, so "Vercel", "Vercel Inc." and
-- "vercel.com" stay one brand.
ALTER TABLE brand DROP CONSTRAINT brand_kind_check,
    ADD CONSTRAINT brand_kind_check CHECK (kind IN ('self', 'competitor', 'technology', 'untracked', 'hidden')),
    ADD COLUMN key text,
    ADD CONSTRAINT brand_key_found CHECK ((key IS NOT NULL) = (kind IN ('untracked', 'hidden')));
CREATE UNIQUE INDEX brand_key ON brand (key) WHERE key IS NOT NULL;

-- The brands answers are scored against and every score reads.
CREATE VIEW tracked_brand AS
    SELECT id, name, aliases, domains, kind FROM brand WHERE kind IN ('self', 'competitor', 'technology');

-- find_brands: read each new answer for other brands. brand_logos: look up
-- logos, at most brand_logo_month_limit new brands a month. Both need a
-- Treg token, and both are paid.
ALTER TABLE instance_settings
    ADD COLUMN find_brands boolean NOT NULL DEFAULT true,
    ADD COLUMN brand_logos boolean NOT NULL DEFAULT true,
    ADD COLUMN brand_logo_month_limit int NOT NULL DEFAULT 100 CHECK (brand_logo_month_limit BETWEEN 0 AND 1000);

-- Each paid call for other brands: find (the model reading one answer) or
-- lookup (Context.dev for one brand). run_id is the check a find belonged
-- to, which its per-check spend cap counts; job_run_id is the job that
-- made it. status is ok, not_found (a lookup Context.dev had nothing for,
-- which is final), failed (tried again later, a bounded number of times)
-- or refused (Treg declined it, which costs nothing and is not an
-- attempt). cost_usd is NULL when Treg reported no cost, never 0 for
-- unknown.
CREATE TABLE brand_call (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    purpose      text NOT NULL CHECK (purpose IN ('find', 'lookup')),
    answer_id    bigint REFERENCES answer (id) ON DELETE SET NULL,
    brand_id     bigint REFERENCES brand (id) ON DELETE SET NULL,
    run_id       bigint REFERENCES run (id) ON DELETE SET NULL,
    job_run_id   bigint REFERENCES job_run (id) ON DELETE SET NULL,
    tool         text NOT NULL,
    status       text NOT NULL CHECK (status IN ('ok', 'not_found', 'failed', 'refused')),
    cost_usd     numeric(12, 6),
    treg_call_id text NOT NULL DEFAULT '',
    error        text,
    called_at    timestamptz NOT NULL,
    CHECK ((status IN ('failed', 'refused')) = (error IS NOT NULL)),
    CHECK (purpose = 'lookup' OR status <> 'not_found')
);
CREATE INDEX brand_call_called ON brand_call (called_at);
CREATE INDEX brand_call_answer ON brand_call (answer_id) WHERE purpose = 'find';
CREATE INDEX brand_call_brand ON brand_call (brand_id) WHERE purpose = 'lookup';
CREATE INDEX brand_call_run ON brand_call (run_id) WHERE run_id IS NOT NULL;

-- The untracked and hidden brands each answer named, in the order the
-- answer named them. Tracked brands' mentions stay in brand_mention.
CREATE TABLE discovered_mention (
    answer_id bigint NOT NULL REFERENCES answer (id) ON DELETE CASCADE,
    brand_id  bigint NOT NULL REFERENCES brand (id) ON DELETE CASCADE,
    ord       int NOT NULL CHECK (ord >= 1),
    PRIMARY KEY (answer_id, brand_id)
);
CREATE INDEX discovered_mention_brand ON discovered_mention (brand_id);

-- A brand's logo and colour, as Context.dev gave them. The browser loads
-- the logo from Byname, never from a third party.
CREATE TABLE brand_logo (
    brand_id  bigint PRIMARY KEY REFERENCES brand (id) ON DELETE CASCADE,
    domain    text NOT NULL DEFAULT '',
    color     text CHECK (color ~ '^#[0-9a-f]{6}$'),
    image     bytea NOT NULL CHECK (octet_length(image) BETWEEN 1 AND 524288),
    mime      text NOT NULL CHECK (mime IN ('image/png', 'image/jpeg', 'image/webp', 'image/gif', 'image/svg+xml', 'image/x-icon')),
    looked_up_at timestamptz NOT NULL
);

-- Everything Byname has spent, one row per charge: each check's answers,
-- each social search and each call for other brands. The monthly spend
-- cap and GET /v1/costs read it, so a new kind of charge is added here
-- once.
CREATE VIEW spend AS
    SELECT 'checks'::text AS source, id AS run_id, started_at AS at, spend_usd AS usd FROM run WHERE started_at IS NOT NULL
    UNION ALL
    SELECT 'social', NULL, called_at, cost_usd FROM social_call WHERE cost_usd IS NOT NULL
    UNION ALL
    SELECT 'brands', run_id, called_at, cost_usd FROM brand_call WHERE cost_usd IS NOT NULL;

ALTER TABLE job_run DROP CONSTRAINT job_run_job_check,
    ADD CONSTRAINT job_run_job_check CHECK (job IN ('ai_check', 'model_refresh', 'search_sync', 'source_fetch',
        'site_changes', 'recrawl_watch', 'verdicts', 'social_fetch', 'other_brands', 'brand_logos'));
