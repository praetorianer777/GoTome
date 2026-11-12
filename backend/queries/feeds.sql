-- name: PutFeedTracker :one
-- The user's tracker of the address, made or given the new filters, and
-- fetched as soon as can be.
INSERT INTO feed_trackers (user_id, url, keywords, authors)
VALUES (sqlc.arg(user_id)::uuid, sqlc.arg(url)::text, sqlc.arg(keywords)::text[], sqlc.arg(authors)::text[])
ON CONFLICT (user_id, url) DO UPDATE SET
    keywords = EXCLUDED.keywords, authors = EXCLUDED.authors, next_fetch_at = now()
RETURNING *;

-- name: ListFeedTrackers :many
SELECT * FROM feed_trackers WHERE user_id = $1 ORDER BY lower(coalesce(title, url)), id;

-- name: DeleteFeedTracker :one
WITH gone AS (
    DELETE FROM feed_trackers WHERE id = sqlc.arg(id)::uuid AND user_id = sqlc.arg(user_id)::uuid RETURNING id
)
SELECT count(*)::int AS deleted FROM gone;

-- name: RetryFeedTracker :one
-- The user's tracker, to be fetched as soon as can be, even one that
-- failed too often.
UPDATE feed_trackers SET next_fetch_at = now()
WHERE id = sqlc.arg(id)::uuid AND user_id = sqlc.arg(user_id)::uuid
RETURNING *;

-- name: NextDueFeed :one
-- The tracker that has waited longest to be fetched, if one is due.
SELECT * FROM feed_trackers
WHERE next_fetch_at <= now()
ORDER BY next_fetch_at, id
LIMIT 1;

-- name: LockFeedTracker :one
-- The tracker's filters, held while what its feed answered is recorded, or
-- no row once its owner ended it.
SELECT keywords, authors FROM feed_trackers WHERE id = $1 FOR UPDATE;

-- name: FeedAnswered :exec
-- The feed answered, with items or as unchanged; a title, ETag or date of
-- NULL keeps what was there.
UPDATE feed_trackers SET
    title         = coalesce(sqlc.narg(title)::text, title),
    etag          = coalesce(sqlc.narg(etag)::text, etag),
    last_modified = coalesce(sqlc.narg(last_modified)::text, last_modified),
    checked_at    = now(),
    failures      = 0,
    last_error    = NULL,
    next_fetch_at = sqlc.arg(next_fetch_at)::timestamptz
WHERE id = sqlc.arg(id)::uuid;

-- name: FeedFailed :exec
-- The feed could not be fetched or read; a next time of NULL stops it.
UPDATE feed_trackers SET
    failures      = sqlc.arg(failures)::int,
    last_error    = sqlc.arg(last_error)::text,
    next_fetch_at = sqlc.narg(next_fetch_at)::timestamptz
WHERE id = sqlc.arg(id)::uuid;

-- name: PutFeedItem :one
-- An item the feed lists; says whether it is new. One listed before is
-- matched again, as the filters may have changed since.
INSERT INTO feed_items (tracker_id, item_key, title, link, authors, published_at, match_text, matched, backlog)
VALUES (sqlc.arg(tracker_id)::uuid, sqlc.arg(item_key)::text, sqlc.arg(title)::text, sqlc.narg(link)::text,
        sqlc.arg(authors)::text[], sqlc.narg(published_at)::timestamptz, sqlc.arg(match_text)::text,
        sqlc.arg(matched)::boolean, sqlc.arg(backlog)::boolean)
ON CONFLICT (tracker_id, item_key) DO UPDATE SET
    last_seen_at = now(), match_text = EXCLUDED.match_text, matched = EXCLUDED.matched
RETURNING (xmax = 0)::boolean AS inserted;

-- name: ListFeedItemsToMatch :many
-- What the tracker's items are matched by, for filters that changed.
SELECT item_key, title, authors, match_text FROM feed_items
WHERE tracker_id = $1;

-- name: SetFeedItemsMatched :exec
UPDATE feed_items i SET matched = m.matched
FROM (SELECT unnest(sqlc.arg(item_keys)::text[]) AS item_key, unnest(sqlc.arg(matched)::boolean[]) AS matched) m
WHERE i.tracker_id = sqlc.arg(tracker_id)::uuid AND i.item_key = m.item_key AND i.matched <> m.matched;

-- name: PruneFeedItems :exec
-- Items the feed has not listed for a long time: it will not list them
-- again.
DELETE FROM feed_items
WHERE tracker_id = sqlc.arg(tracker_id)::uuid AND last_seen_at < sqlc.arg(before)::timestamptz;

-- name: ListFeedItems :many
-- The items of the user's feeds that name what their filters ask for,
-- the newest first.
SELECT i.tracker_id, i.item_key, i.title, i.link, i.authors, i.published_at, i.first_seen_at,
       coalesce(t.title, t.url)::text AS feed
FROM feed_items i
JOIN feed_trackers t ON t.id = i.tracker_id
WHERE t.user_id = sqlc.arg(user_id)::uuid AND i.matched
ORDER BY coalesce(i.published_at, i.first_seen_at) DESC, i.item_key
LIMIT 100;
