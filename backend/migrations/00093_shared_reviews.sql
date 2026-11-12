-- +goose Up
-- Whether the person shares their ratings and reviews with others on the
-- server (#427). Off until they choose.
ALTER TABLE users ADD COLUMN share_reviews boolean NOT NULL DEFAULT false;

-- A person's review of a book, apart from their private note. It is seen by
-- others only while the person shares and the review is not private.
CREATE TABLE book_reviews (
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    book_id    uuid NOT NULL REFERENCES books (id) ON DELETE CASCADE,
    body       text NOT NULL CHECK (btrim(body) <> ''),
    private    boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, book_id)
);

CREATE INDEX book_reviews_book_idx ON book_reviews (book_id);

-- +goose Down
DROP TABLE book_reviews;
ALTER TABLE users DROP COLUMN share_reviews;
