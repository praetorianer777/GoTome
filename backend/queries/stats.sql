-- name: LibraryFigures :one
-- What the books the viewer sees hold, of one library when named: the
-- counts of the statistics page.
WITH vb AS (
    SELECT b.id, b.series_id, b.publisher_id, b.cover_key, b.primary_text_file_id
    FROM books b
    WHERE b.deleted_at IS NULL AND NOT b.placeholder
      AND b.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean))
      AND (sqlc.narg(library_id)::uuid IS NULL OR b.library_id = sqlc.narg(library_id)::uuid)
),
vf AS (
    SELECT f.book_id, f.kind, f.size_bytes, f.duration_ms, f.missing_at
    FROM book_files f
    JOIN vb ON vb.id = f.book_id
    WHERE f.trashed_at IS NULL
)
SELECT (SELECT count(*) FROM vb)::int AS books,
       (SELECT count(*) FROM vf)::int AS files,
       (SELECT COALESCE(sum(size_bytes), 0) FROM vf)::bigint AS bytes,
       (SELECT count(DISTINCT book_id) FROM vf WHERE kind = 'audio')::int AS audiobooks,
       (SELECT COALESCE(sum(duration_ms), 0) FROM vf WHERE kind = 'audio')::bigint AS audio_ms,
       (SELECT COALESCE(sum(f.page_count), 0) FROM vb JOIN book_files f ON f.id = vb.primary_text_file_id)::bigint AS pages,
       (SELECT count(DISTINCT c.author_id) FROM book_contributors c JOIN vb ON vb.id = c.book_id WHERE c.role = 'author')::int AS authors,
       (SELECT count(DISTINCT series_id) FROM vb)::int AS series,
       (SELECT count(DISTINCT publisher_id) FROM vb)::int AS publishers,
       (SELECT count(DISTINCT bt.tag_id) FROM book_tags bt JOIN vb ON vb.id = bt.book_id)::int AS tags,
       (SELECT count(*) FROM vb WHERE cover_key IS NULL)::int AS without_cover,
       (SELECT count(*) FROM vb WHERE NOT EXISTS (
            SELECT 1 FROM book_contributors c WHERE c.book_id = vb.id AND c.role = 'author'))::int AS without_author,
       (SELECT count(*) FROM vb
        WHERE EXISTS (SELECT 1 FROM vf WHERE vf.book_id = vb.id AND vf.kind = 'ebook')
          AND NOT EXISTS (SELECT 1 FROM book_files f WHERE f.id = vb.primary_text_file_id AND f.chunked_at IS NOT NULL))::int AS without_text,
       (SELECT count(DISTINCT book_id) FROM vf WHERE missing_at IS NOT NULL)::int AS with_missing_files;

-- name: FilesByFormat :many
-- The viewer's files by format, the most first.
SELECT f.format, count(*)::int AS files, COALESCE(sum(f.size_bytes), 0)::bigint AS bytes
FROM book_files f
JOIN books b ON b.id = f.book_id
WHERE f.trashed_at IS NULL AND b.deleted_at IS NULL AND NOT b.placeholder
  AND b.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean))
  AND (sqlc.narg(library_id)::uuid IS NULL OR b.library_id = sqlc.narg(library_id)::uuid)
GROUP BY f.format
ORDER BY files DESC, f.format;

-- name: BooksByLanguage :many
-- The viewer's books by language, without its region, the most first; ''
-- is no language known.
SELECT COALESCE(lower(split_part(replace(b.language, '_', '-'), '-', 1)), '')::text AS language, count(*)::int AS books
FROM books b
WHERE b.deleted_at IS NULL AND NOT b.placeholder
  AND b.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean))
  AND (sqlc.narg(library_id)::uuid IS NULL OR b.library_id = sqlc.narg(library_id)::uuid)
GROUP BY 1
ORDER BY books DESC, language
LIMIT 12;

-- name: BooksAddedPerMonth :many
-- How many of the viewer's books arrived in each of the last twelve months.
SELECT to_char(date_trunc('month', b.created_at), 'YYYY-MM')::text AS month, count(*)::int AS books
FROM books b
WHERE b.deleted_at IS NULL AND NOT b.placeholder
  AND b.created_at >= date_trunc('month', now()) - interval '11 months'
  AND b.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean))
  AND (sqlc.narg(library_id)::uuid IS NULL OR b.library_id = sqlc.narg(library_id)::uuid)
GROUP BY 1
ORDER BY 1;

-- name: WorkProgress :one
-- How far the background work on the viewer's books is: each piece as
-- done of all, and how much of it was done in the last hour. Lookups and
-- duplicate checks are counted from their jobs (stats.jobsQuery).
WITH vb AS (
    SELECT b.id, b.primary_text_file_id
    FROM books b
    WHERE b.deleted_at IS NULL AND NOT b.placeholder
      AND b.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean))
      AND (sqlc.narg(library_id)::uuid IS NULL OR b.library_id = sqlc.narg(library_id)::uuid)
),
vf AS (
    SELECT f.id, f.extract_state, f.updated_at, f.chunked_at
    FROM book_files f
    JOIN vb ON vb.id = f.book_id
    WHERE f.trashed_at IS NULL AND f.missing_at IS NULL
)
SELECT (SELECT count(*) FROM vf WHERE extract_state <> 'skipped')::int AS files,
       (SELECT count(*) FROM vf WHERE extract_state = 'done')::int AS files_read,
       (SELECT count(*) FROM vf WHERE extract_state = 'failed')::int AS files_failed,
       (SELECT count(*) FROM vf WHERE extract_state IN ('done', 'failed')
          AND updated_at > now() - interval '1 hour')::int AS files_last_hour,
       (SELECT count(*) FROM vb WHERE primary_text_file_id IS NOT NULL)::int AS texts,
       (SELECT count(*) FROM vb JOIN vf ON vf.id = vb.primary_text_file_id WHERE vf.chunked_at IS NOT NULL)::int AS texts_indexed,
       (SELECT count(*) FROM vb JOIN vf ON vf.id = vb.primary_text_file_id
        WHERE vf.chunked_at > now() - interval '1 hour')::int AS texts_last_hour,
       (SELECT count(*) FROM metadata_matches m JOIN vb ON vb.id = m.book_id WHERE m.state = 'pending')::int AS matches_pending,
       (SELECT count(*) FROM duplicate_pairs p
        WHERE p.state = 'open' AND p.book_a IN (SELECT id FROM vb) AND p.book_b IN (SELECT id FROM vb))::int AS pairs_open;
