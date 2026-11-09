-- +goose Up
-- The audiobooks a person has lined up to play one after another (#420),
-- in the order they chose, so that the queue follows them to other browsers.
CREATE TABLE play_queue (
    user_id  uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    book_id  uuid NOT NULL REFERENCES books (id) ON DELETE CASCADE,
    position integer NOT NULL,
    PRIMARY KEY (user_id, book_id)
);

CREATE INDEX play_queue_book_idx ON play_queue (book_id);

-- +goose Down
DROP TABLE play_queue;
