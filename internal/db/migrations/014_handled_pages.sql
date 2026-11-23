-- Handled questions: a page someone published to answer a question AI
-- answers miss them on, marked against that question. internal/config
-- writes the marks; internal/results reads every later answer that links
-- to a marked page, for any question, and how the question's own answers
-- name you before and after.

-- page_key makes a URL comparable with how answers link to it: AI apps
-- add tracking parameters, fragments, www. and trailing slashes, so
-- https://www.northwind.example/blog/x/?ref=chatgpt and
-- northwind.example/blog/x are the same page. Every match compares
-- page_key on both sides, so no code normalizes URLs on its own.
CREATE FUNCTION page_key(u text) RETURNS text
    LANGUAGE sql IMMUTABLE PARALLEL SAFE
    AS $$ SELECT rtrim(regexp_replace(regexp_replace(lower(btrim(u)), '^https?://(www\.)?', ''), '[?#].*$', ''), '/') $$;

-- url is the page as it was entered. A question can have several pages,
-- and a page several questions. added_at is when tracking starts: only
-- answers given at or after it count as linking to the page.
CREATE TABLE question_page (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    question_id bigint NOT NULL REFERENCES question (id) ON DELETE CASCADE,
    url         text NOT NULL CHECK (char_length(url) BETWEEN 1 AND 2000),
    url_key     text NOT NULL GENERATED ALWAYS AS (page_key(url)) STORED,
    added_by    bigint REFERENCES users (id) ON DELETE SET NULL,
    added_at    timestamptz NOT NULL DEFAULT now(),
    UNIQUE (question_id, url_key)
);

-- Finds the answers that link to a marked page without reading every
-- citation.
CREATE INDEX citation_page_key ON citation (page_key(url));
