-- +goose Up
-- Sending a book to a person's Kindle by e-mail (#412): the address Amazon
-- gave them, and one row per send with its outcome.
ALTER TABLE users ADD COLUMN kindle_address text
    CHECK (kindle_address IS NULL OR (kindle_address = lower(kindle_address) AND kindle_address <> ''));

CREATE TABLE kindle_sends (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    user_id     uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    book_id     uuid NOT NULL REFERENCES books (id) ON DELETE CASCADE,
    -- The EPUB that goes. Not a foreign key: the file may be trashed or its
    -- library deleted, and a send must not hold that up.
    file_id     uuid NOT NULL,
    -- The title as it was, for the notification when the book is gone.
    title       text NOT NULL,
    -- The address it goes to, as it was when it was asked for.
    address     text NOT NULL,
    -- sending is set by the job before the mail is handed over, and a job
    -- that finds it again does not send: it cannot know whether the mail
    -- left, and sending twice is worse than asking the person to send again.
    state       text NOT NULL DEFAULT 'queued' CHECK (state IN ('queued', 'sending', 'sent', 'failed')),
    message     text NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now(),
    finished_at timestamptz
);

CREATE UNIQUE INDEX kindle_sends_pending_idx ON kindle_sends (user_id, book_id)
    WHERE state IN ('queued', 'sending');
CREATE INDEX kindle_sends_book_idx ON kindle_sends (book_id, user_id, created_at DESC);

-- +goose Down
DROP TABLE kindle_sends;
ALTER TABLE users DROP COLUMN kindle_address;
