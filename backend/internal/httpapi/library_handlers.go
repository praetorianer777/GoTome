package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/auth"
	"github.com/praetorianer777/gotome/backend/internal/ingest"
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
	// LastScan is the newest scan of the library's folder, which may still
	// be waiting or running. A library never scanned has none.
	LastScan *ingest.Scan `json:"lastScan,omitempty"`
	// FilesPending is how many files the scans found that are still to be
	// read for their title, cover and text.
	FilesPending int32 `json:"filesPending"`
}

// scanState is what the library responses add from the scans: the newest
// scan and the files still to be read, by library.
type scanState struct {
	latest  map[uuid.UUID]ingest.Scan
	pending map[uuid.UUID]int32
}

func (s *Server) scanState(r *http.Request, scope library.Scope) (scanState, error) {
	latest, err := s.Scans.Latest(r.Context(), scope)
	if err != nil {
		return scanState{}, err
	}
	pending, err := s.Scans.Pending(r.Context(), scope)
	if err != nil {
		return scanState{}, err
	}
	return scanState{latest: latest, pending: pending}, nil
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

func toLibraryResponse(l library.Library, viewer *auth.User, scans scanState) libraryResponse {
	out := libraryResponse{
		ID: l.ID, Name: l.Name, Mode: l.Mode, Writable: l.Writable,
		Visibility: l.Visibility, CreatedAt: l.CreatedAt, FilesPending: scans.pending[l.ID],
	}
	if scan, ok := scans.latest[l.ID]; ok {
		out.LastScan = &scan
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
	scope := library.ScopeOf(*user)
	libraries, err := s.Libraries.List(r.Context(), scope)
	if err != nil {
		return err
	}
	scans, err := s.scanState(r, scope)
	if err != nil {
		return err
	}
	out := libraryList{Libraries: make([]libraryResponse, len(libraries))}
	for i, l := range libraries {
		out.Libraries[i] = toLibraryResponse(l, user, scans)
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
	scope := library.ScopeOf(*user)
	l, err := s.Libraries.Get(r.Context(), scope, id)
	if err != nil {
		return libraryError(err)
	}
	scans, err := s.scanState(r, scope)
	if err != nil {
		return err
	}
	writeJSON(w, r, http.StatusOK, toLibraryResponse(l, user, scans))
	return nil
}

// scanLibrary asks for a scan. It answers at once with the scan that will do
// it, which is the one already waiting or running if there is one.
func (s *Server) scanLibrary(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "libraryId", "library")
	if err != nil {
		return err
	}
	user := UserFrom(r.Context())
	if _, err := s.Libraries.Get(r.Context(), library.ScopeOf(*user), id); err != nil {
		return libraryError(err)
	}
	scan, err := s.Scans.Request(r.Context(), id, &user.ID)
	if err != nil {
		return err
	}
	writeJSON(w, r, http.StatusAccepted, scan)
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
	// A folder that already holds books is looked through at once. The
	// library exists either way, so a scan that cannot be queued is no reason
	// to answer that adding it failed.
	scans := scanState{latest: map[uuid.UUID]ingest.Scan{}}
	if scan, err := s.Scans.Request(r.Context(), l.ID, &user.ID); err != nil {
		s.Log.Warn("the new library's first scan could not be queued", "library", l.Name, "error", err)
	} else {
		scans.latest[l.ID] = scan
	}
	writeJSON(w, r, http.StatusCreated, toLibraryResponse(l, user, scans))
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
	user := UserFrom(r.Context())
	scans, err := s.scanState(r, library.ScopeOf(*user))
	if err != nil {
		return err
	}
	writeJSON(w, r, http.StatusOK, toLibraryResponse(l, user, scans))
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
