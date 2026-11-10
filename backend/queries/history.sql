-- name: UserHistoryLibrary :one
-- The library the viewer chose for the books they read but the library
-- does not hold, if they still see it.
SELECT l.id
FROM users u
JOIN libraries l ON l.id = u.history_library_id
WHERE u.id = sqlc.arg(viewer)::uuid
  AND l.id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean));

-- name: SetUserHistoryLibrary :exec
UPDATE users SET history_library_id = $2 WHERE id = $1 AND history_library_id IS NULL;

-- name: LibraryHoldingMost :one
-- Of the libraries the viewer sees, the one that holds most books of a
-- series or an author.
SELECT b.library_id
FROM books b
WHERE b.deleted_at IS NULL AND NOT b.placeholder
  AND b.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean))
  AND ((sqlc.arg(series_key)::text <> '' AND EXISTS (SELECT 1 FROM series s WHERE s.id = b.series_id AND s.name_key = sqlc.arg(series_key)::text))
    OR EXISTS (SELECT 1 FROM book_contributors c JOIN authors a ON a.id = c.author_id
               WHERE c.book_id = b.id AND c.role = 'author'
                 AND a.person_key = ANY(sqlc.arg(person_keys)::text[])))
GROUP BY b.library_id
ORDER BY count(*) DESC, b.library_id
LIMIT 1;

-- name: VisibleLibraryIDs :many
SELECT l.id
FROM libraries l
WHERE l.id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean))
ORDER BY lower(l.name), l.id;

-- name: GetBookNote :one
SELECT body FROM book_notes WHERE user_id = $1 AND book_id = $2;

-- name: PutBookNote :exec
INSERT INTO book_notes (user_id, book_id, body) VALUES ($1, $2, $3)
ON CONFLICT (user_id, book_id) DO UPDATE SET body = EXCLUDED.body, updated_at = now();

-- name: DeleteBookNote :exec
DELETE FROM book_notes WHERE user_id = $1 AND book_id = $2;

-- name: MergeBookNotes :exec
-- Everyone's notes on the book; where a person wrote one on both, the
-- surviving book's note keeps and the other's follows it.
WITH moved AS (
    DELETE FROM book_notes x WHERE x.book_id = sqlc.arg(from_book)
    RETURNING user_id, body, updated_at)
INSERT INTO book_notes AS n (user_id, book_id, body, updated_at)
SELECT user_id, sqlc.arg(into_book), body, updated_at FROM moved
ON CONFLICT (user_id, book_id) DO UPDATE SET
    body = n.body || E'\n\n' || EXCLUDED.body,
    updated_at = greatest(n.updated_at, EXCLUDED.updated_at);

-- name: AddFinishOn :execrows
-- A finish on a day, at noon UTC so that every time zone counts it in the
-- same year, unless the person has one on that day already.
INSERT INTO reading_finishes (user_id, book_id, medium, finished_at)
SELECT sqlc.arg(user_id)::uuid, sqlc.arg(book_id)::uuid, 'ebook', (sqlc.arg(day)::date + time '12:00') AT TIME ZONE 'UTC'
WHERE NOT EXISTS (
    SELECT 1 FROM reading_finishes f
    WHERE f.user_id = sqlc.arg(user_id)::uuid AND f.book_id = sqlc.arg(book_id)::uuid
      AND (f.finished_at AT TIME ZONE 'UTC')::date = sqlc.arg(day)::date);

-- name: ListReading :many
-- What the viewer read, is reading, gave up or wishes for, the latest
-- first, placeholders included. Each row finds its book by its key: the
-- rows are one person's.
SELECT ub.book_id, ub.status, ub.rating, ub.started_on, ub.finished_on,
       coalesce(ub.finished_on, ub.started_on, (ub.updated_at AT TIME ZONE 'UTC')::date) AS day
FROM user_books ub
JOIN LATERAL (
    SELECT bk.id, bk.title_key
    FROM books bk
    WHERE bk.id = ub.book_id AND bk.deleted_at IS NULL
      AND bk.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean))
    OFFSET 0
) b ON true
WHERE ub.user_id = sqlc.arg(viewer)::uuid
  AND ub.status = ANY(sqlc.arg(statuses)::text[])
  AND (sqlc.narg(year)::int IS NULL
       OR extract(year FROM coalesce(ub.finished_on, ub.started_on, (ub.updated_at AT TIME ZONE 'UTC')::date)) = sqlc.narg(year)::int)
  AND (sqlc.narg(stars)::int IS NULL OR ub.rating = sqlc.narg(stars)::int)
  AND (sqlc.arg(search_key)::text = ''
       OR b.title_key LIKE '%' || sqlc.arg(search_key)::text || '%'
       OR EXISTS (SELECT 1 FROM book_contributors c JOIN authors a ON a.id = c.author_id
                  WHERE c.book_id = b.id AND c.role = 'author'
                    AND a.name_key LIKE '%' || sqlc.arg(search_key)::text || '%'))
  AND (sqlc.narg(after_day)::date IS NULL
       OR (coalesce(ub.finished_on, ub.started_on, (ub.updated_at AT TIME ZONE 'UTC')::date), ub.book_id)
          < (sqlc.narg(after_day)::date, sqlc.narg(after_id)::uuid))
