package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/library"
	"github.com/praetorianer777/gotome/backend/internal/shelves"
)

// collection is a shelf a person fills by hand. A shared one shows each
// viewer only the books they may see.
type collection struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	// Visibility is private, only its owner sees it, or shared, every
	// signed-in person does.
	Visibility string    `json:"visibility"`
	OwnerID    uuid.UUID `json:"ownerId"`
	OwnerName  string    `json:"ownerName"`
	// Mine is whether the caller owns it, and so may change it.
	Mine bool `json:"mine"`
	// Books is how many of its books the caller may see.
	Books     int       `json:"books"`
	UpdatedAt time.Time `json:"updatedAt"`
	// HasBook says whether it holds the book the list was asked about; left
	// out when none was.
	HasBook *bool `json:"hasBook,omitempty"`
}

func collectionOf(c shelves.Collection, viewer uuid.UUID) collection {
	return collection{
		ID: c.ID, Name: c.Name, Description: c.Description, Visibility: c.Visibility,
		OwnerID: c.OwnerID, OwnerName: c.OwnerName, Mine: c.OwnerID == viewer,
		Books: c.Books, UpdatedAt: c.UpdatedAt,
	}
}

type collectionList struct {
	Collections []collection `json:"collections"`
}

type collectionsQuery struct {
	Book string `query:"book" doc:"A book's ID; each collection then says whether it holds that book."`
}

func (s *Server) listCollections(w http.ResponseWriter, r *http.Request) error {
	var q collectionsQuery
	if err := decodeQuery(r, &q); err != nil {
		return err
	}
	var book *uuid.UUID
	if q.Book != "" {
		id, err := uuid.Parse(q.Book)
		if err != nil {
			return ErrValidation(map[string]string{"book": "There is no such book."})
		}
		book = &id
	}
	user := UserFrom(r.Context())
	list, err := s.Shelves.List(r.Context(), library.ScopeOf(*user), book)
	if err != nil {
		return err
	}
	out := collectionList{Collections: make([]collection, len(list))}
	for i, c := range list {
		out.Collections[i] = collectionOf(c, user.ID)
		if book != nil {
			out.Collections[i].HasBook = &c.HasBook
		}
	}
	writeJSON(w, r, http.StatusOK, out)
	return nil
}

// collectionDetail is a collection with the books the caller may see of
// it, in its order.
type collectionDetail struct {
	collection
	BookList []bookSummary `json:"bookList"`
}

func (s *Server) getCollection(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "collectionId", "collection")
	if err != nil {
		return err
	}
	return s.writeCollection(w, r, http.StatusOK, id)
}

func (s *Server) writeCollection(w http.ResponseWriter, r *http.Request, status int, id uuid.UUID) error {
	user := UserFrom(r.Context())
	scope := library.ScopeOf(*user)
	c, ids, err := s.Shelves.Get(r.Context(), scope, id)
	if err != nil {
		return collectionError(err)
	}
	books, err := s.Books.Summaries(r.Context(), scope, ids)
	if err != nil {
		return err
	}
	out := collectionDetail{collection: collectionOf(c, user.ID), BookList: make([]bookSummary, len(books))}
	for i, b := range books {
		out.BookList[i] = summaryOf(b)
	}
	writeJSON(w, r, status, out)
	return nil
}

// collectionRequest names a collection and says who may look at it.
type collectionRequest struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// Visibility is private or shared; private when left out.
	Visibility string `json:"visibility,omitempty"`
}

func (c collectionRequest) details() shelves.Details {
	return shelves.Details{Name: c.Name, Description: c.Description, Visibility: c.Visibility}
}

func (s *Server) createCollection(w http.ResponseWriter, r *http.Request) error {
	var req collectionRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	id, err := s.Shelves.Create(r.Context(), library.ScopeOf(*UserFrom(r.Context())), req.details())
	if err != nil {
		return collectionError(err)
	}
	return s.writeCollection(w, r, http.StatusCreated, id)
}

