-- +goose Up
-- One author under several spellings: "Goldstein, Barbara", "Barbara
-- Goldstein" and "Goldstein Barbara" have the words of their key in common,
-- only in another order. person_key is those words sorted; whatever matches
-- an author to another (duplicates, placeholders) compares it, and the
-- clean-up page offers to make the spellings one.
-- +goose StatementBegin
CREATE FUNCTION person_key(name_key text) RETURNS text
LANGUAGE sql IMMUTABLE PARALLEL SAFE
RETURN array_to_string(ARRAY(
    SELECT w FROM unnest(string_to_array(name_key, ' ')) AS w ORDER BY w COLLATE "C"
), ' ');
-- +goose StatementEnd

ALTER TABLE authors ADD COLUMN person_key text GENERATED ALWAYS AS (person_key(name_key)) STORED;
CREATE INDEX authors_person_key_idx ON authors (person_key);

-- What the clean-up page found about the catalogue, one function per kind,
-- each for the books a viewer sees: a subject a suggestion is about, a
-- value that changes when what was found does (a dismissal holds for that
-- value only), the books it touches, and the names it found with the books
-- of each, the one suggested first.

-- Authors whose spellings are one person.
-- +goose StatementBegin
CREATE FUNCTION cleanup_author_spellings(viewer uuid, sees_all boolean)
RETURNS TABLE (subject text, value text, books integer, names text[], counts integer[])
LANGUAGE sql STABLE
BEGIN ATOMIC
    SELECT a.person_key,
           string_agg(a.name_key, '|' ORDER BY a.name_key),
           sum(u.books)::integer,
           array_agg(a.name ORDER BY u.books DESC, a.name LIKE '%,%' DESC, a.name),
           array_agg(u.books ORDER BY u.books DESC, a.name LIKE '%,%' DESC, a.name)
    FROM (SELECT c.author_id, count(DISTINCT c.book_id)::integer AS books
          FROM book_contributors c
          JOIN books b ON b.id = c.book_id
          WHERE c.role = 'author' AND b.deleted_at IS NULL
            AND b.library_id IN (SELECT * FROM visible_library_ids(viewer, sees_all))
          GROUP BY c.author_id) u
    JOIN authors a ON a.id = u.author_id
    GROUP BY a.person_key
    HAVING count(*) > 1;
END;
-- +goose StatementEnd

-- Authors that are a value a tool left in place of a name.
-- +goose StatementBegin
CREATE FUNCTION cleanup_placeholder_authors(viewer uuid, sees_all boolean, name_keys text[])
RETURNS TABLE (subject text, value text, books integer, names text[], counts integer[])
LANGUAGE sql STABLE
BEGIN ATOMIC
    SELECT a.name_key, a.name_key, count(DISTINCT c.book_id)::integer,
           ARRAY[a.name], ARRAY[count(DISTINCT c.book_id)::integer]
    FROM authors a
    JOIN book_contributors c ON c.author_id = a.id AND c.role = 'author'
    JOIN books b ON b.id = c.book_id
    WHERE a.name_key = ANY(name_keys) AND b.deleted_at IS NULL
      AND b.library_id IN (SELECT * FROM visible_library_ids(viewer, sees_all))
    GROUP BY a.id;
END;
-- +goose StatementEnd

-- An author whose books are in a series of the same name. Where every one
-- of the author's books is, the author is the series ("Jerry Cotton") and
-- the value is removeAuthor; where only some are, the series is the author
-- ("Karen Rose") and the value is clearSeries. The books are those of both.
-- +goose StatementBegin
CREATE FUNCTION cleanup_name_clashes(viewer uuid, sees_all boolean)
RETURNS TABLE (subject text, value text, books integer, names text[], counts integer[])
LANGUAGE sql STABLE
BEGIN ATOMIC
    SELECT a.name_key,
           CASE WHEN bool_and(COALESCE(s.name_key = a.name_key, false)) THEN 'removeAuthor' ELSE 'clearSeries' END,
           count(*) FILTER (WHERE s.name_key = a.name_key)::integer,
           ARRAY[a.name], ARRAY[count(*)::integer]
    FROM authors a
    JOIN book_contributors c ON c.author_id = a.id AND c.role = 'author'
    JOIN books b ON b.id = c.book_id
    LEFT JOIN series s ON s.id = b.series_id
    WHERE b.deleted_at IS NULL
      AND b.library_id IN (SELECT * FROM visible_library_ids(viewer, sees_all))
    GROUP BY a.id
    HAVING bool_or(s.name_key = a.name_key);
END;
-- +goose StatementEnd

-- Whether a title ends in what a shop added to it: the edition it sold or
-- the genre after a colon or a dash. It is cleanup.TitleAdditions, which
-- takes them off; a test holds the two to one answer. Matching it takes
-- long enough that 50,000 titles are matched as they are written, into
-- books.title_addition, not each time the clean-up page asks.
-- +goose StatementBegin
CREATE FUNCTION title_has_addition(title text) RETURNS boolean
LANGUAGE sql IMMUTABLE PARALLEL SAFE
RETURN title ~* '\s*(\((german|english|kindle|french|spanish|italian|deutsche|englische)\s+(edition|ausgabe)\)|(:|\s[-–])\s*((ein|historischer|psycho|fantasy|science[- ]fiction)[- ]?)?(roman|kriminalroman|krimi|thriller|psychothriller|liebesroman|fantasyroman|erzählung|erzählungen|novelle|novel|a novel))\s*$';
-- +goose StatementEnd

ALTER TABLE books ADD COLUMN title_addition boolean NOT NULL GENERATED ALWAYS AS (title_has_addition(title)) STORED;

-- Books whose title ends in what a shop added to it.
-- +goose StatementBegin
CREATE FUNCTION cleanup_titles(viewer uuid, sees_all boolean)
RETURNS TABLE (subject text, value text, books integer, names text[], counts integer[])
LANGUAGE sql STABLE
BEGIN ATOMIC
    SELECT b.id::text, b.title, 1, ARRAY[b.title], ARRAY[1]
    FROM books b
    WHERE b.deleted_at IS NULL AND b.title_addition
      AND b.library_id IN (SELECT * FROM visible_library_ids(viewer, sees_all));
END;
-- +goose StatementEnd

-- A suggestion a person said to leave: it stays away while what was found
-- is the value it was dismissed for.
CREATE TABLE cleanup_dismissals (
    kind         text NOT NULL,
    subject      text NOT NULL,
    value        text NOT NULL,
    dismissed_by uuid REFERENCES users (id) ON DELETE SET NULL,
    dismissed_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (kind, subject)
);

-- A bulk change made of suggestions changes each book its own way.
ALTER TABLE bulk_change_books ADD COLUMN change jsonb;

-- +goose Down
ALTER TABLE bulk_change_books DROP COLUMN change;
DROP TABLE cleanup_dismissals;
DROP FUNCTION cleanup_titles;
ALTER TABLE books DROP COLUMN title_addition;
DROP FUNCTION title_has_addition;
DROP FUNCTION cleanup_name_clashes;
DROP FUNCTION cleanup_placeholder_authors;
DROP FUNCTION cleanup_author_spellings;
DROP INDEX authors_person_key_idx;
ALTER TABLE authors DROP COLUMN person_key;
DROP FUNCTION person_key;
