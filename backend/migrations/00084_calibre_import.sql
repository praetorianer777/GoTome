-- +goose Up
-- An import of the metadata of a Calibre library (#394). metadata.db is
-- read, never kept: the import holds what the preview counted and, per book
-- of Calibre's, what the job came to, until it is stale.
CREATE TABLE calibre_imports (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    user_id     uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    library_id  uuid NOT NULL REFERENCES libraries (id) ON DELETE CASCADE,
    sees_all    boolean NOT NULL,
    -- Whether Calibre's ratings become the person's own.
    ratings     boolean NOT NULL DEFAULT false,
    -- Per custom column (by label) what to do with it: ignore, status or tags.
    columns     jsonb NOT NULL DEFAULT '{}',
    -- What the preview counted: books, formats, covers, series, tags and the
    -- custom columns.
    summary     jsonb NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    started_at  timestamptz,
    finished_at timestamptz
);

CREATE INDEX calibre_imports_user_idx ON calibre_imports (user_id, created_at DESC);

CREATE TABLE calibre_import_books (
    import_id  uuid NOT NULL REFERENCES calibre_imports (id) ON DELETE CASCADE,
    calibre_id bigint NOT NULL,
    title      text NOT NULL,
    -- How many of the book's formats the library holds, and how many it
    -- lacks (not scanned yet, or gone).
    found      int NOT NULL,
    missing    int NOT NULL,
    -- Empty until the book's turn has come: changed, unchanged, skipped or
    -- failed.
    outcome    text,
    message    text NOT NULL DEFAULT '',
    book_id    uuid REFERENCES books (id) ON DELETE SET NULL,
    PRIMARY KEY (import_id, calibre_id)
);

-- +goose Down
DROP TABLE calibre_import_books;
DROP TABLE calibre_imports;
