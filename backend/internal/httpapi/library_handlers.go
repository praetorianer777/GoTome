package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/auth"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

// libraryResponse is a library as the API shows it.
type libraryResponse struct {
	ID         uuid.UUID `json:"id"`
	Name       string    `json:"name"`
	Mode       string    `json:"mode"`
	Writable   bool      `json:"writable"`
	Visibility string    `json:"visibility"`
	CreatedAt  time.Time `json:"createdAt"`
	// RootPath and OwnerID are for those who manage storage. Where the files
	// lie on the server is nothing a reader needs to be told.
	RootPath *string    `json:"rootPath,omitempty"`
	OwnerID  *uuid.UUID `json:"ownerId,omitempty"`
}

type libraryList struct {
	Libraries []libraryResponse `json:"libraries"`
}

type memberList struct {
	Members []library.Member `json:"members"`
}

type createLibraryRequest struct {
	Name string `json:"name"`
	// Mode is "managed" or "external".
	Mode string `json:"mode"`
	// RootPath is the folder inside the app container. Required for an
	// external library; a managed one gets its own when this is left out.
	RootPath *string `json:"rootPath,omitempty"`
	// Visibility is "shared" or "private".
	Visibility string `json:"visibility"`
	// Writable lets GOtome change files in an external library.
	Writable *bool `json:"writable,omitempty"`
}

type updateLibraryRequest struct {
	Name       *string `json:"name,omitempty"`
	Visibility *string `json:"visibility,omitempty"`
	Writable   *bool   `json:"writable,omitempty"`
}

func toLibraryResponse(l library.Library, viewer *auth.User) libraryResponse {
	out := libraryResponse{
		ID: l.ID, Name: l.Name, Mode: l.Mode, Writable: l.Writable,
		Visibility: l.Visibility, CreatedAt: l.CreatedAt,
	}
	if auth.Allows(viewer.Role, auth.StorageManage) {
		out.RootPath = &l.RootPath
		out.OwnerID = l.OwnerID
	}
	return out
}

// libraryError maps what the library service refuses to what the API answers.
func libraryError(err error) error {
	var invalid *library.ValidationError
	switch {
	case errors.As(err, &invalid):
		return ErrValidation(invalid.Fields)
	case errors.Is(err, library.ErrNotFound):
		return ErrNotFound("There is no such library.")
	case errors.Is(err, library.ErrNoSuchUser):
		return ErrNotFound("There is no such user.")
	}
	return err
}

// pathID reads a UUID out of the route. One that is not a UUID names nothing,
// so it is answered like an ID that does not exist.
func pathID(r *http.Request, name, what string) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil {
		return uuid.Nil, ErrNotFound("There is no such " + what + ".")
	}
	return id, nil
}

func (s *Server) listLibraries(w http.ResponseWriter, r *http.Request) error {
	user := UserFrom(r.Context())
	libraries, err := s.Libraries.List(r.Context(), library.ScopeOf(*user))
	if err != nil {
		return err
	}
	out := libraryList{Libraries: make([]libraryResponse, len(libraries))}
	for i, l := range libraries {
		out.Libraries[i] = toLibraryResponse(l, user)
	}
	writeJSON(w, r, http.StatusOK, out)
	return nil
}

func (s *Server) getLibrary(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "libraryId", "library")
	if err != nil {
		return err
	}
	user := UserFrom(r.Context())
	l, err := s.Libraries.Get(r.Context(), library.ScopeOf(*user), id)
	if err != nil {
		return libraryError(err)
	}
	writeJSON(w, r, http.StatusOK, toLibraryResponse(l, user))
	return nil
}

func (s *Server) createLibrary(w http.ResponseWriter, r *http.Request) error {
	var req createLibraryRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	in := library.NewLibrary{Name: req.Name, Mode: req.Mode, Visibility: req.Visibility}
	if req.RootPath != nil {
		in.RootPath = *req.RootPath
	}
	if req.Writable != nil {
		in.Writable = *req.Writable
	}
	user := UserFrom(r.Context())
	l, err := s.Libraries.Create(r.Context(), user.ID, in)
	if err != nil {
		return libraryError(err)
	}
	writeJSON(w, r, http.StatusCreated, toLibraryResponse(l, user))
	return nil
}

func (s *Server) updateLibrary(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "libraryId", "library")
	if err != nil {
		return err
	}
	var req updateLibraryRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	l, err := s.Libraries.Update(r.Context(), id, library.Changes{Name: req.Name, Visibility: req.Visibility, Writable: req.Writable})
	if err != nil {
		return libraryError(err)
	}
	writeJSON(w, r, http.StatusOK, toLibraryResponse(l, UserFrom(r.Context())))
	return nil
}

func (s *Server) deleteLibrary(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "libraryId", "library")
	if err != nil {
		return err
	}
	if err := s.Libraries.Delete(r.Context(), id); err != nil {
		return libraryError(err)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// managedLibrary resolves the library of a member route, which only those who
// manage storage reach, so it is looked up with their scope.
func (s *Server) managedLibrary(r *http.Request) (uuid.UUID, error) {
	id, err := pathID(r, "libraryId", "library")
	if err != nil {
		return uuid.Nil, err
	}
	if _, err := s.Libraries.Get(r.Context(), library.ScopeOf(*UserFrom(r.Context())), id); err != nil {
		return uuid.Nil, libraryError(err)
	}
	return id, nil
}

func (s *Server) listLibraryMembers(w http.ResponseWriter, r *http.Request) error {
	id, err := s.managedLibrary(r)
	if err != nil {
		return err
	}
	members, err := s.Libraries.Members(r.Context(), id)
	if err != nil {
		return err
	}
	writeJSON(w, r, http.StatusOK, memberList{Members: members})
	return nil
}

func (s *Server) putLibraryMember(w http.ResponseWriter, r *http.Request) error {
	id, err := s.managedLibrary(r)
	if err != nil {
		return err
	}
	userID, err := pathID(r, "userId", "user")
	if err != nil {
		return err
	}
	if err := s.Libraries.AddMember(r.Context(), id, userID); err != nil {
		return libraryError(err)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) deleteLibraryMember(w http.ResponseWriter, r *http.Request) error {
	id, err := s.managedLibrary(r)
	if err != nil {
		return err
	}
	userID, err := pathID(r, "userId", "user")
	if err != nil {
		return err
	}
	if err := s.Libraries.RemoveMember(r.Context(), id, userID); err != nil {
		return libraryError(err)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