func (s *Server) updateCollection(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "collectionId", "collection")
	if err != nil {
		return err
	}
	var req collectionRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	if err := s.Shelves.Update(r.Context(), library.ScopeOf(*UserFrom(r.Context())), id, req.details()); err != nil {
		return collectionError(err)
	}
	return s.writeCollection(w, r, http.StatusOK, id)
}

func (s *Server) deleteCollection(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "collectionId", "collection")
	if err != nil {
		return err
	}
	if err := s.Shelves.Delete(r.Context(), library.ScopeOf(*UserFrom(r.Context())), id); err != nil {
		return collectionError(err)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// collectionAddRequest puts books into a collection: those named, or, when
// none are, every book of the list library and filter give.
type collectionAddRequest struct {
	Books   []uuid.UUID `json:"books,omitempty"`
	Library string      `json:"library,omitempty"`
	Filter  string      `json:"filter,omitempty"`
}

type collectionAddResult struct {
	// Added is how many books were not in the collection before.
	Added int `json:"added"`
}

func (s *Server) addToCollection(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "collectionId", "collection")
	if err != nil {
		return err
	}
	var req collectionAddRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	tooMany := ErrValidation(map[string]string{"books": fmt.Sprintf("At most %d books are added at once; choose fewer.", catalog.MaxSelection)})
	if len(req.Books) > catalog.MaxSelection {
		return tooMany
	}
	scope := library.ScopeOf(*UserFrom(r.Context()))
	books := req.Books
	// Books named are taken as they are, as a book wished for is added from
	// its page; a list never holds those.
	if len(books) == 0 {
		libraryID, tree, err := bookSelection(req.Library, req.Filter)
		if err != nil {
			return err
		}
		books, err = s.Books.Select(r.Context(), scope, libraryID, tree, nil)
		switch {
		case errors.Is(err, catalog.ErrTooMany):
			return tooMany
		case catalog.IsFilterError(err):
			return ErrValidation(map[string]string{"filter": err.Error()})
		case err != nil:
			return err
		}
	}
	n, err := s.Shelves.Add(r.Context(), scope, id, books)
	if err != nil {
		return collectionError(err)
	}
	writeJSON(w, r, http.StatusOK, collectionAddResult{Added: n})
	return nil
}

func (s *Server) removeFromCollection(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "collectionId", "collection")
	if err != nil {
		return err
	}
	book, err := pathID(r, "bookId", "book")
	if err != nil {
		return err
	}
	if err := s.Shelves.Remove(r.Context(), library.ScopeOf(*UserFrom(r.Context())), id, book); err != nil {
		return collectionError(err)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// collectionOrder is the order to put a collection's books in. The books
// named take the places they held between them; the rest stay where they
// are.
type collectionOrder struct {
	Books []uuid.UUID `json:"books"`
}

func (s *Server) reorderCollection(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "collectionId", "collection")
	if err != nil {
		return err
	}
	var req collectionOrder
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	if len(req.Books) > shelves.MaxBooks {
		return ErrValidation(map[string]string{"books": "That is more books than a collection holds."})
	}
	if err := s.Shelves.Reorder(r.Context(), library.ScopeOf(*UserFrom(r.Context())), id, req.Books); err != nil {
		return collectionError(err)
	}
	return s.writeCollection(w, r, http.StatusOK, id)
}

func collectionError(err error) error {
	var invalid shelves.Invalid
	switch {
	case errors.As(err, &invalid):
		return ErrValidation(invalid)
	case errors.Is(err, shelves.ErrNotFound):
		return ErrNotFound("There is no such collection.")
	case errors.Is(err, shelves.ErrNotOwner):
		return ErrForbidden("Only its owner changes a collection.")
	case errors.Is(err, shelves.ErrFull):
		return ErrValidation(map[string]string{"books": fmt.Sprintf("A collection holds at most %d books.", shelves.MaxBooks)})
	}
	return err
}
