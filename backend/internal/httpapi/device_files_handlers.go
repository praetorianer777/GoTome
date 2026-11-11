package httpapi

import (
	"encoding/hex"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/auth"
	"github.com/praetorianer777/gotome/backend/internal/ingest"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

// maxDeviceHashes bounds one lookup: a device with more files asks in turns.
const maxDeviceHashes = 500

type deviceFilesRequest struct {
	// Hashes are the SHA-256 of files the device holds, in hex.
	Hashes []string `json:"hashes"`
}

type deviceFileMatch struct {
	SHA256 string    `json:"sha256"`
	BookID uuid.UUID `json:"bookId"`
}

type deviceFileMatches struct {
	// Matches has an entry for each hash a visible book has a file of; a
	// hash of a book the caller may not see is left out like an unknown one.
	Matches []deviceFileMatch `json:"matches"`
}

// matchDeviceFiles tells the device which of its files GOtome has, and
// records those books as on it, so that their reading progress flows.
func (s *Server) matchDeviceFiles(w http.ResponseWriter, r *http.Request) error {
	device := DeviceFrom(r.Context())
	if device == uuid.Nil {
		return errNotADevice()
	}
	var req deviceFilesRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	if len(req.Hashes) > maxDeviceHashes {
		return ErrBadRequest("Ask about at most 500 files at a time.")
	}
	hashes := make([][]byte, 0, len(req.Hashes))
	for _, h := range req.Hashes {
		b, err := hex.DecodeString(strings.ToLower(h))
		if err != nil || len(b) != 32 {
			return ErrValidation(map[string]string{"hashes": "A hash is the 64 hex digits of a SHA-256."})
		}
		hashes = append(hashes, b)
	}
	matched, err := s.Deliveries.Match(r.Context(), library.ScopeOf(*UserFrom(r.Context())), device, hashes)
	if err != nil {
		return err
	}
	out := deviceFileMatches{Matches: make([]deviceFileMatch, len(matched))}
	for i, m := range matched {
		out.Matches[i] = deviceFileMatch{SHA256: hex.EncodeToString(m.SHA256), BookID: m.BookID}
	}
	writeJSON(w, r, http.StatusOK, out)
	return nil
}

// Why a device cannot upload, as the app words it.
const (
	noUploadToken   = "token"
	noUploadRole    = "role"
	noUploadLibrary = "library"
	noUploadQuota   = "quota"
)

// deviceUpload says whether the device may upload now, and if not why.
type deviceUpload struct {
	Allowed bool `json:"allowed"`
	// Reason is left out when uploading is allowed: token (not allowed when
	// the device was made), role (the account may not upload), library (no
	// managed library to put books into) or quota (the storage is full).
	Reason string `json:"reason,omitempty" enum:"token,role,library,quota"`
	// Library is the name of the library uploads go to.
	Library string `json:"library,omitempty"`
}

// uploadTarget is the managed library a device's uploads go to: one the
// account owns, else the first managed library it sees by name.
func (s *Server) uploadTarget(r *http.Request) (library.Library, bool, error) {
	user := UserFrom(r.Context())
	libs, err := s.Libraries.List(r.Context(), library.ScopeOf(*user))
	if err != nil {
		return library.Library{}, false, err
	}
	var first *library.Library
	for i, l := range libs {
		if l.Mode != library.ModeManaged {
			continue
		}
		if l.OwnerID != nil && *l.OwnerID == user.ID {
			return l, true, nil
		}
		if first == nil {
			first = &libs[i]
		}
	}
	if first == nil {
		return library.Library{}, false, nil
	}
	return *first, true, nil
}

// uploadStatus works out what deviceUpload says.
func (s *Server) uploadStatus(r *http.Request, device uuid.UUID) (deviceUpload, library.Library, error) {
	user := UserFrom(r.Context())
	may, err := s.Auth.DeviceMayUpload(r.Context(), device)
	if err != nil {
		return deviceUpload{}, library.Library{}, err
	}
	switch {
	case !may:
		return deviceUpload{Reason: noUploadToken}, library.Library{}, nil
	case !auth.Allows(user.Role, auth.BooksUpload):
		return deviceUpload{Reason: noUploadRole}, library.Library{}, nil
	}
	lib, ok, err := s.uploadTarget(r)
	if err != nil {
		return deviceUpload{}, library.Library{}, err
	}
	if !ok {
		return deviceUpload{Reason: noUploadLibrary}, library.Library{}, nil
	}
	if err := s.Scans.CheckRoom(r.Context(), user.ID, 1); err != nil {
		var over *ingest.QuotaError
		if errors.As(err, &over) {
			return deviceUpload{Reason: noUploadQuota, Library: lib.Name}, lib, nil
		}
		return deviceUpload{}, library.Library{}, err
	}
	return deviceUpload{Allowed: true, Library: lib.Name}, lib, nil
}

// uploadFromDevice is the upload of the library routes, for a device that
// its owner allowed to upload, into the library uploadTarget names.
func (s *Server) uploadFromDevice(w http.ResponseWriter, r *http.Request) error {
	device := DeviceFrom(r.Context())
	if device == uuid.Nil {
		return errNotADevice()
	}
	status, lib, err := s.uploadStatus(r, device)
	if err != nil {
		return err
	}
	switch status.Reason {
	case noUploadToken:
		return ErrForbidden("This device was not allowed to upload books when it was added. Add it again with that allowed.")
	case noUploadLibrary:
		return ErrConflict("There is no managed library to put uploaded books into. An administrator can make one.")
	case noUploadQuota:
		return uploadError(s.Scans.CheckRoom(r.Context(), UserFrom(r.Context()).ID, 1))
	}
	user := UserFrom(r.Context())
	return s.storeUpload(w, r, lib, library.ScopeOf(*user), user)
}
