-- +goose Up
-- What looking a book up found: a record a provider has for it, how well it
-- fits, and what became of it. A match sure enough is applied by itself; the
-- rest wait as pending for a person to take or dismiss.
CREATE TABLE metadata_matches (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    book_id    uuid NOT NULL REFERENCES books (id) ON DELETE CASCADE,
    provider   text NOT NULL,
    -- What the provider calls the book.
    record_id  text NOT NULL,
    score      double precision NOT NULL CHECK (score BETWEEN 0 AND 1),
    -- The record as the provider gave it, for the review to show.
    record     jsonb NOT NULL,
    state      text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending', 'applied', 'dismissed')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (book_id, provider, record_id)
);

CREATE INDEX metadata_matches_pending_idx ON metadata_matches (created_at) WHERE state = 'pending';

-- +goose Down
DROP TABLE metadata_matches;
