package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/library"
	"github.com/praetorianer777/gotome/backend/internal/reviews"
)

// sharing is whether the caller shares their ratings and reviews.
type sharing struct {
	// ShareReviews is whether others on the server see the caller's stars
	// and reviews of books they both see.
	ShareReviews bool `json:"shareReviews"`
}

func (s *Server) getSharing(w http.ResponseWriter, r *http.Request) error {
	on, err := s.Reviews.Sharing(r.Context(), UserFrom(r.Context()).ID)
	if err != nil {
		return err
	}
	writeJSON(w, r, http.StatusOK, sharing{ShareReviews: on})
	return nil
}

func (s *Server) putSharing(w http.ResponseWriter, r *http.Request) error {
	var req sharing
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	if err := s.Reviews.SetSharing(r.Context(), UserFrom(r.Context()).ID, req.ShareReviews); err != nil {
		return err
	}
	writeJSON(w, r, http.StatusOK, req)
	return nil
}

// sharedReview is a review somebody shares with the caller.
type sharedReview struct {
	// Author is the reviewer's user name.
	Author string `json:"author"`
	// Stars is their rating of the book, 0 when they gave none.
	Stars     int       `json:"stars"`
	Body      string    `json:"body"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// ownReview is the caller's own review of a book.
type ownReview struct {
	Body      string    `json:"body"`
	Private   bool      `json:"private"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// bookReviews is what others on the server shared about a book.
type bookReviews struct {
	// Stars is how many shared ratings Average is the mean of.
	Stars int `json:"stars"`
	// Average is the mean of the shared stars, 0 when none are shared.
	Average float64        `json:"average"`
	Reviews []sharedReview `json:"reviews"`
	// Mine is the caller's own review, if they wrote one.
	Mine *ownReview `json:"mine,omitempty"`
}

func (s *Server) listBookReviews(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "bookId", "book")
	if err != nil {
		return err
	}
	got, err := s.Reviews.OfBook(r.Context(), library.ScopeOf(*UserFrom(r.Context())), id)
	if errors.Is(err, reviews.ErrNotFound) {
		return ErrNotFound("There is no such book.")
	}
	if err != nil {
		return err
	}
	out := bookReviews{Stars: got.Stars, Average: got.Average, Reviews: make([]sharedReview, len(got.Reviews))}
	for i, v := range got.Reviews {
		out.Reviews[i] = sharedReview{Author: v.Author, Stars: v.Stars, Body: v.Body, UpdatedAt: v.UpdatedAt}
	}
	if got.Mine != nil {
		out.Mine = &ownReview{Body: got.Mine.Body, Private: got.Mine.Private, UpdatedAt: got.Mine.UpdatedAt}
	}
	writeJSON(w, r, http.StatusOK, out)
	return nil
}

type reviewRequest struct {
	// Body is the review; empty removes it.
	Body string `json:"body"`
	// Private keeps the review, and the stars with it, from others even
	// while the caller shares.
	Private bool `json:"private"`
}

func (s *Server) setReview(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "bookId", "book")
	if err != nil {
		return err
	}
	var req reviewRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	if len(req.Body) > reviews.MaxBytes {
		return ErrValidation(map[string]string{"body": "A review may be at most 20,000 characters."})
	}
	err = s.Reviews.Set(r.Context(), library.ScopeOf(*UserFrom(r.Context())), id, req.Body, req.Private)
	if errors.Is(err, reviews.ErrNotFound) {
		return ErrNotFound("There is no such book.")
	}
	if err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// personReview is a review on a person's page.
type personReview struct {
	BookID     uuid.UUID `json:"bookId"`
	Title      string    `json:"title"`
	AuthorSort string    `json:"authorSort"`
	Stars      int       `json:"stars"`
	Body       string    `json:"body"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

// personReviews is what a person shares with the caller.
type personReviews struct {
	Username string         `json:"username"`
	Reviews  []personReview `json:"reviews"`
}

func (s *Server) listPersonReviews(w http.ResponseWriter, r *http.Request) error {
	name := chi.URLParam(r, "username")
	list, err := s.Reviews.OfPerson(r.Context(), library.ScopeOf(*UserFrom(r.Context())), name)
	if errors.Is(err, reviews.ErrNotFound) {
		return ErrNotFound("There is no such person.")
	}
	if err != nil {
		return err
	}
	out := personReviews{Username: name, Reviews: make([]personReview, len(list))}
	for i, v := range list {
		out.Reviews[i] = personReview{BookID: v.BookID, Title: v.Title, AuthorSort: v.AuthorSort, Stars: v.Stars, Body: v.Body, UpdatedAt: v.UpdatedAt}
	}
	writeJSON(w, r, http.StatusOK, out)
	return nil
}
