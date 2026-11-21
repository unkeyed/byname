-- Notifications: the channels alerts go out on, every alert Byname raised,
-- each alert's delivery to each channel, and who has read which alert.
-- Written by internal/notify, whose registry is the list of event kinds.

-- settings is what the channel shows and edits in the clear (host, port,
-- recipients, a webhook's URL). ciphertext is its secrets (a password, an
-- API key, a Slack webhook URL, a webhook's signing secret) as JSON sealed
-- with ENCRYPTION_KEY, which no API returns. events are the event kinds the
-- channel gets.
CREATE TABLE notification_channel (
    id               bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name             text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 80),
    kind             text NOT NULL CHECK (kind IN ('smtp', 'email_api', 'slack_webhook', 'webhook')),
    settings         jsonb NOT NULL DEFAULT '{}',
    ciphertext       text NOT NULL,
    events           text[] NOT NULL DEFAULT '{}',
    enabled          boolean NOT NULL DEFAULT true,
    last_test_at     timestamptz,
    last_test_status text,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX notification_channel_name ON notification_channel (lower(name));

-- One row per thing that happened. (kind, subject) is its natural key, such
-- as (check_completed, run/12), so a retried job never raises it twice.
CREATE TABLE alert (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    kind       text NOT NULL,
    subject    text NOT NULL,
    payload    jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (kind, subject)
);

-- One row per alert and channel. A pending row is due at next_attempt_at;
-- claiming it counts an attempt and pushes next_attempt_at out, so a process
-- that dies mid-send leaves it to be retried, not lost.
CREATE TABLE alert_delivery (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    alert_id        bigint NOT NULL REFERENCES alert (id) ON DELETE CASCADE,
    channel_id      bigint NOT NULL REFERENCES notification_channel (id) ON DELETE CASCADE,
    status          text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'sent', 'failed')),
    attempts        int NOT NULL DEFAULT 0,
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    sent_at         timestamptz,
    error           text,
    created_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (alert_id, channel_id)
);
CREATE INDEX alert_delivery_due ON alert_delivery (next_attempt_at) WHERE status = 'pending';

CREATE TABLE alert_read (
    user_id  bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    alert_id bigint NOT NULL REFERENCES alert (id) ON DELETE CASCADE,
    read_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, alert_id)
);
