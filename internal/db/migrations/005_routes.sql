-- Routes: how each surface's calls reach a provider. Treg is the default;
-- a surface can instead use the pinned endpoint's provider with its own key
-- (scraper), a direct model API key (provider), or an OpenAI-compatible
-- gateway. The route and credential lists repeat internal/engines;
-- TestRouteChecksMatchEngines parses this file and fails when they disagree.
--
-- Also onboarding: the steps saved so far and when it was finished. Nothing
-- runs on schedule before it is finished.

ALTER TABLE credential DROP CONSTRAINT credential_kind_check;
ALTER TABLE credential ADD CONSTRAINT credential_kind_check CHECK (kind IN (
    'treg', 'openai', 'anthropic', 'perplexity', 'google', 'dataforseo', 'cloro', 'serpapi', 'gateway'));

-- credential_id is NULL on the Treg route, which uses the one Treg token,
-- and names the key every other route calls with. base_url is the
-- gateway's, and only a gateway has one. A key in use cannot be deleted.
ALTER TABLE surface
    ADD COLUMN route text NOT NULL DEFAULT 'treg' CHECK (route IN ('treg', 'provider', 'scraper', 'gateway')),
    ADD COLUMN base_url text CHECK (base_url ~ '^https?://'),
    ADD COLUMN credential_id bigint REFERENCES credential (id) ON DELETE RESTRICT,
    ADD CONSTRAINT surface_gateway_base_url CHECK ((route = 'gateway') = (base_url IS NOT NULL)),
    ADD CONSTRAINT surface_route_credential CHECK ((route = 'treg') = (credential_id IS NULL));

-- test_status is 'ok' after a test call that answered, else why it failed;
-- NULL until the pair is tested, and again after its route changes.
ALTER TABLE surface_model ADD COLUMN test_status text;

ALTER TABLE instance_settings
    ADD COLUMN onboarding_steps text[] NOT NULL DEFAULT '{}',
    ADD COLUMN onboarded_at timestamptz;

-- An instance already set up and checking before onboarding existed counts
-- as onboarded, so upgrading never stops its schedule.
UPDATE instance_settings SET onboarded_at = now()
WHERE next_check_at IS NOT NULL OR EXISTS (SELECT 1 FROM run);
