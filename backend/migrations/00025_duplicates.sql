-- +goose Up
-- Two books that look like one: stored once, smaller ID first. Its state is
-- what a person decided about it; detection only adds pairs and evidence,
-- and takes back what no longer holds while a pair is still open.
CREATE TABLE duplicate_pairs (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    book_a     uuid NOT NULL REFERENCES books (id) ON DELETE CASCADE,
    book_b     uuid NOT NULL REFERENCES books (id) ON DELETE CASCADE,
    state      text NOT NULL DEFAULT 'open'
               CHECK (state IN ('open', 'kept_both', 'merged', 'replaced')),
    found_at   timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (book_a < book_b),
    UNIQUE (book_a, book_b)
);

CREATE INDEX duplicate_pairs_b_idx ON duplicate_pairs (book_b);

-- Why two books look like one. Detail says what they share: the hash, the
-- ISBN, or the title and author as compared.
CREATE TABLE duplicate_evidence (
    pair_id uuid NOT NULL REFERENCES duplicate_pairs (id) ON DELETE CASCADE,
    kind    text NOT NULL CHECK (kind IN ('sha256', 'content', 'isbn', 'title_author')),
    detail  text NOT NULL,
    PRIMARY KEY (pair_id, kind, detail)
);

-- +goose Down
DROP TABLE duplicate_evidence;
DROP TABLE duplicate_pairs;
