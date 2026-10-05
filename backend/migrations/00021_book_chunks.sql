-- +goose Up
-- A book's text in pieces of about 8,000 characters, read from its primary
-- text file: what full-text search, near-duplicate detection and embeddings
-- read. Derived data: rebuilt from the file whenever it is read again.
--
-- The text is in the column of its language, each of which the search index
-- stems its own way (docs/decisions/search-engine.md): English, German, and
-- every other language as written. library_id follows the book's through the
-- foreign key, so a search can filter on it inside the index.
CREATE TABLE book_chunks (
    -- The search index needs one unique column of a plain type as its key.
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    book_id    uuid NOT NULL,
    library_id uuid NOT NULL,
    file_id    uuid NOT NULL REFERENCES book_files (id) ON DELETE CASCADE,
    position   integer NOT NULL CHECK (position >= 0),
    -- The title of the chapter the chunk starts in, as the file names it.
    chapter    text NOT NULL DEFAULT '',
    -- The pages it spans: a PDF's own, or worked out from the text before it.
    page_from  integer CHECK (page_from > 0),
    page_to    integer CHECK (page_to >= page_from),
    -- Where it starts in the book's whole text, in characters.
    char_offset integer NOT NULL CHECK (char_offset >= 0),
    -- A two-letter code, or '' when the language could not be told.
    lang       text NOT NULL,
    body_en    text,
    body_de    text,
    body_xx    text,
    CHECK (num_nonnulls(body_en, body_de, body_xx) = 1),
    UNIQUE (book_id, position),
    FOREIGN KEY (book_id, library_id) REFERENCES books (id, library_id) ON DELETE CASCADE ON UPDATE CASCADE
);

CREATE INDEX book_chunks_file_idx ON book_chunks (file_id);

-- Every word of every chunk as written, lower case: the vocabulary a typo is
-- looked up in before a search (#55). Words are added as chunks are and never
-- counted or removed, so that writing a book's chunks takes no lock another
-- book's writing waits for; a word no chunk has any more finds nothing.
CREATE TABLE search_words (
    word text PRIMARY KEY
);

CREATE INDEX search_words_trgm_idx ON search_words USING gin (word gin_trgm_ops);

-- When the file's text was last cut into chunks; NULL for a file whose text
-- is still to be chunked, which the app queues at start.
ALTER TABLE book_files ADD COLUMN chunked_at timestamptz;

-- Until now a book's text was read from the first file found to have any.
-- It is read from the best one: an EPUB, then a Kindle file, then a PDF, as
-- ingest.choosePrimary picks from here on.
UPDATE books b
SET primary_text_file_id = (
    SELECT f.id
    FROM book_files f
    WHERE f.book_id = b.id AND f.has_text AND NOT f.drm
      AND f.missing_at IS NULL AND f.trashed_at IS NULL
    ORDER BY CASE f.format WHEN 'epub' THEN 3 WHEN 'azw3' THEN 2 WHEN 'mobi' THEN 2 WHEN 'azw' THEN 2 WHEN 'pdf' THEN 1 ELSE 0 END DESC,
             f.created_at, f.id
    LIMIT 1
)
WHERE b.primary_text_file_id IS NOT NULL;

-- +goose Down
ALTER TABLE book_files DROP COLUMN chunked_at;
DROP TABLE search_words;
DROP TABLE book_chunks;
