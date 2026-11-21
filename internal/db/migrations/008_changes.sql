-- Site changes: merged changes to the site, Google's first crawl after
-- each, and the difference-in-differences verdict against control pages.
-- Everything here is written by internal/changes.

-- When a change's interim read and final verdict fall, in days after the
-- first crawl, and how far a page must move against the control pages to
-- count: average position in places, CTR in percentage points, and the
-- impressions ratio (its inverse counts as worse).
ALTER TABLE instance_settings
    ADD COLUMN change_interim_days int NOT NULL DEFAULT 17 CHECK (change_interim_days BETWEEN 7 AND 90),
    ADD COLUMN change_verdict_days int NOT NULL DEFAULT 31 CHECK (change_verdict_days BETWEEN 14 AND 180),
    ADD COLUMN change_position_threshold double precision NOT NULL DEFAULT 2.5 CHECK (change_position_threshold > 0),
    ADD COLUMN change_ctr_threshold double precision NOT NULL DEFAULT 0.8 CHECK (change_ctr_threshold > 0),
    ADD COLUMN change_impressions_ratio double precision NOT NULL DEFAULT 2.7 CHECK (change_impressions_ratio > 1),
    ADD CONSTRAINT change_days_order CHECK (change_verdict_days > change_interim_days);

-- At most one row: the GitHub repo whose merged pull requests are site
-- changes. ciphertext is a fine-grained, read-only token sealed with
-- ENCRYPTION_KEY; no API returns it. Disconnecting clears the token and
-- keeps the repo and its rules. path_rules is an ordered list of
-- {"glob": ..., "url": ...}: the first glob a changed file matches gives
-- the page URL. polled_through is the latest pull request update seen, so
-- the next poll reads back only to there.
CREATE TABLE repo_connection (
    id               boolean PRIMARY KEY DEFAULT true CHECK (id),
    provider         text NOT NULL DEFAULT 'github' CHECK (provider IN ('github')),
    owner            text NOT NULL CHECK (owner ~ '^[A-Za-z0-9][A-Za-z0-9-]{0,38}$'),
    name             text NOT NULL CHECK (name ~ '^[A-Za-z0-9._-]{1,100}$'),
    default_branch   text NOT NULL DEFAULT '',
    ciphertext       text,
    token_expires_at timestamptz,
    path_rules       jsonb NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(path_rules) = 'array'),
    connected_at     timestamptz NOT NULL DEFAULT now(),
    polled_through   timestamptz,
    last_poll_at     timestamptz,
    last_poll_error  text
);

-- A change to one page: from a merged pull request (one row per pull
-- request and page, so a re-poll upserts) or logged by hand. merged_at is
-- when it went live. first_crawl_at is Google's first crawl seen after it;
-- interim_on and verdict_on follow from it in the instance's time zone.
-- before holds the page's numbers over the verdict-length window that
-- ends the day before the merge. interim and final are the two reads;
-- verdict is the final read's outcome.
CREATE TABLE change (
    id             bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    url            text NOT NULL CHECK (url ~ '^https?://[^/]+'),
    kind           text NOT NULL CHECK (kind IN ('new_page', 'edit', 'removed')),
    source         text NOT NULL CHECK (source IN ('github', 'manual')),
    repo           text,
    pr_number      int,
    pr_title       text,
    pr_url         text,
    merged_at      timestamptz NOT NULL,
    notes          text NOT NULL DEFAULT '',
    first_crawl_at timestamptz,
    interim_on     date,
    verdict_on     date,
    before         jsonb,
    interim        jsonb,
    final          jsonb,
    verdict        text CHECK (verdict IN ('improved', 'worsened', 'no_change', 'insufficient_data')),
    created_at     timestamptz NOT NULL DEFAULT now(),
    CHECK ((source = 'github') = (pr_number IS NOT NULL AND repo IS NOT NULL)),
    CHECK ((first_crawl_at IS NULL) = (interim_on IS NULL) AND (first_crawl_at IS NULL) = (verdict_on IS NULL)),
    CHECK ((final IS NULL) = (verdict IS NULL))
);
CREATE UNIQUE INDEX change_pr_page ON change (repo, pr_number, url) WHERE pr_number IS NOT NULL;
CREATE INDEX change_merged ON change (merged_at DESC);

-- Pages left alone as the baseline for verdicts, until hold_until. Past
-- that day a page drops out of the control set.
CREATE TABLE control_page (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    url        text NOT NULL UNIQUE CHECK (url ~ '^https?://[^/]+'),
    hold_until date NOT NULL,
    note       text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now()
);

-- Each URL Inspection call recrawl watch made, with Google's answer, or
-- the error when Google refused. Each call spends one of the property's
-- daily inspections, so the count since midnight caps the next pass.
CREATE TABLE url_inspection (
    id               bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    url              text NOT NULL,
    checked_at       timestamptz NOT NULL,
    verdict          text NOT NULL DEFAULT '',
    coverage_state   text NOT NULL DEFAULT '',
    last_crawl_at    timestamptz,
    google_canonical text NOT NULL DEFAULT '',
    user_canonical   text NOT NULL DEFAULT '',
    error            text
);
CREATE INDEX url_inspection_url ON url_inspection (url, checked_at DESC);
CREATE INDEX url_inspection_checked ON url_inspection (checked_at);

-- Every job name the code writes. TestJobCheckCoversEveryJob fails when
-- one is missing.
ALTER TABLE job_run DROP CONSTRAINT job_run_job_check,
    ADD CONSTRAINT job_run_job_check CHECK (job IN ('ai_check', 'model_refresh', 'gsc_sync', 'source_fetch',
        'site_changes', 'recrawl_watch', 'verdicts'));
