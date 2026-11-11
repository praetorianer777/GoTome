-- name: ListAutoPairs :many
-- The open pairs of two live books of one library the viewer sees whose
-- evidence includes shared bytes, shared content or shared text, after the
-- pair given, in the order of their IDs, each with all its evidence. What
-- class a pair falls in is worked out from the evidence (dedupe.Classify).
SELECT p.id, p.book_a, p.book_b, a.library_id, p.found_at,
       array_agg(e.kind ORDER BY e.kind, e.detail)::text[] AS kinds,
       array_agg(e.detail ORDER BY e.kind, e.detail)::text[] AS details
FROM duplicate_pairs p
JOIN books a ON a.id = p.book_a
JOIN books b ON b.id = p.book_b
JOIN duplicate_evidence e ON e.pair_id = p.id
WHERE p.state = 'open'
  AND a.library_id = b.library_id
  AND a.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean))
  AND (sqlc.narg(library_id)::uuid IS NULL OR a.library_id = sqlc.narg(library_id)::uuid)
  AND a.deleted_at IS NULL AND b.deleted_at IS NULL AND NOT a.placeholder AND NOT b.placeholder
  AND (sqlc.narg(after_id)::uuid IS NULL OR p.id > sqlc.narg(after_id)::uuid)
  AND EXISTS (SELECT 1 FROM duplicate_evidence x WHERE x.pair_id = p.id AND x.kind IN ('sha256', 'content', 'overlap'))
GROUP BY p.id, a.library_id
ORDER BY p.id
LIMIT sqlc.arg(max_rows);

-- name: ListPairFacts :many
-- What decides which of two books survives, and whether the rules may
-- touch them at all.
SELECT b.id,
       (EXISTS (SELECT 1 FROM user_books u WHERE u.book_id = b.id)
        OR EXISTS (SELECT 1 FROM reading_progress r WHERE r.book_id = b.id)) AS has_state,
       (cardinality(b.locked_fields) > 0
        OR EXISTS (SELECT 1 FROM jsonb_each_text(b.field_sources) s WHERE s.value = 'manual')) AS has_manual,
       ARRAY(SELECT DISTINCT f.format FROM book_files f
             WHERE f.book_id = b.id AND f.trashed_at IS NULL AND f.missing_at IS NULL)::text[] AS formats,
       EXISTS (SELECT 1 FROM book_files f WHERE f.book_id = b.id AND f.kind = 'audio' AND f.trashed_at IS NULL) AS has_audio
FROM books b
WHERE b.id = ANY(sqlc.arg(ids)::uuid[]);

-- name: CountNameUsage :many
-- How many books of the library spell an author or a series as given.
SELECT a.name::text AS name, count(DISTINCT c.book_id)::int AS books
FROM authors a
JOIN book_contributors c ON c.author_id = a.id AND c.role = 'author'
JOIN books b ON b.id = c.book_id AND b.library_id = sqlc.arg(library_id)
WHERE a.name = ANY(sqlc.arg(names)::text[])
GROUP BY a.name
UNION ALL
SELECT s.name::text, count(*)::int
FROM series s
JOIN books b ON b.series_id = s.id AND b.library_id = sqlc.arg(library_id)
WHERE s.name = ANY(sqlc.arg(names)::text[])
GROUP BY s.name;

-- name: ListLivePairFiles :many
-- The live files of a book that the rules may sort out.
SELECT f.id, f.book_id, f.kind, f.format, f.modified_at, f.ereader_findings
FROM book_files f
WHERE f.book_id = ANY(sqlc.arg(ids)::uuid[]) AND f.trashed_at IS NULL AND f.missing_at IS NULL
ORDER BY f.id;

-- name: GetPairForRun :one
-- The pair, if it is still open between two live books of one library.
SELECT p.id, p.book_a, p.book_b FROM duplicate_pairs p
JOIN books a ON a.id = p.book_a
JOIN books b ON b.id = p.book_b
WHERE p.id = $1 AND p.state = 'open' AND a.deleted_at IS NULL AND b.deleted_at IS NULL
  AND a.library_id = b.library_id;

-- name: IsLibraryWritable :one
SELECT writable FROM libraries WHERE id = $1;

-- name: HasBookRelation :one
-- Whether a person or a rule has related the two books already.
SELECT EXISTS (SELECT 1 FROM book_relations WHERE book_a = least(sqlc.arg(one)::uuid, sqlc.arg(other)::uuid)
                                              AND book_b = greatest(sqlc.arg(one)::uuid, sqlc.arg(other)::uuid));
