-- name: LockBooksForMerge :many
-- Both books, in ID order so that two merges of the same pair wait for
-- each other rather than deadlock, if the viewer sees them.
SELECT b.id, b.library_id, b.placeholder
FROM books b
WHERE b.id = ANY(sqlc.arg(ids)::uuid[]) AND b.deleted_at IS NULL
  AND b.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean))
ORDER BY b.id
FOR UPDATE OF b;

-- name: CopyBookFields :exec
-- The fields named, from one book onto the other, with where each came
-- from and whether a person locked it.
UPDATE books s SET
    title               = CASE WHEN 'title' = ANY(sqlc.arg(fields)::text[]) THEN l.title ELSE s.title END,
    sort_title          = CASE WHEN 'title' = ANY(sqlc.arg(fields)::text[]) THEN l.sort_title ELSE s.sort_title END,
    title_key           = CASE WHEN 'title' = ANY(sqlc.arg(fields)::text[]) THEN l.title_key ELSE s.title_key END,
    subtitle            = CASE WHEN 'subtitle' = ANY(sqlc.arg(fields)::text[]) THEN l.subtitle ELSE s.subtitle END,
    description         = CASE WHEN 'description' = ANY(sqlc.arg(fields)::text[]) THEN l.description ELSE s.description END,
    language            = CASE WHEN 'language' = ANY(sqlc.arg(fields)::text[]) THEN l.language ELSE s.language END,
    published_on        = CASE WHEN 'published' = ANY(sqlc.arg(fields)::text[]) THEN l.published_on ELSE s.published_on END,
    published_precision = CASE WHEN 'published' = ANY(sqlc.arg(fields)::text[]) THEN l.published_precision ELSE s.published_precision END,
    publisher_id        = CASE WHEN 'publisher' = ANY(sqlc.arg(fields)::text[]) THEN l.publisher_id ELSE s.publisher_id END,
    series_id           = CASE WHEN 'series' = ANY(sqlc.arg(fields)::text[]) THEN l.series_id ELSE s.series_id END,
    series_index        = CASE WHEN 'series' = ANY(sqlc.arg(fields)::text[]) THEN l.series_index ELSE s.series_index END,
    page_count          = CASE WHEN 'pageCount' = ANY(sqlc.arg(fields)::text[]) THEN l.page_count ELSE s.page_count END,
    cover_key           = CASE WHEN 'cover' = ANY(sqlc.arg(fields)::text[]) THEN l.cover_key ELSE s.cover_key END,
    field_sources       = s.field_sources || coalesce(
        (SELECT jsonb_object_agg(k, v) FROM jsonb_each(l.field_sources) AS e(k, v) WHERE k = ANY(sqlc.arg(fields)::text[])),
        '{}'::jsonb),
    locked_fields       = ARRAY(SELECT DISTINCT f FROM unnest(s.locked_fields || ARRAY(
        SELECT lf FROM unnest(l.locked_fields) AS lf WHERE lf = ANY(sqlc.arg(fields)::text[]))) AS f ORDER BY f),
    updated_at          = now()
FROM books l
WHERE s.id = sqlc.arg(into_book) AND l.id = sqlc.arg(from_book);

-- name: DropContributors :exec
DELETE FROM book_contributors WHERE book_id = $1;

-- name: CopyContributors :exec
INSERT INTO book_contributors (book_id, author_id, role, position)
SELECT sqlc.arg(into_book)::uuid, c.author_id, c.role, c.position
FROM book_contributors c WHERE c.book_id = sqlc.arg(from_book);

-- name: DropTags :exec
DELETE FROM book_tags WHERE book_id = $1;

-- name: CopyTags :exec
INSERT INTO book_tags (book_id, tag_id)
SELECT sqlc.arg(into_book)::uuid, t.tag_id FROM book_tags t WHERE t.book_id = sqlc.arg(from_book);

-- name: MergeBookIdentifiers :exec
-- The book's own identifiers, those of no file, joined to the other's.
WITH moved AS (
    DELETE FROM book_identifiers x WHERE x.book_id = sqlc.arg(from_book) AND x.file_id IS NULL
    RETURNING x.type, x.value)