ORDER BY day DESC, ub.book_id DESC
LIMIT sqlc.arg(max_rows);

-- name: ReadingYears :many
-- The years the viewer's reading lies in, for the filter.
SELECT extract(year FROM coalesce(ub.finished_on, ub.started_on, (ub.updated_at AT TIME ZONE 'UTC')::date))::int AS year
FROM user_books ub
-- Each row looks its book up by its key, as ListReading does: as a join,
-- the planner hashes every book of the library.
JOIN LATERAL (
    SELECT 1
    FROM books bk
    WHERE bk.id = ub.book_id AND bk.deleted_at IS NULL
      AND bk.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean))
    OFFSET 0
) b ON true
WHERE ub.user_id = sqlc.arg(viewer)::uuid AND ub.status IN ('reading', 'completed', 'abandoned')
GROUP BY 1
ORDER BY 1 DESC;

-- name: MatchImportBooks :many
-- The books of libraries the viewer sees, placeholders included, that an
-- imported row may be: those with one of its ISBNs first, else those with
-- its title and an author.
SELECT b.id,
       EXISTS (SELECT 1 FROM book_identifiers i
               WHERE i.book_id = b.id AND i.type = 'isbn' AND i.value = ANY(sqlc.arg(isbns)::text[])) AS by_isbn
FROM books b
WHERE b.deleted_at IS NULL
  AND b.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean))
  AND (
    EXISTS (SELECT 1 FROM book_identifiers i
            WHERE i.book_id = b.id AND i.type = 'isbn' AND i.value = ANY(sqlc.arg(isbns)::text[]))
    OR (b.title_key = sqlc.arg(title_key)::text AND cardinality(sqlc.arg(person_keys)::text[]) > 0
        AND EXISTS (SELECT 1 FROM book_contributors c JOIN authors a ON a.id = c.author_id
                    WHERE c.book_id = b.id AND c.role = 'author'
                      AND a.person_key = ANY(sqlc.arg(person_keys)::text[])))
  )
ORDER BY 2 DESC, b.placeholder, b.created_at, b.id
LIMIT 6;

-- name: CreateReadingImport :one
INSERT INTO reading_imports (user_id, source, library_id, sees_all)
VALUES ($1, $2, $3, $4)
RETURNING id;

-- name: DeleteStaleReadingImports :exec
-- Previews nobody went on with.
DELETE FROM reading_imports
WHERE user_id = $1 AND started_at IS NULL AND created_at < now() - interval '1 day';

-- name: AddReadingImportRow :exec
INSERT INTO reading_import_rows (import_id, row_no, title, authors, isbns, status, rating, rounded, read_on, shelves, review, match, book_id, candidates)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14);

-- name: GetReadingImport :one
SELECT * FROM reading_imports WHERE id = $1 AND user_id = $2;

-- name: ListReadingImportRows :many
SELECT * FROM reading_import_rows WHERE import_id = $1 ORDER BY row_no;

-- name: ChooseReadingImportRow :execrows
UPDATE reading_import_rows r
SET book_id = sqlc.narg(book_id), skip = sqlc.arg(skip)
WHERE r.import_id = sqlc.arg(import_id) AND r.row_no = sqlc.arg(row_no) AND r.match = 'ambiguous'
  AND (sqlc.arg(skip)::boolean OR sqlc.narg(book_id)::uuid = ANY(r.candidates));

-- name: StartReadingImport :execrows
UPDATE reading_imports SET started_at = now(), overwrite = $2
WHERE id = $1 AND started_at IS NULL;

-- name: NextReadingImportRows :many
SELECT * FROM reading_import_rows
WHERE import_id = $1 AND outcome IS NULL
ORDER BY row_no
LIMIT $2;

-- name: SetReadingImportRowOutcome :exec
UPDATE reading_import_rows SET outcome = $3, message = $4, book_id = coalesce($5, book_id), review = ''
WHERE import_id = $1 AND row_no = $2;

-- name: FinishReadingImport :execrows
UPDATE reading_imports SET finished_at = now() WHERE id = $1 AND finished_at IS NULL;

-- name: CountReadingImportOutcomes :many
SELECT COALESCE(outcome, '')::text AS outcome, count(*)::int AS rows
FROM reading_import_rows WHERE import_id = $1 GROUP BY 1;

-- name: FindOwnedCollection :one
SELECT id FROM collections WHERE owner_id = $1 AND lower(name) = lower($2::text) ORDER BY created_at LIMIT 1;

-- name: GetReadingImportAny :one
SELECT * FROM reading_imports WHERE id = $1;

-- name: LibraryHoldingMostOf :one
-- Of these books, the library that holds most.
SELECT library_id FROM books WHERE id = ANY(sqlc.arg(ids)::uuid[]) GROUP BY library_id ORDER BY count(*) DESC, library_id LIMIT 1;
