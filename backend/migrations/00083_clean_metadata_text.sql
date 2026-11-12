-- +goose Up
-- The title without what a shop added to its end, as cleanup.CleanTitle
-- takes it off: again while something goes, and never to nothing. A test
-- holds the two to one answer.
-- +goose StatementBegin
CREATE FUNCTION title_without_addition(title text) RETURNS text
LANGUAGE plpgsql IMMUTABLE PARALLEL SAFE AS $$
DECLARE
    pattern CONSTANT text := '\s*(\((german|english|kindle|french|spanish|italian|deutsche|englische)\s+(edition|ausgabe)\)|(:|\s[-–])\s*((ein|historischer|psycho|fantasy|science[- ]fiction)[- ]?)?(roman|kriminalroman|krimi|thriller|psychothriller|liebesroman|fantasyroman|erzählung|erzählungen|novelle|novel|a novel))\s*$';
    cur  text := regexp_replace(title, '^\s+|\s+$', '', 'g');
    nxt  text;
BEGIN
    LOOP
        nxt := regexp_replace(regexp_replace(cur, pattern, '', 'ig'), '^\s+|\s+$', '', 'g');
        IF nxt = cur OR nxt = '' THEN
            RETURN cur;
        END IF;
        cur := nxt;
    END LOOP;
END;
$$;
-- +goose StatementEnd

-- The keys of what tools write when they have no author: cleanup's
-- placeholderAuthors, which a test holds this list to.
-- +goose StatementBegin
CREATE FUNCTION placeholder_author_keys() RETURNS text[]
LANGUAGE sql IMMUTABLE PARALLEL SAFE
RETURN ARRAY['authors sort', 'author sort', 'sort author', 'unknown', 'unknown author',
    'unbekannt', 'unbekannter autor', 'author', 'autor', 'various', 'verschiedene',
    'n a', 'none', 'null'];
-- +goose StatementEnd

-- What a book's metadata vector is made from (#184): the title without a
-- shop's additions, and no placeholder author. The text is hashed into the
-- vector's source, so every metadata vector is made again; content vectors
-- have another source and stay.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION book_metadata_text(book uuid, with_tags boolean DEFAULT true) RETURNS text
LANGUAGE sql STABLE AS $$
    SELECT concat_ws(E'\n',
        title_without_addition(b.title) || coalesce(': ' || nullif(btrim(b.subtitle), ''), ''),
        (SELECT string_agg(a.name, ', ' ORDER BY c.position)
           FROM book_contributors c JOIN authors a ON a.id = c.author_id
          WHERE c.book_id = b.id AND c.role = 'author'
            AND NOT (a.name_key = ANY (placeholder_author_keys()))),
        (SELECT s.name FROM series s WHERE s.id = b.series_id),
        (SELECT string_agg(t.name, ', ' ORDER BY t.name_key)
           FROM book_tags bt JOIN tags t ON t.id = bt.tag_id
          WHERE bt.book_id = b.id AND with_tags),
        nullif(btrim(b.description), ''))
    FROM books b WHERE b.id = book
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION book_metadata_text(book uuid, with_tags boolean DEFAULT true) RETURNS text
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
DROP FUNCTION placeholder_author_keys();
DROP FUNCTION title_without_addition(text);
