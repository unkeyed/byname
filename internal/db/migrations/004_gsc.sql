-- Search Console: the connection, the daily numbers it syncs, and goals.
-- Everything here is written by internal/gsc.

-- The terms that make a search query branded, lowercased. A query that
-- contains any of them is brand; NULL means the brand's name and aliases.
ALTER TABLE instance_settings ADD COLUMN branded_terms text[]
    CHECK (branded_terms IS NULL OR cardinality(branded_terms) BETWEEN 1 AND 50);

-- At most one row: how Byname reads Search Console. ciphertext is the
-- OAuth refresh token or the service account's JSON key, sealed with
-- ENCRYPTION_KEY; no API ever returns it. property is NULL until someone
-- picks one from the account's list. last_sync_at is the last sync that
-- finished; picking another property clears it, so the next tick syncs.
CREATE TABLE gsc_connection (
    id               boolean PRIMARY KEY DEFAULT true CHECK (id),
    method           text NOT NULL CHECK (method IN ('oauth', 'service_account')),
    account          text NOT NULL DEFAULT '',
    ciphertext       text NOT NULL,
    property         text CHECK (property <> ''),
    property_kind    text CHECK (property_kind IN ('domain', 'url_prefix')),
    backfill         boolean NOT NULL DEFAULT true,
    connected_at     timestamptz NOT NULL DEFAULT now(),
    last_sync_at     timestamptz,
    last_sync_error  text,
    CHECK ((property IS NULL) = (property_kind IS NULL))
);

-- The daily numbers, keyed by property so switching properties keeps each
-- one's history apart. Dates are Google's (Pacific Time). is_final is false
-- for days Google still revises (on or after the response's
-- firstIncompleteDate); a later sync overwrites them. position is Google's
-- average position, so sums over days weight it by impressions.
-- Anonymized queries never appear in the query tables: Google leaves them
-- out of any response grouped by query.
CREATE TABLE gsc_site_daily (
    property    text NOT NULL,
    date        date NOT NULL,
    clicks      bigint NOT NULL CHECK (clicks >= 0),
    impressions bigint NOT NULL CHECK (impressions >= 0),
    position    double precision NOT NULL,
    is_final    boolean NOT NULL,
    PRIMARY KEY (property, date)
);

CREATE TABLE gsc_page_daily (
    property    text NOT NULL,
    date        date NOT NULL,
    page        text NOT NULL,
    clicks      bigint NOT NULL CHECK (clicks >= 0),
    impressions bigint NOT NULL CHECK (impressions >= 0),
    position    double precision NOT NULL,
    is_final    boolean NOT NULL,
    PRIMARY KEY (property, date, page)
);
CREATE INDEX gsc_page_daily_page ON gsc_page_daily (property, page, date);

CREATE TABLE gsc_query_daily (
    property    text NOT NULL,
    date        date NOT NULL,
    query       text NOT NULL,
    clicks      bigint NOT NULL CHECK (clicks >= 0),
    impressions bigint NOT NULL CHECK (impressions >= 0),
    position    double precision NOT NULL,
    is_final    boolean NOT NULL,
    PRIMARY KEY (property, date, query)
);

CREATE TABLE gsc_query_page_daily (
    property    text NOT NULL,
    date        date NOT NULL,
    query       text NOT NULL,
    page        text NOT NULL,
    clicks      bigint NOT NULL CHECK (clicks >= 0),
    impressions bigint NOT NULL CHECK (impressions >= 0),
    position    double precision NOT NULL,
    is_final    boolean NOT NULL,
    PRIMARY KEY (property, date, query, page)
);

-- Search goals: reach target on metric over a trailing window by due_on.
-- One goal per metric, window and due date, so adding the same goal again
-- replaces its target.
CREATE TABLE goal (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    metric      text NOT NULL CHECK (metric IN ('clicks', 'impressions', 'non_brand_clicks')),
    window_days int NOT NULL CHECK (window_days IN (28, 90)),
    target      bigint NOT NULL CHECK (target > 0),
    due_on      date NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (metric, window_days, due_on)
);

-- The daily Search Console sync joins the jobs log.
ALTER TABLE job_run DROP CONSTRAINT job_run_job_check;
ALTER TABLE job_run ADD CONSTRAINT job_run_job_check CHECK (job IN ('ai_check', 'model_refresh', 'gsc_sync'));
