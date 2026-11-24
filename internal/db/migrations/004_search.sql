-- Search traffic from each search engine's webmaster tools: the
-- connections, the numbers each sync stores, and goals. Everything here is
-- written by internal/search. Provider names (google, bing) come from its
-- registry, the one list of them, so no CHECK repeats it.

-- The terms that make a search query branded, lowercased. A query that
-- contains any of them is brand; NULL means the brand's name and aliases.
ALTER TABLE instance_settings ADD COLUMN branded_terms text[]
    CHECK (branded_terms IS NULL OR cardinality(branded_terms) BETWEEN 1 AND 50);

-- At most one row per provider: how Byname reads it. ciphertext is the
-- credential (Google's refresh token or service account JSON key, Bing's
-- API key), sealed with ENCRYPTION_KEY; no API ever returns it. site is
-- NULL until someone picks one from the account's list; site_kind is
-- Google's property type and NULL for other providers. last_sync_at is the
-- last sync that finished; picking another site clears it, so the next
-- tick syncs.
CREATE TABLE search_connection (
    provider         text PRIMARY KEY,
    method           text NOT NULL,
    account          text NOT NULL DEFAULT '',
    ciphertext       text NOT NULL,
    site             text CHECK (site <> ''),
    site_kind        text CHECK (site_kind IN ('domain', 'url_prefix')),
    backfill         boolean NOT NULL DEFAULT true,
    connected_at     timestamptz NOT NULL DEFAULT now(),
    last_sync_at     timestamptz,
    last_sync_error  text
);

-- The numbers, keyed by provider and site so each engine's and each site's
-- history stays apart. Days are the provider's own (Google: Pacific Time,
-- Bing: UTC). is_final is false while the provider still revises a row; a
-- later sync overwrites it. position is the provider's average position,
-- 0 when it gives none, so sums weight it by impressions over the rows
-- that have one.
--
-- Site totals are one row per day.
CREATE TABLE search_site_daily (
    provider    text NOT NULL,
    site        text NOT NULL,
    day         date NOT NULL,
    clicks      bigint NOT NULL CHECK (clicks >= 0),
    impressions bigint NOT NULL CHECK (impressions >= 0),
    position    double precision NOT NULL CHECK (position >= 0),
    is_final    boolean NOT NULL,
    PRIMARY KEY (provider, site, day)
);

-- Searches and pages: each row covers first_day to last_day, one day for
-- Google and one week for Bing. Reads sum only the rows wholly inside a
-- window, so a week is never split. Neither lists every search: Google
-- leaves out anonymized ones and Bing lists its top searches each week, so
-- site totals minus these are unlisted clicks.
CREATE TABLE search_query_stats (
    provider    text NOT NULL,
    site        text NOT NULL,
    first_day   date NOT NULL,
    last_day    date NOT NULL,
    query       text NOT NULL,
    clicks      bigint NOT NULL CHECK (clicks >= 0),
    impressions bigint NOT NULL CHECK (impressions >= 0),
    position    double precision NOT NULL CHECK (position >= 0),
    is_final    boolean NOT NULL,
    CHECK (last_day >= first_day),
    PRIMARY KEY (provider, site, first_day, query)
);

CREATE TABLE search_page_stats (
    provider    text NOT NULL,
    site        text NOT NULL,
    first_day   date NOT NULL,
    last_day    date NOT NULL,
    page        text NOT NULL,
    clicks      bigint NOT NULL CHECK (clicks >= 0),
    impressions bigint NOT NULL CHECK (impressions >= 0),
    position    double precision NOT NULL CHECK (position >= 0),
    is_final    boolean NOT NULL,
    CHECK (last_day >= first_day),
    PRIMARY KEY (provider, site, first_day, page)
);
CREATE INDEX search_page_stats_page ON search_page_stats (provider, site, page, first_day);

-- Search goals: reach target on metric over a trailing window by due_on,
-- on one provider. One goal per provider, metric, window and due date, so
-- adding the same goal again replaces its target.
CREATE TABLE goal (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    provider    text NOT NULL,
    metric      text NOT NULL CHECK (metric IN ('clicks', 'impressions', 'non_brand_clicks')),
    window_days int NOT NULL CHECK (window_days IN (28, 90)),
    target      bigint NOT NULL CHECK (target > 0),
    due_on      date NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (provider, metric, window_days, due_on)
);

-- The daily sync joins the jobs log, one search_sync per provider synced.
ALTER TABLE job_run ADD COLUMN provider text;
ALTER TABLE job_run DROP CONSTRAINT job_run_job_check;
ALTER TABLE job_run ADD CONSTRAINT job_run_job_check CHECK (job IN ('ai_check', 'model_refresh', 'search_sync'));
ALTER TABLE job_run ADD CONSTRAINT job_run_provider_check CHECK ((job = 'search_sync') = (provider IS NOT NULL));
