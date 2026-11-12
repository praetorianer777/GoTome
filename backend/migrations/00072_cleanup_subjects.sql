-- +goose Up
-- Applying or dismissing one suggestion needs that suggestion only: the
-- clean-up functions take the subjects they are to find (NULL: all), so the
-- books of those names are read through the key indexes and not every
-- book's.
DROP FUNCTION cleanup_author_spellings(uuid, boolean);
DROP FUNCTION cleanup_placeholder_authors(uuid, boolean, text[]);
DROP FUNCTION cleanup_name_clashes(uuid, boolean);
DROP FUNCTION cleanup_titles(uuid, boolean);
DROP FUNCTION cleanup_title_series(uuid, boolean);

-- +goose StatementBegin
CREATE FUNCTION cleanup_author_spellings(viewer uuid, sees_all boolean, subjects text[])
RETURNS TABLE (subject text, value text, books integer, names text[], counts integer[])
LANGUAGE sql STABLE
BEGIN ATOMIC
    SELECT a.person_key,
           string_agg(a.name_key, '|' ORDER BY a.name_key),
           sum(u.books)::integer,
           array_agg(a.name ORDER BY u.books DESC, a.name LIKE '%,%' DESC, a.name),
           array_agg(u.books ORDER BY u.books DESC, a.name LIKE '%,%' DESC, a.name)
    FROM (SELECT c.author_id, count(DISTINCT c.book_id)::integer AS books
          FROM authors a0
          JOIN book_contributors c ON c.author_id = a0.id
          JOIN books b ON b.id = c.book_id
          WHERE c.role = 'author' AND b.deleted_at IS NULL
            AND (subjects IS NULL OR a0.person_key = ANY(subjects))
            AND b.library_id IN (SELECT * FROM visible_library_ids(viewer, sees_all))
          GROUP BY c.author_id) u
    JOIN authors a ON a.id = u.author_id
    GROUP BY a.person_key
    HAVING count(*) > 1;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION cleanup_placeholder_authors(viewer uuid, sees_all boolean, subjects text[], name_keys text[])
RETURNS TABLE (subject text, value text, books integer, names text[], counts integer[])
LANGUAGE sql STABLE
BEGIN ATOMIC
    SELECT a.name_key, a.name_key, count(DISTINCT c.book_id)::integer,
           ARRAY[a.name], ARRAY[count(DISTINCT c.book_id)::integer]
    FROM authors a
    JOIN book_contributors c ON c.author_id = a.id AND c.role = 'author'
    JOIN books b ON b.id = c.book_id
    WHERE a.name_key = ANY(name_keys) AND b.deleted_at IS NULL
      AND (subjects IS NULL OR a.name_key = ANY(subjects))
      AND b.library_id IN (SELECT * FROM visible_library_ids(viewer, sees_all))
    GROUP BY a.id;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION cleanup_name_clashes(viewer uuid, sees_all boolean, subjects text[])
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
      AND (subjects IS NULL OR a.name_key = ANY(subjects))
      AND b.library_id IN (SELECT * FROM visible_library_ids(viewer, sees_all))
    GROUP BY a.id
    HAVING bool_or(s.name_key = a.name_key);
END;
-- +goose StatementEnd

