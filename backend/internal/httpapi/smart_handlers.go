package httpapi

import (
	"cmp"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/filter"
	"github.com/praetorianer777/gotome/backend/internal/library"
	"github.com/praetorianer777/gotome/backend/internal/shelves"
)

// smartShelf is a rule tree under a name. What is on it is worked out each
// time, for the caller: rules on status and rating are theirs.
type smartShelf struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
	// Filter is the rule tree as JSON, as listBooks takes it.
	Filter string `json:"filter"`
	// Visibility is private, only its owner sees it, or shared, every
	// signed-in person does.
	Visibility string    `json:"visibility"`
	OwnerID    uuid.UUID `json:"ownerId"`
	OwnerName  string    `json:"ownerName"`
	// Mine is whether the caller owns it, and so may change it.
	Mine bool `json:"mine"`
	// Books is how many books the caller may see match it now.
	Books     int       `json:"books"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func smartShelfOf(s shelves.SmartShelf, viewer uuid.UUID) smartShelf {
	tree, _ := json.Marshal(s.Filter)
	return smartShelf{
		ID: s.ID, Name: s.Name, Filter: string(tree), Visibility: s.Visibility,
		OwnerID: s.OwnerID, OwnerName: s.OwnerName, Mine: s.OwnerID == viewer,
		Books: s.Books, UpdatedAt: s.UpdatedAt,
	}
}

type smartShelfList struct {
	Shelves []smartShelf `json:"shelves"`
}

func (s *Server) listSmartShelves(w http.ResponseWriter, r *http.Request) error {
	user := UserFrom(r.Context())
	list, err := s.Shelves.SmartShelves(r.Context(), library.ScopeOf(*user))
	if err != nil {
		return err
	}
	out := smartShelfList{Shelves: make([]smartShelf, len(list))}
	for i, sh := range list {
		out.Shelves[i] = smartShelfOf(sh, user.ID)
	}
	writeJSON(w, r, http.StatusOK, out)
	return nil
}

func (s *Server) getSmartShelf(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "shelfId", "smart shelf")
	if err != nil {
		return err
	}
	return s.writeSmartShelf(w, r, http.StatusOK, id)
}

func (s *Server) writeSmartShelf(w http.ResponseWriter, r *http.Request, status int, id uuid.UUID) error {
	user := UserFrom(r.Context())
	sh, err := s.Shelves.SmartShelf(r.Context(), library.ScopeOf(*user), id)
	if err != nil {
		return smartShelfError(err)
	}
	writeJSON(w, r, status, smartShelfOf(sh, user.ID))
	return nil
}

// smartShelfRequest names a smart shelf, gives its rules and says who may
// look at it.
type smartShelfRequest struct {
	Name string `json:"name"`
	// Filter is a rule tree as JSON, as listBooks takes it. A tree the
	// library's filter would refuse is refused.
	Filter string `json:"filter"`
	// Visibility is private or shared; private when left out.
	Visibility string `json:"visibility,omitempty"`
}

func (req smartShelfRequest) details() (shelves.SmartDetails, error) {
	d := shelves.SmartDetails{Name: req.Name, Visibility: req.Visibility}
	if req.Filter == "" {
		return d, ErrValidation(map[string]string{"filter": "Give the shelf its rules."})
	}
	tree, err := filter.Parse([]byte(req.Filter))
	if err != nil {
		return d, ErrValidation(map[string]string{"filter": err.Error()})
	}
	d.Filter = tree
	return d, nil
}

func (s *Server) createSmartShelf(w http.ResponseWriter, r *http.Request) error {
	var req smartShelfRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	d, err := req.details()
	if err != nil {
		return err
	}
	id, err := s.Shelves.CreateSmart(r.Context(), library.ScopeOf(*UserFrom(r.Context())), d)
	if err != nil {
		return smartShelfError(err)
	}
	return s.writeSmartShelf(w, r, http.StatusCreated, id)
}

func (s *Server) updateSmartShelf(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "shelfId", "smart shelf")
	if err != nil {
		return err
	}
	var req smartShelfRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	d, err := req.details()
	if err != nil {
		return err
	}
	if err := s.Shelves.UpdateSmart(r.Context(), library.ScopeOf(*UserFrom(r.Context())), id, d); err != nil {
		return smartShelfError(err)
	}
	return s.writeSmartShelf(w, r, http.StatusOK, id)
}

func (s *Server) deleteSmartShelf(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "shelfId", "smart shelf")
	if err != nil {
		return err
	}
	if err := s.Shelves.DeleteSmart(r.Context(), library.ScopeOf(*UserFrom(r.Context())), id); err != nil {
		return smartShelfError(err)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

type smartBooksQuery struct {
	Sort   string `query:"sort" enum:"title,author,added" doc:"What the books are ordered by; title when left out."`
	Order  string `query:"order" enum:"asc,desc" doc:"The direction; asc when left out."`
	Cursor string `query:"cursor" doc:"The nextCursor of the page before."`
	Limit  int    `query:"limit" doc:"How many books a page holds, at most 200; 50 when left out."`
}

func (s *Server) listSmartShelfBooks(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "shelfId", "smart shelf")
	if err != nil {
		return err
	}
	var q smartBooksQuery
	if err := decodeQuery(r, &q); err != nil {
		return err
	}
	p := catalog.ListParams{
		Order: cmp.Or(q.Sort, catalog.OrderTitle), Desc: q.Order == "desc", After: q.Cursor, Limit: cmp.Or(q.Limit, defaultPage),
	}
	page, err := s.Shelves.SmartBooks(r.Context(), library.ScopeOf(*UserFrom(r.Context())), id, p)
	switch {
	case errors.Is(err, catalog.ErrBadCursor):
		return ErrValidation(map[string]string{"cursor": "This cursor belongs to another list; start again from the first page."})
	case err != nil:
		return smartShelfError(err)
	}
	out := bookList{Books: make([]bookSummary, len(page.Books)), NextCursor: page.Next}
	for i, b := range page.Books {
		out.Books[i] = summaryOf(b)
	}
	writeJSON(w, r, http.StatusOK, out)
	return nil
}

type countBooksQuery struct {
	Library string `query:"library" doc:"A library's ID; left out, every library the caller may see."`
	Filter  string `query:"filter" doc:"A rule tree as JSON, as listBooks takes it."`
}

type bookCount struct {
	// Count is how many books the caller may see match.
	Count int `json:"count"`
}

func (s *Server) countBooks(w http.ResponseWriter, r *http.Request) error {
	var q countBooksQuery
	if err := decodeQuery(r, &q); err != nil {
		return err
	}
	libraryID, tree, err := bookSelection(q.Library, q.Filter)
	if err != nil {
		return err
	}
	n, err := s.Books.Count(r.Context(), library.ScopeOf(*UserFrom(r.Context())), libraryID, tree)
	switch {
	case catalog.IsFilterError(err):
		return ErrValidation(map[string]string{"filter": err.Error()})
	case err != nil:
		return err
	}
	writeJSON(w, r, http.StatusOK, bookCount{Count: n})
	return nil
}

func smartShelfError(err error) error {
	var invalid shelves.Invalid
	switch {
	case errors.As(err, &invalid):
		return ErrValidation(invalid)
	case errors.Is(err, shelves.ErrNotFound):
		return ErrNotFound("There is no such smart shelf.")
	case errors.Is(err, shelves.ErrNotOwner):
		return ErrForbidden("Only its owner changes a smart shelf.")
	}
	return err
}
