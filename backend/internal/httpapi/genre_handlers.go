package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/genres"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

type genreSuggestionsQuery struct {
	Library string `query:"library" doc:"A library's ID; left out, every library the caller may see."`
	Genre   string `query:"genre" doc:"One genre's key, such as crime; left out, every genre."`
	Cursor  string `query:"cursor" doc:"Where the page before ended, as its nextCursor says; the surest suggestions come first."`
	Limit   int    `query:"limit" doc:"How many suggestions a page holds, at most 100; 50 when left out."`
}

// genreSuggestions are genres suggested for books that wait for a person
// to accept or reject them.
type genreSuggestions struct {
	Suggestions []genreSuggestion `json:"suggestions"`
	// Waiting is how many wait per genre, of the library when one is named.
	Waiting    []genreWaiting `json:"waiting"`
	NextCursor string         `json:"nextCursor,omitempty"`
}

type genreSuggestion struct {
	Book  bookSummary `json:"book"`
	Genre string      `json:"genre"`
	// Method is how it was found: ensemble (the book's words, vectors and
	// nearest books together), neighbours (the genre of the nearest books),
	// or vector (the book's vectors near the genre's description), which
	// earlier versions suggested with.
	Method string `json:"method"`
	// Score is how sure the method is: a probability for ensemble, a share
	// for neighbours, a margin for vector.
	Score float64 `json:"score"`
	// Reasons say why the ensemble suggested it; absent for other methods.
	Reasons *genreReasons `json:"reasons,omitempty"`
}

// genreReasons are what made the ensemble suggest a genre.
type genreReasons struct {
	// Words are the book's words that weighed most for the genre, most
	// first; empty where its words were not read.
	Words []string `json:"words"`
	// Inputs are each input's probability for the genre: words (the
	// whole text's words), content (the text's vector), metadata (the
	// vector of title, authors and description) and neighbours (the share
	// of the nearest books of its library that have the genre).
	Inputs []genreInput `json:"inputs"`
}

type genreInput struct {
	Input string  `json:"input"`
	Score float64 `json:"score"`
}

func reasonsOf(w *genres.Why) *genreReasons {
	if w == nil {
		return nil
	}
	out := &genreReasons{Words: w.Words, Inputs: []genreInput{}}
	if out.Words == nil {
		out.Words = []string{}
	}
	for _, name := range genres.InputNames {
		if v, ok := w.Inputs[name]; ok {
			out.Inputs = append(out.Inputs, genreInput{Input: name, Score: v})
		}
	}
	return out
}

type genreWaiting struct {
	Genre   string `json:"genre"`
	Waiting int    `json:"waiting"`
}

// genreDecision accepts or rejects a genre of a book.
type genreDecision struct {
	State string `json:"state"`
}

const (
	defaultGenrePage = 50
	maxGenrePage     = 100
)

func (s *Server) listGenreSuggestions(w http.ResponseWriter, r *http.Request) error {
	var q genreSuggestionsQuery
	if err := decodeQuery(r, &q); err != nil {
		return err
	}
	scope := library.ScopeOf(*UserFrom(r.Context()))
	var libraryID *uuid.UUID
	if q.Library != "" {
		id, err := uuid.Parse(q.Library)
		if err != nil {
			return ErrNotFound("There is no such library.")
		}
		if _, err := s.Libraries.Get(r.Context(), scope, id); err != nil {
			return libraryError(err)
		}
		libraryID = &id
	}
	if q.Genre != "" {
		if _, ok := genres.Get(q.Genre); !ok {
			return ErrValidation(map[string]string{"genre": "There is no such genre."})
		}
	}
	limit := defaultGenrePage
	if q.Limit > 0 {
		limit = min(q.Limit, maxGenrePage)
	}
	var after *genres.Cursor
	if q.Cursor != "" {
		c, ok := parseGenreCursor(q.Cursor)
		if !ok {
			return ErrValidation(map[string]string{"cursor": "Give the nextCursor of the page before."})
		}
		after = &c
	}
	page, next, err := s.Genres.Suggestions(r.Context(), scope, libraryID, q.Genre, after, limit)
	if err != nil {
		return err
	}
	ids := make([]uuid.UUID, len(page))
	for i, sg := range page {
		ids[i] = sg.BookID
	}
	books, err := s.Books.Summaries(r.Context(), scope, ids)
	if err != nil {
		return err
	}
	byID := make(map[uuid.UUID]bookSummary, len(books))
	for _, b := range books {
		byID[b.ID] = summaryOf(b)
	}
	out := genreSuggestions{Suggestions: []genreSuggestion{}, Waiting: []genreWaiting{}}
	for _, sg := range page {
		b, ok := byID[sg.BookID]
		if !ok {
			continue
		}
		out.Suggestions = append(out.Suggestions, genreSuggestion{Book: b, Genre: sg.Genre, Method: sg.Method, Score: sg.Score,
			Reasons: reasonsOf(sg.Why)})
	}
	if next != nil {
		out.NextCursor = strconv.FormatFloat(next.Score, 'g', -1, 64) + "_" + next.BookID.String() + "_" + next.Genre
	}
	waiting, err := s.Genres.Waiting(r.Context(), scope, libraryID)
	if err != nil {
		return err
	}
	for _, g := range genres.List {
		if n := waiting[g.Key]; n > 0 {
			out.Waiting = append(out.Waiting, genreWaiting{Genre: g.Key, Waiting: n})
		}
	}
	writeJSON(w, r, http.StatusOK, out)
	return nil
}

func parseGenreCursor(s string) (genres.Cursor, bool) {
	parts := strings.SplitN(s, "_", 3)
	if len(parts) != 3 {
		return genres.Cursor{}, false
	}
	score, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return genres.Cursor{}, false
	}
	id, err := uuid.Parse(parts[1])
	if err != nil {
		return genres.Cursor{}, false
	}
	return genres.Cursor{Score: score, BookID: id, Genre: parts[2]}, true
}

func (s *Server) decideGenre(w http.ResponseWriter, r *http.Request) error {
	bookID, err := pathID(r, "bookId", "book")
	if err != nil {
		return err
	}
	var req genreDecision
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	if req.State != genres.Accepted && req.State != genres.Rejected {
		return ErrValidation(map[string]string{"state": "Say accepted or rejected."})
	}
	err = s.Genres.Decide(r.Context(), library.ScopeOf(*UserFrom(r.Context())), bookID, chi.URLParam(r, "genre"), req.State)
	if errors.Is(err, genres.ErrNotFound) {
		return ErrNotFound("The book has no such genre.")
	}
	if err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
