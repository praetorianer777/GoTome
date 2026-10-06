-- +goose Up
-- What a book is about, as vectors for suggesting similar books
-- (docs/decisions/embedding-runtime.md): one from passages of its text and
-- one from its title, authors, series, tags and description. Derived data:
-- made again whenever the model or what it was made from changes, which
-- source_hash and the model columns tell.
CREATE TABLE book_vectors (
    book_id       uuid NOT NULL REFERENCES books (id) ON DELETE CASCADE,
    kind          text NOT NULL CHECK (kind IN ('content', 'metadata')),
    -- "intfloat/multilingual-e5-small"
    model         text NOT NULL,
    -- The model's revision and weights: "614241f…/int8".
    model_version text NOT NULL,
    source_hash   bytea NOT NULL CHECK (octet_length(source_hash) = 32),
    embedding     vector(384) NOT NULL,
    embedded_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (book_id, kind)
);

-- What a book's metadata vector is made from, and its hash, in one place:
-- the title, the authors in their order, the series, the tags and the
-- description, a line each.
-- +goose StatementBegin
CREATE FUNCTION book_metadata_text(book uuid) RETURNS text
LANGUAGE sql STABLE AS $$
    SELECT concat_ws(E'\n',
        b.title || coalesce(': ' || nullif(btrim(b.subtitle), ''), ''),
        (SELECT string_agg(a.name, ', ' ORDER BY c.position)
           FROM book_contributors c JOIN authors a ON a.id = c.author_id
          WHERE c.book_id = b.id AND c.role = 'author'),
        (SELECT s.name FROM series s WHERE s.id = b.series_id),
        (SELECT string_agg(t.name, ', ' ORDER BY t.name_key)
           FROM book_tags bt JOIN tags t ON t.id = bt.tag_id
          WHERE bt.book_id = b.id),
        nullif(btrim(b.description), ''))
    FROM books b WHERE b.id = book
$$;
-- +goose StatementEnd

-- +goose Down
DROP FUNCTION book_metadata_text(uuid);
DROP TABLE book_vectors;
