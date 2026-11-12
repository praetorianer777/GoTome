-- name: ListCollections :many
-- The viewer's own collections and everyone's shared ones, each with how
-- many of its books the viewer may see, and whether it holds the book
-- asked about.
SELECT c.id, c.owner_id, u.username::text AS owner_name, c.name, c.description, c.visibility,
       c.created_at, c.updated_at,
       (SELECT count(*) FROM collection_items i JOIN books b ON b.id = i.book_id
        WHERE i.collection_id = c.id AND b.deleted_at IS NULL
          AND b.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean)))::int AS books,
       EXISTS (SELECT 1 FROM collection_items i JOIN books b ON b.id = i.book_id
               WHERE i.collection_id = c.id AND i.book_id = sqlc.narg(book)::uuid AND b.deleted_at IS NULL
                 AND b.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean))) AS has_book
FROM collections c
JOIN users u ON u.id = c.owner_id
WHERE c.owner_id = sqlc.arg(viewer)::uuid OR c.visibility = 'shared'
ORDER BY c.owner_id = sqlc.arg(viewer)::uuid DESC, lower(c.name), c.id;

-- name: GetCollection :one
SELECT c.id, c.owner_id, u.username::text AS owner_name, c.name, c.description, c.visibility,
       c.created_at, c.updated_at
FROM collections c
JOIN users u ON u.id = c.owner_id
WHERE c.id = sqlc.arg(id) AND (c.owner_id = sqlc.arg(viewer)::uuid OR c.visibility = 'shared');

-- name: LockCollection :one
SELECT owner_id, visibility FROM collections WHERE id = $1 FOR UPDATE;

-- name: CreateCollection :one
INSERT INTO collections (owner_id, name, description, visibility)
VALUES ($1, $2, $3, $4)
RETURNING id;

-- name: UpdateCollection :exec
UPDATE collections
SET name = $2, description = $3, visibility = $4, updated_at = now()
WHERE id = $1;

-- name: TouchCollection :exec
UPDATE collections SET updated_at = now() WHERE id = $1;

-- name: DeleteCollection :exec
DELETE FROM collections WHERE id = $1;

-- name: CollectionBookIDs :many
-- The collection's books the viewer may see, in its order.
SELECT i.book_id
FROM collection_items i
JOIN books b ON b.id = i.book_id
WHERE i.collection_id = sqlc.arg(collection_id)
  AND b.deleted_at IS NULL
  AND b.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean))
ORDER BY i.position, i.added_at, i.book_id;

-- name: CollectionItems :many
-- Every book of the collection with its position, seen or not.
SELECT book_id, position FROM collection_items
WHERE collection_id = $1
ORDER BY position, added_at, book_id;

-- name: AddCollectionItems :execrows
-- Puts the books among ids the viewer may see at the end of the
-- collection, in the order of ids; those it holds already stay where they
-- are.
INSERT INTO collection_items (collection_id, book_id, position)
SELECT sqlc.arg(collection_id), b.id,
       (SELECT COALESCE(max(position), 0) FROM collection_items WHERE collection_id = sqlc.arg(collection_id)) + n.ord::int
FROM unnest(sqlc.arg(ids)::uuid[]) WITH ORDINALITY AS n(id, ord)
JOIN books b ON b.id = n.id
WHERE b.deleted_at IS NULL
  AND b.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean))
ON CONFLICT (collection_id, book_id) DO NOTHING;

-- name: RemoveCollectionItem :execrows
-- Takes a book the viewer may see out of the collection.
DELETE FROM collection_items i
USING books b
WHERE i.collection_id = sqlc.arg(collection_id) AND i.book_id = sqlc.arg(book_id)
  AND b.id = i.book_id AND b.deleted_at IS NULL
  AND b.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean));

-- name: SetCollectionPositions :exec
UPDATE collection_items i SET position = (sqlc.arg(positions)::int[])[n.ord]
FROM unnest(sqlc.arg(ids)::uuid[]) WITH ORDINALITY AS n(id, ord)
WHERE i.collection_id = sqlc.arg(collection_id) AND i.book_id = n.id;

-- name: CountCollectionItems :one
SELECT count(*)::int FROM collection_items WHERE collection_id = $1;
