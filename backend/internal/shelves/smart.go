package shelves

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/filter"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

// SmartShelf is a rule tree under a name. What is on it is worked out when
// it is looked at, for whoever looks: rules on status and rating are theirs.
type SmartShelf struct {
	ID         uuid.UUID
	OwnerID    uuid.UUID
	OwnerName  string
	Name       string
	Filter     filter.Node
	Visibility string
	UpdatedAt  time.Time
	// Books is how many books the viewer may see match it.
	Books int
}

// SmartDetails are what a smart shelf's owner sets on it.
type SmartDetails struct {
	Name       string
	Visibility string
	Filter     filter.Node
}

// check finds what is wrong with the details, the rule tree included, and
// returns the tree as it is stored.
func (d *SmartDetails) check() ([]byte, error) {
	named := Details{Name: d.Name, Visibility: d.Visibility}
	var problems Invalid
	if !errors.As(named.check(), &problems) {
		problems = Invalid{}
	}
	d.Name, d.Visibility = named.Name, named.Visibility
	if err := catalog.CheckFilter(d.Filter); err != nil {
		problems["filter"] = err.Error()
	}
	if len(problems) > 0 {
		return nil, problems
	}
	return json.Marshal(d.Filter)
}

// SmartShelves returns the scope's own smart shelves, then everyone else's
// shared ones, each with how many books it holds for the viewer.
func (s *Service) SmartShelves(ctx context.Context, scope library.Scope) ([]SmartShelf, error) {
	rows, err := sqlc.New(s.pool).ListSmartShelves(ctx, scope.Viewer)
	if err != nil {
		return nil, err
	}
	out := make([]SmartShelf, len(rows))
	for i, r := range rows {
		if out[i], err = s.smartShelf(ctx, scope, sqlc.GetSmartShelfRow(r)); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// SmartShelf returns a smart shelf the scope may look at; ErrNotFound for
// one they may not.
func (s *Service) SmartShelf(ctx context.Context, scope library.Scope, id uuid.UUID) (SmartShelf, error) {
	r, err := sqlc.New(s.pool).GetSmartShelf(ctx, sqlc.GetSmartShelfParams{ID: id, Viewer: scope.Viewer})
	if errors.Is(err, pgx.ErrNoRows) {
		return SmartShelf{}, ErrNotFound
	}
	if err != nil {
		return SmartShelf{}, err
	}
	return s.smartShelf(ctx, scope, r)
}

func (s *Service) smartShelf(ctx context.Context, scope library.Scope, r sqlc.GetSmartShelfRow) (SmartShelf, error) {
	shelf := SmartShelf{
		ID: r.ID, OwnerID: r.OwnerID, OwnerName: r.OwnerName, Name: r.Name, Visibility: r.Visibility, UpdatedAt: r.UpdatedAt,
	}
	tree, err := filter.Parse(r.Filter)
	if err != nil {
		return SmartShelf{}, err
	}
	shelf.Filter = tree
	shelf.Books, err = s.books.Count(ctx, scope, nil, tree)
	return shelf, err
}

// SmartBooks returns a page of what is on a smart shelf the scope may look
// at, for them; p's Filter is the shelf's.
func (s *Service) SmartBooks(ctx context.Context, scope library.Scope, id uuid.UUID, p catalog.ListParams) (catalog.Page, error) {
	r, err := sqlc.New(s.pool).GetSmartShelf(ctx, sqlc.GetSmartShelfParams{ID: id, Viewer: scope.Viewer})
	if errors.Is(err, pgx.ErrNoRows) {
		return catalog.Page{}, ErrNotFound
	}
	if err != nil {
		return catalog.Page{}, err
	}
	if p.Filter, err = filter.Parse(r.Filter); err != nil {
		return catalog.Page{}, err
	}
	return s.books.List(ctx, scope, p)
}

// CreateSmart makes a smart shelf of the scope's user and returns its ID.
// A rule tree the library's filter would refuse is refused here too.
func (s *Service) CreateSmart(ctx context.Context, scope library.Scope, d SmartDetails) (uuid.UUID, error) {
	tree, err := d.check()
	if err != nil {
		return uuid.Nil, err
	}
	return sqlc.New(s.pool).CreateSmartShelf(ctx, sqlc.CreateSmartShelfParams{
		OwnerID: scope.Viewer, Name: d.Name, Filter: tree, Visibility: d.Visibility,
	})
}

// UpdateSmart sets the name, rules and visibility of a smart shelf the
// scope's user owns.
func (s *Service) UpdateSmart(ctx context.Context, scope library.Scope, id uuid.UUID, d SmartDetails) error {
	tree, err := d.check()
	if err != nil {
		return err
	}
	return s.ownedBy(ctx, scope, lockSmart(ctx, id), func(q *sqlc.Queries) error {
		return q.UpdateSmartShelf(ctx, sqlc.UpdateSmartShelfParams{ID: id, Name: d.Name, Filter: tree, Visibility: d.Visibility})
	})
}

// DeleteSmart removes a smart shelf the scope's user owns.
func (s *Service) DeleteSmart(ctx context.Context, scope library.Scope, id uuid.UUID) error {
	return s.ownedBy(ctx, scope, lockSmart(ctx, id), func(q *sqlc.Queries) error {
		return q.DeleteSmartShelf(ctx, id)
	})
}

func lockSmart(ctx context.Context, id uuid.UUID) func(*sqlc.Queries) (sqlc.LockCollectionRow, error) {
	return func(q *sqlc.Queries) (sqlc.LockCollectionRow, error) {
		r, err := q.LockSmartShelf(ctx, id)
		return sqlc.LockCollectionRow(r), err
	}
}
