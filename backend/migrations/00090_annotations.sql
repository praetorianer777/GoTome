-- +goose Up
-- A person's highlights and notes in a book (#408), private to them like
-- user_books. A client that keeps its own copy (KOReader's sidecar) names
-- each by a stable key, so that sending the set again changes nothing.
CREATE TABLE annotations (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    user_id     uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    book_id     uuid NOT NULL REFERENCES books (id) ON DELETE CASCADE,
    file_id     uuid REFERENCES book_files (id) ON DELETE SET NULL,
    -- What locator is: an EPUB CFI, a PDF page with rectangles, a KOReader xpointer.
    locator_kind text NOT NULL CHECK (locator_kind IN ('cfi', 'pdf', 'xpointer')),
    locator     text NOT NULL,
    locator_end text,
    page        integer CHECK (page IS NULL OR page > 0),
    -- A PDF's rectangles as fractions of the page: [{"x":..,"y":..,"w":..,"h":..}].
    rects       jsonb,
    passage     text NOT NULL CHECK (btrim(passage) <> ''),
    note        text,
    color       text NOT NULL DEFAULT 'yellow'
                CHECK (color IN ('yellow', 'orange', 'red', 'green', 'blue', 'purple', 'gray')),
    chapter     text,
    -- How far into the book it lies, 0 to 1, to order a book's by.
    fraction    double precision CHECK (fraction IS NULL OR fraction BETWEEN 0 AND 1),
    client_id   text NOT NULL,
    client_key  text NOT NULL,
    -- A person changed it here: a device's next send leaves its note and colour.
    edited_here boolean NOT NULL DEFAULT false,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, book_id, client_id, client_key)
);

CREATE INDEX annotations_book_idx ON annotations (book_id);
CREATE INDEX annotations_user_idx ON annotations (user_id, created_at DESC, id DESC);

-- +goose Down
DROP TABLE annotations;
