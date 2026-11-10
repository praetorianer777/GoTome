// Package perf times the main views of the web app: the scale test on a
// generated library and `gotome perf-check` on a real one ask the list
// below, so that the two cannot drift apart.
package perf

import (
	"fmt"
	"net/url"
	"slices"
)

// DescribePhrase is what the search by description is asked: generic, so
// that it names nothing of the library it is asked on.
const DescribePhrase = "a detective in a small town"

// View is one main view, asked as the web app asks it.
type View struct {
	Name string
	// Path is the address under /api/v1; empty for a view the library has
	// nothing to ask about (no collection, no series).
	Path string
	// AdminOnly is a view only an administrator may use.
	AdminOnly bool
	// Feed is an OPDS feed for e-readers, which is XML, not JSON.
	Feed bool
	// After is the view whose nextCursor stands for @next in the path.
	After string
}

func (v View) need(values ...string) View {
	if slices.Contains(values, "") {
		v.Path = ""
	}
	return v
}

// Params are what the views are asked about, picked from the library.
type Params struct {
	Book, SimilarBook, Library, Collection, Shelf string
	// Author is a name, Series a name key, Tag a name, Language a code.
	Author, Series, Tag, Language string
	// Quick is a frequent word of titles; Word and Phrase are common in
	// the texts. Describe is DescribePhrase when empty.
	Quick, Word, Phrase, Describe string
}

// WholeByDesign are the views that read every book, or every file, the
// viewer sees, because what they answer is about all of them; they are held
// to the scale test's budget alone, and marked as such in the tables.
var WholeByDesign = map[string]string{
	// It counts each value over every book the filter leaves.
	"facets": "counts every book",
	// The exact cosine scan of #66 compares the book with every other one;
	// at 50,000 books an approximate index is not worth what it costs.
	"similar books": "compares with every book",
	// Recommendations compare every book with each centre of the viewer's
	// taste.
	"recommendations": "compares with every book",
	// So does a description, with the vectors of every visible book.
	"search by description": "compares with every book",
	// Each library's files are counted and their sizes added up.
	"libraries": "counts every file",
	// Suggestions are about the whole catalogue: every author credit is
	// grouped by person, every title matched, and each kind counted.
	"clean-up: authors": "reads every credit and title",
	"clean-up: titles":  "reads every credit and title",
	"clean-up: series":  "reads every credit and title",
	// The overview counts what the books hold and how far the work on
	// them is.
	"overview":              "counts every book and file",
	"overview: work":        "counts every book and file",
	"overview of a library": "counts every book and file",
}

