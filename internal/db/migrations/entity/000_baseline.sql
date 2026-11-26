-- The entity baseline: every table and view an entity owns, as the
-- global migrations leave them in public, with their seed rows. Generated
-- by make entity-baseline; never edit it.
-- Migrations apply it with search_path set to the entity's schema and
-- then public, so its references to shared tables, such as users and
-- credential, reach public's.

CREATE TABLE alert (
    id bigint NOT NULL,
    kind text NOT NULL,
    subject text NOT NULL,
    payload jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE alert_delivery (
    id bigint NOT NULL,
    alert_id bigint NOT NULL,
    channel_id bigint NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    attempts integer DEFAULT 0 NOT NULL,
    next_attempt_at timestamp with time zone DEFAULT now() NOT NULL,
    sent_at timestamp with time zone,
    error text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT alert_delivery_status_check CHECK ((status = ANY (ARRAY['pending'::text, 'sent'::text, 'failed'::text])))
);

ALTER TABLE alert_delivery ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME alert_delivery_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

ALTER TABLE alert ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME alert_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE alert_read (
    user_id bigint NOT NULL,
    alert_id bigint NOT NULL,
    read_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE answer (
    id bigint NOT NULL,
    run_id bigint NOT NULL,
    question_id bigint NOT NULL,
    surface_model_id bigint NOT NULL,
    endpoint text NOT NULL,
    fidelity text NOT NULL,
    reported_model text DEFAULT ''::text NOT NULL,
    status text NOT NULL,
    error text DEFAULT ''::text NOT NULL,
    attempts integer NOT NULL,
    mentioned boolean NOT NULL,
    cited boolean NOT NULL,
    "position" integer,
    cost_usd numeric(12,6),
    text text DEFAULT ''::text NOT NULL,
    raw jsonb,
    fan_out text[] DEFAULT '{}'::text[] NOT NULL,
    call_id text DEFAULT ''::text NOT NULL,
    answered_at timestamp with time zone DEFAULT now() NOT NULL,
    sample smallint DEFAULT 1 NOT NULL,
    CONSTRAINT answer_attempts_check CHECK ((attempts >= 1)),
    CONSTRAINT answer_check CHECK ((("position" IS NULL) OR mentioned)),
    CONSTRAINT answer_fidelity_check CHECK ((fidelity = ANY (ARRAY['app'::text, 'model_match'::text, 'approximation'::text]))),
    CONSTRAINT answer_position_check CHECK (("position" >= 1)),
    CONSTRAINT answer_sample_check CHECK ((sample >= 1)),
    CONSTRAINT answer_status_check CHECK ((status = ANY (ARRAY['ok'::text, 'no_answer'::text, 'failed'::text])))
);

ALTER TABLE answer ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME answer_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE brand (
    id bigint NOT NULL,
    name text NOT NULL,
    aliases text[] DEFAULT '{}'::text[] NOT NULL,
    domains text[] DEFAULT '{}'::text[] NOT NULL,
    kind text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    key text,
    CONSTRAINT brand_key_found CHECK (((key IS NOT NULL) = (kind = ANY (ARRAY['untracked'::text, 'hidden'::text])))),
    CONSTRAINT brand_kind_check CHECK ((kind = ANY (ARRAY['self'::text, 'competitor'::text, 'technology'::text, 'untracked'::text, 'hidden'::text]))),
    CONSTRAINT brand_name_check CHECK (((char_length(name) >= 1) AND (char_length(name) <= 80)))
);

CREATE TABLE brand_call (
    id bigint NOT NULL,
    purpose text NOT NULL,
    answer_id bigint,
    brand_id bigint,
    run_id bigint,
    job_run_id bigint,
    tool text NOT NULL,
    status text NOT NULL,
    cost_usd numeric(12,6),
    treg_call_id text DEFAULT ''::text NOT NULL,
    error text,
    called_at timestamp with time zone NOT NULL,
    CONSTRAINT brand_call_check CHECK (((status = ANY (ARRAY['failed'::text, 'refused'::text])) = (error IS NOT NULL))),
    CONSTRAINT brand_call_check1 CHECK (((purpose = 'lookup'::text) OR (status <> 'not_found'::text))),
    CONSTRAINT brand_call_purpose_check CHECK ((purpose = ANY (ARRAY['find'::text, 'lookup'::text]))),
    CONSTRAINT brand_call_status_check CHECK ((status = ANY (ARRAY['ok'::text, 'not_found'::text, 'failed'::text, 'refused'::text])))
);

ALTER TABLE brand_call ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME brand_call_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

ALTER TABLE brand ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME brand_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE brand_logo (
    brand_id bigint NOT NULL,
    domain text DEFAULT ''::text NOT NULL,
    color text,
    image bytea NOT NULL,
    mime text NOT NULL,
    looked_up_at timestamp with time zone NOT NULL,
    CONSTRAINT brand_logo_color_check CHECK ((color ~ '^#[0-9a-f]{6}$'::text)),
    CONSTRAINT brand_logo_image_check CHECK (((octet_length(image) >= 1) AND (octet_length(image) <= 524288))),
    CONSTRAINT brand_logo_mime_check CHECK ((mime = ANY (ARRAY['image/png'::text, 'image/jpeg'::text, 'image/webp'::text, 'image/gif'::text, 'image/svg+xml'::text, 'image/x-icon'::text])))
);

CREATE TABLE brand_mention (
    answer_id bigint NOT NULL,
    brand_id bigint NOT NULL,
    "position" integer,
    CONSTRAINT brand_mention_position_check CHECK (("position" >= 1))
);

CREATE TABLE change (
    id bigint NOT NULL,
    url text NOT NULL,
    kind text NOT NULL,
    source text NOT NULL,
    repo text,
    pr_number integer,
    pr_title text,
    pr_url text,
    merged_at timestamp with time zone NOT NULL,
    notes text DEFAULT ''::text NOT NULL,
    first_crawl_at timestamp with time zone,
    interim_on date,
    verdict_on date,
    before jsonb,
    interim jsonb,
    final jsonb,
    verdict text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT change_check CHECK (((source = 'github'::text) = ((pr_number IS NOT NULL) AND (repo IS NOT NULL)))),
    CONSTRAINT change_check1 CHECK ((((first_crawl_at IS NULL) = (interim_on IS NULL)) AND ((first_crawl_at IS NULL) = (verdict_on IS NULL)))),
    CONSTRAINT change_check2 CHECK (((final IS NULL) = (verdict IS NULL))),
    CONSTRAINT change_kind_check CHECK ((kind = ANY (ARRAY['new_page'::text, 'edit'::text, 'removed'::text]))),
    CONSTRAINT change_source_check CHECK ((source = ANY (ARRAY['github'::text, 'manual'::text]))),
    CONSTRAINT change_url_check CHECK ((url ~ '^https?://[^/]+'::text)),
    CONSTRAINT change_verdict_check CHECK ((verdict = ANY (ARRAY['improved'::text, 'worsened'::text, 'no_change'::text, 'insufficient_data'::text])))
);

ALTER TABLE change ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME change_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE citation (
    answer_id bigint NOT NULL,
    ord integer NOT NULL,
    url text NOT NULL,
    domain text NOT NULL,
    is_self boolean NOT NULL
);

CREATE TABLE control_page (
    id bigint NOT NULL,
    url text NOT NULL,
    hold_until date NOT NULL,
    note text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT control_page_url_check CHECK ((url ~ '^https?://[^/]+'::text))
);

ALTER TABLE control_page ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME control_page_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE discovered_mention (
    answer_id bigint NOT NULL,
    brand_id bigint NOT NULL,
    ord integer NOT NULL,
    offered boolean,
    CONSTRAINT discovered_mention_ord_check CHECK ((ord >= 1))
);

CREATE TABLE goal (
    id bigint NOT NULL,
    metric text NOT NULL,
    window_days integer NOT NULL,
    target bigint NOT NULL,
    due_on date NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    provider text NOT NULL,
    CONSTRAINT goal_metric_check CHECK ((metric = ANY (ARRAY['clicks'::text, 'impressions'::text, 'non_brand_clicks'::text]))),
    CONSTRAINT goal_target_check CHECK ((target > 0)),
    CONSTRAINT goal_window_days_check CHECK ((window_days = ANY (ARRAY[28, 90])))
);

ALTER TABLE goal ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME goal_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE instance_settings (
    id boolean DEFAULT true NOT NULL,
    match_mode text DEFAULT 'whole_word'::text NOT NULL,
    market text DEFAULT 'US'::text NOT NULL,
    language text DEFAULT 'en'::text NOT NULL,
    time_zone text DEFAULT 'UTC'::text NOT NULL,
    check_every_days integer DEFAULT 3 NOT NULL,
    check_time time without time zone DEFAULT '06:00:00'::time without time zone NOT NULL,
    checks_paused boolean DEFAULT false NOT NULL,
    next_check_at timestamp with time zone,
    last_check_at timestamp with time zone,
    spend_cap_run_usd numeric(12,6) DEFAULT 5 NOT NULL,
    spend_cap_month_usd numeric(12,6),
    concurrency integer DEFAULT 8 NOT NULL,
    retries integer DEFAULT 1 NOT NULL,
    timeout_s integer DEFAULT 180 NOT NULL,
    raw_retention_days integer,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    branded_terms text[],
    onboarding_steps text[] DEFAULT '{}'::text[] NOT NULL,
    onboarded_at timestamp with time zone,
    change_interim_days integer DEFAULT 17 NOT NULL,
    change_verdict_days integer DEFAULT 31 NOT NULL,
    change_position_threshold double precision DEFAULT 2.5 NOT NULL,
    change_ctr_threshold double precision DEFAULT 0.8 NOT NULL,
    change_impressions_ratio double precision DEFAULT 2.7 NOT NULL,
    social_terms text[],
    social_fetch_every_hours integer DEFAULT 6 NOT NULL,
    social_digest_cadence text DEFAULT 'weekly'::text NOT NULL,
    social_digest_weekday integer DEFAULT 1 NOT NULL,
    social_digest_sent_through date,
    social_enabled boolean DEFAULT false NOT NULL,
    find_brands boolean DEFAULT true NOT NULL,
    brand_logos boolean DEFAULT true NOT NULL,
    brand_logo_month_limit integer DEFAULT 100 NOT NULL,
    other_brands_min_questions integer DEFAULT 3 NOT NULL,
    samples integer DEFAULT 1 NOT NULL,
    CONSTRAINT change_days_order CHECK ((change_verdict_days > change_interim_days)),
    CONSTRAINT instance_settings_brand_logo_month_limit_check CHECK (((brand_logo_month_limit >= 0) AND (brand_logo_month_limit <= 1000))),
    CONSTRAINT instance_settings_branded_terms_check CHECK (((branded_terms IS NULL) OR ((cardinality(branded_terms) >= 1) AND (cardinality(branded_terms) <= 50)))),
    CONSTRAINT instance_settings_change_ctr_threshold_check CHECK ((change_ctr_threshold > (0)::double precision)),
    CONSTRAINT instance_settings_change_impressions_ratio_check CHECK ((change_impressions_ratio > (1)::double precision)),
    CONSTRAINT instance_settings_change_interim_days_check CHECK (((change_interim_days >= 7) AND (change_interim_days <= 90))),
    CONSTRAINT instance_settings_change_position_threshold_check CHECK ((change_position_threshold > (0)::double precision)),
    CONSTRAINT instance_settings_change_verdict_days_check CHECK (((change_verdict_days >= 14) AND (change_verdict_days <= 180))),
    CONSTRAINT instance_settings_check_every_days_check CHECK (((check_every_days >= 1) AND (check_every_days <= 30))),
    CONSTRAINT instance_settings_concurrency_check CHECK (((concurrency >= 1) AND (concurrency <= 32))),
    CONSTRAINT instance_settings_id_check CHECK (id),
    CONSTRAINT instance_settings_language_check CHECK ((language ~ '^[a-z]{2}$'::text)),
    CONSTRAINT instance_settings_market_check CHECK ((market ~ '^[A-Z]{2}$'::text)),
    CONSTRAINT instance_settings_match_mode_check CHECK ((match_mode = ANY (ARRAY['whole_word'::text, 'self_anywhere'::text]))),
    CONSTRAINT instance_settings_other_brands_min_questions_check CHECK (((other_brands_min_questions >= 1) AND (other_brands_min_questions <= 50))),
    CONSTRAINT instance_settings_raw_retention_days_check CHECK ((raw_retention_days >= 1)),
    CONSTRAINT instance_settings_retries_check CHECK (((retries >= 0) AND (retries <= 3))),
    CONSTRAINT instance_settings_samples_check CHECK (((samples >= 1) AND (samples <= 5))),
    CONSTRAINT instance_settings_social_digest_cadence_check CHECK ((social_digest_cadence = ANY (ARRAY['daily'::text, 'weekly'::text]))),
    CONSTRAINT instance_settings_social_digest_weekday_check CHECK (((social_digest_weekday >= 0) AND (social_digest_weekday <= 6))),
    CONSTRAINT instance_settings_social_fetch_every_hours_check CHECK ((social_fetch_every_hours = ANY (ARRAY[1, 3, 6, 12, 24]))),
    CONSTRAINT instance_settings_social_terms_check CHECK (((cardinality(social_terms) >= 1) AND (cardinality(social_terms) <= 5))),
    CONSTRAINT instance_settings_spend_cap_month_usd_check CHECK ((spend_cap_month_usd > (0)::numeric)),
    CONSTRAINT instance_settings_spend_cap_run_usd_check CHECK ((spend_cap_run_usd > (0)::numeric)),
    CONSTRAINT instance_settings_timeout_s_check CHECK (((timeout_s >= 30) AND (timeout_s <= 600)))
);

CREATE TABLE job_run (
    id bigint NOT NULL,
    job text NOT NULL,
    trigger text NOT NULL,
    run_id bigint,
    status text DEFAULT 'running'::text NOT NULL,
    started_at timestamp with time zone DEFAULT now() NOT NULL,
    finished_at timestamp with time zone,
    error text,
    provider text,
    CONSTRAINT job_run_check CHECK ((((job = 'ai_check'::text) = (run_id IS NOT NULL)) OR (status = 'skipped'::text))),
    CONSTRAINT job_run_job_check CHECK ((job = ANY (ARRAY['ai_check'::text, 'model_refresh'::text, 'search_sync'::text, 'source_fetch'::text, 'site_changes'::text, 'recrawl_watch'::text, 'verdicts'::text, 'social_fetch'::text, 'other_brands'::text, 'brand_logos'::text]))),
    CONSTRAINT job_run_provider_check CHECK (((job = 'search_sync'::text) = (provider IS NOT NULL))),
    CONSTRAINT job_run_status_check CHECK ((status = ANY (ARRAY['queued'::text, 'running'::text, 'completed'::text, 'capped'::text, 'refused'::text, 'failed'::text, 'skipped'::text]))),
    CONSTRAINT job_run_trigger_check CHECK ((trigger = ANY (ARRAY['scheduled'::text, 'run_now'::text])))
);

ALTER TABLE job_run ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME job_run_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE notification_channel (
    id bigint NOT NULL,
    name text NOT NULL,
    kind text NOT NULL,
    settings jsonb DEFAULT '{}'::jsonb NOT NULL,
    ciphertext text NOT NULL,
    events text[] DEFAULT '{}'::text[] NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    last_test_at timestamp with time zone,
    last_test_status text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT notification_channel_kind_check CHECK ((kind = ANY (ARRAY['smtp'::text, 'email_api'::text, 'slack_webhook'::text, 'webhook'::text]))),
    CONSTRAINT notification_channel_name_check CHECK (((char_length(name) >= 1) AND (char_length(name) <= 80)))
);

ALTER TABLE notification_channel ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME notification_channel_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE question (
    id bigint NOT NULL,
    set_id bigint NOT NULL,
    ord integer NOT NULL,
    group_name text NOT NULL,
    text text NOT NULL,
    names_brand boolean DEFAULT false NOT NULL,
    count_in_scores_override boolean,
    counts_in_scores boolean GENERATED ALWAYS AS (COALESCE(count_in_scores_override, (NOT names_brand))) STORED NOT NULL,
    CONSTRAINT question_group_name_check CHECK (((char_length(group_name) >= 1) AND (char_length(group_name) <= 80))),
    CONSTRAINT question_text_check CHECK (((char_length(text) >= 10) AND (char_length(text) <= 300)))
);

ALTER TABLE question ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME question_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE question_map (
    old_question_id bigint NOT NULL,
    new_question_id bigint NOT NULL,
    CONSTRAINT question_map_check CHECK ((old_question_id <> new_question_id))
);

CREATE TABLE question_page (
    id bigint NOT NULL,
    question_id bigint NOT NULL,
    url text NOT NULL,
    url_key text GENERATED ALWAYS AS (page_key(url)) STORED NOT NULL,
    added_by bigint,
    added_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT question_page_url_check CHECK (((char_length(url) >= 1) AND (char_length(url) <= 2000)))
);

ALTER TABLE question_page ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME question_page_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE question_set (
    id bigint NOT NULL,
    version integer NOT NULL,
    notes text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT question_set_version_check CHECK ((version >= 1))
);

ALTER TABLE question_set ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME question_set_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE repo_connection (
    id boolean DEFAULT true NOT NULL,
    provider text DEFAULT 'github'::text NOT NULL,
    owner text NOT NULL,
    name text NOT NULL,
    default_branch text DEFAULT ''::text NOT NULL,
    ciphertext text,
    token_expires_at timestamp with time zone,
    path_rules jsonb DEFAULT '[]'::jsonb NOT NULL,
    connected_at timestamp with time zone DEFAULT now() NOT NULL,
    polled_through timestamp with time zone,
    last_poll_at timestamp with time zone,
    last_poll_error text,
    CONSTRAINT repo_connection_id_check CHECK (id),
    CONSTRAINT repo_connection_name_check CHECK ((name ~ '^[A-Za-z0-9._-]{1,100}$'::text)),
    CONSTRAINT repo_connection_owner_check CHECK ((owner ~ '^[A-Za-z0-9][A-Za-z0-9-]{0,38}$'::text)),
    CONSTRAINT repo_connection_path_rules_check CHECK ((jsonb_typeof(path_rules) = 'array'::text)),
    CONSTRAINT repo_connection_provider_check CHECK ((provider = 'github'::text))
);

CREATE TABLE run (
    id bigint NOT NULL,
    question_set_id bigint NOT NULL,
    trigger text NOT NULL,
    status text DEFAULT 'queued'::text NOT NULL,
    queued_at timestamp with time zone DEFAULT now() NOT NULL,
    started_at timestamp with time zone,
    finished_at timestamp with time zone,
    spend_usd numeric(12,6) DEFAULT 0 NOT NULL,
    spend_missing_calls integer DEFAULT 0 NOT NULL,
    error text,
    refusal_code text,
    lease_owner text,
    lease_until timestamp with time zone,
    CONSTRAINT run_status_check CHECK ((status = ANY (ARRAY['queued'::text, 'running'::text, 'completed'::text, 'capped'::text, 'refused'::text, 'failed'::text]))),
    CONSTRAINT run_trigger_check CHECK ((trigger = ANY (ARRAY['scheduled'::text, 'run_now'::text])))
);

ALTER TABLE run ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME run_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE search_connection (
    provider text NOT NULL,
    method text NOT NULL,
    account text DEFAULT ''::text NOT NULL,
    ciphertext text NOT NULL,
    site text,
    site_kind text,
    backfill boolean DEFAULT true NOT NULL,
    connected_at timestamp with time zone DEFAULT now() NOT NULL,
    last_sync_at timestamp with time zone,
    last_sync_error text,
    CONSTRAINT search_connection_site_check CHECK ((site <> ''::text)),
    CONSTRAINT search_connection_site_kind_check CHECK ((site_kind = ANY (ARRAY['domain'::text, 'url_prefix'::text])))
);

CREATE TABLE search_page_stats (
    provider text NOT NULL,
    site text NOT NULL,
    first_day date NOT NULL,
    last_day date NOT NULL,
    page text NOT NULL,
    clicks bigint NOT NULL,
    impressions bigint NOT NULL,
    "position" double precision NOT NULL,
    is_final boolean NOT NULL,
    CONSTRAINT search_page_stats_check CHECK ((last_day >= first_day)),
    CONSTRAINT search_page_stats_clicks_check CHECK ((clicks >= 0)),
    CONSTRAINT search_page_stats_impressions_check CHECK ((impressions >= 0)),
    CONSTRAINT search_page_stats_position_check CHECK (("position" >= (0)::double precision))
);

CREATE TABLE search_query_stats (
    provider text NOT NULL,
    site text NOT NULL,
    first_day date NOT NULL,
    last_day date NOT NULL,
    query text NOT NULL,
    clicks bigint NOT NULL,
    impressions bigint NOT NULL,
    "position" double precision NOT NULL,
    is_final boolean NOT NULL,
    CONSTRAINT search_query_stats_check CHECK ((last_day >= first_day)),
    CONSTRAINT search_query_stats_clicks_check CHECK ((clicks >= 0)),
    CONSTRAINT search_query_stats_impressions_check CHECK ((impressions >= 0)),
    CONSTRAINT search_query_stats_position_check CHECK (("position" >= (0)::double precision))
);

CREATE TABLE search_site_daily (
    provider text NOT NULL,
    site text NOT NULL,
    day date NOT NULL,
    clicks bigint NOT NULL,
    impressions bigint NOT NULL,
    "position" double precision NOT NULL,
    is_final boolean NOT NULL,
    CONSTRAINT search_site_daily_clicks_check CHECK ((clicks >= 0)),
    CONSTRAINT search_site_daily_impressions_check CHECK ((impressions >= 0)),
    CONSTRAINT search_site_daily_position_check CHECK (("position" >= (0)::double precision))
);

CREATE TABLE social_call (
    id bigint NOT NULL,
    network text NOT NULL,
    route text NOT NULL,
    term text NOT NULL,
    tool text NOT NULL,
    treg_call_id text DEFAULT ''::text NOT NULL,
    status text NOT NULL,
    http_status integer,
    cost_usd numeric(12,6),
    posts integer DEFAULT 0 NOT NULL,
    error text,
    called_at timestamp with time zone NOT NULL,
    CONSTRAINT social_call_check CHECK (((status = 'ok'::text) = (error IS NULL))),
    CONSTRAINT social_call_check1 CHECK (((route = 'treg'::text) OR (cost_usd = (0)::numeric))),
    CONSTRAINT social_call_route_check CHECK ((route = ANY (ARRAY['treg'::text, 'direct'::text]))),
    CONSTRAINT social_call_status_check CHECK ((status = ANY (ARRAY['ok'::text, 'failed'::text, 'refused'::text, 'unavailable'::text])))
);

ALTER TABLE social_call ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME social_call_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE social_filter (
    id bigint NOT NULL,
    kind text NOT NULL,
    mode text NOT NULL,
    value text NOT NULL,
    pattern text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT social_filter_check CHECK (((kind = 'topic'::text) = (pattern <> ''::text))),
    CONSTRAINT social_filter_kind_check CHECK ((kind = ANY (ARRAY['topic'::text, 'subreddit'::text]))),
    CONSTRAINT social_filter_mode_check CHECK ((mode = ANY (ARRAY['leave_out'::text, 'only'::text]))),
    CONSTRAINT social_filter_value_check CHECK (((value = lower(value)) AND ((char_length(value) >= 2) AND (char_length(value) <= 100))))
);

ALTER TABLE social_filter ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME social_filter_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE social_left_out (
    id bigint NOT NULL,
    network text NOT NULL,
    handle text NOT NULL,
    label text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT social_left_out_handle_check CHECK (((handle = lower(handle)) AND (handle <> ''::text))),
    CONSTRAINT social_left_out_label_check CHECK (((char_length(label) >= 1) AND (char_length(label) <= 80)))
);

ALTER TABLE social_left_out ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME social_left_out_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE social_mention (
    id bigint NOT NULL,
    network text NOT NULL,
    post_id text NOT NULL,
    url text NOT NULL,
    author_handle text NOT NULL,
    author_name text DEFAULT ''::text NOT NULL,
    text text DEFAULT ''::text NOT NULL,
    posted_at timestamp with time zone NOT NULL,
    posted_approx boolean DEFAULT false NOT NULL,
    likes integer,
    replies integer,
    reposts integer,
    score integer,
    views bigint,
    engagement bigint GENERATED ALWAYS AS (
CASE
    WHEN (network = 'youtube'::text) THEN COALESCE(views, (0)::bigint)
    ELSE ((((COALESCE(likes, 0) + COALESCE(replies, 0)) + COALESCE(reposts, 0)) + COALESCE(score, 0)))::bigint
END) STORED,
    term text NOT NULL,
    call_id bigint,
    first_seen_at timestamp with time zone NOT NULL,
    last_seen_at timestamp with time zone NOT NULL,
    subreddit text GENERATED ALWAYS AS (
CASE
    WHEN (network = 'reddit'::text) THEN COALESCE(lower("substring"(url, '/r/([A-Za-z0-9_]+)'::text)), ''::text)
    ELSE ''::text
END) STORED NOT NULL,
    CONSTRAINT social_mention_author_handle_check CHECK ((author_handle = lower(author_handle))),
    CONSTRAINT social_mention_post_id_check CHECK ((post_id <> ''::text))
);

ALTER TABLE social_mention ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME social_mention_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE social_network (
    network text NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    CONSTRAINT social_network_network_check CHECK ((network = ANY (ARRAY['x'::text, 'reddit'::text, 'hackernews'::text, 'linkedin'::text, 'youtube'::text, 'threads'::text, 'bluesky'::text])))
);

CREATE TABLE source_page (
    url text NOT NULL,
    domain text NOT NULL,
    status text NOT NULL,
    fetched_at timestamp with time zone NOT NULL,
    http_status integer,
    content_hash text,
    self_present boolean,
    brands_present text[] DEFAULT '{}'::text[] NOT NULL,
    error text,
    CONSTRAINT source_page_check CHECK (((status = 'ok'::text) = (self_present IS NOT NULL))),
    CONSTRAINT source_page_status_check CHECK ((status = ANY (ARRAY['ok'::text, 'failed'::text, 'disallowed'::text, 'refused'::text])))
);

CREATE VIEW spend AS
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
  WHERE (brand_call.cost_usd IS NOT NULL);

CREATE TABLE surface (
    key text NOT NULL,
    enabled boolean DEFAULT false NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    route text DEFAULT 'treg'::text NOT NULL,
    base_url text,
    credential_id bigint,
    CONSTRAINT surface_base_url_check CHECK ((base_url ~ '^https?://'::text)),
    CONSTRAINT surface_gateway_base_url CHECK (((route = 'gateway'::text) = (base_url IS NOT NULL))),
    CONSTRAINT surface_key_check CHECK ((key = ANY (ARRAY['chatgpt_app'::text, 'gemini_app'::text, 'google_ai_mode'::text, 'google_ai_overview'::text, 'copilot'::text, 'bing_ai_mode'::text, 'brave'::text, 'claude_code'::text, 'gemini_cli'::text, 'codex_cli'::text, 'claude_app'::text, 'perplexity_app'::text]))),
    CONSTRAINT surface_route_check CHECK ((route = ANY (ARRAY['treg'::text, 'provider'::text, 'scraper'::text, 'gateway'::text]))),
    CONSTRAINT surface_route_credential CHECK (((route = 'treg'::text) = (credential_id IS NULL)))
);

CREATE TABLE surface_model (
    id bigint NOT NULL,
    surface_key text NOT NULL,
    model text,
    fidelity text NOT NULL,
    force_web_search boolean DEFAULT false NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    last_tested_at timestamp with time zone,
    test_searched boolean,
    paused_reason text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    test_status text,
    CONSTRAINT surface_model_fidelity_check CHECK ((fidelity = ANY (ARRAY['app'::text, 'model_match'::text, 'approximation'::text]))),
    CONSTRAINT surface_model_model_check CHECK ((model <> ''::text))
);

ALTER TABLE surface_model ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME surface_model_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE VIEW tracked_brand AS
 SELECT id,
    name,
    aliases,
    domains,
    kind
   FROM brand
  WHERE (kind = ANY (ARRAY['self'::text, 'competitor'::text, 'technology'::text]));

CREATE TABLE url_inspection (
    id bigint NOT NULL,
    url text NOT NULL,
    checked_at timestamp with time zone NOT NULL,
    verdict text DEFAULT ''::text NOT NULL,
    coverage_state text DEFAULT ''::text NOT NULL,
    last_crawl_at timestamp with time zone,
    google_canonical text DEFAULT ''::text NOT NULL,
    user_canonical text DEFAULT ''::text NOT NULL,
    error text
);

ALTER TABLE url_inspection ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME url_inspection_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

ALTER TABLE ONLY alert_delivery
    ADD CONSTRAINT alert_delivery_alert_id_channel_id_key UNIQUE (alert_id, channel_id);

ALTER TABLE ONLY alert_delivery
    ADD CONSTRAINT alert_delivery_pkey PRIMARY KEY (id);

ALTER TABLE ONLY alert
    ADD CONSTRAINT alert_kind_subject_key UNIQUE (kind, subject);

ALTER TABLE ONLY alert
    ADD CONSTRAINT alert_pkey PRIMARY KEY (id);

ALTER TABLE ONLY alert_read
    ADD CONSTRAINT alert_read_pkey PRIMARY KEY (user_id, alert_id);

ALTER TABLE ONLY answer
    ADD CONSTRAINT answer_cell_sample UNIQUE (run_id, question_id, surface_model_id, sample);

ALTER TABLE ONLY answer
    ADD CONSTRAINT answer_pkey PRIMARY KEY (id);

ALTER TABLE ONLY brand_call
    ADD CONSTRAINT brand_call_pkey PRIMARY KEY (id);

ALTER TABLE ONLY brand_logo
    ADD CONSTRAINT brand_logo_pkey PRIMARY KEY (brand_id);

ALTER TABLE ONLY brand_mention
    ADD CONSTRAINT brand_mention_pkey PRIMARY KEY (answer_id, brand_id);

ALTER TABLE ONLY brand
    ADD CONSTRAINT brand_pkey PRIMARY KEY (id);

ALTER TABLE ONLY change
    ADD CONSTRAINT change_pkey PRIMARY KEY (id);

ALTER TABLE ONLY citation
    ADD CONSTRAINT citation_pkey PRIMARY KEY (answer_id, ord);

ALTER TABLE ONLY control_page
    ADD CONSTRAINT control_page_pkey PRIMARY KEY (id);

ALTER TABLE ONLY control_page
    ADD CONSTRAINT control_page_url_key UNIQUE (url);

ALTER TABLE ONLY discovered_mention
    ADD CONSTRAINT discovered_mention_pkey PRIMARY KEY (answer_id, brand_id);

ALTER TABLE ONLY goal
    ADD CONSTRAINT goal_pkey PRIMARY KEY (id);

ALTER TABLE ONLY goal
    ADD CONSTRAINT goal_provider_metric_window_days_due_on_key UNIQUE (provider, metric, window_days, due_on);

ALTER TABLE ONLY instance_settings
    ADD CONSTRAINT instance_settings_pkey PRIMARY KEY (id);

ALTER TABLE ONLY job_run
    ADD CONSTRAINT job_run_pkey PRIMARY KEY (id);

ALTER TABLE ONLY job_run
    ADD CONSTRAINT job_run_run_id_key UNIQUE (run_id);

ALTER TABLE ONLY notification_channel
    ADD CONSTRAINT notification_channel_pkey PRIMARY KEY (id);

ALTER TABLE ONLY question_map
    ADD CONSTRAINT question_map_pkey PRIMARY KEY (old_question_id);

ALTER TABLE ONLY question_page
    ADD CONSTRAINT question_page_pkey PRIMARY KEY (id);

ALTER TABLE ONLY question_page
    ADD CONSTRAINT question_page_question_id_url_key_key UNIQUE (question_id, url_key);

ALTER TABLE ONLY question
    ADD CONSTRAINT question_pkey PRIMARY KEY (id);

ALTER TABLE ONLY question
    ADD CONSTRAINT question_set_id_ord_key UNIQUE (set_id, ord);

ALTER TABLE ONLY question_set
    ADD CONSTRAINT question_set_pkey PRIMARY KEY (id);

ALTER TABLE ONLY question_set
    ADD CONSTRAINT question_set_version_key UNIQUE (version);

ALTER TABLE ONLY repo_connection
    ADD CONSTRAINT repo_connection_pkey PRIMARY KEY (id);

ALTER TABLE ONLY run
    ADD CONSTRAINT run_pkey PRIMARY KEY (id);

ALTER TABLE ONLY search_connection
    ADD CONSTRAINT search_connection_pkey PRIMARY KEY (provider);

ALTER TABLE ONLY search_page_stats
    ADD CONSTRAINT search_page_stats_pkey PRIMARY KEY (provider, site, first_day, page);

ALTER TABLE ONLY search_query_stats
    ADD CONSTRAINT search_query_stats_pkey PRIMARY KEY (provider, site, first_day, query);

ALTER TABLE ONLY search_site_daily
    ADD CONSTRAINT search_site_daily_pkey PRIMARY KEY (provider, site, day);

ALTER TABLE ONLY social_call
    ADD CONSTRAINT social_call_pkey PRIMARY KEY (id);

ALTER TABLE ONLY social_filter
    ADD CONSTRAINT social_filter_kind_value_key UNIQUE (kind, value);

ALTER TABLE ONLY social_filter
    ADD CONSTRAINT social_filter_pkey PRIMARY KEY (id);

ALTER TABLE ONLY social_left_out
    ADD CONSTRAINT social_left_out_network_handle_key UNIQUE (network, handle);

ALTER TABLE ONLY social_left_out
    ADD CONSTRAINT social_left_out_pkey PRIMARY KEY (id);

ALTER TABLE ONLY social_mention
    ADD CONSTRAINT social_mention_network_post_id_key UNIQUE (network, post_id);

ALTER TABLE ONLY social_mention
    ADD CONSTRAINT social_mention_pkey PRIMARY KEY (id);

ALTER TABLE ONLY social_network
    ADD CONSTRAINT social_network_pkey PRIMARY KEY (network);

ALTER TABLE ONLY source_page
    ADD CONSTRAINT source_page_pkey PRIMARY KEY (url);

ALTER TABLE ONLY surface_model
    ADD CONSTRAINT surface_model_pkey PRIMARY KEY (id);

ALTER TABLE ONLY surface_model
    ADD CONSTRAINT surface_model_surface_key_model_key UNIQUE NULLS NOT DISTINCT (surface_key, model);

ALTER TABLE ONLY surface
    ADD CONSTRAINT surface_pkey PRIMARY KEY (key);

ALTER TABLE ONLY url_inspection
    ADD CONSTRAINT url_inspection_pkey PRIMARY KEY (id);

CREATE INDEX alert_delivery_due ON alert_delivery USING btree (next_attempt_at) WHERE (status = 'pending'::text);

CREATE INDEX answer_question ON answer USING btree (question_id);

CREATE INDEX answer_surface_model ON answer USING btree (surface_model_id);

CREATE INDEX brand_call_answer ON brand_call USING btree (answer_id) WHERE (purpose = 'find'::text);

CREATE INDEX brand_call_brand ON brand_call USING btree (brand_id) WHERE (purpose = 'lookup'::text);

CREATE INDEX brand_call_called ON brand_call USING btree (called_at);

CREATE INDEX brand_call_run ON brand_call USING btree (run_id) WHERE (run_id IS NOT NULL);

CREATE UNIQUE INDEX brand_key ON brand USING btree (key) WHERE (key IS NOT NULL);

CREATE INDEX brand_mention_brand ON brand_mention USING btree (brand_id);

CREATE UNIQUE INDEX brand_name ON brand USING btree (lower(name));

CREATE UNIQUE INDEX brand_one_self ON brand USING btree (kind) WHERE (kind = 'self'::text);

CREATE INDEX change_merged ON change USING btree (merged_at DESC);

CREATE UNIQUE INDEX change_pr_page ON change USING btree (repo, pr_number, url) WHERE (pr_number IS NOT NULL);

CREATE INDEX citation_page_key ON citation USING btree (page_key(url));

CREATE INDEX citation_url ON citation USING btree (url);

CREATE INDEX discovered_mention_brand ON discovered_mention USING btree (brand_id);

CREATE INDEX job_run_job ON job_run USING btree (job, started_at DESC);

CREATE UNIQUE INDEX notification_channel_name ON notification_channel USING btree (lower(name));

CREATE INDEX question_map_new ON question_map USING btree (new_question_id);

CREATE INDEX run_open ON run USING btree (id) WHERE (status = ANY (ARRAY['queued'::text, 'running'::text]));

CREATE INDEX search_page_stats_page ON search_page_stats USING btree (provider, site, page, first_day);

CREATE INDEX social_call_called ON social_call USING btree (called_at);

CREATE INDEX social_mention_author ON social_mention USING btree (network, author_handle);

CREATE INDEX social_mention_first_seen ON social_mention USING btree (first_seen_at);

CREATE INDEX social_mention_posted ON social_mention USING btree (posted_at DESC);

CREATE INDEX source_page_fetched ON source_page USING btree (fetched_at);

CREATE INDEX url_inspection_checked ON url_inspection USING btree (checked_at);

CREATE INDEX url_inspection_url ON url_inspection USING btree (url, checked_at DESC);

ALTER TABLE ONLY alert_delivery
    ADD CONSTRAINT alert_delivery_alert_id_fkey FOREIGN KEY (alert_id) REFERENCES alert(id) ON DELETE CASCADE;

ALTER TABLE ONLY alert_delivery
    ADD CONSTRAINT alert_delivery_channel_id_fkey FOREIGN KEY (channel_id) REFERENCES notification_channel(id) ON DELETE CASCADE;

ALTER TABLE ONLY alert_read
    ADD CONSTRAINT alert_read_alert_id_fkey FOREIGN KEY (alert_id) REFERENCES alert(id) ON DELETE CASCADE;

ALTER TABLE ONLY alert_read
    ADD CONSTRAINT alert_read_user_id_fkey FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE;

ALTER TABLE ONLY answer
    ADD CONSTRAINT answer_question_id_fkey FOREIGN KEY (question_id) REFERENCES question(id);

ALTER TABLE ONLY answer
    ADD CONSTRAINT answer_run_id_fkey FOREIGN KEY (run_id) REFERENCES run(id) ON DELETE CASCADE;

ALTER TABLE ONLY answer
    ADD CONSTRAINT answer_surface_model_id_fkey FOREIGN KEY (surface_model_id) REFERENCES surface_model(id);

ALTER TABLE ONLY brand_call
    ADD CONSTRAINT brand_call_answer_id_fkey FOREIGN KEY (answer_id) REFERENCES answer(id) ON DELETE SET NULL;

ALTER TABLE ONLY brand_call
    ADD CONSTRAINT brand_call_brand_id_fkey FOREIGN KEY (brand_id) REFERENCES brand(id) ON DELETE SET NULL;

ALTER TABLE ONLY brand_call
    ADD CONSTRAINT brand_call_job_run_id_fkey FOREIGN KEY (job_run_id) REFERENCES job_run(id) ON DELETE SET NULL;

ALTER TABLE ONLY brand_call
    ADD CONSTRAINT brand_call_run_id_fkey FOREIGN KEY (run_id) REFERENCES run(id) ON DELETE SET NULL;

ALTER TABLE ONLY brand_logo
    ADD CONSTRAINT brand_logo_brand_id_fkey FOREIGN KEY (brand_id) REFERENCES brand(id) ON DELETE CASCADE;

ALTER TABLE ONLY brand_mention
    ADD CONSTRAINT brand_mention_answer_id_fkey FOREIGN KEY (answer_id) REFERENCES answer(id) ON DELETE CASCADE;

ALTER TABLE ONLY brand_mention
    ADD CONSTRAINT brand_mention_brand_id_fkey FOREIGN KEY (brand_id) REFERENCES brand(id) ON DELETE CASCADE;

ALTER TABLE ONLY citation
    ADD CONSTRAINT citation_answer_id_fkey FOREIGN KEY (answer_id) REFERENCES answer(id) ON DELETE CASCADE;

ALTER TABLE ONLY discovered_mention
    ADD CONSTRAINT discovered_mention_answer_id_fkey FOREIGN KEY (answer_id) REFERENCES answer(id) ON DELETE CASCADE;

ALTER TABLE ONLY discovered_mention
    ADD CONSTRAINT discovered_mention_brand_id_fkey FOREIGN KEY (brand_id) REFERENCES brand(id) ON DELETE CASCADE;

ALTER TABLE ONLY job_run
    ADD CONSTRAINT job_run_run_id_fkey FOREIGN KEY (run_id) REFERENCES run(id) ON DELETE CASCADE;

ALTER TABLE ONLY question_map
    ADD CONSTRAINT question_map_new_question_id_fkey FOREIGN KEY (new_question_id) REFERENCES question(id);

ALTER TABLE ONLY question_map
    ADD CONSTRAINT question_map_old_question_id_fkey FOREIGN KEY (old_question_id) REFERENCES question(id);

ALTER TABLE ONLY question_page
    ADD CONSTRAINT question_page_added_by_fkey FOREIGN KEY (added_by) REFERENCES users(id) ON DELETE SET NULL;

ALTER TABLE ONLY question_page
    ADD CONSTRAINT question_page_question_id_fkey FOREIGN KEY (question_id) REFERENCES question(id) ON DELETE CASCADE;

ALTER TABLE ONLY question
    ADD CONSTRAINT question_set_id_fkey FOREIGN KEY (set_id) REFERENCES question_set(id);

ALTER TABLE ONLY run
    ADD CONSTRAINT run_question_set_id_fkey FOREIGN KEY (question_set_id) REFERENCES question_set(id);

ALTER TABLE ONLY social_call
    ADD CONSTRAINT social_call_network_fkey FOREIGN KEY (network) REFERENCES social_network(network);

ALTER TABLE ONLY social_left_out
    ADD CONSTRAINT social_left_out_network_fkey FOREIGN KEY (network) REFERENCES social_network(network);

ALTER TABLE ONLY social_mention
    ADD CONSTRAINT social_mention_call_id_fkey FOREIGN KEY (call_id) REFERENCES social_call(id) ON DELETE SET NULL;

ALTER TABLE ONLY social_mention
    ADD CONSTRAINT social_mention_network_fkey FOREIGN KEY (network) REFERENCES social_network(network);

ALTER TABLE ONLY surface
    ADD CONSTRAINT surface_credential_id_fkey FOREIGN KEY (credential_id) REFERENCES credential(id) ON DELETE RESTRICT;

ALTER TABLE ONLY surface_model
    ADD CONSTRAINT surface_model_surface_key_fkey FOREIGN KEY (surface_key) REFERENCES surface(key);

INSERT INTO instance_settings (id, match_mode, market, language, time_zone, check_every_days, check_time, checks_paused, next_check_at, last_check_at, spend_cap_run_usd, spend_cap_month_usd, concurrency, retries, timeout_s, raw_retention_days, branded_terms, onboarding_steps, onboarded_at, change_interim_days, change_verdict_days, change_position_threshold, change_ctr_threshold, change_impressions_ratio, social_terms, social_fetch_every_hours, social_digest_cadence, social_digest_weekday, social_digest_sent_through, social_enabled, find_brands, brand_logos, brand_logo_month_limit, other_brands_min_questions, samples) VALUES
    ('true', 'whole_word', 'US', 'en', 'UTC', '3', '06:00:00', 'false', NULL, NULL, '5.000000', NULL, '8', '1', '180', NULL, NULL, '{}', NULL, '17', '31', '2.5', '0.8', '2.7', NULL, '6', 'weekly', '1', NULL, 'false', 'true', 'true', '100', '3', '1');

INSERT INTO social_network (network, enabled) VALUES
    ('bluesky', 'true'),
    ('hackernews', 'true'),
    ('linkedin', 'true'),
    ('reddit', 'true'),
    ('threads', 'true'),
    ('x', 'true'),
    ('youtube', 'true');
