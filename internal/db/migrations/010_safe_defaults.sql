-- Safe defaults for people new to Byname: social searches stay off until
-- someone turns them on, and a question that names your own brand is still
-- asked but left out of the scores, because an answer naming you is no news
-- when the question did.

-- Social searches spend Treg credit, so they wait for someone to turn them
-- on. An instance that has already searched keeps its feed.
ALTER TABLE instance_settings ADD COLUMN social_enabled boolean NOT NULL DEFAULT false;
UPDATE instance_settings SET social_enabled = true WHERE EXISTS (SELECT 1 FROM social_call);

-- names_brand is whether the question's text names your brand, by the rules
-- answers are scored with; internal/config keeps it current whenever the
-- questions or the brand table change, and on every start, which is also
-- how questions from before this migration get it. count_in_scores_override
-- is a person's choice, NULL for the default; counts_in_scores is what every
-- score reads.
ALTER TABLE question
    ADD COLUMN names_brand boolean NOT NULL DEFAULT false,
    ADD COLUMN count_in_scores_override boolean,
    ADD COLUMN counts_in_scores boolean NOT NULL GENERATED ALWAYS AS (coalesce(count_in_scores_override, NOT names_brand)) STORED;
