-- +goose Up
-- The books whose title, an author or the series looks like the words, as
-- the quick search matches them (#350): one row per book, with how alike
-- its best match is and which it was. The quick search ranks by it, and the
-- filter field words keeps a list to it. The settings hold only while the
-- function runs: <% compares with the threshold of the session, and the
-- planner takes a trigram comparison for as cheap as any other and reads
-- every title rather than the index (150 ms instead of 6 on 50,000).
-- Visibility is the caller's: it returns books of every library.
-- +goose StatementBegin
CREATE FUNCTION quick_search_matches(words text)
RETURNS TABLE (book_id uuid, score real, match text)
LANGUAGE sql STABLE PARALLEL SAFE
ROWS 200
SET pg_trgm.word_similarity_threshold = 0.45
SET enable_seqscan = off
AS $$
WITH matched AS (
    SELECT b.id, word_similarity(words, b.title_key) AS score, 'title' AS match
    FROM books b
    WHERE words <% b.title_key AND b.deleted_at IS NULL
  UNION ALL
    SELECT c.book_id, word_similarity(words, a.name_key), 'author'
    FROM authors a
    JOIN book_contributors c ON c.author_id = a.id AND c.role = 'author'
    WHERE words <% a.name_key
  UNION ALL
    SELECT b.id, word_similarity(words, sr.name_key), 'series'
    FROM series sr
    JOIN books b ON b.series_id = sr.id AND b.deleted_at IS NULL
    WHERE words <% sr.name_key
)
SELECT DISTINCT ON (id) id, score, match
FROM matched
ORDER BY id, score DESC, match = 'title' DESC
$$;
-- +goose StatementEnd

-- +goose Down
DROP FUNCTION quick_search_matches(text);
