-- +goose Up
-- A name as the clean-up compares series found in titles: lower case, runs
-- of anything but letters and digits one space. It is cleanup.looseKey.
-- catalog.Key also takes accents off, which Postgres cannot do IMMUTABLE.
-- +goose StatementBegin
CREATE FUNCTION loose_key(name text) RETURNS text
LANGUAGE sql IMMUTABLE PARALLEL SAFE
RETURN btrim(regexp_replace(lower(name), '[^[:alnum:]]+', ' ', 'g'));
-- +goose StatementEnd

-- The series a title holds, as loose_key, or NULL: "Rain Wild Chronicles
-- 01 - Drachenhüter", "Drachenhüter (Rain Wild Chronicles #1)". It is
-- cleanup.SeriesInTitle, which reads the series, the position and the
-- title; a test holds the two to one answer. Like title_addition it is
-- matched as titles are written, into books.title_series.
-- +goose StatementBegin
CREATE FUNCTION title_series_key(title text) RETURNS text
LANGUAGE sql IMMUTABLE PARALLEL SAFE
BEGIN ATOMIC
    SELECT CASE
        WHEN char_length(p.series) >= 2
         AND loose_key(p.series) <> ALL (ARRAY['band', 'bd', 'teil', 'folge', 'buch', 'book', 'vol', 'volume', 'nr', 'no',
                                               'part', 'chapter', 'kapitel', 'episode', 'cd', 'disc', 'track'])
         AND NOT (char_length(p.number) = 4 AND p.number >= '1800' AND p.number < '2100')
        THEN loose_key(p.series)
    END
    FROM (SELECT btrim(m[1]) AS series, m[2] AS number
          FROM (SELECT COALESCE(
                    regexp_match(btrim(title),
                        '^(.*?[[:alpha:]].*?)(?:\s*[,(:–—-]\s*|\s+)'
                        '(?:(?:band|bd\.|teil|folge|buch|book|vol\.|volume|nr\.|no\.|#)\s*)?'
                        '(\d{1,4}(?:\.\d)?)\s*\)?\s*[:–—-]\s*(.*[[:alpha:]].*)$', 'i'),
                    (SELECT ARRAY[l[2], l[3]]
                     FROM regexp_match(btrim(title),
                        '^(.*?[[:alpha:]].*?)\s*\(\s*([^()]*?[[:alpha:]][^()]*?)(?:\s*,\s*|\s+)'
                        '(?:band|bd\.|teil|folge|buch|book|vol\.|volume|nr\.|no\.|#)\s*(\d{1,4}(?:\.\d)?)\s*\)$', 'i') AS l
                     WHERE l IS NOT NULL)) AS m) x) p;
END;
-- +goose StatementEnd

ALTER TABLE books ADD COLUMN title_series text GENERATED ALWAYS AS (title_series_key(title)) STORED;
CREATE INDEX books_title_series_idx ON books (title_series) WHERE title_series IS NOT NULL;
CREATE INDEX series_loose_key_idx ON series (loose_key(name));

-- Series that books without one hold in their titles. A series is taken
-- for one where at least two books of a library hold it, or a book the
-- viewer sees is in a series of that name already: "Fahrenheit 451 - …"
-- alone is a title. The value is the books' IDs, so a dismissal holds
-- until a book joins or leaves, and the names are their titles, in the
-- same order.
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

-- +goose Down
DROP FUNCTION cleanup_title_series;
DROP INDEX series_loose_key_idx;
DROP INDEX books_title_series_idx;
ALTER TABLE books DROP COLUMN title_series;
DROP FUNCTION title_series_key;
DROP FUNCTION loose_key;