// Views are the views of the web app's pages, with what p names filled in.
func Views(p Params) []View {
	if p.Describe == "" {
		p.Describe = DescribePhrase
	}
	filter := func(tree string) string { return url.Values{"filter": {tree}}.Encode() }
	byAuthor := fmt.Sprintf(`{"field": "author", "op": "in", "values": [%q]}`, p.Author)
	byPerson := fmt.Sprintf(`{"field": "person", "op": "in", "values": [%q]}`, p.Author)
	bySeries := fmt.Sprintf(`{"field": "series", "op": "in", "values": [%q]}`, p.Series)
	byWords := fmt.Sprintf(`{"field": "words", "op": "like", "values": [%q]}`, p.Quick)
	tagAndLanguage := fmt.Sprintf(`{"all": [{"field": "tag", "op": "in", "values": [%q]}, {"field": "language", "op": "in", "values": [%q]}]}`, p.Tag, p.Language)
	prefix := p.Quick[:min(3, len(p.Quick))]
	return []View{
		{Name: "library by title", Path: "/books?limit=50"},
		{Name: "library by author", Path: "/books?sort=author&limit=50"},
		{Name: "library newest first", Path: "/books?sort=added&order=desc&limit=50"},
		{Name: "library page 2", Path: "/books?limit=50&cursor=@next", After: "library by title"},
		View{Name: "one library", Path: "/books?limit=50&library=" + p.Library}.need(p.Library),
		View{Name: "filter by author", Path: "/books?limit=50&" + filter(byAuthor)}.need(p.Author),
		{Name: "authors", Path: "/authors?limit=60"},
		{Name: "series", Path: "/series?limit=60"},
		View{Name: "an author", Path: "/authors/" + url.PathEscape(p.Author)}.need(p.Author),
		View{Name: "an author's books", Path: "/books?limit=50&sort=series&" + filter(byPerson)}.need(p.Author),
		View{Name: "an author's facets", Path: "/books/facets?" + filter(byPerson)}.need(p.Author),
		View{Name: "a series", Path: "/series/" + url.PathEscape(p.Series)}.need(p.Series),
		View{Name: "a series' books", Path: "/books?limit=50&sort=series&" + filter(bySeries)}.need(p.Series),
		View{Name: "filter by tag and language", Path: "/books?limit=50&" + filter(tagAndLanguage)}.need(p.Tag, p.Language),
		{Name: "filter by status", Path: "/books?limit=50&" + filter(`{"field": "status", "op": "in", "values": ["reading"]}`)},
		{Name: "filter by year", Path: "/books?limit=50&" + filter(`{"field": "published", "op": "between", "values": ["1900", "1950"]}`)},
		{Name: "filter by missing files", Path: "/books?limit=50&" + filter(`{"field": "files", "op": "in", "values": ["missing"]}`)},
		{Name: "filter by no file on disk", Path: "/books?limit=50&" + filter(`{"field": "files", "op": "in", "values": ["none-on-disk"]}`)},
		{Name: "filter by e-reader findings", Path: "/books?limit=50&" + filter(`{"field": "ereader", "op": "in", "values": ["large-fonts", "large-image"]}`)},
		{Name: "facets", Path: "/books/facets"},
		View{Name: "facets of a filter", Path: "/books/facets?" + filter(tagAndLanguage)}.need(p.Tag, p.Language),
		View{Name: "count of a filter", Path: "/books/count?" + filter(tagAndLanguage)}.need(p.Tag, p.Language),
		View{Name: "quick search", Path: "/books/search?q=" + url.QueryEscape(p.Quick)}.need(p.Quick),
		View{Name: "every quick search match", Path: "/books?limit=50&" + filter(byWords)}.need(p.Quick),
		View{Name: "every quick search match by author", Path: "/books?limit=50&sort=author&" + filter(byWords)}.need(p.Quick),
		View{Name: "facets of every quick search match", Path: "/books/facets?" + filter(byWords)}.need(p.Quick),
		View{Name: "author names", Path: "/books/names?kind=author&q=" + url.QueryEscape(prefix)}.need(p.Quick),
		View{Name: "full-text search", Path: "/search?q=" + url.QueryEscape(p.Word)}.need(p.Word),
		View{Name: "full-text search for a phrase", Path: "/search?q=" + url.QueryEscape(`"`+p.Phrase+`"`)}.need(p.Phrase),
		View{Name: "book", Path: "/books/" + p.Book}.need(p.Book),
		View{Name: "similar books", Path: "/books/" + p.SimilarBook + "/similar"}.need(p.SimilarBook),
		{Name: "duplicates", Path: "/duplicates"},
		{Name: "review", Path: "/matches"},
		View{Name: "collection", Path: "/collections/" + p.Collection}.need(p.Collection),
		View{Name: "smart shelf", Path: "/smart-shelves/" + p.Shelf + "/books"}.need(p.Shelf),
		{Name: "collections", Path: "/collections"},
		{Name: "reading statistics", Path: "/me/stats?days=30"},
		{Name: "reading history", Path: "/me/reading"},
		{Name: "recommendations", Path: "/me/recommendations"},
		{Name: "new books", Path: "/releases"},
		{Name: "items of followed feeds", Path: "/feeds/items"},
		{Name: "genre review", Path: "/genres/suggestions"},
		{Name: "genre review page 2", Path: "/genres/suggestions?cursor=@next", After: "genre review"},
		{Name: "genre review of a genre", Path: "/genres/suggestions?genre=crime"},
		{Name: "search by description", Path: "/search/similar?q=" + url.QueryEscape(p.Describe)},
		{Name: "clean-up: authors", Path: "/cleanup?kind=authors"},
		{Name: "clean-up: titles", Path: "/cleanup?kind=titles"},
		{Name: "clean-up: series", Path: "/cleanup?kind=series"},
		{Name: "trash", Path: "/trash"},
		{Name: "jobs", Path: "/jobs"},
		{Name: "libraries", Path: "/libraries"},
		{Name: "users", Path: "/users", AdminOnly: true},
		{Name: "e-reader catalogue: newest", Path: "/opds/books", Feed: true},
		{Name: "e-reader catalogue: authors", Path: "/opds/authors", Feed: true},
		View{Name: "e-reader catalogue: an author", Path: "/opds/books?" + url.Values{"author": {p.Author}}.Encode(), Feed: true}.need(p.Author),
		View{Name: "e-reader catalogue: search", Path: "/opds/search?q=" + url.QueryEscape(p.Quick), Feed: true}.need(p.Quick),
		{Name: "overview", Path: "/statistics"},
		View{Name: "overview of a library", Path: "/statistics?library=" + p.Library}.need(p.Library),
		{Name: "overview: work", Path: "/statistics/work"},
	}
}
