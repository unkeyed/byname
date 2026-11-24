-- Search traffic from each search engine's webmaster tools, where 004 kept
-- Google's alone: the connections, the numbers each sync stores, goals and
-- the sync's jobs, each naming its provider. Everything here is written by
-- internal/search. Provider names (google, bing) come from its registry,
-- the one list of them, so no CHECK repeats it. What Search Console stored
-- moves over as provider google, so an upgrade keeps the connection and
-- the history.

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

-- The ciphertext moves as it is: internal/search opens Google's under the
-- labels Search Console sealed it with.
INSERT INTO search_connection (provider, method, account, ciphertext, site, site_kind, backfill, connected_at, last_sync_at, last_sync_error)
SELECT 'google', method, account, ciphertext, property, property_kind, backfill, connected_at, last_sync_at, last_sync_error
FROM gsc_connection;

INSERT INTO search_site_daily (provider, site, day, clicks, impressions, position, is_final)
SELECT 'google', property, date, clicks, impressions, position, is_final FROM gsc_site_daily;

INSERT INTO search_query_stats (provider, site, first_day, last_day, query, clicks, impressions, position, is_final)
SELECT 'google', property, date, date, query, clicks, impressions, position, is_final FROM gsc_query_daily;

INSERT INTO search_page_stats (provider, site, first_day, last_day, page, clicks, impressions, position, is_final)
SELECT 'google', property, date, date, page, clicks, impressions, position, is_final FROM gsc_page_daily;

-- gsc_query_page_daily goes without a successor: nothing ever read it.
DROP TABLE gsc_connection, gsc_site_daily, gsc_query_daily, gsc_page_daily, gsc_query_page_daily;

-- Search goals: reach target on metric over a trailing window by due_on,
-- on one provider. One goal per provider, metric, window and due date, so
-- adding the same goal again replaces its target. Every goal so far was
-- Google's.
ALTER TABLE goal ADD COLUMN provider text NOT NULL DEFAULT 'google';
ALTER TABLE goal ALTER COLUMN provider DROP DEFAULT;
ALTER TABLE goal DROP CONSTRAINT goal_metric_window_days_due_on_key;
ALTER TABLE goal ADD UNIQUE (provider, metric, window_days, due_on);

-- The daily sync logs one search_sync per provider synced. The job CHECK
-- lists every job name the code writes; TestJobCheckCoversEveryJob fails
-- when one is missing.
ALTER TABLE job_run ADD COLUMN provider text;
ALTER TABLE job_run DROP CONSTRAINT job_run_job_check;
UPDATE job_run SET job = 'search_sync', provider = 'google' WHERE job = 'gsc_sync';
ALTER TABLE job_run ADD CONSTRAINT job_run_job_check CHECK (job IN ('ai_check', 'model_refresh', 'search_sync', 'source_fetch',
    'site_changes', 'recrawl_watch', 'verdicts', 'social_fetch', 'other_brands', 'brand_logos'));
ALTER TABLE job_run ADD CONSTRAINT job_run_provider_check CHECK ((job = 'search_sync') = (provider IS NOT NULL));

-- Onboarding's Search Console step became the search engines step.
UPDATE instance_settings SET onboarding_steps = array_replace(onboarding_steps, 'search_console', 'search')
WHERE 'search_console' = ANY (onboarding_steps);

-- Search alerts raised so far were Google's. Their messages and links read
-- the provider and engine from the payload, and search_moved's subject
-- names the provider, so the first sync after the upgrade finds the move
-- it already raised instead of raising it again.
UPDATE alert SET payload = payload || '{"provider": "google", "engine": "Google"}'
WHERE kind IN ('goal_reached', 'search_moved') AND NOT payload ? 'provider';
UPDATE alert SET subject = 'search/google/' || substr(subject, length('search/') + 1)
WHERE kind = 'search_moved' AND subject ~ '^search/[a-z_]+/\d{4}-\d{2}-\d{2}$';
