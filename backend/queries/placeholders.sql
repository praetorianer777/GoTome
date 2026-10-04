-- name: SetPlaceholder :exec
UPDATE books SET placeholder = $2 WHERE id = $1;

-- name: FindPlaceholder :one
-- The placeholder in the library that is the book described: one that
-- shares an ISBN with it, or else has its title and one of its authors.
-- The one wished for first wins.
SELECT b.id
FROM books b
WHERE b.library_id = sqlc.arg(library_id)
  AND b.placeholder
  AND b.deleted_at IS NULL
  AND (
    EXISTS (SELECT 1 FROM book_identifiers i
            WHERE i.book_id = b.id AND i.type = 'isbn' AND i.value = ANY(sqlc.arg(isbns)::text[]))
    OR (b.title_key = sqlc.arg(title_key)
        AND EXISTS (SELECT 1 FROM book_contributors c JOIN authors a ON a.id = c.author_id
                    WHERE c.book_id = b.id AND c.role = 'author'
                      AND a.name_key = ANY(sqlc.arg(author_keys)::text[])))
  )
ORDER BY (EXISTS (SELECT 1 FROM book_identifiers i
                  WHERE i.book_id = b.id AND i.type = 'isbn' AND i.value = ANY(sqlc.arg(isbns)::text[]))) DESC,
         b.created_at, b.id
LIMIT 1
FOR UPDATE;

-- name: BookIsUntouched :one
-- Whether a book is only what a scan made of its files: no person or
-- provider set anything on it, and nobody has read, rated, wished or
-- shelved it.
SELECT COALESCE(NOT b.placeholder
   AND cardinality(b.locked_fields) = 0
   AND NOT EXISTS (SELECT 1 FROM jsonb_each_text(b.field_sources) s
                   WHERE s.value NOT LIKE 'file:%' AND s.value <> 'filename')
   AND NOT EXISTS (SELECT 1 FROM user_books u WHERE u.book_id = b.id)
   AND NOT EXISTS (SELECT 1 FROM reading_progress p WHERE p.book_id = b.id)
   AND NOT EXISTS (SELECT 1 FROM collection_items c WHERE c.book_id = b.id), false)::boolean AS untouched
FROM books b
WHERE b.id = $1
FOR UPDATE OF b;

-- name: MoveFileIdentifiers :exec
-- The identifiers a book's files carry, ahead of the files themselves.
UPDATE book_identifiers i SET book_id = sqlc.arg(to_book)
WHERE i.book_id = sqlc.arg(from_book) AND i.file_id IS NOT NULL;

-- name: MoveBookFiles :exec
UPDATE book_files SET book_id = sqlc.arg(to_book) WHERE book_id = sqlc.arg(from_book);

-- name: MergeBookInto :exec
UPDATE books SET deleted_at = now(), merged_into_id = sqlc.arg(into_book) WHERE id = sqlc.arg(id);

-- name: IsLibraryVisible :one
SELECT EXISTS (
    SELECT 1 FROM libraries l
    WHERE l.id = sqlc.arg(id)
      AND l.id IN (SELECT visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean))
);
