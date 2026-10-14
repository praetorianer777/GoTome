-- name: ListVisibleLibraries :many
SELECT l.*
FROM libraries l
WHERE l.id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean))
ORDER BY lower(l.name);

-- name: GetVisibleLibrary :one
SELECT l.*
FROM libraries l
WHERE l.id = $1
  AND l.id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean));

-- name: ListLibraryRoots :many
-- Every root, whoever may see it: a new library must not overlap any of them.
SELECT id, root_path FROM libraries;

-- name: CreateLibrary :one
INSERT INTO libraries (id, name, root_path, mode, writable, visibility, owner_id)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: UpdateLibrary :one
UPDATE libraries
SET name       = COALESCE(sqlc.narg(name), name),
    visibility = COALESCE(sqlc.narg(visibility), visibility),
    writable   = COALESCE(sqlc.narg(writable), writable),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteLibrary :execrows
DELETE FROM libraries WHERE id = $1;

-- name: HoldLibrary :exec
-- Deleting a library cascades through its books, files and chunks and locks
-- them in an order of its own, while extraction, chunking, merges, the
-- trash, uploads, scans and edits lock a book and its files in theirs: the
-- two deadlocked (#199). Such a writer holds the library's row first, as
-- the first lock of its transaction. FOR KEY SHARE keeps writers from
-- waiting for each other, as every insert of a book takes it already; the
-- delete, whose first lock is this row's, waits until they are done, or
-- they until it is, and then find nothing.
SELECT 1 FROM libraries WHERE id = $1 FOR KEY SHARE;

-- name: HoldLibrariesOfFiles :exec
-- HoldLibrary for the libraries of the files, in one order.
SELECT 1 FROM libraries
WHERE id IN (SELECT library_id FROM book_files WHERE id = ANY(sqlc.arg(ids)::uuid[]))
ORDER BY id
FOR KEY SHARE;

-- name: HoldLibrariesOfBooks :exec
-- HoldLibrary for the libraries of the books, in one order.
SELECT 1 FROM libraries
WHERE id IN (SELECT library_id FROM books WHERE id = ANY(sqlc.arg(ids)::uuid[]))
ORDER BY id
FOR KEY SHARE;

-- name: ListLibraryMembers :many
SELECT u.id, u.username, u.role, m.added_at
FROM library_members m
JOIN users u ON u.id = m.user_id
WHERE m.library_id = $1
ORDER BY lower(u.username);

-- name: AddLibraryMember :exec
INSERT INTO library_members (library_id, user_id)
VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: RemoveLibraryMember :execrows
DELETE FROM library_members WHERE library_id = $1 AND user_id = $2;
