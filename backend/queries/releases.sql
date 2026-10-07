-- name: PutReleaseSubject :one
-- The subject of that kind and key, made if nobody followed it before; it
-- keeps the spelling it was first followed by.
INSERT INTO release_subjects (kind, name, name_key)
VALUES (sqlc.arg(kind)::text, sqlc.arg(name)::text, sqlc.arg(name_key)::text)
ON CONFLICT (kind, name_key) DO UPDATE SET kind = EXCLUDED.kind
RETURNING id, kind, name, polled_at;

-- name: PutTracker :one
INSERT INTO trackers (user_id, subject_id)
VALUES (sqlc.arg(user_id)::uuid, sqlc.arg(subject_id)::uuid)
ON CONFLICT (user_id, subject_id) DO UPDATE SET user_id = EXCLUDED.user_id
RETURNING id, created_at;

-- name: DeleteTracker :one
-- The caller's tracker gone, and its subject with what was found for it
-- once nobody follows that any more.
WITH gone AS (
    DELETE FROM trackers WHERE id = sqlc.arg(id)::uuid AND user_id = sqlc.arg(user_id)::uuid
    RETURNING subject_id
), orphan AS (
    DELETE FROM release_subjects s
    USING gone g
    WHERE s.id = g.subject_id
      AND NOT EXISTS (SELECT 1 FROM trackers t WHERE t.subject_id = s.id AND t.id <> sqlc.arg(id)::uuid)
)
SELECT count(*)::int AS deleted FROM gone;

-- name: ListTrackers :many
SELECT t.id, s.kind, s.name, t.created_at, s.polled_at
FROM trackers t
JOIN release_subjects s ON s.id = t.subject_id
WHERE t.user_id = sqlc.arg(user_id)::uuid
ORDER BY s.kind, lower(s.name);

-- name: NextReleaseSubject :one
-- The followed subject asked about longest ago, if that is before the
-- given time.
SELECT s.id, s.kind, s.name
FROM release_subjects s
WHERE (s.polled_at IS NULL OR s.polled_at < sqlc.arg(before)::timestamptz)
  AND EXISTS (SELECT 1 FROM trackers t WHERE t.subject_id = s.id)
ORDER BY s.polled_at NULLS FIRST, s.id
LIMIT 1;

-- name: MarkReleaseSubjectPolled :exec
UPDATE release_subjects SET polled_at = now() WHERE id = $1;

-- name: PutReleasePoll :one
-- Records that the provider answered about the subject, and says whether
-- it was its first answer.
INSERT INTO release_polls (subject_id, provider)
VALUES (sqlc.arg(subject_id)::uuid, sqlc.arg(provider)::text)
ON CONFLICT (subject_id, provider) DO UPDATE SET last_at = now()
RETURNING (xmax = 0)::boolean AS first;

-- name: PutRelease :one
-- A book a provider listed for the subject. One listed already, by this
-- provider or another, is the same release, which takes a more exact date,
-- or a moved one from the provider that first listed it, and what it
-- lacked. Says whether it is new.
INSERT INTO releases (subject_id, dedupe_key, title, authors, author_keys, series, series_index,
                      release_date, precision, isbns, provider, provider_id, cover_url, backlog)
VALUES (sqlc.arg(subject_id)::uuid, sqlc.arg(dedupe_key)::text, sqlc.arg(title)::text,
        sqlc.arg(authors)::text[], sqlc.arg(author_keys)::text[],
        sqlc.narg(series)::text, sqlc.narg(series_index)::float8,
        sqlc.narg(release_date)::date, sqlc.narg(precision)::text, sqlc.arg(isbns)::text[],
        sqlc.arg(provider)::text, sqlc.arg(provider_id)::text, sqlc.narg(cover_url)::text,
        sqlc.arg(backlog)::boolean)
ON CONFLICT (subject_id, dedupe_key) DO UPDATE SET
    release_date = CASE WHEN precision_rank(EXCLUDED.precision) > precision_rank(releases.precision)
                          OR (EXCLUDED.provider = releases.provider AND EXCLUDED.precision = releases.precision)
                        THEN EXCLUDED.release_date ELSE releases.release_date END,
    precision    = CASE WHEN precision_rank(EXCLUDED.precision) > precision_rank(releases.precision)
                        THEN EXCLUDED.precision ELSE releases.precision END,
    series       = coalesce(releases.series, EXCLUDED.series),
    series_index = coalesce(releases.series_index, EXCLUDED.series_index),
    cover_url    = coalesce(releases.cover_url, EXCLUDED.cover_url),
    isbns        = ARRAY(SELECT DISTINCT unnest(releases.isbns || EXCLUDED.isbns)),
    author_keys  = ARRAY(SELECT DISTINCT unnest(releases.author_keys || EXCLUDED.author_keys))
