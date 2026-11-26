-- Entities: each brand an instance tracks is an entity with its own
-- settings, brands, questions, runs and everything else they lead to. The
-- first entity keeps its tables where every earlier release put them:
-- the install's own schema, the first on its role's search_path, which is
-- public unless DATABASE_URL or the role says otherwise. Each later one
-- gets a schema of its own, entity_<id>, holding a copy of every table that is
-- not shared. Sign-in, keys, OAuth and the provider keys in credential are
-- shared and live in the install's own schema only.
--
-- Tables an entity owns change through migrations/entity, which
-- internal/db applies to every entity and records in
-- entity_migration. This file is the last that adds them to the
-- install's own schema.

-- slug is the entity's name in URLs. schema_name is the install's own
-- schema for exactly one entity, the first, and entity_<id> for every other.
CREATE TABLE entity (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    slug        text NOT NULL UNIQUE CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$' AND char_length(slug) <= 40),
    name        text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 80),
    schema_name text NOT NULL UNIQUE CHECK (schema_name !~ '^entity_[0-9]+$' OR schema_name = 'entity_' || id),
    created_at  timestamptz NOT NULL DEFAULT now()
);

-- Which entity migrations each entity has. It lives in the install's
-- own schema only, so no entity's schema can hide it.
CREATE TABLE entity_migration (
    entity_id bigint NOT NULL REFERENCES entity (id) ON DELETE CASCADE,
    name         text NOT NULL,
    applied_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (entity_id, name)
);

-- The instance as it stands becomes the first entity, named after its
-- own brand. Its tables are already what the entity baseline creates.
INSERT INTO entity (slug, name, schema_name)
SELECT coalesce(nullif(btrim(left(btrim(regexp_replace(lower(name), '[^a-z0-9]+', '-', 'g'), '-'), 40), '-'), ''), 'default'),
       name, current_schema()
FROM (SELECT coalesce((SELECT name FROM brand WHERE kind = 'self'), 'Default') AS name) self;

INSERT INTO entity_migration (entity_id, name)
SELECT id, '000_baseline.sql' FROM entity;
