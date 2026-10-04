// Package shelves is what people put books on: collections, filled by hand
// in an order their owner chooses. A shelf is never an access boundary: it
// shows each viewer only the books of libraries they may see.
package shelves

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

// Who may look at a collection.
const (
	// Private collections are their owner's alone.
	Private = "private"
	// Shared collections every signed-in person may look at.
	Shared = "shared"
)

// MaxBooks is the most books a collection holds.
const MaxBooks = 5000

// MaxName is the longest a collection's name may be, in characters.
const MaxName = 200

var (
	// ErrNotFound covers a collection that does not exist and one the
	// viewer may not see alike.
	ErrNotFound = errors.New("no such collection")
	// ErrNotOwner is a change to a shared collection by someone who may look
	// at it but does not own it.
	ErrNotOwner = errors.New("only its owner changes a collection")
	// ErrFull is an addition past MaxBooks.
	ErrFull = fmt.Errorf("a collection holds at most %d books", MaxBooks)
)

// Invalid says, per field, what is wrong with a request.
type Invalid map[string]string

func (e Invalid) Error() string {
	var parts []string
	for _, k := range slices.Sorted(maps.Keys(e)) {
		parts = append(parts, k+": "+e[k])
	}
	return strings.Join(parts, "; ")
}

// Collection is a shelf as one viewer sees it.
type Collection struct {
	ID          uuid.UUID
	OwnerID     uuid.UUID
	OwnerName   string
	Name        string
	Description string
	Visibility  string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	// Books is how many of its books the viewer may see.
	Books int
	// HasBook is whether it holds the book a list was asked about.
	HasBook bool
}

// Details are what a collection's owner sets on it.
type Details struct {
	Name        string
	Description string
	Visibility  string
}

func (d *Details) check() error {
	d.Name = strings.TrimSpace(d.Name)
	d.Description = strings.TrimSpace(d.Description)
	problems := Invalid{}
	switch {
	case d.Name == "":
		problems["name"] = "Give the collection a name."
	case len([]rune(d.Name)) > MaxName:
		problems["name"] = fmt.Sprintf("A name has at most %d characters.", MaxName)
	}
	if d.Visibility == "" {
		d.Visibility = Private
	}
	if d.Visibility != Private && d.Visibility != Shared {
		problems["visibility"] = "Visibility is private or shared."
	}
	if len(problems) > 0 {
		return problems
	}
	return nil
}

// Service keeps the collections.
type Service struct {
	pool *pgxpool.Pool
}

// NewService returns a Service on the pool.
func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

// List returns the scope's own collections, then everyone else's shared
// ones. With book set, each says whether it holds that book.
func (s *Service) List(ctx context.Context, scope library.Scope, book *uuid.UUID) ([]Collection, error) {
	rows, err := sqlc.New(s.pool).ListCollections(ctx, sqlc.ListCollectionsParams{
		Viewer: scope.Viewer, SeesAll: scope.SeesAll, Book: book,
	})
	if err != nil {
		return nil, err
	}
	out := make([]Collection, len(rows))
	for i, r := range rows {
		out[i] = Collection{
			ID: r.ID, OwnerID: r.OwnerID, OwnerName: r.OwnerName, Name: r.Name, Description: r.Description,
			Visibility: r.Visibility, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, Books: int(r.Books), HasBook: r.HasBook,
		}
	}
	return out, nil
}

// Get returns a collection the scope may look at, and the IDs of its books
// they may see, in its order; ErrNotFound for one they may not.
func (s *Service) Get(ctx context.Context, scope library.Scope, id uuid.UUID) (Collection, []uuid.UUID, error) {
	q := sqlc.New(s.pool)
	r, err := q.GetCollection(ctx, sqlc.GetCollectionParams{ID: id, Viewer: scope.Viewer})
	if errors.Is(err, pgx.ErrNoRows) {
		return Collection{}, nil, ErrNotFound
	}
	if err != nil {
		return Collection{}, nil, err
	}
	books, err := q.CollectionBookIDs(ctx, sqlc.CollectionBookIDsParams{CollectionID: id, Viewer: scope.Viewer, SeesAll: scope.SeesAll})
	if err != nil {
		return Collection{}, nil, err
	}
	return Collection{
		ID: r.ID, OwnerID: r.OwnerID, OwnerName: r.OwnerName, Name: r.Name, Description: r.Description,
		Visibility: r.Visibility, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, Books: len(books),
	}, books, nil
}

// Create makes a collection of the scope's user, private unless the
// details say shared, and returns its ID.
func (s *Service) Create(ctx context.Context, scope library.Scope, d Details) (uuid.UUID, error) {
	if err := d.check(); err != nil {
		return uuid.Nil, err
	}
	return sqlc.New(s.pool).CreateCollection(ctx, sqlc.CreateCollectionParams{
		OwnerID: scope.Viewer, Name: d.Name, Description: d.Description, Visibility: d.Visibility,
	})
}