RETURNING id, (xmax = 0)::boolean AS inserted, backlog, release_date, precision, first_seen_at;

-- name: QueueReleaseNotices :many
-- Who is to be told that the release reached the stage, each once, and
-- remembers it: an active person following its subject since before the
-- given time, not told of the same book through another subject they
-- follow, and without a book in a library they see that is the release:
-- the same title and an author, or an ISBN.
INSERT INTO release_notices (user_id, release_id, stage)
SELECT t.user_id, r.id, sqlc.arg(stage)::text
FROM releases r
JOIN trackers t ON t.subject_id = r.subject_id
JOIN users u ON u.id = t.user_id
WHERE r.id = sqlc.arg(release_id)::uuid
  AND u.disabled_at IS NULL
  AND t.created_at < sqlc.arg(followed_before)::timestamptz
  AND NOT EXISTS (
      SELECT 1 FROM release_notices n
      JOIN releases o ON o.id = n.release_id
      WHERE n.user_id = t.user_id AND n.stage = sqlc.arg(stage)::text AND o.dedupe_key = r.dedupe_key)
  -- Two tests rather than one with OR, so that each finds its books by
  -- an index instead of reading them all.
  AND NOT EXISTS (
      SELECT 1 FROM books b
      WHERE b.title_key = r.dedupe_key AND b.deleted_at IS NULL AND NOT b.placeholder
        AND b.library_id IN (SELECT * FROM visible_library_ids(u.id, u.role = ANY(sqlc.arg(sees_all_roles)::text[])))
        AND (cardinality(r.author_keys) = 0 OR EXISTS (
            SELECT 1 FROM book_contributors c JOIN authors a ON a.id = c.author_id
            WHERE c.book_id = b.id AND a.name_key = ANY(r.author_keys))))
  AND NOT EXISTS (
      SELECT 1 FROM book_identifiers i
      JOIN books b ON b.id = i.book_id
      WHERE i.type = 'isbn' AND i.value = ANY(r.isbns) AND b.deleted_at IS NULL AND NOT b.placeholder
        AND b.library_id IN (SELECT * FROM visible_library_ids(u.id, u.role = ANY(sqlc.arg(sees_all_roles)::text[]))))
ON CONFLICT DO NOTHING
RETURNING user_id;

-- name: ListDueReleases :many
-- Releases whose day came within the last week, for telling that they
-- are out; those told already are skipped by QueueReleaseNotices.
SELECT r.id, r.release_date, r.title, r.authors, s.name AS subject
FROM releases r
JOIN release_subjects s ON s.id = r.subject_id
WHERE r.precision = 'day'
  AND r.release_date <= current_date
  AND r.release_date > current_date - 7;

-- name: ListReleases :many
-- The books of the subjects the viewer follows, upcoming (later than
-- today, or with no date and not of the subject's past) or out within the
-- last year, each book once; in_library says whether a library they see
-- has it.
WITH mine AS (
    SELECT DISTINCT ON (r.dedupe_key) r.*, s.kind AS subject_kind, s.name AS subject_name
    FROM releases r
    JOIN trackers t ON t.subject_id = r.subject_id AND t.user_id = sqlc.arg(viewer)::uuid
    JOIN release_subjects s ON s.id = r.subject_id
    WHERE CASE WHEN sqlc.arg(upcoming)::boolean
               THEN r.release_date > current_date OR (r.release_date IS NULL AND NOT r.backlog)
               ELSE r.release_date <= current_date AND r.release_date > current_date - 365 END
    ORDER BY r.dedupe_key, s.kind, r.id
)
SELECT m.id, m.title, m.authors, m.series, m.series_index, m.release_date, m.precision,
       m.provider, m.cover_url, m.subject_kind, m.subject_name,
       (EXISTS (
            SELECT 1 FROM books b
            WHERE b.title_key = m.dedupe_key AND b.deleted_at IS NULL AND NOT b.placeholder
              AND b.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean))
              AND (cardinality(m.author_keys) = 0 OR EXISTS (
                  SELECT 1 FROM book_contributors c JOIN authors a ON a.id = c.author_id
                  WHERE c.book_id = b.id AND a.name_key = ANY(m.author_keys))))
        OR EXISTS (
            SELECT 1 FROM book_identifiers i
            JOIN books b ON b.id = i.book_id
            WHERE i.type = 'isbn' AND i.value = ANY(m.isbns) AND b.deleted_at IS NULL AND NOT b.placeholder
              AND b.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean)))
       )::boolean AS in_library
FROM mine m
ORDER BY CASE WHEN sqlc.arg(upcoming)::boolean THEN m.release_date END ASC NULLS LAST,
         CASE WHEN NOT sqlc.arg(upcoming)::boolean THEN m.release_date END DESC,
         lower(m.title)
LIMIT 200;
