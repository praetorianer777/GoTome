-- name: UpsertAuthor :one
-- Finds the author by the compared form of the name, or adds them. The
-- spelling that arrived first is kept: a later file spelling the name without
-- its accents must not take them away.
INSERT INTO authors (name, sort_name, name_key)
VALUES ($1, $2, $3)
ON CONFLICT (name_key) DO UPDATE SET name_key = EXCLUDED.name_key
RETURNING *;

-- name: UpsertSeries :one
INSERT INTO series (name, name_key)
VALUES ($1, $2)
ON CONFLICT (name_key) DO UPDATE SET name_key = EXCLUDED.name_key
RETURNING *;

-- name: UpsertPublisher :one
INSERT INTO publishers (name, name_key)
VALUES ($1, $2)
ON CONFLICT (name_key) DO UPDATE SET name_key = EXCLUDED.name_key
RETURNING *;

-- name: UpsertTag :one
INSERT INTO tags (name, name_key)
VALUES ($1, $2)
ON CONFLICT (name_key) DO UPDATE SET name_key = EXCLUDED.name_key
RETURNING *;

-- name: CreateBook :one
INSERT INTO books (
    library_id, title, sort_title, title_key, subtitle, description, language,
    published_on, published_precision, publisher_id, series_id, series_index, page_count,
    field_sources
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
RETURNING *;

-- name: GetVisibleBook :one
-- A book the viewer may see. A book that lost a merge or was deleted is not
-- a book any more.
SELECT b.*
FROM books b
WHERE b.id = $1
  AND b.deleted_at IS NULL
  AND b.library_id IN (SELECT visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean));

-- name: CreateBookFile :one
INSERT INTO book_files (
    book_id, library_id, kind, format, rel_path, size_bytes, modified_at,
    sha256, original_sha256, part_index, uploaded_by
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $8, $9, $10)
RETURNING *;

-- name: ListBookFiles :many
SELECT * FROM book_files
WHERE book_id = $1 AND trashed_at IS NULL
ORDER BY kind, part_index NULLS FIRST, rel_path;

-- name: AddBookContributor :exec
INSERT INTO book_contributors (book_id, author_id, role, position)
VALUES ($1, $2, $3, $4)
ON CONFLICT DO NOTHING;

-- name: ListBookContributors :many
SELECT a.id, a.name, a.sort_name, c.role, c.position
FROM book_contributors c
JOIN authors a ON a.id = c.author_id
WHERE c.book_id = $1
ORDER BY c.position, a.sort_name;

-- name: AddBookTag :exec
INSERT INTO book_tags (book_id, tag_id)
VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: ListBookTags :many
SELECT t.id, t.name
FROM book_tags bt
JOIN tags t ON t.id = bt.tag_id
WHERE bt.book_id = $1
ORDER BY t.name_key;

-- name: AddBookIdentifier :exec
INSERT INTO book_identifiers (book_id, file_id, type, value)
VALUES ($1, $2, $3, $4)
ON CONFLICT DO NOTHING;

-- name: ListBookIdentifiers :many
SELECT id, file_id, type, value
FROM book_identifiers
WHERE book_id = $1
ORDER BY type, value;

-- name: GetSeries :one
SELECT * FROM series WHERE id = $1;

-- name: GetPublisher :one
SELECT * FROM publishers WHERE id = $1;

-- name: LockBookOfFile :one
-- Locked, because two files of one book may be read at the same time and
-- both decide what the book is called.
SELECT b.*
FROM books b
JOIN book_files f ON f.book_id = b.id
WHERE f.id = $1
FOR UPDATE OF b;

-- name: LockVisibleBook :one
-- A book the viewer may see, locked for a person's edit: a file read at the
-- same time must not write over it half done.
SELECT b.*
FROM books b
WHERE b.id = $1
  AND b.deleted_at IS NULL
  AND b.library_id IN (SELECT visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean))
FOR UPDATE;

-- name: SetBookLocks :exec
UPDATE books SET locked_fields = $2 WHERE id = $1;

-- name: DeleteBookOwnIdentifiers :exec
-- The identifiers of the book itself; those its files carry stay.
DELETE FROM book_identifiers WHERE book_id = $1 AND file_id IS NULL;

-- name: UpdateBookDescribed :exec
-- Every field that describes the book, as worked out by the caller from what
-- was there and what a source says.
UPDATE books
SET title               = sqlc.arg(title),
    sort_title          = sqlc.arg(sort_title),
    title_key           = sqlc.arg(title_key),
    subtitle            = sqlc.narg(subtitle),
    description         = sqlc.narg(description),
    language            = sqlc.narg(language),
    published_on        = sqlc.narg(published_on),
    published_precision = sqlc.narg(published_precision),
    publisher_id        = sqlc.narg(publisher_id),
    series_id           = sqlc.narg(series_id),
    series_index        = sqlc.narg(series_index),
    page_count          = sqlc.narg(page_count),
    cover_key           = sqlc.narg(cover_key),
    field_sources       = sqlc.arg(field_sources),
    updated_at          = now()
WHERE id = $1;

-- name: CountBookContributors :one
SELECT count(*) FROM book_contributors WHERE book_id = $1;

-- name: DeleteBookContributors :exec
DELETE FROM book_contributors WHERE book_id = $1;

-- name: CountBookTags :one
SELECT count(*) FROM book_tags WHERE book_id = $1;

-- name: DeleteBookTags :exec
DELETE FROM book_tags WHERE book_id = $1;

-- name: DeleteFileIdentifiers :exec
DELETE FROM book_identifiers WHERE file_id = $1;

-- name: GetVisibleBookCover :one
-- The cover of a book the viewer may see; no row for a book that is hidden,
-- gone, or has none.
SELECT b.cover_key
FROM books b
WHERE b.id = $1
  AND b.deleted_at IS NULL
  AND b.cover_key IS NOT NULL
  AND b.library_id IN (SELECT visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean));

-- name: RefreshAuthorSort :exec
UPDATE books b
SET author_sort = COALESCE((
    SELECT a.sort_name
    FROM book_contributors c
    JOIN authors a ON a.id = c.author_id
    WHERE c.book_id = b.id AND c.role = 'author'
    ORDER BY c.position
    LIMIT 1
), '')
WHERE b.id = $1;

-- name: GetVisibleFile :one
-- A file of a book the viewer may see, with where it lies.
SELECT f.id, f.book_id, f.format, f.rel_path, f.sha256, f.missing_at, l.root_path
FROM book_files f
JOIN books b ON b.id = f.book_id
JOIN libraries l ON l.id = f.library_id
WHERE f.id = $1
  AND f.trashed_at IS NULL
  AND b.deleted_at IS NULL
  AND f.library_id IN (SELECT visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean));
