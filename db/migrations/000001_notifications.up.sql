-- notifications-service: the per-user feed and inbox.
--
-- Everything here is written by consumers of NATS subjects (user-activity,
-- user-follow) and read by the GraphQL API. The design goal is that every
-- read a page makes is one index range scan: the feed is materialised per
-- user at write time (feed_items), the inbox is per user by construction, and
-- the unread count is an index-only scan of a partial index.

-- One row per activity event, the source the feed and profile history point
-- at. event_id is the producer's id: inserting the same event twice is a
-- no-op, which is what makes redelivery safe.
CREATE TABLE IF NOT EXISTS activities (
    id              uuid PRIMARY KEY,
    event_id        uuid NOT NULL UNIQUE,
    actor_id        varchar(100) NOT NULL,
    type            varchar(40)  NOT NULL,
    anime_id        varchar(100),
    work_id         varchar(100),
    status          varchar(20),
    previous_status varchar(20),
    score           numeric(4,1),
    occurred_at     timestamptz NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_activities_actor ON activities (actor_id, occurred_at DESC);

-- Fan-out: one row per (recipient, activity). actor_id is denormalised so an
-- unfollow can delete a recipient's rows for that actor without a join.
CREATE TABLE IF NOT EXISTS feed_items (
    user_id     varchar(100) NOT NULL,
    activity_id uuid NOT NULL REFERENCES activities(id) ON DELETE CASCADE,
    actor_id    varchar(100) NOT NULL,
    occurred_at timestamptz NOT NULL,
    PRIMARY KEY (user_id, activity_id)
);
CREATE INDEX IF NOT EXISTS idx_feed_items_user_time  ON feed_items (user_id, occurred_at DESC, activity_id);
CREATE INDEX IF NOT EXISTS idx_feed_items_user_actor ON feed_items (user_id, actor_id);

-- The inbox. event_id is unique per recipient rather than globally, because
-- one event can legitimately notify two people (follow_accepted tells the
-- follower, and new_follower tells the followee).
CREATE TABLE IF NOT EXISTS notifications (
    id          uuid PRIMARY KEY,
    event_id    uuid NOT NULL,
    user_id     varchar(100) NOT NULL,
    type        varchar(40)  NOT NULL,
    actor_id    varchar(100),
    activity_id uuid REFERENCES activities(id) ON DELETE SET NULL,
    payload     jsonb NOT NULL DEFAULT '{}'::jsonb,
    read_at     timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, event_id)
);
CREATE INDEX IF NOT EXISTS idx_notifications_user_time   ON notifications (user_id, created_at DESC, id);
CREATE INDEX IF NOT EXISTS idx_notifications_user_unread ON notifications (user_id) WHERE read_at IS NULL;

-- Absent row = default (in_app on, push and email off). Only overrides are
-- stored, so a new type or channel needs no backfill.
CREATE TABLE IF NOT EXISTS notification_preferences (
    user_id    varchar(100) NOT NULL,
    type       varchar(40)  NOT NULL,
    channel    varchar(10)  NOT NULL,
    enabled    boolean NOT NULL DEFAULT true,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, type, channel),
    CONSTRAINT notification_preferences_channel CHECK (channel IN ('in_app', 'push', 'email'))
);

-- Devices for push. endpoint is the identity: a browser re-subscribing
-- replaces its row rather than adding one, and a device that changes hands
-- moves to the new user.
CREATE TABLE IF NOT EXISTS push_subscriptions (
    id           uuid PRIMARY KEY,
    user_id      varchar(100) NOT NULL,
    platform     varchar(10)  NOT NULL,
    endpoint     text NOT NULL UNIQUE,
    p256dh       text,
    auth         text,
    user_agent   text,
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT push_subscriptions_platform CHECK (platform IN ('web', 'ios', 'android'))
);
CREATE INDEX IF NOT EXISTS idx_push_subscriptions_user ON push_subscriptions (user_id);
