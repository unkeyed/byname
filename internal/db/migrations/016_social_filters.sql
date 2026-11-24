-- Social filters: topics and subreddits people leave out of social
-- mentions, or keep to. A brand's name can mean something else somewhere
-- else, so people hide the posts about that. Like social_left_out, reads
-- apply them, so a filter covers past mentions too and removing it brings
-- them back.

-- The subreddit a Reddit post is in, from its link
-- (https://www.reddit.com/r/<name>/comments/...), lowercase; empty
-- elsewhere.
ALTER TABLE social_mention
    ADD COLUMN subreddit text NOT NULL GENERATED ALWAYS AS (CASE WHEN network = 'reddit'
        THEN coalesce(lower(substring(url FROM '/r/([A-Za-z0-9_]+)')), '') ELSE '' END) STORED;

-- kind topic is a word or phrase matched as a whole word in a post's text,
-- in any case; pattern is the regular expression that does it, written by
-- internal/social. kind subreddit is a subreddit's name, without r/, and
-- matches Reddit posts only. mode leave_out hides the posts it matches;
-- mode only hides the posts of its kind that match no filter of that kind
-- and mode.
CREATE TABLE social_filter (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    kind       text NOT NULL CHECK (kind IN ('topic', 'subreddit')),
    mode       text NOT NULL CHECK (mode IN ('leave_out', 'only')),
    value      text NOT NULL CHECK (value = lower(value) AND char_length(value) BETWEEN 2 AND 100),
    pattern    text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (kind, value),
    CHECK ((kind = 'topic') = (pattern <> ''))
);
