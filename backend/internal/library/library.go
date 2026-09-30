// Package library manages libraries: the directories books live in, and the
// boundary of who may see them.
package library

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/praetorianer777/gotome/backend/internal/auth"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
)

// Modes and visibilities, as stored.
const (
	// ModeManaged libraries are laid out by GOtome; uploads land in them.
	ModeManaged = "managed"
	// ModeExternal libraries are folders somebody else arranges, scanned in place.
	ModeExternal = "external"

	VisibilityShared  = "shared"
	VisibilityPrivate = "private"

	maxNameLen = 100
)

var (
	// ErrNotFound covers a library that does not exist and one the viewer may
	// not see alike: the answer must not say which.
	ErrNotFound = errors.New("no such library")
	// ErrNoSuchUser is returned when adding a member who does not exist.
	ErrNoSuchUser = errors.New("no such user")
)

// ValidationError says which fields were refused and why, in words for the
// person who typed them.
type ValidationError struct {
	Fields map[string]string
}

func (e *ValidationError) Error() string { return "invalid library details" }

// Scope is who is asking, as the visibility rule needs to know it. Every
// query that returns anything belonging to a library takes a Scope and hands
// it to the database's visible_library_ids function, which is the one place
// the rule is written down.
type Scope struct {
	Viewer uuid.UUID
	// SeesAll is true for administrators, who run the storage every library
	// lives on and so see all of them.
	SeesAll bool
}

// ScopeOf is the scope of a signed-in user.
func ScopeOf(u auth.User) Scope {
	return Scope{Viewer: u.ID, SeesAll: auth.Allows(u.Role, auth.StorageManage)}
}

