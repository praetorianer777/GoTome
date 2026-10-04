-- +goose Up
-- What happened that a person should hear of: a kind and the values its
-- text is made from, which the web app words; where to go to see more; and
-- the book it is about, if any, through which it is seen only while the
-- book's library may be seen.
CREATE TABLE notifications (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    kind       text NOT NULL,
    data       jsonb NOT NULL DEFAULT '{}',
    link       text NOT NULL DEFAULT '',
    book_id    uuid REFERENCES books (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    read_at    timestamptz
);

CREATE INDEX notifications_user_idx ON notifications (user_id, created_at DESC, id DESC);
CREATE INDEX notifications_unread_idx ON notifications (user_id) WHERE read_at IS NULL;

-- +goose Down
DROP TABLE notifications;
