-- name: ListDiscoverTaste :many
-- The books the viewer finished or rated that they still see, the best
-- rated first and then the latest finished, up to max, with what Discover
-- asks the providers about: their authors, series and genres (those of its
-- tags and the ones a person accepted). Placeholders are among them: a
-- book read but not held is taste all the same.
SELECT b.id AS book_id, b.title, ub.status, ub.rating,
       coalesce((SELECT array_agg(a.name ORDER BY c.position)
                 FROM book_contributors c JOIN authors a ON a.id = c.author_id
                 WHERE c.book_id = b.id AND c.role = 'author'), '{}')::text[] AS authors,
       coalesce((SELECT array_agg(g.genre)
                 FROM book_genres g
                 WHERE g.book_id = b.id AND (g.state = 'accepted' OR (g.state = 'suggested' AND g.source = 'tags'))), '{}')::text[] AS genres
FROM (
    SELECT u.book_id, u.status, u.rating, u.finished_on
    FROM user_books u
    WHERE u.user_id = sqlc.arg(viewer)::uuid AND (u.status = 'completed' OR u.rating IS NOT NULL)
    ORDER BY coalesce(u.rating, 3) DESC, u.finished_on DESC NULLS LAST, u.book_id
    LIMIT sqlc.arg(max)::int
) ub
JOIN books b ON b.id = ub.book_id AND b.deleted_at IS NULL
WHERE b.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean)) AND (b.age_rating IS NULL OR b.age_rating <= (SELECT age_limit(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean)))
ORDER BY coalesce(ub.rating, 3) DESC, ub.finished_on DESC NULLS LAST, b.id;

-- name: ListDiscoverSeries :many
-- The series the viewer reads, the latest touched first: where they stand
-- (the highest place of a book finished or being read) and the places the
-- libraries they see hold, for the gaps among them.
SELECT s.name, s.name_key,
       coalesce(max(b.series_index) FILTER (WHERE ub.status IN ('completed', 'reading')), 0)::float8 AS last_read,
       coalesce(array_agg(b.series_index) FILTER (WHERE NOT b.placeholder AND b.series_index IS NOT NULL), '{}')::float8[] AS held,
       max(ub.updated_at)::timestamptz AS touched
FROM series s
JOIN books b ON b.series_id = s.id AND b.deleted_at IS NULL
    AND b.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean)) AND (b.age_rating IS NULL OR b.age_rating <= (SELECT age_limit(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean)))
LEFT JOIN user_books ub ON ub.book_id = b.id AND ub.user_id = sqlc.arg(viewer)::uuid
WHERE s.id IN (
    SELECT r.series_id FROM books r
    JOIN user_books ru ON ru.book_id = r.id AND ru.user_id = sqlc.arg(viewer)::uuid
    WHERE r.series_id IS NOT NULL AND r.deleted_at IS NULL
      AND ru.status IN ('completed', 'reading', 'abandoned')
      AND r.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean)) AND (r.age_rating IS NULL OR r.age_rating <= (SELECT age_limit(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean))))
GROUP BY s.id
ORDER BY max(ub.updated_at) DESC NULLS LAST, s.id
LIMIT sqlc.arg(max)::int;

-- name: ListKnownReleases :many
-- What the providers listed of a followed subject, if it was asked about
-- since the given time: Discover does not ask again what following knows.
SELECT r.title, r.authors, r.author_keys, r.series, r.series_index, r.release_date, r.precision,
       r.isbns, r.provider, r.provider_id, r.cover_url
FROM releases r
JOIN release_subjects s ON s.id = r.subject_id
WHERE s.kind = sqlc.arg(kind)::text AND s.name_key = sqlc.arg(name_key)::text
  AND s.polled_at IS NOT NULL AND s.polled_at >= sqlc.arg(since)::timestamptz
ORDER BY r.release_date DESC NULLS LAST, r.id;

