-- name: QueueDeliveries :execrows
-- Puts the books among ids the viewer may see, and that have an ebook on
-- disk, in the queue of the user's device; one delivered before is queued
-- again, one queued already stays as it is.
INSERT INTO deliveries (device_id, book_id)
SELECT d.id, b.id
FROM devices d
CROSS JOIN unnest(sqlc.arg(ids)::uuid[]) AS n(id)
JOIN books b ON b.id = n.id
WHERE d.id = sqlc.arg(device_id) AND d.user_id = sqlc.arg(viewer)::uuid
  AND b.deleted_at IS NULL AND NOT b.placeholder
  AND b.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean)) AND (b.age_rating IS NULL OR b.age_rating <= (SELECT age_limit(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean)))
  AND EXISTS (SELECT 1 FROM book_files f
              WHERE f.book_id = b.id AND f.kind = 'ebook' AND f.trashed_at IS NULL AND f.missing_at IS NULL)
ON CONFLICT (device_id, book_id) DO UPDATE
SET manual = true,
    queued_at = CASE WHEN deliveries.delivered_at IS NOT NULL THEN now() ELSE deliveries.queued_at END,
    delivered_at = NULL
WHERE deliveries.delivered_at IS NOT NULL OR NOT deliveries.manual;

-- name: OwnsDevice :one
SELECT EXISTS (SELECT 1 FROM devices WHERE id = $1 AND user_id = $2);

-- name: ListBookDeliveries :many
-- The book's deliveries to the user's devices, the latest first.
SELECT dl.id, dl.device_id, d.name AS device_name, dl.queued_at, dl.delivered_at
FROM deliveries dl
JOIN devices d ON d.id = dl.device_id
WHERE dl.book_id = $1 AND d.user_id = $2
ORDER BY dl.queued_at DESC;

-- name: TakeBackDelivery :execrows
-- Takes a book off one of the user's devices: out of its queue before the
-- device has it, or off a Kobo reader, whose next sync removes it.
DELETE FROM deliveries dl
USING devices d
WHERE dl.id = $1 AND d.id = dl.device_id AND d.user_id = $2
  AND (dl.delivered_at IS NULL
       OR EXISTS (SELECT 1 FROM kobo_books k WHERE k.device_id = dl.device_id AND k.book_id = dl.book_id));

-- name: ListDeviceBooks :many
-- The books of one of the user's devices that the user still sees, waiting
-- or delivered, with when a Kobo reader was told of each; the last sent
-- first, after the cursor.
SELECT dl.id, dl.book_id, dl.queued_at, dl.delivered_at, k.synced_at,
       (coalesce(sd.fixed, false) AND dl.delivered_at IS NOT NULL)::boolean AS sent_fixed
FROM deliveries dl
JOIN devices d ON d.id = dl.device_id
JOIN books b ON b.id = dl.book_id
LEFT JOIN kobo_books k ON k.device_id = dl.device_id AND k.book_id = dl.book_id
LEFT JOIN device_sends sd ON sd.device_id = dl.device_id AND sd.book_id = dl.book_id
WHERE dl.device_id = sqlc.arg(device_id) AND d.user_id = sqlc.arg(viewer)::uuid
  AND b.deleted_at IS NULL
  AND b.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean)) AND (b.age_rating IS NULL OR b.age_rating <= (SELECT age_limit(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean)))
  AND (sqlc.narg(before_at)::timestamptz IS NULL OR (dl.queued_at, dl.id) < (sqlc.narg(before_at)::timestamptz, sqlc.narg(before_id)::uuid))
ORDER BY dl.queued_at DESC, dl.id DESC
LIMIT sqlc.arg(max_rows);

-- name: ListQueuedBooks :many
-- The books queued for the device that its user still sees, the first sent
-- first.
SELECT dl.book_id
FROM deliveries dl
JOIN books b ON b.id = dl.book_id
WHERE dl.device_id = sqlc.arg(device_id) AND dl.delivered_at IS NULL
  AND b.deleted_at IS NULL
  AND b.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean)) AND (b.age_rating IS NULL OR b.age_rating <= (SELECT age_limit(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean)))
ORDER BY dl.queued_at
LIMIT 200;

-- name: MarkDelivered :execrows
-- The device has downloaded the book: it leaves the queue, once.
UPDATE deliveries SET delivered_at = now()
WHERE device_id = $1 AND book_id = $2 AND delivered_at IS NULL;

