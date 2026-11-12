-- +goose Up
-- A book keeps a vector of each model it was embedded with (#173): a model
-- served on a GPU may have another length than e5-small's 384, and while
-- every book is embedded again with another model, the vectors of the one
-- before still answer for it, and come back at once if it is chosen again.
ALTER TABLE book_vectors ALTER COLUMN embedding TYPE vector;
ALTER TABLE book_vectors DROP CONSTRAINT book_vectors_pkey;
ALTER TABLE book_vectors ADD PRIMARY KEY (book_id, kind, model);

-- +goose Down
DELETE FROM book_vectors v
WHERE vector_dims(v.embedding) <> 384
   OR EXISTS (SELECT 1 FROM book_vectors o
              WHERE o.book_id = v.book_id AND o.kind = v.kind AND o.embedded_at > v.embedded_at);
ALTER TABLE book_vectors DROP CONSTRAINT book_vectors_pkey;
ALTER TABLE book_vectors ADD PRIMARY KEY (book_id, kind);
ALTER TABLE book_vectors ALTER COLUMN embedding TYPE vector(384);
