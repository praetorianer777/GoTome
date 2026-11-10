-- name: ListTasteBooks :many
-- The books the viewer finished or rated that they still see, with their
-- vectors of the model: the best rated first, then the latest finished,
-- up to max. A placeholder has no vectors and is not among them.
SELECT ub.book_id, ub.status, ub.rating, v.kind, v.embedding::text AS embedding
FROM (
    SELECT u.book_id, u.status, u.rating, u.finished_on
    FROM user_books u
    WHERE u.user_id = sqlc.arg(viewer)::uuid AND (u.status = 'completed' OR u.rating IS NOT NULL)
    ORDER BY coalesce(u.rating, 3) DESC, u.finished_on DESC NULLS LAST, u.book_id
    LIMIT sqlc.arg(max)::int
) ub
JOIN books b ON b.id = ub.book_id AND b.deleted_at IS NULL AND NOT b.placeholder
JOIN book_vectors v ON v.book_id = ub.book_id
    AND v.model = sqlc.arg(model)::text AND v.model_version = sqlc.arg(model_version)::text
WHERE b.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean))
ORDER BY ub.book_id, v.kind;

-- name: ListRecommended :many
-- The books of the libraries the viewer sees nearest the centres of their
-- taste, by the cosine of the vectors of the model, each with the centre it
-- is nearest, blended as ListSimilarBooks blends: the best first. A centre
-- is the empty string where it has no vector of that kind. A book the
-- viewer read, is reading or gave up, one they said they are not
-- interested in, and a placeholder are left out; so is a volume of a
-- series they read while an earlier one they have not read is in the
-- libraries (series_rule), and keep names books that are left in although
-- they are the viewer's (a check hides books it asks for again).
-- centres is MATERIALIZED so that its text is read into vectors once: inlined,
-- the planner rescans it for every book and parses them again each time. The
-- books left in are found first and the series rule is worked out once per
-- series, not once per book.
WITH centres AS MATERIALIZED (
    SELECT c.n AS centre,
           NULLIF((sqlc.arg(contents)::text[])[c.n], '')::vector AS content,
           NULLIF((sqlc.arg(metadatas)::text[])[c.n], '')::vector AS metadata
    FROM generate_series(1, cardinality(sqlc.arg(contents)::text[])) c(n)
),
read_series AS MATERIALIZED (
    SELECT DISTINCT r.series_id
    FROM user_books ru JOIN books r ON r.id = ru.book_id
    WHERE ru.user_id = sqlc.arg(viewer)::uuid AND ru.status IN ('completed', 'reading', 'abandoned')
      AND r.series_id IS NOT NULL
),
first_open AS MATERIALIZED (
    SELECT e.series_id, min(e.series_index) AS series_index
    FROM books e
    WHERE e.series_id IN (SELECT series_id FROM read_series)
      AND e.series_index IS NOT NULL AND e.deleted_at IS NULL AND NOT e.placeholder
      AND e.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean))
      AND NOT EXISTS (
          SELECT 1 FROM user_books eu
          WHERE eu.user_id = sqlc.arg(viewer)::uuid AND eu.book_id = e.id
            AND eu.status IN ('completed', 'reading', 'abandoned'))
    GROUP BY e.series_id
),
visible AS MATERIALIZED (
    SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean)
),
taken AS MATERIALIZED (
    SELECT ub.book_id FROM user_books ub
    WHERE ub.user_id = sqlc.arg(viewer)::uuid AND ub.status IN ('completed', 'reading', 'abandoned')
),
dismissed AS MATERIALIZED (
    SELECT d.book_id FROM recommendation_dismissals d WHERE d.user_id = sqlc.arg(viewer)::uuid
),
eligible AS MATERIALIZED (
    SELECT b.id
    FROM books b
    LEFT JOIN first_open f ON sqlc.arg(series_rule)::boolean AND f.series_id = b.series_id
    WHERE b.deleted_at IS NULL AND NOT b.placeholder
      AND b.library_id IN (SELECT * FROM visible)
      AND (b.id = ANY(sqlc.arg(keep)::uuid[]) OR b.id NOT IN (SELECT book_id FROM taken))
      AND b.id NOT IN (SELECT book_id FROM dismissed)
      AND (f.series_index IS NULL OR b.series_index IS NULL OR f.series_index >= b.series_index)
),
best AS (
    SELECT e.id AS book_id, pick.centre, pick.score
    FROM eligible e
    LEFT JOIN book_vectors vc ON vc.book_id = e.id AND vc.kind = 'content'
        AND vc.model = sqlc.arg(model)::text AND vc.model_version = sqlc.arg(model_version)::text
    LEFT JOIN book_vectors vm ON vm.book_id = e.id AND vm.kind = 'metadata'
        AND vm.model = sqlc.arg(model)::text AND vm.model_version = sqlc.arg(model_version)::text
    CROSS JOIN LATERAL (
        SELECT s.centre::int AS centre, s.score
        FROM (
            SELECT c.centre,
                   (CASE WHEN n.content IS NOT NULL AND n.metadata IS NOT NULL
                         THEN 0.6 * n.content + 0.4 * n.metadata
                         ELSE coalesce(n.content, n.metadata)
                    END)::float8 AS score
            FROM centres c
            CROSS JOIN LATERAL (
                SELECT 1 - (vc.embedding <=> c.content) AS content,
                       1 - (vm.embedding <=> c.metadata) AS metadata
            ) n
        ) s
        WHERE s.score IS NOT NULL
        ORDER BY s.score DESC, s.centre
        LIMIT 1
    ) pick
)
SELECT best.book_id, best.centre, best.score
FROM best
ORDER BY best.score DESC, best.book_id
LIMIT sqlc.arg(max)::int;

-- name: ListVectorsOf :many
-- The vectors of the model that these books have.
SELECT v.book_id, v.kind, v.embedding::text AS embedding
FROM book_vectors v
WHERE v.book_id = ANY(sqlc.arg(ids)::uuid[])
  AND v.model = sqlc.arg(model)::text AND v.model_version = sqlc.arg(model_version)::text;

-- name: DismissRecommendation :exec
INSERT INTO recommendation_dismissals (user_id, book_id) VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: MergeRecommendationDismissals :exec
-- Everyone's dismissals of the book follow it into the surviving one.
WITH moved AS (
    DELETE FROM recommendation_dismissals x WHERE x.book_id = sqlc.arg(from_book)
    RETURNING user_id, created_at)
INSERT INTO recommendation_dismissals (user_id, book_id, created_at)
SELECT user_id, sqlc.arg(into_book), created_at FROM moved
ON CONFLICT DO NOTHING;

-- name: ListRecommendationCheckUsers :many
-- The people who rated at least min_books books with 4 or 5 stars that
-- have vectors of the model, with those books, for a check to hide some of.
SELECT u.id AS user_id, (u.role = 'admin')::boolean AS sees_all,
       array_agg(DISTINCT ub.book_id)::uuid[] AS books
FROM users u
JOIN user_books ub ON ub.user_id = u.id AND ub.rating >= 4
JOIN books b ON b.id = ub.book_id AND b.deleted_at IS NULL AND NOT b.placeholder
WHERE EXISTS (SELECT 1 FROM book_vectors v WHERE v.book_id = ub.book_id
              AND v.model = sqlc.arg(model)::text AND v.model_version = sqlc.arg(model_version)::text)
GROUP BY u.id
HAVING count(DISTINCT ub.book_id) >= sqlc.arg(min_books)::int
ORDER BY u.id;
