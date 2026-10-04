package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/analysis/analyzer/standard"
	"github.com/blevesearch/bleve/v2/analysis/lang/de"
	"github.com/blevesearch/bleve/v2/analysis/lang/en"
	"github.com/blevesearch/bleve/v2/mapping"
	"github.com/blevesearch/bleve/v2/search/query"
)

// Bleve is a pure-Go index beside the database, in the app's process. It
// keeps its own copy of the text: highlighting needs the stored text and
// its term positions.
type Bleve struct {
	dir   string
	index bleve.Index
}

type bleveDoc struct {
	Book    int    `json:"book"`
	Library string `json:"lib"`
	BodyEN  string `json:"body_en,omitempty"`
	BodyDE  string `json:"body_de,omitempty"`
	BodyXX  string `json:"body_xx,omitempty"`
}

// bleveBatch is how many chunks one batch indexes.
const bleveBatch = 1000

func bleveMapping() mapping.IndexMapping {
	text := func(analyzer string) *mapping.FieldMapping {
		f := bleve.NewTextFieldMapping()
		f.Analyzer = analyzer
		f.Store = true
		f.IncludeTermVectors = true
		f.IncludeInAll = false
		return f
	}
	doc := bleve.NewDocumentStaticMapping()
	doc.AddFieldMappingsAt("body_en", text(en.AnalyzerName))
	doc.AddFieldMappingsAt("body_de", text(de.AnalyzerName))
	doc.AddFieldMappingsAt("body_xx", text(standard.Name))
	lib := bleve.NewKeywordFieldMapping()
	lib.Store = false
	lib.IncludeInAll = false
	doc.AddFieldMappingsAt("lib", lib)
	book := bleve.NewNumericFieldMapping()
	book.Index = false
	doc.AddFieldMappingsAt("book", book)
	m := bleve.NewIndexMapping()
	m.DefaultMapping = doc
	return m
}

func (e *Bleve) Build(ctx context.Context, src Source) (Steps, error) {
	if err := os.RemoveAll(e.dir); err != nil {
		return nil, err
	}
	idx, err := bleve.New(e.dir, bleveMapping())
	if err != nil {
		return nil, err
	}
	e.index = idx
	t := time.Now()
	b := idx.NewBatch()
	var total int
	err = src(func(r Row) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		d := bleveDoc{Book: r.Book, Library: strconv.Itoa(int(r.Library))}
		switch textColumn(r.Lang) {
		case "body_en":
			d.BodyEN = r.Text
		case "body_de":
			d.BodyDE = r.Text
		default:
			d.BodyXX = r.Text
		}
		if err := b.Index(strconv.FormatInt(r.ID, 10), d); err != nil {
			return err
		}
		if b.Size() == bleveBatch {
			if err := idx.Batch(b); err != nil {
				return err
			}
			total += bleveBatch
			if total%(bleveBatch*500) == 0 {
				fmt.Printf("  %d chunks indexed\n", total)
			}
			b.Reset()
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := idx.Batch(b); err != nil {
		return nil, err
	}
	return Steps{"index": since(t)}, nil
}

func (e *Bleve) Sizes(context.Context) (map[string]int64, error) {
	var n int64
	err := filepath.Walk(e.dir, func(_ string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			n += info.Size()
		}
		return err
	})
	return map[string]int64{"index (text stored inside)": n}, err
}

func (e *Bleve) request(q Query) *bleve.SearchRequest {
	var alts []query.Query
	for _, f := range []string{"body_en", "body_de", "body_xx"} {
		switch q.Kind {
		case Phrase:
			m := bleve.NewMatchPhraseQuery(q.Text)
			m.SetField(f)
			alts = append(alts, m)
		default:
			m := bleve.NewMatchQuery(q.Text)
			m.SetField(f)
			m.SetOperator(query.MatchQueryOperatorAnd)
			if q.Kind == Fuzzy {
				m.SetFuzziness(1)
			}
			alts = append(alts, m)
		}
	}
	var qq query.Query = bleve.NewDisjunctionQuery(alts...)
	if q.Kind == Filtered {
		var libs []query.Query
		for _, l := range FilterLibraries {
			t := bleve.NewTermQuery(strconv.Itoa(int(l)))
			t.SetField("lib")
			libs = append(libs, t)
		}
		qq = bleve.NewConjunctionQuery(qq, bleve.NewDisjunctionQuery(libs...))
	}
	req := bleve.NewSearchRequestOptions(qq, TopK, 0, false)
	req.Fields = []string{"book"}
	if q.Kind == Snippets {
		req.Highlight = bleve.NewHighlightWithStyle("html")
		req.Highlight.Fields = []string{"body_en", "body_de", "body_xx"}
	}
	return req
}

func (e *Bleve) Search(ctx context.Context, q Query) ([]Hit, error) {
	if q.Kind == Repaired {
		// Bleve highlights its fuzzy hits itself.
		return nil, errUnsupported
	}
	res, err := e.index.SearchInContext(ctx, e.request(q))
	if err != nil {
		return nil, err
	}
	hits := make([]Hit, 0, len(res.Hits))
	for _, h := range res.Hits {
		id, _ := strconv.ParseInt(h.ID, 10, 64)
		book, _ := h.Fields["book"].(float64)
		hit := Hit{ID: id, Book: int(book), Score: h.Score}
		for _, frags := range h.Fragments {
			if len(frags) > 0 {
				hit.Snippet = strings.Join(frags, " … ")
				break
			}
		}
		hits = append(hits, hit)
	}
	return hits, nil
}

func (e *Bleve) Plan(_ context.Context, q Query) (string, error) {
	if q.Kind == Repaired {
		return "", errUnsupported
	}
	return "Filters are part of the query: a conjunction with a term query on the library, evaluated inside the index before the top 10 are taken.", nil
}

// Rebuild is a full build for Bleve: it has no stored rows to build from
// apart from its own index.
func (e *Bleve) Rebuild(context.Context) (time.Duration, error) { return 0, nil }

func (e *Bleve) Close() {
	if e.index != nil {
		e.index.Close()
	}
}
