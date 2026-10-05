package httpapi

import (
	"cmp"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/library"
	"github.com/praetorianer777/gotome/backend/internal/search"
)

type fullTextQuery struct {
	Q       string `query:"q" doc:"The words to look for in the books' text, all of which a passage must hold in any form; a phrase in double quotes must stand in that order. When few books hold them, words no book holds are taken for typos and the words like them are looked for too."`
	Library string `query:"library" doc:"A library's ID; left out, every library the caller may see."`
	Filter  string `query:"filter" doc:"A rule tree the books must match, as listBooks takes it."`
	Limit   int    `query:"limit" doc:"How many books a page holds, at most 50; 20 when left out."`
	Offset  int    `query:"offset" doc:"How many of the books found to pass over: results are ranked, so pages are counted, not keyed."`
}

// snippetPart is a piece of a passage: text as it is, and whether it is
// one of the words looked for.
type snippetPart struct {
	Text  string `json:"text"`
	Match bool   `json:"match,omitempty"`
}

// textHit is a passage of a book that matched.
type textHit struct {
	FileID uuid.UUID `json:"fileId"`
	// Format is the file's: a PDF's passage is found by its page, another's
	// by its words.
	Format string `json:"format"`
	// Position counts the passages of the book's text from 0.
	Position int    `json:"position"`
	Chapter  string `json:"chapter,omitempty"`
	// PageFrom and PageTo are left out when the pages are not known.
	PageFrom int `json:"pageFrom,omitempty"`
	PageTo   int `json:"pageTo,omitempty"`
	// Offset is where the passage starts in the book's text, in characters.
	Offset  int           `json:"offset"`
	Snippet []snippetPart `json:"snippet"`
}

// textBook is a book found, with its best passages.
type textBook struct {
	Book bookSummary `json:"book"`
	// Corrected says the book was found only once the query's typos were
	// repaired; such books come after the others.
	Corrected bool      `json:"corrected,omitempty"`
	Hits      []textHit `json:"hits"`
}

type fullTextResult struct {
	Books []textBook `json:"books"`
	// More says there are books found after this page.
	More bool `json:"more"`
}

const defaultTextPage = 20

func (s *Server) searchText(w http.ResponseWriter, r *http.Request) error {
	var q fullTextQuery
	if err := decodeQuery(r, &q); err != nil {
		return err
	}
	libraryID, tree, err := bookSelection(q.Library, q.Filter)
	if err != nil {
		return err
	}
	scope := library.ScopeOf(*UserFrom(r.Context()))
	res, err := s.Search.Search(r.Context(), scope, search.Query{
		Text: q.Q, LibraryID: libraryID, Filter: tree,
		Limit: cmp.Or(q.Limit, defaultTextPage), Offset: q.Offset,
	})
	switch {
	case errors.Is(err, search.ErrEmptyQuery):
		return ErrValidation(map[string]string{"q": "Name a word to look for."})
	case catalog.IsFilterError(err):
		return ErrValidation(map[string]string{"filter": err.Error()})
	case err != nil:
		return err
	}

	ids := make([]uuid.UUID, len(res.Books))
	for i, b := range res.Books {
		ids[i] = b.ID
	}
	summaries, err := s.Books.Summaries(r.Context(), scope, ids)
	if err != nil {
		return err
	}
	byID := make(map[uuid.UUID]catalog.Summary, len(summaries))
	for _, sm := range summaries {
		byID[sm.ID] = sm
	}
	out := fullTextResult{Books: []textBook{}, More: res.More}
	for _, b := range res.Books {
		sm, ok := byID[b.ID]
		if !ok {
			// Gone between the search and the summaries.
			continue
		}
		book := textBook{Book: summaryOf(sm), Corrected: b.Corrected, Hits: make([]textHit, len(b.Hits))}
		for i, h := range b.Hits {
			hit := textHit{
				FileID: h.FileID, Format: h.Format, Position: h.Position, Chapter: h.Chapter,
				PageFrom: h.PageFrom, PageTo: h.PageTo, Offset: h.Offset,
				Snippet: make([]snippetPart, len(h.Snippet)),
			}
			for j, p := range h.Snippet {
				hit.Snippet[j] = snippetPart{Text: p.Text, Match: p.Match}
			}
			book.Hits[i] = hit
		}
		out.Books = append(out.Books, book)
	}
	writeJSON(w, r, http.StatusOK, out)
	return nil
}
