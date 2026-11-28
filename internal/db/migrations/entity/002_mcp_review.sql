CREATE TABLE mcp_review (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    -- The tool's definition hash and the prompt version: an unchanged tool
    -- is never paid for twice, and a new prompt reviews every tool again.
    review_key text NOT NULL,
    tool_name text NOT NULL,
    job_run_id bigint REFERENCES job_run (id) ON DELETE SET NULL,
    status text NOT NULL CHECK (status IN ('ok', 'failed', 'refused')),
    title_score smallint CHECK (title_score BETWEEN 0 AND 100),
    title_reason text,
    description_score smallint CHECK (description_score BETWEEN 0 AND 100),
    description_reason text,
    inputs_score smallint CHECK (inputs_score BETWEEN 0 AND 100),
    inputs_reason text,
    -- NULL when the call reported no cost, never 0.
    cost_usd numeric(12,6),
    treg_call_id text NOT NULL DEFAULT '',
    error text,
    called_at timestamptz NOT NULL,
    CHECK ((status IN ('failed', 'refused')) = (error IS NOT NULL)),
    CHECK ((status = 'ok') = (title_score IS NOT NULL AND description_score IS NOT NULL AND inputs_score IS NOT NULL))
);

CREATE INDEX mcp_review_ok ON mcp_review (review_key) WHERE status = 'ok';

ALTER TABLE job_run DROP CONSTRAINT job_run_job_check,
    ADD CONSTRAINT job_run_job_check CHECK (job IN ('ai_check', 'model_refresh', 'search_sync', 'source_fetch', 'site_changes',
        'recrawl_watch', 'verdicts', 'social_fetch', 'other_brands', 'brand_logos', 'mcp_review'));

CREATE OR REPLACE VIEW spend AS
 SELECT 'checks'::text AS source,
    run.id AS run_id,
    run.started_at AS at,
    run.spend_usd AS usd
   FROM run
  WHERE (run.started_at IS NOT NULL)
UNION ALL
 SELECT 'social'::text AS source,
    NULL::bigint AS run_id,
    social_call.called_at AS at,
    social_call.cost_usd AS usd
   FROM social_call
  WHERE (social_call.cost_usd IS NOT NULL)
UNION ALL
 SELECT 'brands'::text AS source,
    brand_call.run_id,
    brand_call.called_at AS at,
    brand_call.cost_usd AS usd
   FROM brand_call
  WHERE (brand_call.cost_usd IS NOT NULL)
UNION ALL
 SELECT 'mcp_review'::text AS source,
    NULL::bigint AS run_id,
    mcp_review.called_at AS at,
    mcp_review.cost_usd AS usd
   FROM mcp_review
  WHERE (mcp_review.cost_usd IS NOT NULL);