INSERT INTO book_identifiers (book_id, type, value)
SELECT sqlc.arg(into_book), type, value FROM moved
ON CONFLICT DO NOTHING;

-- name: MergeUserBooks :exec
-- Everyone's standing with the book: the further status, their rating where
-- the surviving book has none, the earliest start and the latest finish.
WITH moved AS (
    DELETE FROM user_books x WHERE x.book_id = sqlc.arg(from_book)
    RETURNING user_id, status, rating, started_on, finished_on, updated_at)
INSERT INTO user_books AS u (user_id, book_id, status, rating, started_on, finished_on, updated_at)
SELECT user_id, sqlc.arg(into_book), status, rating, started_on, finished_on, updated_at FROM moved
ON CONFLICT (user_id, book_id) DO UPDATE SET
    status = CASE WHEN array_position(ARRAY['unread', 'wishlist', 'abandoned', 'reading', 'completed'], EXCLUDED.status)
                     > array_position(ARRAY['unread', 'wishlist', 'abandoned', 'reading', 'completed'], u.status)
                  THEN EXCLUDED.status ELSE u.status END,
    rating      = coalesce(u.rating, EXCLUDED.rating),
    started_on  = least(u.started_on, EXCLUDED.started_on),
    finished_on = greatest(u.finished_on, EXCLUDED.finished_on),
    updated_at  = greatest(u.updated_at, EXCLUDED.updated_at);

-- name: MergeReadingProgress :exec
-- Everyone's place in the book, per medium: the further one.
WITH moved AS (
    DELETE FROM reading_progress x WHERE x.book_id = sqlc.arg(from_book)
    RETURNING user_id, medium, file_id, locator, fraction, chapter, page, position_ms, client_id, updated_at)
INSERT INTO reading_progress AS p (user_id, book_id, medium, file_id, locator, fraction, chapter, page, position_ms, client_id, updated_at)
SELECT user_id, sqlc.arg(into_book), medium, file_id, locator, fraction, chapter, page, position_ms, client_id, updated_at FROM moved
ON CONFLICT (user_id, book_id, medium) DO UPDATE SET
    file_id = EXCLUDED.file_id, locator = EXCLUDED.locator, fraction = EXCLUDED.fraction,
    chapter = EXCLUDED.chapter, page = EXCLUDED.page, position_ms = EXCLUDED.position_ms,
    client_id = EXCLUDED.client_id, updated_at = EXCLUDED.updated_at
WHERE EXCLUDED.fraction > p.fraction;

-- name: MoveReadingHistory :exec
WITH sessions AS (
    UPDATE reading_sessions rs SET book_id = sqlc.arg(into_book) WHERE rs.book_id = sqlc.arg(from_book))
UPDATE reading_finishes rf SET book_id = sqlc.arg(into_book) WHERE rf.book_id = sqlc.arg(from_book);

-- name: MergeCollectionItems :exec
-- The shelves the book is on; where both are, the surviving book's place.
WITH moved AS (
    DELETE FROM collection_items x WHERE x.book_id = sqlc.arg(from_book)
    RETURNING collection_id, position, added_at)
INSERT INTO collection_items (collection_id, book_id, position, added_at)
SELECT collection_id, sqlc.arg(into_book), position, added_at FROM moved
ON CONFLICT DO NOTHING;

-- name: MergeRelations :exec
-- The book's links to its other editions and translations; one to the
-- surviving book itself goes.
WITH moved AS (
    DELETE FROM book_relations r WHERE sqlc.arg(from_book) IN (r.book_a, r.book_b)
    RETURNING CASE WHEN r.book_a = sqlc.arg(from_book) THEN r.book_b ELSE r.book_a END AS other, r.kind)
INSERT INTO book_relations (book_a, book_b, kind)
SELECT least(sqlc.arg(into_book)::uuid, other), greatest(sqlc.arg(into_book)::uuid, other), kind
FROM moved WHERE other <> sqlc.arg(into_book)
ON CONFLICT DO NOTHING;

