-- +goose Up
-- Notifications about what happens in the libraries (#71). An event is
-- queued per person who is to hear of it and folded, once events stop
-- arriving, into one notification per person, kind and library, so that a
-- large import says so once.

-- A notification about a library, seen only while the library may be.
ALTER TABLE notifications ADD COLUMN library_id uuid REFERENCES libraries (id) ON DELETE CASCADE;

-- What a person chose to hear of, per kind and channel. No row is the
-- kind's default. The web app is the only channel so far.
CREATE TABLE notification_subscriptions (
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    kind    text NOT NULL,
    channel text NOT NULL CHECK (channel IN ('app')),
    enabled boolean NOT NULL,
    PRIMARY KEY (user_id, kind, channel)
);

CREATE TABLE notification_events (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    kind       text NOT NULL,
    library_id uuid REFERENCES libraries (id) ON DELETE CASCADE,
    book_id    uuid REFERENCES books (id) ON DELETE CASCADE,
    data       jsonb NOT NULL DEFAULT '{}',
    link       text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX notification_events_created_idx ON notification_events (created_at);

-- +goose Down
DROP TABLE notification_events;
DROP TABLE notification_subscriptions;
ALTER TABLE notifications DROP COLUMN library_id;