-- name: RecordOnDevice :execrows
-- The device has the book, whether it was sent there or the device took it
-- itself; a book the viewer does not see is not recorded.
INSERT INTO deliveries (device_id, book_id, delivered_at)
SELECT sqlc.arg(device_id), b.id, now()
FROM books b
WHERE b.id = sqlc.arg(book_id) AND b.deleted_at IS NULL AND NOT b.placeholder
  AND b.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean)) AND (b.age_rating IS NULL OR b.age_rating <= (SELECT age_limit(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean)))
ON CONFLICT (device_id, book_id) DO UPDATE
SET delivered_at = coalesce(deliveries.delivered_at, now());

-- name: ForgetOnDevice :exec
-- The book is no longer on the device, or no longer waits for it.
DELETE FROM deliveries WHERE device_id = $1 AND book_id = $2;

-- name: MatchDeviceFiles :many
-- The visible books that have a live file with one of the hashes, one book
-- for each hash, recorded as on the device in the same statement.
WITH hits AS (
    SELECT DISTINCT ON (m.hash) m.hash, m.book_id, m.file_id
    FROM (
        SELECT h.hash, f.book_id, f.id AS file_id
        FROM unnest(sqlc.arg(hashes)::bytea[]) AS h(hash)
        JOIN book_files f ON f.sha256 = h.hash AND f.trashed_at IS NULL
        UNION ALL
        SELECT h.hash, f.book_id, f.id
        FROM unnest(sqlc.arg(hashes)::bytea[]) AS h(hash)
        JOIN book_files f ON f.original_sha256 = h.hash AND f.trashed_at IS NULL
        UNION ALL
        SELECT h.hash, f.book_id, f.id
        FROM unnest(sqlc.arg(hashes)::bytea[]) AS h(hash)
        JOIN device_copies c ON c.sha256 = h.hash
        JOIN book_files f ON f.sha256 = c.source_sha256 AND f.trashed_at IS NULL
    ) m
    JOIN books b ON b.id = m.book_id AND b.deleted_at IS NULL AND NOT b.placeholder
    WHERE b.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean)) AND (b.age_rating IS NULL OR b.age_rating <= (SELECT age_limit(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean)))
    ORDER BY m.hash, m.file_id
), recorded AS (
    INSERT INTO deliveries (device_id, book_id, delivered_at)
    SELECT sqlc.arg(device_id), x.book_id, now()
    FROM (SELECT DISTINCT book_id FROM hits) x
    ON CONFLICT (device_id, book_id) DO UPDATE
    SET delivered_at = coalesce(deliveries.delivered_at, now())
    RETURNING book_id
)
SELECT hits.hash::bytea AS hash, hits.book_id, hits.file_id FROM hits;

-- name: ListDeviceSources :many
-- The sources of one of the user's devices, with the name of the shelf.
SELECT s.id, s.kind, s.collection_id, s.smart_shelf_id,
       coalesce(c.name, ss.name, '')::text AS name
FROM device_sources s
JOIN devices d ON d.id = s.device_id
LEFT JOIN collections c ON c.id = s.collection_id
LEFT JOIN smart_shelves ss ON ss.id = s.smart_shelf_id
WHERE s.device_id = $1 AND d.user_id = $2
ORDER BY s.created_at, s.id;

-- name: ClearDeviceSources :exec
DELETE FROM device_sources s USING devices d
WHERE s.device_id = $1 AND d.id = s.device_id AND d.user_id = $2;

-- name: AddDeviceSource :exec
INSERT INTO device_sources (device_id, kind, collection_id, smart_shelf_id)
SELECT d.id, sqlc.arg(kind), sqlc.narg(collection_id), sqlc.narg(smart_shelf_id)
FROM devices d WHERE d.id = sqlc.arg(device_id) AND d.user_id = sqlc.arg(viewer)::uuid
ON CONFLICT DO NOTHING;

-- name: DeleteDeviceSourcesNotIn :exec
DELETE FROM device_sources s USING devices d
WHERE s.device_id = $1 AND d.id = s.device_id AND d.user_id = $2 AND NOT (s.id = ANY(sqlc.arg(keep)::uuid[]));

-- name: ListUsersWithDeviceSources :many
SELECT DISTINCT d.user_id FROM device_sources s JOIN devices d ON d.id = s.device_id;

