-- +goose Up
-- A change a person asked for on many books at once, and what it came to for
-- each. The books are fixed when it is asked for: a book that matches the
-- filter later is not changed.
CREATE TABLE bulk_changes (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    created_by  uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    -- Whether who asked saw every library then; the books are changed as
    -- they may.
    sees_all    boolean NOT NULL,
    action      text NOT NULL CHECK (action IN ('edit', 'fetch', 'writeBack')),
    -- The edit, as catalog.Change; empty for the other actions.
    change      jsonb NOT NULL DEFAULT '{}',
    created_at  timestamptz NOT NULL DEFAULT now(),
    finished_at timestamptz
);

CREATE INDEX bulk_changes_created_by_idx ON bulk_changes (created_by, created_at);

CREATE TABLE bulk_change_books (
    bulk_change_id uuid NOT NULL REFERENCES bulk_changes (id) ON DELETE CASCADE,
    position       integer NOT NULL,
    book_id        uuid NOT NULL REFERENCES books (id) ON DELETE CASCADE,
    -- NULL until the book's turn has come.
    outcome        text CHECK (outcome IN ('changed', 'unchanged', 'locked', 'review', 'notFound', 'failed')),
    message        text,
    -- The fields left as they were because they are locked.
    skipped        text[] NOT NULL DEFAULT '{}',
    PRIMARY KEY (bulk_change_id, position),
    UNIQUE (bulk_change_id, book_id)
);

-- +goose Down
DROP TABLE bulk_change_books;
DROP TABLE bulk_changes;
