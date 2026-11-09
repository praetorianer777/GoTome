-- +goose Up
-- A library may join an ebook and its audiobook into one book by itself.
ALTER TABLE libraries ADD COLUMN join_formats boolean NOT NULL DEFAULT false;

-- The same book in another form, in another library: linked, as the files
-- of a book cannot leave their library's folder.
ALTER TABLE book_relations DROP CONSTRAINT book_relations_kind_check;
ALTER TABLE book_relations ADD CONSTRAINT book_relations_kind_check
    CHECK (kind IN ('edition', 'translation', 'related', 'recording', 'format'));

-- What an automatic join moved, to take it back: the files of the book
-- merged away, which a split gives it again.
CREATE TABLE book_joins (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    into_book  uuid NOT NULL REFERENCES books (id) ON DELETE CASCADE,
    from_book  uuid NOT NULL REFERENCES books (id) ON DELETE CASCADE,
    file_ids   uuid[] NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    undone_at  timestamptz
);

CREATE INDEX book_joins_into_idx ON book_joins (into_book) WHERE undone_at IS NULL;

-- Books whose files are all of one kind, which is what makes two of them an
-- ebook and its audiobook.
CREATE FUNCTION book_has_only(book uuid, file_kind text) RETURNS boolean
LANGUAGE sql STABLE PARALLEL SAFE AS $$
    SELECT EXISTS (SELECT 1 FROM book_files f WHERE f.book_id = book AND f.kind = file_kind AND f.trashed_at IS NULL)
       AND NOT EXISTS (SELECT 1 FROM book_files f WHERE f.book_id = book AND f.kind <> file_kind AND f.trashed_at IS NULL)
$$;

-- +goose Down
DROP FUNCTION book_has_only(uuid, text);
DROP TABLE book_joins;
DELETE FROM book_relations WHERE kind = 'format';
ALTER TABLE book_relations DROP CONSTRAINT book_relations_kind_check;
ALTER TABLE book_relations ADD CONSTRAINT book_relations_kind_check
    CHECK (kind IN ('edition', 'translation', 'related', 'recording'));
ALTER TABLE libraries DROP COLUMN join_formats;
