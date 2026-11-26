-- Workspaces: each brand an instance tracks is a workspace with its own
-- settings, brands, questions, runs and everything else they lead to. The
-- first workspace keeps its tables in public, where every earlier release
-- put them. Each later one gets a schema of its own, ws_<id>, holding a
-- copy of every table that is not shared. Sign-in, keys, OAuth and the
-- provider keys in credential are shared and live in public only.
--
-- Tables a workspace owns change through migrations/workspace, which
-- internal/db applies to every workspace and records in
-- workspace_migration. This file is the last that adds them to public.

-- slug is the workspace's name in URLs. schema_name is public for exactly
-- one workspace, the first, and ws_<id> for every other.
CREATE TABLE workspace (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    slug        text NOT NULL UNIQUE CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$' AND char_length(slug) <= 40),
    name        text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 80),
    schema_name text NOT NULL UNIQUE CHECK (schema_name = 'public' OR schema_name = 'ws_' || id),
    created_at  timestamptz NOT NULL DEFAULT now()
);

-- Which workspace migrations each workspace has. It lives in public only,
-- so no workspace's schema can hide it.
CREATE TABLE workspace_migration (
    workspace_id bigint NOT NULL REFERENCES workspace (id) ON DELETE CASCADE,
    name         text NOT NULL,
    applied_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, name)
);

-- The instance as it stands becomes the first workspace, named after its
-- own brand. Its tables are already what the workspace baseline creates.
INSERT INTO workspace (slug, name, schema_name)
SELECT coalesce(nullif(btrim(left(btrim(regexp_replace(lower(name), '[^a-z0-9]+', '-', 'g'), '-'), 40), '-'), ''), 'default'),
       name, 'public'
FROM (SELECT coalesce((SELECT name FROM brand WHERE kind = 'self'), 'Default') AS name) self;

INSERT INTO workspace_migration (workspace_id, name)
SELECT id, '000_baseline.sql' FROM workspace;