// Update sets the details of a collection the scope's user owns.
func (s *Service) Update(ctx context.Context, scope library.Scope, id uuid.UUID, d Details) error {
	if err := d.check(); err != nil {
		return err
	}
	return s.owned(ctx, scope, id, func(q *sqlc.Queries) error {
		return q.UpdateCollection(ctx, sqlc.UpdateCollectionParams{
			ID: id, Name: d.Name, Description: d.Description, Visibility: d.Visibility,
		})
	})
}

// Delete removes a collection the scope's user owns. The books stay.
func (s *Service) Delete(ctx context.Context, scope library.Scope, id uuid.UUID) error {
	return s.owned(ctx, scope, id, func(q *sqlc.Queries) error {
		return q.DeleteCollection(ctx, id)
	})
}

// Add puts the books among ids the scope may see at the end of a
// collection its user owns, in the order of ids, and returns how many were
// not in it yet. Books it holds already keep their place.
func (s *Service) Add(ctx context.Context, scope library.Scope, id uuid.UUID, ids []uuid.UUID) (int, error) {
	var added int64
	err := s.owned(ctx, scope, id, func(q *sqlc.Queries) error {
		var err error
		added, err = q.AddCollectionItems(ctx, sqlc.AddCollectionItemsParams{
			CollectionID: id, Ids: ids, Viewer: scope.Viewer, SeesAll: scope.SeesAll,
		})
		if err != nil || added == 0 {
			return err
		}
		n, err := q.CountCollectionItems(ctx, id)
		if err != nil {
			return err
		}
		if n > MaxBooks {
			return ErrFull
		}
		return q.TouchCollection(ctx, id)
	})
	return int(added), err
}

// Remove takes a book the scope may see out of a collection its user owns;
// ErrNotFound when the collection does not hold it.
func (s *Service) Remove(ctx context.Context, scope library.Scope, id, book uuid.UUID) error {
	return s.owned(ctx, scope, id, func(q *sqlc.Queries) error {
		n, err := q.RemoveCollectionItem(ctx, sqlc.RemoveCollectionItemParams{
			CollectionID: id, BookID: book, Viewer: scope.Viewer, SeesAll: scope.SeesAll,
		})
		if err != nil {
			return err
		}
		if n == 0 {
			return ErrNotFound
		}
		return q.TouchCollection(ctx, id)
	})
}

// Reorder puts the books named, each one of the collection's books the
// scope may see, into the order given. They take the places they held
// between them, so the books not named, among them those of libraries the
// owner no longer sees, stay where they are.
func (s *Service) Reorder(ctx context.Context, scope library.Scope, id uuid.UUID, order []uuid.UUID) error {
	return s.owned(ctx, scope, id, func(q *sqlc.Queries) error {
		visible, err := q.CollectionBookIDs(ctx, sqlc.CollectionBookIDsParams{CollectionID: id, Viewer: scope.Viewer, SeesAll: scope.SeesAll})
		if err != nil {
			return err
		}
		items, err := q.CollectionItems(ctx, id)
		if err != nil {
			return err
		}
		named := make(map[uuid.UUID]bool, len(order))
		for _, b := range order {
			if named[b] {
				return Invalid{"books": "A book is named twice."}
			}
			if !slices.Contains(visible, b) {
				return Invalid{"books": "A book named is not in the collection."}
			}
			named[b] = true
		}
		// Positions may tie, so they are first made 1 to n in the current
		// order; the named books then share out the places they hold.
		positions := make([]int32, 0, len(order))
		for i, it := range items {
			if named[it.BookID] {
				positions = append(positions, int32(i+1))
			}
		}
		all := make([]uuid.UUID, len(items))
		spread := make([]int32, len(items))
		for i, it := range items {
			all[i], spread[i] = it.BookID, int32(i+1)
		}
		if err := q.SetCollectionPositions(ctx, sqlc.SetCollectionPositionsParams{CollectionID: id, Ids: all, Positions: spread}); err != nil {
			return err
		}
		if err := q.SetCollectionPositions(ctx, sqlc.SetCollectionPositionsParams{CollectionID: id, Ids: order, Positions: positions}); err != nil {
			return err
		}
		return q.TouchCollection(ctx, id)
	})
}

// owned runs change in a transaction that holds the collection locked, when
// the scope's user owns it. A collection they may look at but do not own is
// ErrNotOwner, any other ErrNotFound.
func (s *Service) owned(ctx context.Context, scope library.Scope, id uuid.UUID, change func(*sqlc.Queries) error) error {
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		c, err := q.LockCollection(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if c.OwnerID != scope.Viewer {
			if c.Visibility == Shared {
				return ErrNotOwner
			}
			return ErrNotFound
		}
		return change(q)
	})
}