-- name: ListDiscoverHeld :many
-- Which of the candidates the viewer has no use for, and why. A candidate
-- is given as texts "index|key" of its titles' keys, its authors' keys and
-- its ISBNs, by an index of its own (keys hold no bar). It is: owned, when a book of a library the viewer
-- sees is it (an ISBN, or a title's key and an author, in either order);
-- wished or read, when it is a book of the viewer's own (a wish, a book in
-- their history, held or not); dismissed, when they said they are not
-- interested, in Discover or in the recommendations of a book it is.
-- The first reason in that order is the one answered.
WITH title AS (
    SELECT split_part(t.v, '|', 1)::int AS idx, split_part(t.v, '|', 2) AS key
    FROM unnest(sqlc.arg(titles)::text[]) t(v)
), author AS (
    SELECT split_part(t.v, '|', 1)::int AS idx, split_part(t.v, '|', 2) AS key
    FROM unnest(sqlc.arg(authors)::text[]) t(v)
), isbn AS (
    SELECT split_part(t.v, '|', 1)::int AS idx, split_part(t.v, '|', 2) AS value
    FROM unnest(sqlc.arg(isbns)::text[]) t(v)
), same AS (
    -- Books of the same title as a candidate, with an author in common
    -- or, where the candidate names none, without one.
    SELECT t.idx, b.id AS book_id, b.library_id, b.placeholder, b.age_rating
    FROM title t
    JOIN books b ON b.title_key = t.key AND b.deleted_at IS NULL
    WHERE NOT EXISTS (SELECT 1 FROM author a WHERE a.idx = t.idx)
       OR EXISTS (
            SELECT 1 FROM book_contributors c
            JOIN authors au ON au.id = c.author_id
            JOIN author a ON a.idx = t.idx AND (au.name_key = a.key OR au.person_key = a.key)
            WHERE c.book_id = b.id)
), reasons AS (
    SELECT s.idx, 1 AS rank, 'owned'::text AS reason
    FROM same s
    WHERE NOT s.placeholder
      AND s.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean)) AND (s.age_rating IS NULL OR s.age_rating <= (SELECT age_limit(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean)))
    UNION ALL
    SELECT i.idx, 1, 'owned'
    FROM isbn i
    JOIN book_identifiers bi ON bi.type = 'isbn' AND bi.value = i.value
    JOIN books b ON b.id = bi.book_id AND b.deleted_at IS NULL AND NOT b.placeholder
    WHERE b.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean)) AND (b.age_rating IS NULL OR b.age_rating <= (SELECT age_limit(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean)))
    UNION ALL
    SELECT s.idx, 2, CASE WHEN ub.status = 'wishlist' THEN 'wished' ELSE 'read' END
    FROM same s
    JOIN user_books ub ON ub.book_id = s.book_id AND ub.user_id = sqlc.arg(viewer)::uuid
        AND (ub.status <> 'unread' OR ub.rating IS NOT NULL)
    UNION ALL
    SELECT i.idx, 2, CASE WHEN ub.status = 'wishlist' THEN 'wished' ELSE 'read' END
    FROM isbn i
    JOIN book_identifiers bi ON bi.type = 'isbn' AND bi.value = i.value
    JOIN user_books ub ON ub.book_id = bi.book_id AND ub.user_id = sqlc.arg(viewer)::uuid
        AND (ub.status <> 'unread' OR ub.rating IS NOT NULL)
    UNION ALL
    SELECT s.idx, 3, 'dismissed'
    FROM same s
    JOIN recommendation_dismissals d ON d.book_id = s.book_id AND d.user_id = sqlc.arg(viewer)::uuid
    UNION ALL
    SELECT t.idx, 3, 'dismissed'
    FROM title t
    JOIN discover_dismissals d ON d.user_id = sqlc.arg(viewer)::uuid AND t.key = ANY(d.title_keys)
    WHERE cardinality(d.author_keys) = 0
       OR NOT EXISTS (SELECT 1 FROM author a WHERE a.idx = t.idx)
       OR EXISTS (SELECT 1 FROM author a WHERE a.idx = t.idx AND a.key = ANY(d.author_keys))
)
SELECT DISTINCT ON (r.idx) r.idx, r.reason
FROM reasons r
ORDER BY r.idx, r.rank;

-- name: GetDiscoverRun :one
SELECT user_id, started_at, finished_at, requested, planned, planned_authors, planned_series, planned_genres, pass_until
FROM discover_runs WHERE user_id = $1;

-- name: StartDiscoverRun :exec
-- A run begins afresh; the candidates of the last one stay until this one
-- is finished.
INSERT INTO discover_runs (user_id, started_at, finished_at, requested)
VALUES ($1, now(), NULL, false)
ON CONFLICT (user_id) DO UPDATE
SET started_at = now(), finished_at = NULL, requested = false,
    planned = false, planned_authors = 0, planned_series = 0, planned_genres = 0, pass_until = NULL;

-- name: HoldDiscoverRun :exec
-- A pass takes the run until its deadline and says what the plan holds.
UPDATE discover_runs
SET pass_until = sqlc.arg(until), planned = true, planned_authors = sqlc.arg(authors),
    planned_series = sqlc.arg(series), planned_genres = sqlc.arg(genres)
WHERE user_id = sqlc.arg(user_id);

-- name: ReleaseDiscoverRun :exec
UPDATE discover_runs SET pass_until = NULL WHERE user_id = $1;

