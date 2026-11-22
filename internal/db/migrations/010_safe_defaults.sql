-- Safe defaults for people new to Byname: social searches stay off until
-- someone turns them on, and a question that names your own brand is still
-- asked but left out of the scores, because an answer naming you is no news
-- when the question did.

-- Social searches spend Treg credit, so they wait for someone to turn them
-- on. An instance that has already searched keeps its feed.
ALTER TABLE instance_settings ADD COLUMN social_enabled boolean NOT NULL DEFAULT false;
UPDATE instance_settings SET social_enabled = true WHERE EXISTS (SELECT 1 FROM social_call);