-- name: ListUserDeviceSources :many
-- Every source of the user's devices, for a pass.
SELECT s.id, s.device_id, s.kind, s.collection_id, s.smart_shelf_id
FROM device_sources s JOIN devices d ON d.id = s.device_id
WHERE d.user_id = $1
ORDER BY s.created_at, s.id;

-- name: SourceDeliveries :exec
-- Queues the books among ids the viewer may see, with an ebook on disk, for
-- the device as the source's, and records the source as their origin; and
-- drops the source's origin from every other book of the device.
WITH wanted AS (
    SELECT b.id
    FROM unnest(sqlc.arg(ids)::uuid[]) AS n(id)
    JOIN books b ON b.id = n.id
    WHERE b.deleted_at IS NULL AND NOT b.placeholder
      AND b.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean)) AND (b.age_rating IS NULL OR b.age_rating <= (SELECT age_limit(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean)))
      AND EXISTS (SELECT 1 FROM book_files f
                  WHERE f.book_id = b.id AND f.kind = 'ebook' AND f.trashed_at IS NULL AND f.missing_at IS NULL)
), queued AS (
    INSERT INTO deliveries (device_id, book_id, manual)
    SELECT sqlc.arg(device_id), w.id, false FROM wanted w
    ON CONFLICT (device_id, book_id) DO NOTHING
), dropped AS (
    DELETE FROM delivery_origins o USING deliveries dl
    WHERE o.source_id = sqlc.arg(source_id) AND dl.id = o.delivery_id
      AND dl.book_id NOT IN (SELECT id FROM wanted)
)
SELECT 1;

-- name: SourceOrigins :execrows
-- Records the source as the origin of the device's deliveries of the books
-- among ids that are on its list.
INSERT INTO delivery_origins (delivery_id, source_id)
SELECT dl.id, sqlc.arg(source_id)
FROM deliveries dl
JOIN books b ON b.id = dl.book_id
WHERE dl.device_id = sqlc.arg(device_id) AND dl.book_id = ANY(sqlc.arg(ids)::uuid[])
  AND b.deleted_at IS NULL AND NOT b.placeholder
  AND b.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean)) AND (b.age_rating IS NULL OR b.age_rating <= (SELECT age_limit(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean)))
  AND EXISTS (SELECT 1 FROM book_files f
              WHERE f.book_id = b.id AND f.kind = 'ebook' AND f.trashed_at IS NULL AND f.missing_at IS NULL)
ON CONFLICT DO NOTHING;

-- name: DropUnsourcedDeliveries :execrows
-- Takes off the user's devices what only a source put there and none holds
-- any more: out of the queue, or off a Kobo reader at its next sync.
-- Others keep what they downloaded.
DELETE FROM deliveries dl USING devices d
WHERE d.id = dl.device_id AND d.user_id = $1
  AND NOT dl.manual
  AND NOT EXISTS (SELECT 1 FROM delivery_origins o WHERE o.delivery_id = dl.id)
  AND (dl.delivered_at IS NULL
       OR EXISTS (SELECT 1 FROM kobo_books k WHERE k.device_id = dl.device_id AND k.book_id = dl.book_id));

-- name: ListDeliveryOrigins :many
-- Where each of the deliveries among ids came from: the shelf's name, or
-- the list. A shelf the viewer may no longer look at is not named.
SELECT dl.id AS delivery_id, s.kind, coalesce(c.name, ss.name, '')::text AS name
FROM deliveries dl
JOIN delivery_origins o ON o.delivery_id = dl.id
JOIN device_sources s ON s.id = o.source_id
LEFT JOIN collections c ON c.id = s.collection_id
LEFT JOIN smart_shelves ss ON ss.id = s.smart_shelf_id
WHERE dl.id = ANY(sqlc.arg(ids)::uuid[])
  AND (c.id IS NULL OR c.owner_id = sqlc.arg(viewer)::uuid OR c.visibility = 'shared')
  AND (ss.id IS NULL OR ss.owner_id = sqlc.arg(viewer)::uuid OR ss.visibility = 'shared')
ORDER BY dl.id, s.created_at;

-- name: GetSourceUser :one
-- The role of an active account, for the scope its devices' sources are
-- worked out in.
SELECT role FROM users WHERE id = $1 AND disabled_at IS NULL;
