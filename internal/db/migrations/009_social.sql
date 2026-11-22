-- Social mentions: public posts that name the brand on X, Reddit, Hacker
-- News, LinkedIn, YouTube, Threads and Bluesky, found through one search
-- per network (Treg's for the first six, Bluesky's own public API), the
-- accounts left out of what people read (the team, the company's own
-- accounts), and the digest's schedule. Everything here is written by
-- internal/social, whose network registry is the list of networks.

-- social_terms is what each search looks for, at most 5; NULL searches for
-- the brand's name and primary domain. The digest covers the day, or the
-- week starting on social_digest_weekday (0 is Sunday), in the instance's
-- time zone. social_digest_sent_through is the start of the latest period
-- whose digest was emitted.
ALTER TABLE instance_settings
    ADD COLUMN social_terms text[] CHECK (cardinality(social_terms) BETWEEN 1 AND 5),
    ADD COLUMN social_fetch_every_hours int NOT NULL DEFAULT 6 CHECK (social_fetch_every_hours IN (1, 3, 6, 12, 24)),
    ADD COLUMN social_digest_cadence text NOT NULL DEFAULT 'weekly' CHECK (social_digest_cadence IN ('daily', 'weekly')),
    ADD COLUMN social_digest_weekday int NOT NULL DEFAULT 1 CHECK (social_digest_weekday BETWEEN 0 AND 6),
    ADD COLUMN social_digest_sent_through date;

-- One row per network, seeded on. A network searched through Treg runs
-- only while a Treg token is stored; Bluesky needs none.
CREATE TABLE social_network (
    network text PRIMARY KEY CHECK (network IN ('x', 'reddit', 'hackernews', 'linkedin', 'youtube', 'threads', 'bluesky')),
    enabled boolean NOT NULL DEFAULT true
);
INSERT INTO social_network (network) VALUES ('x'), ('reddit'), ('hackernews'), ('linkedin'), ('youtube'), ('threads'), ('bluesky');

-- Each search call: route treg (through Treg) or direct (the network's own
-- public API). status is ok (the answer parsed), failed (a provider error
-- or a body that did not parse), refused (Treg declined it, which costs
-- nothing) or unavailable (a direct API asked for sign-in). cost_usd is
-- NULL when Treg reported no cost, which is a missing cost, never $0; a
-- direct call's cost is a known 0.
CREATE TABLE social_call (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    network      text NOT NULL REFERENCES social_network (network),
    route        text NOT NULL CHECK (route IN ('treg', 'direct')),
    term         text NOT NULL,
    tool         text NOT NULL,
    treg_call_id text NOT NULL DEFAULT '',
    status       text NOT NULL CHECK (status IN ('ok', 'failed', 'refused', 'unavailable')),
    http_status  int,
    cost_usd     numeric(12, 6),
    posts        int NOT NULL DEFAULT 0,
    error        text,
    called_at    timestamptz NOT NULL,
    CHECK ((status = 'ok') = (error IS NULL)),
    CHECK (route = 'treg' OR cost_usd = 0)
);
CREATE INDEX social_call_called ON social_call (called_at);

-- One row per post, keyed by the network's own post id, so fetching it
-- again refreshes its engagement and changes nothing else. author_handle
-- is normalized (lowercase, no @, no u/). Engagement counts are NULL when
-- the network does not report them, never 0. engagement ranks posts within
-- a network: views on YouTube, whose search gives nothing else, elsewhere
-- the other counts added up. posted_approx is true when the network gave
-- only an age ("2 years ago"). call_id is the call that first found it.
CREATE TABLE social_mention (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    network       text NOT NULL REFERENCES social_network (network),
    post_id       text NOT NULL CHECK (post_id <> ''),
    url           text NOT NULL,
    author_handle text NOT NULL CHECK (author_handle = lower(author_handle)),
    author_name   text NOT NULL DEFAULT '',
    text          text NOT NULL DEFAULT '',
    posted_at     timestamptz NOT NULL,
    posted_approx boolean NOT NULL DEFAULT false,
    likes         int,
    replies       int,
    reposts       int,
    score         int,
    views         bigint,
    engagement    bigint GENERATED ALWAYS AS (CASE WHEN network = 'youtube' THEN coalesce(views, 0)
                      ELSE coalesce(likes, 0) + coalesce(replies, 0) + coalesce(reposts, 0) + coalesce(score, 0) END) STORED,
    term          text NOT NULL,
    call_id       bigint REFERENCES social_call (id) ON DELETE SET NULL,
    first_seen_at timestamptz NOT NULL,
    last_seen_at  timestamptz NOT NULL,
    UNIQUE (network, post_id)
);
CREATE INDEX social_mention_posted ON social_mention (posted_at DESC);
CREATE INDEX social_mention_first_seen ON social_mention (first_seen_at);
CREATE INDEX social_mention_author ON social_mention (network, author_handle);

-- Accounts whose posts people do not need to read: teammates and the
-- company's own accounts. Reads hide their mentions at read time, so the
-- list applies to past mentions too and removing an account brings them
-- back. label groups handles, such as a person's name or "Our accounts".
CREATE TABLE social_left_out (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    network    text NOT NULL REFERENCES social_network (network),
    handle     text NOT NULL CHECK (handle = lower(handle) AND handle <> ''),
    label      text NOT NULL CHECK (char_length(label) BETWEEN 1 AND 80),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (network, handle)
);

-- Every job name the code writes. TestJobCheckCoversEveryJob fails when
-- one is missing.
ALTER TABLE job_run DROP CONSTRAINT job_run_job_check,
    ADD CONSTRAINT job_run_job_check CHECK (job IN ('ai_check', 'model_refresh', 'gsc_sync', 'source_fetch',
        'site_changes', 'recrawl_watch', 'verdicts', 'social_fetch'));