// Library is one library.
type Library struct {
	ID         uuid.UUID  `json:"id"`
	Name       string     `json:"name"`
	RootPath   string     `json:"rootPath"`
	Mode       string     `json:"mode"`
	Writable   bool       `json:"writable"`
	Visibility string     `json:"visibility"`
	OwnerID    *uuid.UUID `json:"ownerId,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
}

// Member is somebody who may see a private library.
type Member struct {
	ID       uuid.UUID `json:"id"`
	Username string    `json:"username"`
	Role     string    `json:"role"`
	AddedAt  time.Time `json:"addedAt"`
}

// NewLibrary is what creating a library takes.
type NewLibrary struct {
	Name string
	Mode string
	// RootPath is required for an external library, where it must exist. A
	// managed one may leave it empty and gets a directory of its own.
	RootPath   string
	Visibility string
	// Writable is only asked of external libraries; managed ones always are.
	Writable bool
}

// Changes is what may be changed afterwards; nil leaves a field as it is.
// Where a library lives and who arranges it are fixed at creation: moving
// one is a copy on disk, not an edit.
type Changes struct {
	Name       *string
	Visibility *string
	Writable   *bool
}

// Service manages libraries.
type Service struct {
	pool *pgxpool.Pool
	// managedRoot is the directory managed libraries are created under.
	managedRoot string
}

// NewService returns a Service. Managed libraries without a path of their own
// are created under dataDir/libraries.
func NewService(pool *pgxpool.Pool, dataDir string) *Service {
	return &Service{pool: pool, managedRoot: filepath.Join(dataDir, "libraries")}
}

// List returns the libraries the scope may see, by name.
func (s *Service) List(ctx context.Context, scope Scope) ([]Library, error) {
	rows, err := sqlc.New(s.pool).ListVisibleLibraries(ctx, sqlc.ListVisibleLibrariesParams{
		Viewer: scope.Viewer, SeesAll: scope.SeesAll,
	})
	if err != nil {
		return nil, err
	}
	libraries := make([]Library, len(rows))
	for i, row := range rows {
		libraries[i] = fromRow(row)
	}
	return libraries, nil
}

// Get returns one library, or ErrNotFound when it does not exist or the scope
// may not see it.
func (s *Service) Get(ctx context.Context, scope Scope, id uuid.UUID) (Library, error) {
	row, err := sqlc.New(s.pool).GetVisibleLibrary(ctx, sqlc.GetVisibleLibraryParams{
		ID: id, Viewer: scope.Viewer, SeesAll: scope.SeesAll,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Library{}, ErrNotFound
	}
	if err != nil {
		return Library{}, err
	}
	return fromRow(row), nil
}

// Create adds a library owned by owner.
func (s *Service) Create(ctx context.Context, owner uuid.UUID, in NewLibrary) (Library, error) {
	q := sqlc.New(s.pool)
	fields := map[string]string{}

	name := validName(in.Name, fields)
	if in.Mode != ModeManaged && in.Mode != ModeExternal {
		fields["mode"] = "Choose whether GOtome manages this library's folder or only reads it."
	}
	if in.Visibility != VisibilityShared && in.Visibility != VisibilityPrivate {
		fields["visibility"] = "Choose who may see this library."
	}

	id, err := uuid.NewV7()
	if err != nil {
		return Library{}, err
	}
	root := strings.TrimSpace(in.RootPath)
	writable := in.Writable
	switch in.Mode {
	case ModeManaged:
		writable = true
		if root == "" {
			root = filepath.Join(s.managedRoot, id.String())
		}
	case ModeExternal:
		if root == "" {
			fields["rootPath"] = "Say which folder the books are in."
		}
	}
	if root != "" {
		if !filepath.IsAbs(root) {
			fields["rootPath"] = "Give the folder as a full path, such as /books."
		} else {
			root = filepath.Clean(root)
			if msg, err := s.overlap(ctx, q, root); err != nil {
				return Library{}, err
			} else if msg != "" {
				fields["rootPath"] = msg
			}
		}
	}
	if len(fields) > 0 {
		return Library{}, &ValidationError{Fields: fields}
	}

	// The folder is touched only once everything else is in order.
	switch in.Mode {
	case ModeManaged:
		if err := os.MkdirAll(root, 0o750); err != nil {
			return Library{}, &ValidationError{Fields: map[string]string{
				"rootPath": "GOtome cannot create that folder: " + reason(err) + ".",
			}}
		}
	case ModeExternal:
		info, err := os.Stat(root)
		if err != nil || !info.IsDir() {
			return Library{}, &ValidationError{Fields: map[string]string{
				"rootPath": "There is no such folder inside the GOtome container. Mount it in the compose file first.",
			}}
		}
	}

	row, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		ID: id, Name: name, RootPath: root, Mode: in.Mode,
		Writable: writable, Visibility: in.Visibility, OwnerID: &owner,
	})
	if err != nil {
		return Library{}, conflict(err)
	}
	return fromRow(row), nil
}

// Update changes a library's name, visibility or whether GOtome may write to it.
func (s *Service) Update(ctx context.Context, id uuid.UUID, c Changes) (Library, error) {
	q := sqlc.New(s.pool)
	fields := map[string]string{}
	if c.Name != nil {
		name := validName(*c.Name, fields)
		c.Name = &name
	}
	if c.Visibility != nil && *c.Visibility != VisibilityShared && *c.Visibility != VisibilityPrivate {
		fields["visibility"] = "Choose who may see this library."
	}
	if len(fields) > 0 {
		return Library{}, &ValidationError{Fields: fields}
	}

	row, err := q.UpdateLibrary(ctx, sqlc.UpdateLibraryParams{ID: id, Name: c.Name, Visibility: c.Visibility, Writable: c.Writable})
	if errors.Is(err, pgx.ErrNoRows) {
		return Library{}, ErrNotFound
	}
	if err != nil {
		var pgErr *pgconn.PgError
		// The table's own rule: a managed library is always writable.
		if errors.As(err, &pgErr) && pgErr.Code == "23514" {
			return Library{}, &ValidationError{Fields: map[string]string{
				"writable": "GOtome manages this library's folder, so it always writes to it.",
			}}
		}
		return Library{}, conflict(err)
	}
	return fromRow(row), nil
}

// Delete removes the library from GOtome. The folder and every file in it
// stay where they are: forgetting a library must never cost anybody a book.
func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	n, err := sqlc.New(s.pool).DeleteLibrary(ctx, id)
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// Members lists who may see a private library besides its owner.
func (s *Service) Members(ctx context.Context, id uuid.UUID) ([]Member, error) {
	rows, err := sqlc.New(s.pool).ListLibraryMembers(ctx, id)
	if err != nil {
		return nil, err
	}
	members := make([]Member, len(rows))
	for i, row := range rows {
		members[i] = Member{ID: row.ID, Username: row.Username, Role: row.Role, AddedAt: row.AddedAt}
	}
	return members, nil
}

// AddMember lets a user see the library. Adding somebody twice changes nothing.
func (s *Service) AddMember(ctx context.Context, id, userID uuid.UUID) error {
	err := sqlc.New(s.pool).AddLibraryMember(ctx, sqlc.AddLibraryMemberParams{LibraryID: id, UserID: userID})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" {
		if pgErr.ConstraintName == "library_members_user_id_fkey" {
			return ErrNoSuchUser
		}
		return ErrNotFound
	}
	return err
}

// RemoveMember takes the library away from a user again. Removing somebody
// who is not a member changes nothing.
func (s *Service) RemoveMember(ctx context.Context, id, userID uuid.UUID) error {
	_, err := sqlc.New(s.pool).RemoveLibraryMember(ctx, sqlc.RemoveLibraryMemberParams{LibraryID: id, UserID: userID})
	return err
}

// overlap says why root cannot be a library's folder next to the existing
// ones, or "" when it can. Two libraries looking at the same files would each
// import them, and neither would know which one a file belongs to.
func (s *Service) overlap(ctx context.Context, q *sqlc.Queries, root string) (string, error) {
	roots, err := q.ListLibraryRoots(ctx)
	if err != nil {
		return "", err
	}
	for _, other := range roots {
		switch {
		case other.RootPath == root:
			return "Another library already uses this folder.", nil
		case within(root, other.RootPath):
			return "This folder is inside another library's folder.", nil
		case within(other.RootPath, root):
			return "Another library's folder is inside this one.", nil
		}
	}
	return "", nil
}

// within reports whether child lies below parent.
func within(child, parent string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func validName(name string, fields map[string]string) string {
	name = strings.Join(strings.Fields(name), " ")
	switch {
	case name == "":
		fields["name"] = "Give the library a name."
	case len(name) > maxNameLen:
		fields["name"] = fmt.Sprintf("A name has at most %d characters.", maxNameLen)
	}
	return name
}

// conflict turns a unique violation into a message at the field it concerns.
func conflict(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		return err
	}
	if pgErr.ConstraintName == "libraries_root_path_key" {
		return &ValidationError{Fields: map[string]string{"rootPath": "Another library already uses this folder."}}
	}
	return &ValidationError{Fields: map[string]string{"name": "Another library already has this name."}}
}

// reason is an OS error without the path, which the person just typed.
func reason(err error) string {
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		return pathErr.Err.Error()
	}
	return err.Error()
}

func fromRow(row sqlc.Library) Library {
	return Library{
		ID: row.ID, Name: row.Name, RootPath: row.RootPath, Mode: row.Mode,
		Writable: row.Writable, Visibility: row.Visibility, OwnerID: row.OwnerID, CreatedAt: row.CreatedAt,
	}
}
