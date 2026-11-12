-- +goose Up
-- GOtome keeps no copy of a book's text (#265, docs/decisions/text-store.md):
-- full-text search runs on the index in the app, and a passage is read
-- from the file. The chunks keep what locates them and nothing of their
-- text. They move into a table of their own rather than losing columns,
-- so that the text and pg_search's index over it are freed at once, not
-- after a rewrite of the table.
CREATE TABLE book_chunks_located (
    -- The full-text index knows a chunk by this ID; it carries over.
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    book_id     uuid NOT NULL,
    library_id  uuid NOT NULL,
    file_id     uuid NOT NULL REFERENCES book_files (id) ON DELETE CASCADE,
    position    integer NOT NULL CHECK (position >= 0),
    -- The title of the chapter the chunk starts in, as the file names it.
    chapter     text NOT NULL DEFAULT '',
    -- The pages it spans: a PDF's own, or worked out from the text before it.
    page_from   integer CHECK (page_from > 0),
    page_to     integer CHECK (page_to >= page_from),
    -- Where it starts in the book's whole text, in characters.
    char_offset integer NOT NULL CHECK (char_offset >= 0),
    -- A two-letter code, or '' when the language could not be told; the
    -- full-text index stems the chunk's words by it.
    lang        text NOT NULL,
    -- Where its text is in the file: the sections it spans by what the
    -- format calls them (an EPUB's documents, a PDF's page numbers; empty
    -- for a format read whole), where in the first it starts and in the
    -- last it ends, in characters of their normalised text.
    sources     text[] NOT NULL DEFAULT '{}',
    source_from integer NOT NULL DEFAULT 0,
    source_to   integer NOT NULL DEFAULT 0,
    UNIQUE (book_id, position),
    FOREIGN KEY (book_id, library_id) REFERENCES books (id, library_id) ON DELETE CASCADE ON UPDATE CASCADE
);

INSERT INTO book_chunks_located (id, book_id, library_id, file_id, position, chapter, page_from, page_to,
                                 char_offset, lang, sources, source_from, source_to)
OVERRIDING SYSTEM VALUE
SELECT id, book_id, library_id, file_id, position, chapter, page_from, page_to,
       char_offset, lang, sources, source_from, source_to
FROM book_chunks;

SELECT setval(pg_get_serial_sequence('book_chunks_located', 'id'), coalesce(max(id), 0) + 1, false)
FROM book_chunks_located;

-- The old table takes its text, its BM25 index and its triggers with it;
-- a dropped table fires no delete trigger, so the index keeps its chunks.
DROP TABLE book_chunks;
ALTER TABLE book_chunks_located RENAME TO book_chunks;
ALTER INDEX book_chunks_located_pkey RENAME TO book_chunks_pkey;
ALTER INDEX book_chunks_located_book_id_position_key RENAME TO book_chunks_book_id_position_key;
CREATE INDEX book_chunks_file_idx ON book_chunks (file_id);

CREATE TRIGGER book_chunks_gone AFTER DELETE ON book_chunks
    REFERENCING OLD TABLE AS gone
    FOR EACH STATEMENT EXECUTE FUNCTION book_chunks_gone();

CREATE TRIGGER book_chunks_moved AFTER UPDATE ON book_chunks
    REFERENCING OLD TABLE AS was NEW TABLE AS moved
    FOR EACH STATEMENT EXECUTE FUNCTION book_chunks_moved();

-- Nothing searches in Postgres any more.
DROP EXTENSION IF EXISTS pg_search;
DELETE FROM index_versions WHERE name = 'pg_search';
DELETE FROM settings WHERE key = 'search.engine';

-- +goose Down
-- The text is gone; every file is read again to get it back.
CREATE EXTENSION IF NOT EXISTS pg_search CASCADE;
ALTER TABLE book_chunks ADD COLUMN body_en text, ADD COLUMN body_de text, ADD COLUMN body_xx text;
DELETE FROM book_chunks;
UPDATE book_files SET chunked_at = NULL;
ALTER TABLE book_chunks ADD CHECK (num_nonnulls(body_en, body_de, body_xx) = 1);
CREATE INDEX book_chunks_bm25 ON book_chunks USING bm25 (
    id,
    book_id,
    library_id,
    (body_en::pdb.simple('stemmer=english')),
    (body_de::pdb.simple('stemmer=german')),
    (body_xx::pdb.simple)
) WITH (key_field = 'id');
