-- name: ListPassageFilesToEmbed :many
-- A page of primary text files after the given one, in ID order, whose
-- passages have no vectors of the model, or of an older chunking of them.
SELECT f.id AS file_id, f.library_id, f.chunked_at::timestamptz AS chunked_at
FROM books b
JOIN book_files f ON f.id = b.primary_text_file_id AND f.chunked_at IS NOT NULL
LEFT JOIN passage_files p ON p.file_id = f.id
WHERE b.deleted_at IS NULL AND NOT b.placeholder AND f.id > sqlc.arg(after)::uuid
  AND (p.file_id IS NULL OR p.model <> sqlc.arg(model)::text
       OR p.model_version <> sqlc.arg(model_version)::text OR p.chunked_at <> f.chunked_at)
ORDER BY f.id
LIMIT sqlc.arg(page)::int;

-- name: PutPassageFile :exec
-- Nothing is recorded for a file chunked again since it was read: its
-- passages are embedded once more.
INSERT INTO passage_files (file_id, model, model_version, chunked_at, passages)
SELECT f.id, sqlc.arg(model)::text, sqlc.arg(model_version)::text, f.chunked_at, sqlc.arg(passages)::int
FROM book_files f
WHERE f.id = sqlc.arg(file_id)::uuid AND f.chunked_at = sqlc.arg(chunked_at)::timestamptz
ON CONFLICT (file_id) DO UPDATE
SET model = excluded.model, model_version = excluded.model_version, chunked_at = excluded.chunked_at,
    passages = excluded.passages, embedded_at = now();

-- name: DeletePassageFiles :exec
-- The store was begun again: no file's passages are in it.
DELETE FROM passage_files;

-- name: CountStoredPassages :one
-- How many passages the store should hold of the model.
SELECT coalesce(sum(passages), 0)::bigint FROM passage_files
WHERE model = sqlc.arg(model)::text AND model_version = sqlc.arg(model_version)::text;

-- name: ListLivePassages :many
-- Every chunk whose vector the store should hold, with its library, in
-- chunk order: of a file embedded with the model as it is chunked now.
SELECT c.id, c.library_id
FROM passage_files p
JOIN book_files f ON f.id = p.file_id AND f.chunked_at = p.chunked_at
JOIN book_chunks c ON c.file_id = p.file_id
WHERE p.model = sqlc.arg(model)::text AND p.model_version = sqlc.arg(model_version)::text
ORDER BY c.id;

-- name: ListPassageBooks :many
-- Of the chunks the store found, those of books the viewer sees, of one
-- library when named, with their book: a chunk of the book's primary text
-- file as it was embedded. The store may be behind the database; this is
-- what makes its answers true.
SELECT c.id AS chunk_id, c.book_id
FROM book_chunks c
JOIN books b ON b.id = c.book_id AND b.primary_text_file_id = c.file_id
JOIN book_files f ON f.id = c.file_id
JOIN passage_files p ON p.file_id = c.file_id AND p.chunked_at = f.chunked_at
WHERE c.id = ANY(sqlc.arg(ids)::bigint[])
  AND p.model = sqlc.arg(model)::text AND p.model_version = sqlc.arg(model_version)::text
  AND b.deleted_at IS NULL AND NOT b.placeholder
  AND c.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean)) AND (b.age_rating IS NULL OR b.age_rating <= (SELECT age_limit(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean)))
  AND (sqlc.narg(library_id)::uuid IS NULL OR c.library_id = sqlc.narg(library_id)::uuid);

-- name: ListPassageLibraries :many
-- The libraries the viewer sees, or the one named of them.
SELECT v.id::uuid AS id FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean) AS v(id)
WHERE sqlc.narg(library_id)::uuid IS NULL OR v.id = sqlc.narg(library_id)::uuid;

-- name: CountPassages :one
-- How far the books the viewer sees, of one library when named, have their
-- passages embedded with the model: those with text, and those whose text
-- has its passages' vectors.
SELECT count(*)::int AS with_text,
       count(p.file_id)::int AS with_passages
FROM books b
JOIN book_files f ON f.id = b.primary_text_file_id AND f.chunked_at IS NOT NULL
LEFT JOIN passage_files p ON p.file_id = f.id AND p.chunked_at = f.chunked_at
     AND p.model = sqlc.arg(model)::text AND p.model_version = sqlc.arg(model_version)::text
WHERE b.deleted_at IS NULL AND NOT b.placeholder
  AND b.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean)) AND (b.age_rating IS NULL OR b.age_rating <= (SELECT age_limit(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean)))
  AND (sqlc.narg(library_id)::uuid IS NULL OR b.library_id = sqlc.narg(library_id)::uuid);

-- name: ScoreMetadataNearQuery :many
-- The cosines of the books' metadata vectors of the model with a query's,
-- for blending with their passages'.
SELECT v.book_id, (1 - (v.embedding <=> sqlc.arg(query)::text::vector))::float8 AS score
FROM book_vectors v
WHERE v.book_id = ANY(sqlc.arg(ids)::uuid[]) AND v.kind = 'metadata'
  AND v.model = sqlc.arg(model)::text AND v.model_version = sqlc.arg(model_version)::text;

-- name: DeletePassageFilesOfModel :exec
-- The model's passages are gone from the store.
DELETE FROM passage_files
WHERE model = sqlc.arg(model)::text AND model_version = sqlc.arg(model_version)::text;
