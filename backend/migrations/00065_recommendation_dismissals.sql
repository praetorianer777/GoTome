-- +goose Up
-- A book a person said they are not interested in (#392): it is left out of
-- their recommendations from then on. Theirs alone.
CREATE TABLE recommendation_dismissals (
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    book_id    uuid NOT NULL REFERENCES books (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, book_id)
);

CREATE INDEX recommendation_dismissals_book_idx ON recommendation_dismissals (book_id);

-- +goose Down
DROP TABLE recommendation_dismissals;
