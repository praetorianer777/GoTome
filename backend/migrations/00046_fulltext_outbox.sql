-- +goose Up
-- The full-text index moves out of Postgres into the app (#265,
-- docs/decisions/text-store.md); while both exist, every change to the
-- chunks reaches it through this outbox, written in the change's
-- transaction and applied by the fulltext.apply_outbox job after the
-- commit, in id order. A row deletes chunks from the index, or adds them
-- with their text (deleting each first, so a row applied twice adds it
-- once); it is gone once the index has committed it. Derived, like the
-- chunks: not in backups.
CREATE TABLE fulltext_outbox (
    id      bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    deletes bigint[] NOT NULL DEFAULT '{}',
    -- [{id, book, library, lang, text}], as internal/fulltext.Doc.
    docs    jsonb NOT NULL DEFAULT '[]'
);

-- Where a chunk's text is in its file, so that a passage is read from there
-- instead of a copy: the sections it spans by what the format calls them
-- (an EPUB's documents, a PDF's page numbers; empty for a format read whole,
-- and for chunks cut before this), and where in the first one it starts and
-- in the last one it ends, in characters of their normalised text.
ALTER TABLE book_chunks
    ADD COLUMN sources text[] NOT NULL DEFAULT '{}',
    ADD COLUMN source_from integer NOT NULL DEFAULT 0,
    ADD COLUMN source_to integer NOT NULL DEFAULT 0;

-- When the file's chunks were last handed to the full-text index; NULL for
-- a primary text file whose chunks the index is still to get, which the
-- ingest.index_files job reads from the file.
ALTER TABLE book_files ADD COLUMN indexed_at timestamptz;

-- Every chunk that goes, however it goes (its book, file or library
-- deleted, its text cut again, a merge, the trash), goes from the index.
-- +goose StatementBegin
CREATE FUNCTION book_chunks_gone() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO fulltext_outbox (deletes)
    SELECT array_agg(id) FROM gone HAVING count(*) > 0;
    RETURN NULL;
END $$;
-- +goose StatementEnd

CREATE TRIGGER book_chunks_gone AFTER DELETE ON book_chunks
    REFERENCING OLD TABLE AS gone
    FOR EACH STATEMENT EXECUTE FUNCTION book_chunks_gone();

-- A chunk's library is a field of the index, which holds no text to write
-- it again from: a book that changes library has its file read again.
-- +goose StatementBegin
CREATE FUNCTION book_chunks_moved() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    UPDATE book_files SET indexed_at = NULL
    WHERE id IN (SELECT n.file_id FROM moved n JOIN was o ON o.id = n.id
                 WHERE o.library_id <> n.library_id);
    RETURN NULL;
END $$;
-- +goose StatementEnd

CREATE TRIGGER book_chunks_moved AFTER UPDATE ON book_chunks
    REFERENCING OLD TABLE AS was NEW TABLE AS moved
    FOR EACH STATEMENT EXECUTE FUNCTION book_chunks_moved();

-- +goose Down
DROP TRIGGER book_chunks_moved ON book_chunks;
DROP FUNCTION book_chunks_moved();
DROP TRIGGER book_chunks_gone ON book_chunks;
DROP FUNCTION book_chunks_gone();
ALTER TABLE book_files DROP COLUMN indexed_at;
ALTER TABLE book_chunks DROP COLUMN sources, DROP COLUMN source_from, DROP COLUMN source_to;
DROP TABLE fulltext_outbox;
