-- name: FindDuplicateEvidence :many
-- Every other book that looks like this one, with why: a file of the same
-- bytes, a file of the same content where the bytes differ, a shared ISBN,
-- or the same title and an author in common. Only books in the library,
-- neither deleted, merged nor wished for, count.
WITH me AS (
    SELECT bk.id, bk.title_key FROM books bk
    WHERE bk.id = sqlc.arg(book_id) AND bk.deleted_at IS NULL AND NOT bk.placeholder
),
files AS (
    SELECT f.book_id, f.sha256, f.content_sha256
    FROM book_files f
    JOIN books b ON b.id = f.book_id
    WHERE f.missing_at IS NULL AND f.trashed_at IS NULL
      AND b.deleted_at IS NULL AND NOT b.placeholder
)
SELECT o.book_id AS other, 'sha256'::text AS kind, encode(f.sha256, 'hex') AS detail
FROM me
JOIN files f ON f.book_id = me.id
JOIN files o ON o.sha256 = f.sha256 AND o.book_id <> me.id
UNION
SELECT o.book_id, 'content', encode(f.content_sha256, 'hex')
FROM me
JOIN files f ON f.book_id = me.id
JOIN files o ON o.content_sha256 = f.content_sha256 AND o.book_id <> me.id
WHERE o.sha256 IS DISTINCT FROM f.sha256
UNION
SELECT o.book_id, 'isbn', i.value
FROM me
JOIN book_identifiers i ON i.book_id = me.id AND i.type = 'isbn'
JOIN book_identifiers o ON o.type = 'isbn' AND o.value = i.value AND o.book_id <> me.id
JOIN books ob ON ob.id = o.book_id AND ob.deleted_at IS NULL AND NOT ob.placeholder
UNION
SELECT ob.id, 'title_author', me.title_key || ' / ' || a.name_key
FROM me
JOIN books ob ON ob.title_key = me.title_key AND ob.id <> me.id
               AND ob.deleted_at IS NULL AND NOT ob.placeholder
JOIN book_contributors c ON c.book_id = me.id AND c.role = 'author'
JOIN book_contributors oc ON oc.book_id = ob.id AND oc.role = 'author' AND oc.author_id = c.author_id
JOIN authors a ON a.id = c.author_id
WHERE me.title_key <> '';

-- name: UpsertDuplicatePair :one
INSERT INTO duplicate_pairs (book_a, book_b) VALUES ($1, $2)
ON CONFLICT (book_a, book_b) DO UPDATE SET updated_at = duplicate_pairs.updated_at
RETURNING id;

-- name: AddDuplicateEvidence :exec
INSERT INTO duplicate_evidence (pair_id, kind, detail)
SELECT unnest(sqlc.arg(pair_ids)::uuid[]), unnest(sqlc.arg(kinds)::text[]), unnest(sqlc.arg(details)::text[])
ON CONFLICT DO NOTHING;

-- name: PruneDuplicateEvidence :exec
-- Takes back, from the book's open pairs, the evidence not found again.
DELETE FROM duplicate_evidence e
USING duplicate_pairs p
WHERE e.pair_id = p.id AND p.state = 'open'
  AND (p.book_a = sqlc.arg(book_id) OR p.book_b = sqlc.arg(book_id))
  AND (e.pair_id, e.kind, e.detail) NOT IN (
      SELECT unnest(sqlc.arg(pair_ids)::uuid[]), unnest(sqlc.arg(kinds)::text[]), unnest(sqlc.arg(details)::text[])
  );

-- name: DropEmptyDuplicatePairs :exec
-- The book's open pairs that no evidence holds up any more.
DELETE FROM duplicate_pairs p
WHERE p.state = 'open'
  AND (p.book_a = sqlc.arg(book_id) OR p.book_b = sqlc.arg(book_id))
  AND NOT EXISTS (SELECT 1 FROM duplicate_evidence e WHERE e.pair_id = p.id);

-- name: ListLibraryBookIDs :many
-- The books of the visible libraries, of one when named, to check.
SELECT b.id FROM books b
WHERE b.library_id IN (SELECT visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean))
  AND (sqlc.narg(library_id)::uuid IS NULL OR b.library_id = sqlc.narg(library_id)::uuid)
  AND b.deleted_at IS NULL AND NOT b.placeholder;

-- name: ListDuplicatePairs :many
-- The pairs in the state whose books the viewer both sees, of a library when
-- named (either book in it), newest first, after the cursor.
SELECT p.id, p.book_a, p.book_b, p.state, p.found_at
FROM duplicate_pairs p
JOIN books a ON a.id = p.book_a
JOIN books b ON b.id = p.book_b
WHERE p.state = sqlc.arg(state)
  AND a.library_id IN (SELECT visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean))
  AND b.library_id IN (SELECT visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean))
  AND a.deleted_at IS NULL AND b.deleted_at IS NULL
  AND (sqlc.narg(library_id)::uuid IS NULL OR sqlc.narg(library_id)::uuid IN (a.library_id, b.library_id))
  AND (sqlc.narg(before)::uuid IS NULL OR p.id < sqlc.narg(before)::uuid)
ORDER BY p.id DESC
LIMIT sqlc.arg(page_size);

-- name: ListDuplicateEvidence :many
SELECT pair_id, kind, detail FROM duplicate_evidence
WHERE pair_id = ANY(sqlc.arg(pair_ids)::uuid[])
ORDER BY pair_id, kind, detail;
