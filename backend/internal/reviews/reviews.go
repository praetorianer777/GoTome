// Package reviews is what people say of books to others on the same
// server (#427): a review of text and, from the stars they gave, a rating.
// Nothing is shared unless the person chose to, and a review is shared
// only to those who see the book: it follows the libraries a viewer sees,
// never the reviewer's.
package reviews

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

// ErrNotFound is a book the viewer may not see, or a person who shares
// nothing with them, which are answered alike.
var ErrNotFound = errors.New("not found")

// MaxBytes is the longest review.
const MaxBytes = 20000

// Service keeps reviews and the choice to share them.
type Service struct {
	pool *pgxpool.Pool
}

// NewService returns a Service on the pool.
func NewService(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

// Sharing says whether the user shares their ratings and reviews.
func (s *Service) Sharing(ctx context.Context, user uuid.UUID) (bool, error) {
	return sqlc.New(s.pool).GetShareReviews(ctx, user)
}

// SetSharing turns the user's sharing on or off. Off hides everything they
// shared at once, as nothing is copied: what others see is read each time.
func (s *Service) SetSharing(ctx context.Context, user uuid.UUID, on bool) error {
	return sqlc.New(s.pool).SetShareReviews(ctx, sqlc.SetShareReviewsParams{ID: user, ShareReviews: on})
}

// Mine is the viewer's own review of a book.
type Mine struct {
	Body      string
	Private   bool
	UpdatedAt time.Time
}

// Shared is a review somebody shares.
type Shared struct {
	Author    string
	Stars     int
	Body      string
	UpdatedAt time.Time
}

// OfBook is what a viewer sees of others' views on a book.
type OfBook struct {
	// Stars is how many shared ratings Average is the mean of.
	Stars   int
	Average float64
	Reviews []Shared
	Mine    *Mine
}

// OfBook returns the shared ratings and reviews of a book, and the
// viewer's own review. A book the viewer may not see is ErrNotFound.
func (s *Service) OfBook(ctx context.Context, scope library.Scope, book uuid.UUID) (OfBook, error) {
	q := sqlc.New(s.pool)
	var out OfBook
	if err := s.mustSee(ctx, q, scope, book); err != nil {
		return out, err
	}
	stars, err := q.SharedStars(ctx, sqlc.SharedStarsParams{BookID: book, Viewer: scope.Viewer, SeesAll: scope.SeesAll})
	if err != nil {
		return out, err
	}
	out.Stars, out.Average = int(stars.Stars), stars.Average
	rows, err := q.ListSharedReviews(ctx, sqlc.ListSharedReviewsParams{BookID: book, Viewer: scope.Viewer, SeesAll: scope.SeesAll})
	if err != nil {
		return out, err
	}
	out.Reviews = make([]Shared, len(rows))
	for i, r := range rows {
		out.Reviews[i] = Shared{Author: r.Username, Stars: stars16(r.Rating), Body: r.Body, UpdatedAt: r.UpdatedAt}
	}
	mine, err := q.GetBookReview(ctx, sqlc.GetBookReviewParams{UserID: scope.Viewer, BookID: book})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return out, err
	default:
		out.Mine = &Mine{Body: mine.Body, Private: mine.Private, UpdatedAt: mine.UpdatedAt}
	}
	return out, nil
}

func stars16(n *int16) int {
	if n == nil {
		return 0
	}
	return int(*n)
}

func (s *Service) mustSee(ctx context.Context, q *sqlc.Queries, scope library.Scope, book uuid.UUID) error {
	ok, err := q.ReviewableBook(ctx, sqlc.ReviewableBookParams{BookID: book, Viewer: scope.Viewer, SeesAll: scope.SeesAll})
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotFound
	}
	return nil
}

// Set writes the viewer's review of a book they may see; an empty text
// removes it.
func (s *Service) Set(ctx context.Context, scope library.Scope, book uuid.UUID, body string, private bool) error {
	body = strings.TrimSpace(body)
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		visible, err := q.LockVisibleBookIDs(ctx, sqlc.LockVisibleBookIDsParams{Ids: []uuid.UUID{book}, Viewer: scope.Viewer, SeesAll: scope.SeesAll})
		if err != nil {
			return err
		}
		if len(visible) == 0 {
			return ErrNotFound
		}
		if body == "" {
			return q.DeleteBookReview(ctx, sqlc.DeleteBookReviewParams{UserID: scope.Viewer, BookID: book})
		}
		return q.PutBookReview(ctx, sqlc.PutBookReviewParams{UserID: scope.Viewer, BookID: book, Body: body, Private: private})
	})
}

// PersonReview is a shared review as a person's page lists it.
type PersonReview struct {
	BookID     uuid.UUID
	Title      string
	AuthorSort string
	Stars      int
	Body       string
	UpdatedAt  time.Time
}

// OfPerson lists the reviews a person shares on books the viewer may see.
// A person who shares none with the viewer, or whom nobody is, is
// ErrNotFound: the two cannot be told apart.
func (s *Service) OfPerson(ctx context.Context, scope library.Scope, username string) ([]PersonReview, error) {
	rows, err := sqlc.New(s.pool).ListPersonReviews(ctx, sqlc.ListPersonReviewsParams{Username: username, Viewer: scope.Viewer, SeesAll: scope.SeesAll})
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, ErrNotFound
	}
	out := make([]PersonReview, len(rows))
	for i, r := range rows {
		out[i] = PersonReview{BookID: r.BookID, Title: r.Title, AuthorSort: r.AuthorSort, Stars: stars16(r.Rating), Body: r.Body, UpdatedAt: r.UpdatedAt}
	}
	return out, nil
}