-- name: CountDiscoverAsked :many
SELECT kind, count(*)::int AS asked FROM discover_asks
WHERE user_id = $1 AND asked_at >= $2 GROUP BY kind;


-- name: FinishDiscoverRun :exec
UPDATE discover_runs SET finished_at = now() WHERE user_id = $1;

-- name: RequestDiscoverRun :exec
-- Asks for a run now: a person's request, from the page. A run in progress
-- goes on and is not asked for again.
INSERT INTO discover_runs (user_id, started_at, finished_at, requested)
VALUES ($1, now(), now() - interval '10 years', true)
ON CONFLICT (user_id) DO UPDATE SET requested = discover_runs.finished_at IS NOT NULL;

-- name: ListDiscoverDue :many
-- The people whose run is asked for, in progress, or a day old, the
-- longest waiting first.
SELECT r.user_id, u.role
FROM discover_runs r
JOIN users u ON u.id = r.user_id AND u.disabled_at IS NULL
WHERE r.finished_at IS NULL OR r.requested OR r.finished_at < sqlc.arg(before)::timestamptz
ORDER BY coalesce(r.finished_at, '-infinity'), r.user_id;

-- name: ListDiscoverAsked :many
SELECT kind, name_key FROM discover_asks WHERE user_id = $1 AND asked_at >= $2;

-- name: PutDiscoverAsk :exec
INSERT INTO discover_asks (user_id, kind, name_key) VALUES ($1, $2, $3)
ON CONFLICT (user_id, kind, name_key) DO UPDATE SET asked_at = now();

-- name: PutDiscoverCandidate :exec
-- A candidate found again keeps the best score and the reason that gave it.
INSERT INTO discover_candidates (user_id, provider, provider_id, work, title, title_keys, author_keys, isbns, record,
                                 score, reason_kind, reason_name, reason_stars, because, found_at)
VALUES (sqlc.arg(user_id), sqlc.arg(provider), sqlc.arg(provider_id), sqlc.arg(work), sqlc.arg(title),
        sqlc.arg(title_keys), sqlc.arg(author_keys), sqlc.arg(isbns), sqlc.arg(record),
        sqlc.arg(score), sqlc.arg(reason_kind), sqlc.arg(reason_name), sqlc.arg(reason_stars), sqlc.arg(because), now())
ON CONFLICT (user_id, provider, provider_id) DO UPDATE
SET found_at = now(),
    record = EXCLUDED.record,
    score = GREATEST(discover_candidates.score, EXCLUDED.score),
    reason_kind = CASE WHEN EXCLUDED.score > discover_candidates.score OR discover_candidates.found_at < sqlc.arg(run_started)::timestamptz
                       THEN EXCLUDED.reason_kind ELSE discover_candidates.reason_kind END,
    reason_name = CASE WHEN EXCLUDED.score > discover_candidates.score OR discover_candidates.found_at < sqlc.arg(run_started)::timestamptz
                       THEN EXCLUDED.reason_name ELSE discover_candidates.reason_name END,
    reason_stars = CASE WHEN EXCLUDED.score > discover_candidates.score OR discover_candidates.found_at < sqlc.arg(run_started)::timestamptz
                        THEN EXCLUDED.reason_stars ELSE discover_candidates.reason_stars END,
    because = CASE WHEN EXCLUDED.score > discover_candidates.score OR discover_candidates.found_at < sqlc.arg(run_started)::timestamptz
                   THEN EXCLUDED.because ELSE discover_candidates.because END;

-- name: ListDiscoverCandidates :many
SELECT id, provider, provider_id, work, title, title_keys, author_keys, isbns, record, score,
       reason_kind, reason_name, reason_stars, because, found_at
FROM discover_candidates
WHERE user_id = $1
ORDER BY score DESC, id;

-- name: DeleteDiscoverCandidates :exec
DELETE FROM discover_candidates WHERE user_id = sqlc.arg(user_id) AND id = ANY(sqlc.arg(ids)::uuid[]);

-- name: DeleteStaleDiscoverCandidates :exec
-- What a finished run did not find again is no longer a candidate.
DELETE FROM discover_candidates WHERE user_id = $1 AND found_at < $2;

-- name: GetDiscoverCandidate :one
SELECT id, title, title_keys, author_keys FROM discover_candidates WHERE id = $1 AND user_id = $2;

-- name: PutDiscoverDismissal :exec
INSERT INTO discover_dismissals (user_id, title_keys, author_keys) VALUES ($1, $2, $3);

-- name: ListDiscoverDismissals :many
SELECT title_keys, author_keys FROM discover_dismissals WHERE user_id = $1;
