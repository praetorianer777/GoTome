-- +goose Up
-- The catalogue. A book is the title a person shelves, rates and reads; a
-- book file is one file on disk. A book has any number of files, one per
-- format, or none at all when it is only wished for. Another edition or a
-- translation is another book, linked through book_relations.

-- The *_key columns hold a name as it is compared: lower case, without
-- accents or punctuation. Two spellings of one author meet in one row.
CREATE TABLE authors (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    name       text NOT NULL CHECK (btrim(name) <> ''),
    -- "Austen, Jane": what lists are ordered by.
    sort_name  text NOT NULL,
    name_key   text NOT NULL UNIQUE CHECK (name_key <> ''),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE series (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    name       text NOT NULL CHECK (btrim(name) <> ''),
    name_key   text NOT NULL UNIQUE CHECK (name_key <> ''),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE publishers (
    id       uuid PRIMARY KEY DEFAULT uuidv7(),
    name     text NOT NULL CHECK (btrim(name) <> ''),
    name_key text NOT NULL UNIQUE CHECK (name_key <> '')
);

CREATE TABLE tags (
    id       uuid PRIMARY KEY DEFAULT uuidv7(),
    name     text NOT NULL CHECK (btrim(name) <> ''),
    name_key text NOT NULL UNIQUE CHECK (name_key <> '')
);

CREATE TABLE books (
    id                  uuid PRIMARY KEY DEFAULT uuidv7(),
    library_id          uuid NOT NULL REFERENCES libraries (id) ON DELETE CASCADE,
    title               text NOT NULL CHECK (btrim(title) <> ''),
    -- "Hobbit, The": what lists are ordered by.
    sort_title          text NOT NULL,
    -- The title as it is compared when looking for duplicates.
    title_key           text NOT NULL,
    subtitle            text,
    description         text,
    -- A BCP 47 tag as the file or a provider gave it: "en", "de-AT".
    language            text,
    -- Many books only say the year; the precision says how much of the date
    -- is known, so that 2010 is not shown as 1 January 2010.
    published_on        date,
    published_precision text CHECK (published_precision IN ('year', 'month', 'day')),
    publisher_id        uuid REFERENCES publishers (id) ON DELETE SET NULL,
    series_id           uuid REFERENCES series (id) ON DELETE SET NULL,
    -- 1, 2, and 2.5 for the novella between them.
    series_index        double precision CHECK (series_index >= 0),
    -- What a metadata provider says others think of it; a user's own rating
    -- lives with the user.
    external_rating     double precision CHECK (external_rating BETWEEN 0 AND 5),
    page_count          integer CHECK (page_count > 0),
    cover_key           text,
    -- Fields a person edited by hand, which automatic updates leave alone.
    locked_fields       text[] NOT NULL DEFAULT '{}',
    -- Where each field's value came from: the file, a provider, a person.
    field_sources       jsonb NOT NULL DEFAULT '{}',
    -- Set on the book that lost a merge, so old links find the survivor.
    merged_into_id      uuid REFERENCES books (id) ON DELETE SET NULL,
    deleted_at          timestamptz,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    CHECK ((published_on IS NULL) = (published_precision IS NULL)),
    -- book_files points at (id, library_id), which keeps a file in the
    -- library of its book.
    UNIQUE (id, library_id)
);

CREATE INDEX books_library_sort_idx ON books (library_id, sort_title, id) WHERE deleted_at IS NULL;
CREATE INDEX books_title_key_idx ON books (title_key);
CREATE INDEX books_series_idx ON books (series_id, series_index) WHERE series_id IS NOT NULL;
CREATE INDEX books_publisher_idx ON books (publisher_id) WHERE publisher_id IS NOT NULL;

CREATE TABLE book_files (
    id               uuid PRIMARY KEY DEFAULT uuidv7(),
    book_id          uuid NOT NULL,
    library_id       uuid NOT NULL REFERENCES libraries (id) ON DELETE CASCADE,
    kind             text NOT NULL CHECK (kind IN ('ebook', 'audio')),
    -- The format in lower case: epub, pdf, mobi, azw3, m4b, mp3, flac, ogg.
    format           text NOT NULL CHECK (format = lower(format) AND format <> ''),
    -- Relative to the library's folder, with forward slashes.
    rel_path         text NOT NULL CHECK (rel_path <> '' AND left(rel_path, 1) <> '/'),
    size_bytes       bigint NOT NULL CHECK (size_bytes >= 0),
    modified_at      timestamptz NOT NULL,
    -- Of the file as it is now; NULL until it has been hashed.
    sha256           bytea CHECK (octet_length(sha256) = 32),
    -- Of the file as it arrived, before GOtome wrote metadata into it.
    original_sha256  bytea CHECK (octet_length(original_sha256) = 32),
    -- Of the content without the metadata, so that two files that differ
    -- only in their metadata are still found to be the same book.
    content_sha256   bytea CHECK (octet_length(content_sha256) = 32),
    -- The position of one file among the parts of an audiobook.
    part_index       integer CHECK (part_index >= 0),
    duration_ms      bigint CHECK (duration_ms >= 0),
    page_count       integer CHECK (page_count > 0),
    -- An EPUB has no pages; its count is worked out from the text.
    pages_estimated  boolean NOT NULL DEFAULT false,
    -- NULL until extraction has looked; false for a scanned PDF.
    has_text         boolean,
    drm              boolean NOT NULL DEFAULT false,
    extract_state    text NOT NULL DEFAULT 'pending'
                     CHECK (extract_state IN ('pending', 'done', 'failed', 'skipped')),
    extract_error    text,
    uploaded_by      uuid REFERENCES users (id) ON DELETE SET NULL,
    -- Set when a scan no longer finds the file; GOtome never deletes it.
    missing_at       timestamptz,
    trashed_at       timestamptz,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (book_id, library_id) REFERENCES books (id, library_id) ON DELETE CASCADE ON UPDATE CASCADE,
    UNIQUE (library_id, rel_path)
);

CREATE INDEX book_files_book_idx ON book_files (book_id);
CREATE INDEX book_files_sha256_idx ON book_files (sha256) WHERE sha256 IS NOT NULL;
CREATE INDEX book_files_content_sha256_idx ON book_files (content_sha256) WHERE content_sha256 IS NOT NULL;

-- The file search, overlap detection and similar-book suggestions read.
ALTER TABLE books
    ADD COLUMN primary_text_file_id uuid REFERENCES book_files (id) ON DELETE SET NULL;

CREATE TABLE book_contributors (
    book_id   uuid NOT NULL REFERENCES books (id) ON DELETE CASCADE,
    author_id uuid NOT NULL REFERENCES authors (id) ON DELETE RESTRICT,
    role      text NOT NULL CHECK (role IN ('author', 'narrator', 'translator', 'editor', 'illustrator')),
    -- The order the names are credited in.
    position  integer NOT NULL CHECK (position >= 0),
    PRIMARY KEY (book_id, author_id, role)
);

CREATE INDEX book_contributors_author_idx ON book_contributors (author_id);

CREATE TABLE book_tags (
    book_id uuid NOT NULL REFERENCES books (id) ON DELETE CASCADE,
    tag_id  uuid NOT NULL REFERENCES tags (id) ON DELETE CASCADE,
    PRIMARY KEY (book_id, tag_id)
);

CREATE INDEX book_tags_tag_idx ON book_tags (tag_id);

-- An identifier belongs to the book, or to one of its files: the EPUB and the
-- audiobook of one book carry different ISBNs.
CREATE TABLE book_identifiers (
    id      uuid PRIMARY KEY DEFAULT uuidv7(),
    book_id uuid NOT NULL REFERENCES books (id) ON DELETE CASCADE,
    file_id uuid REFERENCES book_files (id) ON DELETE CASCADE,
    type    text NOT NULL CHECK (type IN ('isbn', 'asin', 'doi', 'uuid', 'openlibrary', 'google', 'hardcover', 'other')),
    -- As it is compared: an ISBN as 13 digits, a DOI in lower case.
    value   text NOT NULL CHECK (value <> '')
);

CREATE UNIQUE INDEX book_identifiers_key ON book_identifiers (book_id, type, value, file_id) NULLS NOT DISTINCT;
CREATE INDEX book_identifiers_lookup_idx ON book_identifiers (type, value);

-- Two books that are one work in another form. Stored once per pair, smaller
-- ID first.
CREATE TABLE book_relations (
    book_a uuid NOT NULL REFERENCES books (id) ON DELETE CASCADE,
    book_b uuid NOT NULL REFERENCES books (id) ON DELETE CASCADE,
    kind   text NOT NULL CHECK (kind IN ('edition', 'translation', 'related')),
    PRIMARY KEY (book_a, book_b),
    CHECK (book_a < book_b)
);

CREATE INDEX book_relations_b_idx ON book_relations (book_b);

-- +goose Down
DROP TABLE book_relations;
DROP TABLE book_identifiers;
DROP TABLE book_tags;
DROP TABLE book_contributors;
ALTER TABLE books DROP COLUMN primary_text_file_id;
DROP TABLE book_files;
DROP TABLE books;
DROP TABLE tags;
DROP TABLE publishers;
DROP TABLE series;
DROP TABLE authors;
