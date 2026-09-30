-- name: ListVisibleLibraries :many
SELECT l.*
FROM libraries l
WHERE l.id IN (SELECT visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean))
ORDER BY lower(l.name);

-- name: GetVisibleLibrary :one
SELECT l.*
FROM libraries l
WHERE l.id = $1
  AND l.id IN (SELECT visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean));

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
