package httpapi

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/ingest"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

// uploadForm is the multipart form an upload is sent as.
type uploadForm struct {
	// File is the book's file, named as it was on the uploader's computer:
	// the name's extension says what format it is.
	File string `json:"file"`
}

// uploadFile stores the one file of a multipart form. The file is read as it
// arrives, never held in memory whole.
func (s *Server) uploadFile(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "libraryId", "library")
	if err != nil {
		return err
	}
	user := UserFrom(r.Context())
	scope := library.ScopeOf(*user)
	lib, err := s.Libraries.Get(r.Context(), scope, id)
	if errors.Is(err, library.ErrNotFound) {
		return ErrNotFound("There is no such library.")
	}
	if err != nil {
		return err
	}
	if lib.Mode != library.ModeManaged {
		return ErrConflict("Books can only be uploaded into a managed library. Put files for an external library into its folder, and scan it.")
	}

	mr, err := r.MultipartReader()
	if err != nil {
		return ErrBadRequest("Send the file as multipart/form-data, in a field named file.")
	}
	part, err := mr.NextPart()
	if err != nil || part.FormName() != "file" || part.FileName() == "" {
		return ErrBadRequest("Send the file as multipart/form-data, in a field named file.")
	}
	defer part.Close()

	// An audiobook takes longer to arrive than the server's deadlines allow
	// any other request.
	rc := http.NewResponseController(w)
	_ = rc.SetReadDeadline(time.Time{})
	_ = rc.SetWriteDeadline(time.Time{})

	res, err := s.Scans.Upload(r.Context(), lib, scope, part.FileName(), part)
	switch {
	case errors.Is(err, ingest.ErrUnknownFormat):
		return ErrValidation(map[string]string{"file": "GOtome does not read this kind of file. It takes " + knownFormats() + "."})
	case errors.Is(err, ingest.ErrUploadTooLarge):
		return &APIError{
			Status: http.StatusRequestEntityTooLarge, Code: "too_large",
			Message: fmt.Sprintf("The file is larger than the %d MiB an upload may be.", s.Scans.UploadLimit>>20),
		}
	case err != nil && (r.Context().Err() != nil || errors.Is(err, io.ErrUnexpectedEOF)):
		return ErrBadRequest("The upload was cut off before the whole file arrived.")
	case err != nil:
		return err
	}
	writeJSON(w, r, http.StatusOK, res)
	return nil
}

func knownFormats() string {
	formats := catalog.Formats()
	for i, f := range formats {
		formats[i] = strings.ToUpper(f)
	}
	slices.Sort(formats)
	return strings.Join(formats[:len(formats)-1], ", ") + " and " + formats[len(formats)-1]
}
