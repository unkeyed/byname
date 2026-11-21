-- Cited sources: each page an answer cited, fetched once and refreshed
-- after a while, with whether its HTML names you and which other brands it
-- names. Written by internal/sources, read by internal/results.
--
-- status is how the last fetch ended: ok (the page was read and matched),
-- failed (network error, timeout, HTTP error, too large, not HTML),
-- disallowed (robots.txt says no) or refused (not http(s), or the host
-- resolved to a private or loopback address). self_present is NULL unless
-- status is ok. brands_present holds the names of the other brands found,
-- as the brand table spelled them at fetch time.
CREATE TABLE source_page (
    url            text PRIMARY KEY,
    domain         text NOT NULL,
    status         text NOT NULL CHECK (status IN ('ok', 'failed', 'disallowed', 'refused')),
    fetched_at     timestamptz NOT NULL,
    http_status    int,
    content_hash   text,
    self_present   boolean,
    brands_present text[] NOT NULL DEFAULT '{}',
    error          text,
    CHECK ((status = 'ok') = (self_present IS NOT NULL))
);
CREATE INDEX source_page_fetched ON source_page (fetched_at);
CREATE INDEX citation_url ON citation (url);

ALTER TABLE job_run DROP CONSTRAINT job_run_job_check,
    ADD CONSTRAINT job_run_job_check CHECK (job IN ('ai_check', 'model_refresh', 'gsc_sync', 'source_fetch'));
