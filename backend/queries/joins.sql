-- name: ListJoinCandidates :many
-- The books in the library of the book that the library may join to it by
-- itself: an open pair with the same title and author, of which one book
-- has only ebook files and the other only audio files.
SELECT o.id AS other, book_has_only(me.id, 'ebook') AS me_is_ebook
FROM books me
JOIN libraries l ON l.id = me.library_id AND l.join_formats
JOIN duplicate_pairs p ON p.state = 'open' AND me.id IN (p.book_a, p.book_b)
JOIN books o ON o.id = CASE WHEN p.book_a = me.id THEN p.book_b ELSE p.book_a END
WHERE me.id = $1 AND me.deleted_at IS NULL AND NOT me.placeholder
  AND o.deleted_at IS NULL AND NOT o.placeholder AND o.library_id = me.library_id
  AND EXISTS (SELECT 1 FROM duplicate_evidence e WHERE e.pair_id = p.id AND e.kind = 'title_author')
  AND ((book_has_only(me.id, 'ebook') AND book_has_only(o.id, 'audio'))
    OR (book_has_only(me.id, 'audio') AND book_has_only(o.id, 'ebook')));

-- name: ListFormatPairs :many
-- Of the pairs, those with title and author evidence of which one book has
-- only ebook files and the other only audio files, and the ebook's book.
SELECT p.id AS pair_id,
       (CASE WHEN book_has_only(p.book_a, 'ebook') THEN p.book_a ELSE p.book_b END)::uuid AS ebook_book,
       a.library_id = b.library_id AS same_library
FROM duplicate_pairs p
JOIN books a ON a.id = p.book_a
JOIN books b ON b.id = p.book_b
WHERE p.id = ANY(sqlc.arg(pair_ids)::uuid[])
  AND EXISTS (SELECT 1 FROM duplicate_evidence e WHERE e.pair_id = p.id AND e.kind = 'title_author')
  AND ((book_has_only(p.book_a, 'ebook') AND book_has_only(p.book_b, 'audio'))
    OR (book_has_only(p.book_a, 'audio') AND book_has_only(p.book_b, 'ebook')));

-- name: ListBookFileIDs :many
SELECT id FROM book_files WHERE book_id = $1 ORDER BY id;

-- name: RecordJoin :one
INSERT INTO book_joins (into_book, from_book, file_ids)
VALUES (sqlc.arg(into_book), sqlc.arg(from_book), sqlc.arg(file_ids)::uuid[])
RETURNING id;

-- name: GetJoinForUpdate :one
-- A join not taken back yet, if the viewer sees the surviving book's library.
SELECT j.id, j.into_book, j.from_book, j.file_ids
FROM book_joins j
JOIN books b ON b.id = j.into_book
WHERE j.id = sqlc.arg(id) AND j.undone_at IS NULL AND b.deleted_at IS NULL
  AND b.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean))
FOR UPDATE OF j;

-- name: ListBookJoins :many
-- The joins into the book that can still be taken back, with the title of
-- the book that was joined.
SELECT j.id, j.from_book, f.title, j.created_at
FROM book_joins j
JOIN books f ON f.id = j.from_book
WHERE j.into_book = $1 AND j.undone_at IS NULL
ORDER BY j.created_at, j.id;

-- name: AddNarrators :exec
-- The narrators of one book, credited on the other after those it has.
INSERT INTO book_contributors (book_id, author_id, role, position)
SELECT sqlc.arg(into_book)::uuid, c.author_id, c.role,
       (SELECT coalesce(max(x.position) + 1, 0) FROM book_contributors x WHERE x.book_id = sqlc.arg(into_book)::uuid)
       + (row_number() OVER (ORDER BY c.position) - 1)::int
FROM book_contributors c
WHERE c.book_id = sqlc.arg(from_book) AND c.role = 'narrator'
ON CONFLICT DO NOTHING;

-- name: MoveFilesBack :exec
UPDATE book_files SET book_id = sqlc.arg(to_book)
WHERE id = ANY(sqlc.arg(file_ids)::uuid[]) AND book_id = sqlc.arg(from_book);

-- name: MoveFileIdentifiersBack :exec
UPDATE book_identifiers SET book_id = sqlc.arg(to_book)
WHERE file_id = ANY(sqlc.arg(file_ids)::uuid[]) AND book_id = sqlc.arg(from_book);

-- name: RestoreMergedBook :exec
-- The book a join merged away is a book again.
UPDATE books SET deleted_at = NULL, merged_into_id = NULL WHERE id = $1;

-- name: MarkJoinUndone :exec
UPDATE book_joins SET undone_at = now() WHERE id = $1;

-- name: KeepPairBoth :exec
-- A pair of the two stays as it is, not found for joining again.
UPDATE duplicate_pairs SET state = 'kept_both', updated_at = now()
WHERE book_a = least(sqlc.arg(one)::uuid, sqlc.arg(other)::uuid)
  AND book_b = greatest(sqlc.arg(one)::uuid, sqlc.arg(other)::uuid);

-- name: ListBookForms :many
-- The books linked to the book as the same book in another form, which
-- the viewer sees, with the kinds of file each holds.
SELECT o.id, o.title,
       coalesce((SELECT array_agg(DISTINCT f.kind ORDER BY f.kind) FROM book_files f
                 WHERE f.book_id = o.id AND f.trashed_at IS NULL), '{}')::text[] AS kinds
FROM book_relations r
JOIN books o ON o.id = CASE WHEN r.book_a = sqlc.arg(book_id) THEN r.book_b ELSE r.book_a END
WHERE sqlc.arg(book_id) IN (r.book_a, r.book_b) AND r.kind = 'format'
  AND o.deleted_at IS NULL
  AND o.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean))
ORDER BY o.title, o.id;
