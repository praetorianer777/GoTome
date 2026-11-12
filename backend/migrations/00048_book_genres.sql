-- +goose Up
-- Genres of a fixed list (internal/genres), apart from the tags (#175).
-- A row is the genre's from the book's tags (source 'tags') or suggested by
-- the book's vectors ('automatic'); a person accepts or rejects it, and
-- nothing automatic changes a row a person decided.
CREATE TABLE book_genres (
    book_id    uuid NOT NULL REFERENCES books (id) ON DELETE CASCADE,
    genre      text NOT NULL,
    source     text NOT NULL CHECK (source IN ('tags', 'automatic')),
    -- How an automatic one was found ('vector', 'neighbours') and its score.
    method     text,
    score      real,
    state      text NOT NULL DEFAULT 'suggested' CHECK (state IN ('suggested', 'accepted', 'rejected')),
    decided_by uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (book_id, genre)
);

-- The filter and its facet, which ask for the genres a book has: those of
-- its tags and those a person accepted (catalog.hasGenre).
CREATE INDEX book_genres_genre ON book_genres (genre, book_id)
    WHERE (state = 'accepted' OR (state = 'suggested' AND source = 'tags'));

-- The review queue: automatic suggestions nobody has decided, the best first.
CREATE INDEX book_genres_waiting ON book_genres (genre, score DESC, book_id)
    WHERE source = 'automatic' AND state = 'suggested';

-- Which books a pass has looked at, and how: a book whose recipe differs,
-- or that changed since, is looked at again. Derived, not backed up.
CREATE TABLE book_genre_checks (
    book_id    uuid PRIMARY KEY REFERENCES books (id) ON DELETE CASCADE,
    recipe     text NOT NULL,
    checked_at timestamptz NOT NULL DEFAULT now()
);

-- What a book's metadata vector is made from, as in 00029, and the same
-- without the tags: genre-check embeds a tagged book so, to ask what its
-- vector says of its genre without the tags that tell it.
DROP FUNCTION book_metadata_text(uuid);
-- +goose StatementBegin
CREATE FUNCTION book_metadata_text(book uuid, with_tags boolean DEFAULT true) RETURNS text
LANGUAGE sql STABLE AS $$
    SELECT concat_ws(E'\n',
        b.title || coalesce(': ' || nullif(btrim(b.subtitle), ''), ''),
        (SELECT string_agg(a.name, ', ' ORDER BY c.position)
           FROM book_contributors c JOIN authors a ON a.id = c.author_id
          WHERE c.book_id = b.id AND c.role = 'author'),
        (SELECT s.name FROM series s WHERE s.id = b.series_id),
        (SELECT string_agg(t.name, ', ' ORDER BY t.name_key)
           FROM book_tags bt JOIN tags t ON t.id = bt.tag_id
          WHERE bt.book_id = b.id AND with_tags),
        nullif(btrim(b.description), ''))
    FROM books b WHERE b.id = book
$$;
-- +goose StatementEnd

-- +goose Down
DROP FUNCTION book_metadata_text(uuid, boolean);
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
DROP TABLE book_genre_checks;
DROP TABLE book_genres;
