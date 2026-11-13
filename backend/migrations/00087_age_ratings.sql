-- +goose Up
-- A book's age rating (#426) follows the German FSK steps: 0, 6, 12, 16 and
-- 18, 18 being "adult". NULL is unrated. An account's maximum is the highest
-- rating it may see; NULL is no limit.
ALTER TABLE books ADD COLUMN age_rating smallint CHECK (age_rating IN (0, 6, 12, 16, 18));
ALTER TABLE users ADD COLUMN max_age_rating smallint CHECK (max_age_rating IN (0, 6, 12, 16, 18));

-- The one place that says what a viewer's maximum rating is, as
-- visible_library_ids is for libraries: every query that filters books
-- through that function also keeps books whose rating is within
-- age_limit(viewer, sees_all), written (b.age_rating IS NULL OR b.age_rating
-- <= (SELECT age_limit(viewer, sees_all))). The scalar subquery is evaluated
-- once per statement; a function of the row would be called for every row,
-- as a body with a subquery is not inlined, and a COALESCE hides the column
-- from the planner's statistics. Administrators (sees_all) and accounts
-- without a maximum are limited to 18, which every rating meets; unrated
-- books are shown to all.
-- +goose StatementBegin
CREATE FUNCTION age_limit(viewer uuid, sees_all boolean)
RETURNS smallint
LANGUAGE sql
STABLE
AS $$
    SELECT CASE WHEN sees_all THEN 18::smallint
                ELSE COALESCE((SELECT u.max_age_rating FROM users u WHERE u.id = viewer), 18::smallint) END
$$;
-- +goose StatementEnd

-- For rows that name books without being one (a curation action): true only
-- when the viewer sees every book named.
-- +goose StatementBegin
CREATE FUNCTION books_age_visible(ids uuid[], viewer uuid, sees_all boolean)
RETURNS boolean
LANGUAGE sql
STABLE
AS $$
    SELECT sees_all OR NOT EXISTS (
        SELECT 1 FROM books b
        WHERE b.id = ANY(ids) AND NOT (b.age_rating IS NULL OR b.age_rating <= (SELECT age_limit(viewer, sees_all))))
$$;
-- +goose StatementEnd

-- The clean-up suggestions are found by functions of their own.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION cleanup_author_spellings(viewer uuid, sees_all boolean, subjects text[])
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
            AND b.library_id IN (SELECT * FROM visible_library_ids(viewer, sees_all)) AND (b.age_rating IS NULL OR b.age_rating <= (SELECT age_limit(viewer, sees_all)))
          GROUP BY c.author_id) u
    JOIN authors a ON a.id = u.author_id
    GROUP BY a.person_key
    HAVING count(*) > 1;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION cleanup_placeholder_authors(viewer uuid, sees_all boolean, subjects text[], name_keys text[])
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
      AND b.library_id IN (SELECT * FROM visible_library_ids(viewer, sees_all)) AND (b.age_rating IS NULL OR b.age_rating <= (SELECT age_limit(viewer, sees_all)))
    GROUP BY a.id;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION cleanup_name_clashes(viewer uuid, sees_all boolean, subjects text[])
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
      AND b.library_id IN (SELECT * FROM visible_library_ids(viewer, sees_all)) AND (b.age_rating IS NULL OR b.age_rating <= (SELECT age_limit(viewer, sees_all)))
    GROUP BY a.id
    HAVING bool_or(s.name_key = a.name_key);
END;
-- +goose StatementEnd

-- The subjects of titles are book IDs; the caller passes valid ones only.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION cleanup_titles(viewer uuid, sees_all boolean, subjects text[])
RETURNS TABLE (subject text, value text, books integer, names text[], counts integer[])
LANGUAGE sql STABLE
BEGIN ATOMIC
    SELECT b.id::text, b.title, 1, ARRAY[b.title], ARRAY[1]
    FROM books b
    WHERE b.deleted_at IS NULL AND b.title_addition
      AND (subjects IS NULL OR b.id = ANY(subjects::uuid[]))
      AND b.library_id IN (SELECT * FROM visible_library_ids(viewer, sees_all)) AND (b.age_rating IS NULL OR b.age_rating <= (SELECT age_limit(viewer, sees_all)));
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION cleanup_title_series(viewer uuid, sees_all boolean, subjects text[])
RETURNS TABLE (subject text, value text, books integer, names text[], counts integer[])
LANGUAGE sql STABLE
BEGIN ATOMIC
    WITH found AS (
        SELECT b.id, b.library_id, b.title, b.title_series
        FROM books b
        WHERE b.title_series IS NOT NULL AND b.series_id IS NULL
          AND b.deleted_at IS NULL AND NOT b.placeholder
          AND (subjects IS NULL OR b.title_series = ANY(subjects))
          AND b.library_id IN (SELECT * FROM visible_library_ids(viewer, sees_all)) AND (b.age_rating IS NULL OR b.age_rating <= (SELECT age_limit(viewer, sees_all)))
    ), shared AS (
        SELECT f.title_series, f.library_id FROM found f
        GROUP BY f.title_series, f.library_id
        HAVING count(*) > 1
    ), known AS (
        SELECT k.title_series FROM (SELECT DISTINCT f.title_series FROM found f) k
        WHERE EXISTS (SELECT 1 FROM series s JOIN books o ON o.series_id = s.id
                      WHERE loose_key(s.name) = k.title_series
                        AND o.deleted_at IS NULL AND NOT o.placeholder
                        AND o.library_id IN (SELECT * FROM visible_library_ids(viewer, sees_all)) AND (o.age_rating IS NULL OR o.age_rating <= (SELECT age_limit(viewer, sees_all))))
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
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION cleanup_author_spellings(viewer uuid, sees_all boolean, subjects text[])
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
CREATE OR REPLACE FUNCTION cleanup_placeholder_authors(viewer uuid, sees_all boolean, subjects text[], name_keys text[])
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
CREATE OR REPLACE FUNCTION cleanup_name_clashes(viewer uuid, sees_all boolean, subjects text[])
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
CREATE OR REPLACE FUNCTION cleanup_titles(viewer uuid, sees_all boolean, subjects text[])
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
CREATE OR REPLACE FUNCTION cleanup_title_series(viewer uuid, sees_all boolean, subjects text[])
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

DROP FUNCTION books_age_visible(uuid[], uuid, boolean);
DROP FUNCTION age_limit(uuid, boolean);
ALTER TABLE users DROP COLUMN max_age_rating;
ALTER TABLE books DROP COLUMN age_rating;
