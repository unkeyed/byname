-- Byname schema, version 1: the people who sign in and the keys agents use.
-- Written only by internal/auth.
--
-- Every piece of state between two requests lives here (auth_challenges,
-- setup_tokens, login_failures), so any replica can serve any step.
--
-- The role lists below repeat core.Roles; TestRoleChecksMatchCore parses
-- this file and fails when they disagree.

-- Humans who sign into the UI. A password never yields a session on its
-- own, so password_hash is NULL for nearly everyone.
CREATE TABLE users (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    email           text NOT NULL UNIQUE CHECK (email = lower(btrim(email))),
    name            text NOT NULL DEFAULT '',
    role            text NOT NULL CHECK (role IN ('admin', 'editor', 'viewer')),
    password_hash   text,
    -- The WebAuthn user handle. Random, so a passkey never carries the user id.
    webauthn_handle bytea NOT NULL UNIQUE DEFAULT uuid_send(gen_random_uuid()),
    created_at      timestamptz NOT NULL DEFAULT now()
);

-- Session cookies. Only the SHA-256 of the cookie value is stored.
CREATE TABLE sessions (
    token_hash       bytea PRIMARY KEY,
    id               bigint GENERATED ALWAYS AS IDENTITY UNIQUE,
    user_id          bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    method           text NOT NULL CHECK (method <> ''),
    user_agent       text NOT NULL DEFAULT '',
    expires_at       timestamptz NOT NULL,
    created_at       timestamptz NOT NULL DEFAULT now(),
    last_seen_at     timestamptz NOT NULL DEFAULT now(),
    -- When the person last proved a factor in this session. Removing a
    -- sign-in method needs this to be recent.
    authenticated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX sessions_expires ON sessions (expires_at);
CREATE INDEX sessions_user ON sessions (user_id);

-- One-time teammate invite links. Only the token hash is stored. The
-- invited person joins with role.
CREATE TABLE invites (
    token_hash  bytea PRIMARY KEY,
    email       text NOT NULL CHECK (email = lower(btrim(email))),
    role        text NOT NULL CHECK (role IN ('admin', 'editor', 'viewer')),
    created_by  bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    expires_at  timestamptz NOT NULL,
    accepted_at timestamptz
);

-- A login at an external provider (google, github, oidc) linked to a user.
CREATE TABLE user_identities (
    provider     text NOT NULL,
    subject      text NOT NULL,
    user_id      bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    email        text NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz,
    PRIMARY KEY (provider, subject),
    UNIQUE (user_id, provider)
);

-- credential is go-webauthn's Credential record as JSON: public key, sign
-- count, transports, flags and attestation, kept whole so no field the
-- library verifies on a later login is lost.
CREATE TABLE passkeys (
    id           bytea PRIMARY KEY,
    user_id      bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name         text NOT NULL,
    credential   jsonb NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz
);
CREATE INDEX passkeys_user ON passkeys (user_id);

-- A confirmed authenticator app. secret is sealed with ENCRYPTION_KEY.
-- last_step is the newest 30-second time step a code was accepted for; a
-- code for that step or an older one is a replay.
CREATE TABLE totp (
    user_id    bigint PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    secret     text NOT NULL,
    last_step  bigint NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- Single-use recovery codes. Using one deletes its row.
CREATE TABLE recovery_codes (
    user_id   bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    code_hash bytea NOT NULL,
    PRIMARY KEY (user_id, code_hash)
);

-- Short-lived state between two requests of one sign-in step: an OAuth
-- redirect, a WebAuthn ceremony, a login waiting for its second factor, an
-- authenticator app waiting for its first code. The browser holds a random
-- handle; only its hash is stored.
CREATE TABLE auth_challenges (
    handle_hash bytea PRIMARY KEY,
    kind        text NOT NULL,
    data        jsonb NOT NULL,
    expires_at  timestamptz NOT NULL
);
CREATE INDEX auth_challenges_expires ON auth_challenges (expires_at);

-- Setup links. A row without user_id exists only while nobody can sign in;
-- the first account's creation deletes them all. Each boot in that state
-- adds one, so every logged link keeps working until setup is done.
-- A row with user_id is a recovery link from `byname recover`: it
-- registers a passkey for that existing user even after setup closed, and
-- stops working at expires_at.
CREATE TABLE setup_tokens (
    token_hash bytea PRIMARY KEY,
    user_id    bigint REFERENCES users (id) ON DELETE CASCADE,
    expires_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT setup_tokens_recovery_expires CHECK (user_id IS NULL OR expires_at IS NOT NULL)
);

-- Failed sign-in attempts per key: "email:<address>", "ip:<address or /64>"
-- or "user:<id>" for second-factor codes. Rows idle for a day are swept on
-- the next failure.
CREATE TABLE login_failures (
    key            text PRIMARY KEY,
    failures       int NOT NULL CHECK (failures > 0),
    last_failed_at timestamptz NOT NULL
);
CREATE INDEX login_failures_last_failed ON login_failures (last_failed_at);

-- Built-in API keys (used when Unkey is not configured). Keys are 32 random
-- bytes, so a plain SHA-256 is enough; no slow hash needed. scopes is what
-- the key was granted; each request gets only the part its creator's role
-- still allows. One past expires_at stops verifying.
CREATE TABLE api_keys (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name         text NOT NULL,
    prefix       text NOT NULL,
    token_hash   bytea NOT NULL UNIQUE,
    scopes       text[] NOT NULL,
    created_by   bigint REFERENCES users (id) ON DELETE SET NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz,
    revoked_at   timestamptz,
    expires_at   timestamptz
);
