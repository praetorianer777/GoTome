-- name: ListSmartShelves :many
-- The viewer's own smart shelves and everyone's shared ones.
SELECT s.id, s.owner_id, u.username::text AS owner_name, s.name, s.filter, s.visibility, s.updated_at
FROM smart_shelves s
JOIN users u ON u.id = s.owner_id
WHERE s.owner_id = sqlc.arg(viewer)::uuid OR s.visibility = 'shared'
ORDER BY s.owner_id = sqlc.arg(viewer)::uuid DESC, lower(s.name), s.id;

-- name: GetSmartShelf :one
SELECT s.id, s.owner_id, u.username::text AS owner_name, s.name, s.filter, s.visibility, s.updated_at
FROM smart_shelves s
JOIN users u ON u.id = s.owner_id
WHERE s.id = sqlc.arg(id) AND (s.owner_id = sqlc.arg(viewer)::uuid OR s.visibility = 'shared');

-- name: LockSmartShelf :one
SELECT owner_id, visibility FROM smart_shelves WHERE id = $1 FOR UPDATE;

-- name: CreateSmartShelf :one
INSERT INTO smart_shelves (owner_id, name, filter, visibility)
VALUES ($1, $2, $3, $4)
RETURNING id;

-- name: UpdateSmartShelf :exec
UPDATE smart_shelves
SET name = $2, filter = $3, visibility = $4, updated_at = now()
WHERE id = $1;

-- name: DeleteSmartShelf :exec
DELETE FROM smart_shelves WHERE id = $1;
