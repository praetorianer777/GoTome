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
    published_on, published_precision, publisher_id, series_id, series_index, page_count
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
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