-- The subjects of titles are book IDs; the caller passes valid ones only.
-- +goose StatementBegin
CREATE FUNCTION cleanup_titles(viewer uuid, sees_all boolean, subjects text[])
RETURNS TABLE (subject text, value text, books integer, names text[], counts integer[])
LANGUAGE sql STABLE
BEGIN ATOMIC
    SELECT b.id::text, b.title, 1, ARRAY[b.title], ARRAY[1]
    FROM books b
    WHERE b.deleted_at IS NULL AND b.title_addition
      AND (subjects IS NULL OR b.id = ANY(subjects::uuid[]))
      AND b.library_id IN (SELECT * FROM visible_library_ids(viewer, sees_all));
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION cleanup_title_series(viewer uuid, sees_all boolean, subjects text[])
RETURNS TABLE (subject text, value text, books integer, names text[], counts integer[])
LANGUAGE sql STABLE
BEGIN ATOMIC
    WITH found AS (
        SELECT b.id, b.library_id, b.title, b.title_series
        FROM books b
        WHERE b.title_series IS NOT NULL AND b.series_id IS NULL
          AND b.deleted_at IS NULL AND NOT b.placeholder
          AND (subjects IS NULL OR b.title_series = ANY(subjects))
          AND b.library_id IN (SELECT * FROM visible_library_ids(viewer, sees_all))
    ), shared AS (
        SELECT f.title_series, f.library_id FROM found f
        GROUP BY f.title_series, f.library_id
        HAVING count(*) > 1
    ), known AS (
        SELECT k.title_series FROM (SELECT DISTINCT f.title_series FROM found f) k
        WHERE EXISTS (SELECT 1 FROM series s JOIN books o ON o.series_id = s.id
                      WHERE loose_key(s.name) = k.title_series
                        AND o.deleted_at IS NULL AND NOT o.placeholder
                        AND o.library_id IN (SELECT * FROM visible_library_ids(viewer, sees_all)))
    )
    SELECT f.title_series, string_agg(f.id::text, '|' ORDER BY f.id), count(*)::integer,
           array_agg(f.title ORDER BY f.id), array_agg(1 ORDER BY f.id)
    FROM found f
    WHERE f.title_series IN (SELECT k.title_series FROM known k)
       OR (f.title_series, f.library_id) IN (SELECT s.title_series, s.library_id FROM shared s)
    GROUP BY f.title_series;
END;
-- +goose StatementEnd

-- +goose Down
DROP FUNCTION cleanup_title_series(uuid, boolean, text[]);
DROP FUNCTION cleanup_titles(uuid, boolean, text[]);
DROP FUNCTION cleanup_name_clashes(uuid, boolean, text[]);
DROP FUNCTION cleanup_placeholder_authors(uuid, boolean, text[], text[]);
DROP FUNCTION cleanup_author_spellings(uuid, boolean, text[]);

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

-- +goose StatementBegin
CREATE FUNCTION cleanup_title_series(viewer uuid, sees_all boolean)
RETURNS TABLE (subject text, value text, books integer, names text[], counts integer[])
LANGUAGE sql STABLE
BEGIN ATOMIC
    WITH found AS (
        SELECT b.id, b.library_id, b.title, b.title_series
        FROM books b
        WHERE b.title_series IS NOT NULL AND b.series_id IS NULL
          AND b.deleted_at IS NULL AND NOT b.placeholder
          AND b.library_id IN (SELECT * FROM visible_library_ids(viewer, sees_all))
    ), shared AS (
        SELECT f.title_series, f.library_id FROM found f
        GROUP BY f.title_series, f.library_id
        HAVING count(*) > 1
    ), known AS (
        SELECT k.title_series FROM (SELECT DISTINCT f.title_series FROM found f) k
        WHERE EXISTS (SELECT 1 FROM series s JOIN books o ON o.series_id = s.id
                      WHERE loose_key(s.name) = k.title_series
                        AND o.deleted_at IS NULL AND NOT o.placeholder
                        AND o.library_id IN (SELECT * FROM visible_library_ids(viewer, sees_all)))
    )
    SELECT f.title_series, string_agg(f.id::text, '|' ORDER BY f.id), count(*)::integer,
           array_agg(f.title ORDER BY f.id), array_agg(1 ORDER BY f.id)
    FROM found f
    WHERE f.title_series IN (SELECT k.title_series FROM known k)
       OR (f.title_series, f.library_id) IN (SELECT s.title_series, s.library_id FROM shared s)
    GROUP BY f.title_series;
END;
-- +goose StatementEnd
