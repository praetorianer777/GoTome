-- +goose Up
-- The full-text index over the chunks (docs/decisions/search-engine.md): each
-- language's column with its stemmer, every other language as written, and
-- the book and the library as fields a search filters on inside the index,
-- before the best chunks are taken.
-- pg_search needs pgvector, which the image also carries.
CREATE EXTENSION IF NOT EXISTS pg_search CASCADE;

CREATE INDEX book_chunks_bm25 ON book_chunks USING bm25 (
    id,
    book_id,
    library_id,
    (body_en::pdb.simple('stemmer=english')),
    (body_de::pdb.simple('stemmer=german')),
    (body_xx::pdb.simple)
) WITH (key_field = 'id');

-- +goose Down
DROP INDEX book_chunks_bm25;
