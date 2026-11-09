-- +goose Up
-- The library that takes the placeholders of books a person read but the
-- library does not hold, when nothing else decides (#391).
ALTER TABLE users ADD COLUMN history_library_id uuid REFERENCES libraries (id) ON DELETE SET NULL;

-- A person's private note or review of a book. Nobody else ever sees it.
CREATE TABLE book_notes (
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    book_id    uuid NOT NULL REFERENCES books (id) ON DELETE CASCADE,
    body       text NOT NULL CHECK (btrim(body) <> ''),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, book_id)
);

CREATE INDEX book_notes_book_idx ON book_notes (book_id);

-- An import of a reading history from another service. The file itself is
-- not kept: its rows are, until the import is done, as the job works
-- through them one at a time.
CREATE TABLE reading_imports (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    user_id     uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    source      text NOT NULL CHECK (source IN ('goodreads', 'storygraph')),
    library_id  uuid NOT NULL REFERENCES libraries (id) ON DELETE CASCADE,
    sees_all    boolean NOT NULL,
    -- Whether what the person already set on a book is replaced.
    overwrite   boolean NOT NULL DEFAULT false,
    created_at  timestamptz NOT NULL DEFAULT now(),
    started_at  timestamptz,
    finished_at timestamptz
);

CREATE INDEX reading_imports_user_idx ON reading_imports (user_id, created_at DESC);

CREATE TABLE reading_import_rows (
    import_id  uuid NOT NULL REFERENCES reading_imports (id) ON DELETE CASCADE,
    row_no     int NOT NULL,
    title      text NOT NULL,
    authors    text[] NOT NULL DEFAULT '{}',
    isbns      text[] NOT NULL DEFAULT '{}',
    status     text CHECK (status IN ('reading', 'completed', 'abandoned', 'wishlist')),
    rating     smallint CHECK (rating BETWEEN 1 AND 5),
    rounded    boolean NOT NULL DEFAULT false,
    read_on    date,
    shelves    text[] NOT NULL DEFAULT '{}',
    review     text NOT NULL DEFAULT '',
    -- What the preview found: book (one), new (none), ambiguous (several)
    -- or skipped (nothing to import, the reason in message).
    match      text NOT NULL CHECK (match IN ('book', 'new', 'ambiguous', 'skipped')),
    book_id    uuid REFERENCES books (id) ON DELETE SET NULL,
    candidates uuid[] NOT NULL DEFAULT '{}',
    -- The person's answer for an ambiguous row: skip it, or take book_id.
    skip       boolean NOT NULL DEFAULT false,
    -- Empty until the row's turn has come: created, changed, unchanged,
    -- skipped or failed.
    outcome    text,
    message    text NOT NULL DEFAULT '',
    PRIMARY KEY (import_id, row_no)
);

-- +goose Down
DROP TABLE reading_import_rows;
DROP TABLE reading_imports;
DROP TABLE book_notes;
ALTER TABLE users DROP COLUMN history_library_id;
