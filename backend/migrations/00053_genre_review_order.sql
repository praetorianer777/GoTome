-- +goose Up
-- The review queue of every genre, the surest first (#324): without it
-- each page sorts every waiting suggestion and reads every book to do so.
CREATE INDEX book_genres_waiting_order ON book_genres (score DESC, book_id DESC, genre DESC)
    WHERE source = 'automatic' AND state = 'suggested';

-- +goose Down
DROP INDEX book_genres_waiting_order;