-- name: MoveNotifications :exec
UPDATE notifications SET book_id = sqlc.arg(into_book) WHERE book_id = sqlc.arg(from_book);

-- name: DropBookMatches :exec
-- What looking the merged book up found, which was about its details.
DELETE FROM metadata_matches WHERE book_id = $1;

-- name: SettleMergedPairs :exec
-- The pair of the two is merged; the merged book's other open pairs go,
-- and the surviving book's are found again by its check.
WITH merged AS (
    UPDATE duplicate_pairs m SET state = 'merged', updated_at = now()
    WHERE m.book_a = least(sqlc.arg(into_book)::uuid, sqlc.arg(from_book)::uuid)
      AND m.book_b = greatest(sqlc.arg(into_book)::uuid, sqlc.arg(from_book)::uuid))
DELETE FROM duplicate_pairs d
WHERE d.state = 'open' AND sqlc.arg(from_book) IN (d.book_a, d.book_b)
  AND NOT (d.book_a = least(sqlc.arg(into_book)::uuid, sqlc.arg(from_book)::uuid)
           AND d.book_b = greatest(sqlc.arg(into_book)::uuid, sqlc.arg(from_book)::uuid));

-- name: DropBookChunks :exec
DELETE FROM book_chunks WHERE book_id = $1;

-- name: ForwardMergedBooks :exec
-- Books merged into the merged book before lead to the surviving one now.
UPDATE books SET merged_into_id = sqlc.arg(into_book) WHERE merged_into_id = sqlc.arg(from_book);

-- name: SetHeldFiles :exec
-- A wish whose book now has files is in the library.
UPDATE books b SET placeholder = false
WHERE b.id = $1 AND b.placeholder AND EXISTS (SELECT 1 FROM book_files f WHERE f.book_id = b.id);

-- name: FollowMerges :one
-- The book a merged one lives on as, through any merges after.
WITH RECURSIVE chain AS (
    SELECT s.id, s.merged_into_id, s.deleted_at, 0 AS depth FROM books s WHERE s.id = $1
    UNION ALL
    SELECT b.id, b.merged_into_id, b.deleted_at, c.depth + 1
    FROM books b JOIN chain c ON b.id = c.merged_into_id
    WHERE c.depth < 20)
SELECT chain.id FROM chain WHERE chain.deleted_at IS NULL ORDER BY chain.depth LIMIT 1;

-- name: ListLiveBookFiles :many
-- The book's files that are in their folders: neither missing nor trashed.
SELECT id FROM book_files
WHERE book_id = $1 AND missing_at IS NULL AND trashed_at IS NULL
ORDER BY id;

-- name: ProgressByFraction :exec
-- Places read in no file the book still has in its folder say only how
-- far: a locator means nothing in another file, a fraction does.
UPDATE reading_progress p
SET locator = 'fraction:' || p.fraction::text, file_id = NULL
WHERE p.book_id = $1 AND p.locator NOT LIKE 'fraction:%'
  AND (p.file_id IS NULL OR p.file_id NOT IN (
      SELECT f.id FROM book_files f WHERE f.book_id = $1 AND f.trashed_at IS NULL AND f.missing_at IS NULL));

-- name: SetPairReplaced :exec
UPDATE duplicate_pairs SET state = 'replaced', updated_at = now()
WHERE book_a = least(sqlc.arg(into_book)::uuid, sqlc.arg(from_book)::uuid)
  AND book_b = greatest(sqlc.arg(into_book)::uuid, sqlc.arg(from_book)::uuid);

-- name: RelateBooks :exec
-- The two as editions or translations of one work, as a person says.
INSERT INTO book_relations (book_a, book_b, kind)
VALUES (least(sqlc.arg(one)::uuid, sqlc.arg(other)::uuid), greatest(sqlc.arg(one)::uuid, sqlc.arg(other)::uuid), sqlc.arg(kind))
ON CONFLICT (book_a, book_b) DO UPDATE SET kind = EXCLUDED.kind;
