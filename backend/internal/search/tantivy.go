package search

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/fulltext"
	"github.com/praetorianer777/gotome/backend/internal/ingest"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

// TantivySearch is the Searcher on the full-text index in the app (#265).
// The index holds no text: what it finds is checked against book_chunks,
// which it may lag behind, and snippets are made from the passages read
// again from the files.
type TantivySearch struct {
	pool  *pgxpool.Pool
	books *catalog.Service
	index *fulltext.Index
}

// NewTantivySearch returns the Searcher on the index.
func NewTantivySearch(pool *pgxpool.Pool, books *catalog.Service, index *fulltext.Index) *TantivySearch {
	return &TantivySearch{pool: pool, books: books, index: index}
}

// Search takes the best window of chunks among the visible libraries,
// and when that finds fewer than sparseBooks books, takes the query's
// unknown words for typos and searches the words of the text like them as
// well; the books only that finds come after the others, marked as
// corrected.
func (s *TantivySearch) Search(ctx context.Context, scope library.Scope, q Query) (Result, error) {
	p, err := parse(q.Text)
	if err != nil {
		return Result{}, err
	}
	limit := min(max(q.Limit, 1), MaxLimit)
	offset := max(q.Offset, 0)

	var res Result
	err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		chunks, err := s.best(ctx, tx, scope, q, p)
		if err != nil {
			return err
		}
		readings := []reading{{chunks: chunks, query: p}}
		if distinctBooks(chunks) < sparseBooks {
			fixed, changed, err := correct(ctx, tx, p)
			if err != nil {
				return err
			}
			if changed {
				more, err := s.best(ctx, tx, scope, q, fixed)
				if err != nil {
					return err
				}
				readings = append(readings, reading{chunks: more, query: fixed, corrected: true})
			}
		}
		res, err = group(ctx, tx, readings, limit, offset, s.passages)
		return err
	})
	return res, err
}

func indexQuery(p parsed) fulltext.Query {
	return fulltext.Query{Words: p.Words, Groups: p.Groups, Phrases: p.Phrases, Variants: p.Variants}
}

// best asks the index for the window of the best chunks among the visible
// libraries, and the filter's books when it names them, and keeps those
// that are still in book_chunks, of books the filter's conditions match.
func (s *TantivySearch) best(ctx context.Context, tx pgx.Tx, scope library.Scope, q Query, p parsed) ([]found, error) {
	libs, books, err := where(ctx, tx, s.books, scope, q)
	if err != nil || len(libs) == 0 || (books != nil && len(books) == 0) {
		return nil, err
	}
	iq := indexQuery(p)
	iq.Libraries, iq.Books, iq.Limit = libs, books, window
	hits, err := s.index.Search(iq)
	if err != nil || len(hits) == 0 {
		return nil, err
	}
	ids := make([]int64, len(hits))
	scores := make([]float64, len(hits))
	for i, h := range hits {
		ids[i], scores[i] = h.ID, h.Score
	}
	var a args
	idsArg, scoresArg := a.add(ids), a.add(scores)
	outer, err := catalog.BookConditions(scope, q.LibraryID, q.Filter, a.add)
	if err != nil {
		return nil, err
	}
	sql := `SELECT c.id, c.book_id, h.score
	FROM unnest(` + idsArg + `::bigint[], ` + scoresArg + `::float8[]) AS h(id, score)
	JOIN book_chunks c ON c.id = h.id
	JOIN books b ON b.id = c.book_id
	WHERE ` + strings.Join(outer, " AND ") + `
	ORDER BY h.score DESC, c.id`
	rows, err := tx.Query(ctx, sql, a.values...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (found, error) {
		var f found
		return f, r.Scan(&f.id, &f.bookID, &f.score)
	})
}

// passages reads the chunks again from their files and makes their
// snippets; a chunk whose file cannot be read is shown without one.
func (s *TantivySearch) passages(ctx context.Context, tx pgx.Tx, ids []int64, p parsed) (map[int64]Hit, error) {
	rows, err := tx.Query(ctx, `
		SELECT c.id, c.file_id, f.format, c.position, c.chapter, c.page_from, c.page_to, c.char_offset,
		       c.lang, c.sources, c.source_from, c.source_to, l.root_path, f.rel_path
		FROM book_chunks c
		JOIN book_files f ON f.id = c.file_id
		JOIN libraries l ON l.id = f.library_id
		WHERE c.id = ANY($1::bigint[])`, ids)
	if err != nil {
		return nil, err
	}
	out := map[int64]Hit{}
	langs := map[int64]string{}
	var refs []ingest.PassageRef
	for rows.Next() {
		var h Hit
		var from, to *int32
		var position, offset, sourceFrom, sourceTo int32
		var lang, root, rel string
		var sources []string
		if err := rows.Scan(&h.ChunkID, &h.FileID, &h.Format, &position, &h.Chapter, &from, &to, &offset,
			&lang, &sources, &sourceFrom, &sourceTo, &root, &rel); err != nil {
			rows.Close()
			return nil, err
		}
		h.Position, h.Offset = int(position), int(offset)
		if from != nil && to != nil {
			h.PageFrom, h.PageTo = int(*from), int(*to)
		}
		out[h.ChunkID] = h
		langs[h.ChunkID] = lang
		refs = append(refs, ingest.PassageRef{
			ChunkID: h.ChunkID, FileID: h.FileID, Format: h.Format,
			Path: filepath.Join(root, filepath.FromSlash(rel)), Position: h.Position,
			Sources: sources, From: int(sourceFrom), To: int(sourceTo),
		})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	texts := ingest.ReadPassages(ctx, refs)
	var order []int64
	var passages []fulltext.Passage
	for _, r := range refs {
		if text, ok := texts[r.ChunkID]; ok {
			order = append(order, r.ChunkID)
			passages = append(passages, fulltext.Passage{Lang: langs[r.ChunkID], Text: text})
		}
	}
	if len(passages) == 0 {
		return out, nil
	}
	snippets, err := s.index.Snippets(indexQuery(p), passages, snippetChars)
	if err != nil {
		return nil, err
	}
	for i, id := range order {
		if i < len(snippets) {
			h := out[id]
			h.Snippet = snippetParts(snippets[i])
			out[id] = h
		}
	}
	return out, nil
}

// snippetParts cuts a fragment at its matches.
func snippetParts(s fulltext.Snippet) []Part {
	if len(s.Highlighted) == 0 {
		return nil
	}
	var out []Part
	at := 0
	for _, r := range s.Highlighted {
		start, end := r[0], r[1]
		if start < at || end > len(s.Fragment) || start >= end {
			continue
		}
		if start > at {
			out = append(out, Part{Text: s.Fragment[at:start]})
		}
		out = append(out, Part{Text: s.Fragment[start:end], Match: true})
		at = end
	}
	if at < len(s.Fragment) {
		out = append(out, Part{Text: s.Fragment[at:]})
	}
	return out
}
