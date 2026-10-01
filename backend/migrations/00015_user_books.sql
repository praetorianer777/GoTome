-- +goose Up
-- Where each person stands with a book: its status for them, their rating,
-- when they began and finished it. A book without a row is unread and not
-- rated by them. What a provider's readers think is not kept here.
CREATE TABLE user_books (
    user_id     uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    book_id     uuid NOT NULL REFERENCES books (id) ON DELETE CASCADE,
    status      text NOT NULL DEFAULT 'unread'
                CHECK (status IN ('unread', 'reading', 'completed', 'abandoned', 'wishlist')),
    -- Whole stars, 1 to 5.
    rating      smallint CHECK (rating BETWEEN 1 AND 5),
    started_on  date,
    finished_on date,
    updated_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, book_id)
);

CREATE INDEX user_books_book_idx ON user_books (book_id);

-- +goose Down
DROP TABLE user_books;
